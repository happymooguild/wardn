# wardn: catch the regression your thresholds call normal

Most teams find out a new release made things worse in one of two ways: a customer complains, or someone happens to be looking at a dashboard at the right moment. Alerts do not close this gap. They fire on fixed thresholds, "p99 over 2 seconds", "error rate over 5%", which is a good way to catch a fire and a bad way to catch a regression. A number can sit comfortably inside your thresholds and still be worse than it was an hour ago. The old version served checkout at 60ms; the new one serves it at 140ms; nothing is "down" by the alert book; customers already feel it; on-call does not.

We built **wardn** for this exact case: the quiet regression that every static-threshold alert calls normal. It watches your deploys and answers one question after each one, automatically and honestly: is this version worse than the version that was live before it?

wardn reads its data from SigNoz. It does not run a collector or store time series. It adds the two things SigNoz does not have on its own, a trustworthy "a deploy happened" moment and a version-to-version comparison anchored to that moment, and then uses the metrics, logs, and traces you already send to SigNoz to explain what changed.

![The wardn dashboard: latency, error rate, throughput, CPU, and memory per version. Fresh screenshot needed.](images/dashboards.png)

## What wardn does

**It detects the deploy itself, whatever your pipeline is.** Everything starts with one call, `POST /api/v1/deployments`, carrying the app, version, environment, and source. The marker API is deliberately CI-agnostic. Direct CI (a `kubectl`/`helm` rollout) sends it with a curl at the end of the pipeline, once a health check passes. GitOps sends it from ArgoCD's Notifications controller when the app syncs to Healthy. wardn never guesses the deploy time from an image tag or scrapes Git. The system that knows the rollout succeeded tells it, and wardn infers the previous version from history so the caller never has to.

**It compares the version to the one before it, across five metrics.** When a marker lands, wardn waits for a fair window of traffic, then pulls latency, error rate, throughput, CPU, and memory from SigNoz for the window right before the deploy and the window right after. It compares them version to version and decides, per metric, whether the new version regressed. This is the part alerts miss: the comparison is relative to the last version, not to a fixed line.

**It builds the dashboards that surface those regressions.** There is a per-version dashboard for each of the five metrics out of the box, and you can build custom ones on top of anything SigNoz scrapes, picked from a live list of the service's metrics. Every chart is version-labeled, so a jump between two releases is a step on the line, not a smudge you have to date by memory.

**It lets you alert on the regression, not on a number.** Because wardn holds the before and after for each deploy, an alert can be written against the change itself. "Tell me if p99 latency from one version to the next goes up by 5ms or more" is a rule you can set, and it will catch a 60ms to 70ms creep that no static threshold would.

![Deploy markers and the before/after verdict for a service. Fresh screenshot needed.](images/deploys.png)

**For any regression, you can ask AI why.** A worse number tells you something changed, not what. When wardn flags a regression, it feeds the error logs and the slow or failed traces from that same window to an LLM and asks for the likely root cause, with the log lines and span names that support it quoted back. In our demo, a regressed checkout build produced `payment gateway timeout`, `db connection pool exhausted`, and `inventory-service returned 503` in the logs, and the model pointed at a saturated downstream dependency rather than restating that latency went up. You can compare any two versions this way, get a plain-language summary of what moved across every metric, and then drill into the cause. Providers are pluggable: Anthropic (Claude), OpenAI, or Gemini.

![Ask AI comparing two versions and finding the root cause from logs and traces. Fresh screenshot needed.](images/ask-ai.png)

**It keeps the history in its own Postgres.** wardn stores each deploy, its before/after snapshots, and the captured logs and traces in Postgres. That means comparisons and history are not bounded by SigNoz's own retention: a deploy from a month ago is still there to reason over, even after the raw telemetry has rolled off. It also means the model reads evidence as it was in the window, not a live query that might return nothing later.

**It never touches your cluster.** wardn works entirely through the marker API and outbound webhooks. It holds no kube credentials, runs no agent in your workloads, and does not care whether you deploy with Helm, ArgoCD, Flux, or a shell script. Whatever you already do to ship, wardn fits beside it.

## How we use SigNoz

SigNoz is the source of truth for every number, log line, and span wardn reasons over. The whole integration is three endpoints, and the honest version of building it is that the querying was easy and the setup had sharp edges. Both are worth writing down, because the sharp edges are the parts you cannot get from the docs.

### Metrics: PromQL over `/api/v5/query_range`

wardn's SigNoz client posts a PromQL composite query and reads back a time series:

```go
body := v5Request{
    SchemaVersion: "v1",
    Start:         start.UTC().UnixMilli(),
    End:           end.UTC().UnixMilli(),
    RequestType:   "time_series",
    CompositeQuery: compositeQuery{
        Queries: []v5Query{{
            Type: "promql",
            Spec: promqSpec{Name: "A", Query: promql, Step: stepSec},
        }},
    },
}
// POST $SIGNOZ/api/v5/query_range, header: SIGNOZ-API-KEY
```

For each marker it runs this twice, once for `[T-window, T]` and once for `[T+settle, T+settle+window]`, and diffs the two. Because SigNoz already does the aggregation, wardn stores sparse snapshots instead of raw series, and Postgres is enough.

