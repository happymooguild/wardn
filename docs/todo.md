# wardn - TODOs & deferred work

Things intentionally *not* built in the staged [`plan.md`](./plan.md), plus
improvements surfaced while planning. Grouped by why they're deferred.

## Deferred features (design-doc "TODO" markings)

- **OTP-based verification** - Stage 1 ships username/password only. OTP (email/TOTP)
  is a later add on top of the auth layer.
- **AI-created alerts** - describe an alert in natural language → generate the
  query + threshold. Layers on top of the Stage-3 query-builder; the manual builder
  ships first.
- **Rollback (manual + auto)** - deferred past the initial build; direction decided
  (design-doc §9). **wardn is an action-taker: it runs `kubectl rollout undo` on the
  affected Deployment.** Shape:
  - **Manual first:** a "roll back" button in the UI (revert to previous revision).
  - **Auto second:** a per-alert **auto-rollback** checkbox that fires the same action
    when the alert triggers.
  - **Access:** wardn holds a scoped ServiceAccount + Role/RoleBinding (`get`/`patch`
    on the relevant Deployments) in the namespaces it manages - minimum needed to undo
    a rollout, not cluster admin. The team sets this up; only then does the checkbox
    become selectable (greyed out + "no rollback method configured" until it is).
  - **Guardrails (must ship with auto):** circuit-breaker (never roll the same app back
    twice in N minutes → no flapping between versions); sustained-regression confidence
    gate; dry-run / notify-only mode; audit-log + alert on every rollback; a heavier
    capability grant to enable than "can edit alerts."
  - **Known limitation:** for GitOps teams a live `rollout undo` fights ArgoCD self-heal
    (Git re-syncs the bad version). Correct for direct-CI/kubectl teams; GitOps rollback
    needs its own follow-up before shipping to those users.

## Deferred scope (design-doc "goals for later")

- **Multi-backend metrics** - Prometheus / OpenObserve behind the same
  `MetricsProvider` interface. SigNoz only for now.
- **Full SSO / OIDC** - the auth interface is shaped so a provider (Keycloak / Okta /
  Google) is a swap-in, not a rewrite. Not wired for the demo.
- **Cross-backend logs/traces** - AI reasoning is scoped to SigNoz-sourced logs/traces;
  no near-term plan to change this.
- **Statistical modeling** - Stage 1 uses a simple percentage-delta threshold, not
  proper anomaly/statistical modeling.

## Improvements found while planning (raise before the relevant stage)

- **Analyzer can't be synchronous.** The after-window has to elapse first, so the
  marker `POST` returns immediately and a delayed job does the comparison. Fine as an
  in-process timer for the hackathon; wants a real scheduler/queue at scale. *(plan Fork 2)*
- **Bound the AI context.** Logs and traces for a window can be huge - sample the
  top-N error logs and slowest traces rather than dumping everything, or token cost and
  latency explode. *(plan Fork 5)*
- **RBAC is capability-based, not just two roles.** The doc's §10 admin/member split
  needs a per-user grants table ("can edit dashboards", "can edit alerts"), not just a
  `role` column. Captured in Stage 2.
- **Dedicated telemetry store, only if we ever outgrow Postgres.** Everything lives in
  Postgres now (`deploy_events`, `metric_snapshots` as `JSONB`, `analyses`) - right call
  at demo scale. If deploy volume ever gets huge (not a demo concern), revisit a
  purpose-built append-only store for `deploy_events`/`metric_snapshots`. Not before.
- **Idempotency key.** Dedupe is on `(app, version, timestamp)`, but `timestamp` comes
  from the caller - for the demo, consider `(app, version)` so a re-run doesn't create
  a second event.

## Packaging

- **A clean public Helm chart, separate from the e2e chart.** The current
  `deploy/helm/wardn` bundles `sample-app` emitters + demo seed data - great for the
  local end-to-end cluster, not something to publish. Create a separate chart dir with
  just the real components (backend, frontend, Postgres), no emitters, no demo seeding,
  values geared for real deployments. The e2e chart stays as the local test harness.

## Dashboard (skeleton) follow-ups

- **Expand a chart to full screen** - clicking a latency panel (p95 / p99) should
  open it full-screen for a closer look, then collapse back. Deferred; the
  side-by-side panels ship first.
- **Percentiles come from the metrics backend, not raw samples.** The skeleton
  computes p95/p99 in Postgres from the sample-app's readings; in the real product
  these are queried from SigNoz/Prometheus (which already aggregate). The raw
  per-sample "latency over time" panel was removed because CI/GitOps never sends
  per-request data - only the deploy marker.

## Open questions carried from design-doc §13

1. ~~Verify SigNoz's PromQL query-API compatibility **before** building the metrics
   abstraction on it.~~ **Verified (2026-07-25)** against SigNoz v0.133 in the e2e
   cluster: the provider's `POST /api/v5/query_range` with a `promql` composite
   query returns time-series as expected, and a regressed deploy produced a real
   `regressed` verdict (latency_p99 +108%, error_rate +401%) from live SigNoz data.
   Caveat surfaced: self-hosted SigNoz needs a minted **service-account API key**
   (`SIGNOZ-API-KEY` header) - there is no static key - so `up.sh` now creates one.
2. Pick the OIDC library (`goth`, `zitadel/oidc`, or hand-rolled) so the auth interface
   shape is right even before a real provider is wired.
3. Confirm per-app API key is sufficient auth for the Marker API, or whether production
   needs something heavier.
4. Verify exact ArgoCD Notifications field names/syntax against current docs before
   shipping the recipe. *(Stage 4, S4.3.)*
5. Rollback design - its own dedicated discussion. See §9 above.