One bug here taught us something about PromQL we would not have guessed. We shipped a healthy `v1.0.6` and wardn read its latency as 151ms when the running pod was serving 68ms. Grouping the query by version showed both the new `v1.0.6` and the old, already-dead `v1.0.5` returning points at the same recent timestamps. SigNoz was carrying the dead version's last sample forward for about five minutes, and our after-window was averaging the live version together with the ghost of the previous one.

The fix was to query one exact version. That required version to be a filterable label, and a resource attribute (`service.version`) is not filterable in SigNoz PromQL. A datapoint attribute is, which is the same mechanism that makes `service_name` work, so our services attach `version` to every datapoint:

```go
"attributes": []any{
    kv("service_name", service),
    kv("version", version), // datapoint attribute, so it is PromQL-filterable
},
```

Now the query is scoped, `wardn_demo_latency_ms{service_name="checkout", version="v1.0.6"}`, and the ghost is gone. This lines up cleanly because wardn's marker carries the same version string the service emits, so the before-window filters to the previous version and the after-window to the new one.

### Logs and traces: the raw builder query

To explain a regression, wardn pulls the error logs and slow or failed traces for the deploy's window. Same endpoint, `requestType: "raw"`, signal `logs` or `traces`, filtered to the high-signal lines (`severity_text IN ('ERROR','FATAL','CRITICAL')` for logs, `has_error = true` for traces).

Our first logs query returned `400` and failed completely, not partially:

```
"message":"Found 1 errors while parsing the search expression.
           key `deployment.environment` not found"
```

We had a `deployment.environment = 'production'` clause in the filter, and our logs did not carry that attribute. SigNoz does not skip an unknown key in a search expression; it rejects the whole query. That single clause had been quietly starving the AI of evidence. Scoping the filter to `service.name`, which the signal does carry, fixed it, and a regressed deploy then captured 40 error logs and 40 traces per window.

### Discovery: `/api/v2/metrics`

Custom dashboards need to know what a service emits. wardn calls `GET /api/v2/metrics?start=<ms>&end=<ms>&limit=500`, which returns each metric's name, type, and unit, and turns that into the dropdown you pick from when building a dashboard. (It is a `GET` with query params; a `POST` returns the SPA, which cost us a minute of confusion.)

### The setup edges

Two things stood between us and a working `SIGNOZ-API-KEY` on a fresh self-hosted SigNoz (`v0.133`, Helm).

First, a fresh SigNoz ingests nothing until an organization exists. Our exporter logged `connection refused` on `:4318` while the collector logged `cannot create agent without orgId`. SigNoz pushes receiver config to its own collector over OpAMP, and it refuses to register the collector until a first org and admin are created, so the OTLP port never binds. One `POST /api/v1/register` fixes it, and about forty seconds later the receiver comes up.

Second, there is no static API key. You mint one for a service account through the session API, and on `v0.133` the routes moved (the login is `POST /api/v2/sessions/email_password`, and the token is `data.accessToken`). Assigning a role to the service account is a separate call, and skipping it gives a confusing `403`, not `401`. We automated the whole flow in our bring-up script so a new cluster just works.

None of these are dealbreakers, and each has a clean fix. But all four cost real time, and none are in the obvious place in the docs.

## Set it up against your SigNoz

Point wardn at your SigNoz query URL and a minted key:

```yaml
backend:
  signozUrl: "http://signoz.signoz.svc.cluster.local:8080"
  signozApiKey: "<key from the service-account mint flow>"
```

Send a marker after each rollout succeeds. For direct CI, one curl:

```bash
curl -fsS -X POST https://wardn.yourco.com/api/v1/deployments \
  -H "Authorization: Bearer $WARDN_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"app":"checkout","version":"v1.4.2","environment":"production","source":"ci"}'
```

For GitOps, let ArgoCD send it when the app is Healthy:

```yaml
service.webhook.wardn: |
  url: https://wardn.yourco.com/api/v1/deployments
  headers:
    - name: Authorization
      value: Bearer $wardn-api-key
trigger.on-deployed: |
  - when: app.status.operationState.phase in ['Succeeded'] and app.status.health.status == 'Healthy'
    oncePer: app.status.sync.revision
    send: [app-deployed-wardn]
```

The `oncePer: app.status.sync.revision` keeps a flapping health status from firing several markers for one deploy. Set `version` to your real release identifier, and if it matches the `service.version` your app emits, the per-version filtering works with no extra config. From the second deploy onward, the before/after graphs appear, regressions get flagged, and Ask AI can explain any of them.

## What we think of SigNoz for this

The querying side was the easy part, and that is a compliment. One endpoint served metrics, logs, and traces; PromQL meant the aggregation stayed in SigNoz and wardn stayed small; and the metric metadata endpoint made custom dashboards a feature instead of a project. Building a deploy-aware layer on top of SigNoz is genuinely a couple hundred lines of Go.

The setup side is where we would ask for help. The org-before-ingestion behavior, the multi-step key mint, the resource-attribute-versus-datapoint-attribute distinction for filtering, and the all-or-nothing search expression each surfaced as a failure far from its cause. If you are starting: create the org first, mint a service-account key with a role, emit `version` as a datapoint attribute, and keep your log and trace filters to attributes the signal actually carries. Do those four things and reading deploy-aware telemetry out of SigNoz is small and reliable.

wardn is open source. The SigNoz client, the marker API, and the bring-up script that automates the org and key setup are all in the repo.
