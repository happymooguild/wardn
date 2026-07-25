# wardn

**Did that deploy make things worse?** wardn is deploy-aware observability: it
detects when a new version goes live, compares SigNoz metrics before/after, and
can alert Slack or a webhook when things regress.

See [`docs/design-doc.md`](docs/design-doc.md) for the full design and
[`docs/plan.md`](docs/plan.md) for the staged build plan.

---

## What works today

```
  CI / ArgoCD ──POST /api/v1/deployments──► Marker ──► analysis_jobs
                                                      │
  sample-app ──OTLP──► SigNoz ◄──PromQL── Analyzer ◄──┘
                         │                   │
                         │                   └── regressed? ──► Slack / webhook
  sample-app ──POST /metrics──► Postgres ◄── Dashboard (latency by version)
```

- **Marker API** — `POST /api/v1/deployments` (per-app API key). CI curl recipe and
  ArgoCD Notifications ConfigMap live under [`deploy/recipes/`](deploy/recipes/).
- **Analyzer** — in-process worker waits for the after-window, queries SigNoz
  (`POST /api/v5/query_range` PromQL), writes snapshots, sets deploy status.
- **Alerting** — Slack + generic webhook; UI under **Alerting**; fires on `regressed`.
- **Deploys page** — list + before/after snapshot cards.
- **Existing dashboard** — latency-by-version from Postgres ingest (unchanged).
- **sample-app** — still posts to wardn; optionally exports OTLP gauges to SigNoz
  when `OTEL_EXPORTER_OTLP_ENDPOINT` is set.

**Login:** seeded admin **`admin` / `admin@12345`**.

---

## Run it — Docker Compose

```bash
# Optional SigNoz for the analyzer loop:
export SIGNOZ_URL=https://your-signoz
export SIGNOZ_API_KEY=...
export OTEL_EXPORTER_OTLP_ENDPOINT=https://your-otel-http   # no /v1/metrics suffix

docker compose up --build
open http://localhost:8088

# Fire a deploy marker (analysis runs after window_seconds, default 120s):
./demo/deploy-marker.sh v1.0.11
```

Without `SIGNOZ_*`, marker + Deploys/Alerting UI still work; analyzer jobs fail
until SigNoz is configured.

## Run it — kind + Helm

```bash
./deploy/kind/setup.sh
# Pass SigNoz via values, e.g.:
#   helm upgrade wardn ./deploy/helm/wardn --set backend.signozUrl=... --set backend.signozApiKey=...
```

## CI / ArgoCD integration

- **CI:** [`deploy/recipes/ci-marker.sh`](deploy/recipes/ci-marker.sh) — curl after
  your health check succeeds.
- **ArgoCD:** [`deploy/recipes/argocd-notifications-cm.yaml`](deploy/recipes/argocd-notifications-cm.yaml)
  — webhook on `Succeeded` + `Healthy`, `oncePer` sync revision. Put the API key in
  `argocd-notifications-secret` as `wardn-api-key`.

## API

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET`  | `/healthz` | — | liveness |
| `POST` | `/api/v1/auth/login` | — | session cookie |
| `POST` | `/api/v1/auth/logout` | session | |
| `GET`  | `/api/v1/auth/me` | session | |
| `POST` | `/api/v1/metrics` | API key | ingest sample (dashboard) |
| `POST` | `/api/v1/deployments` | API key | deploy marker |
| `GET`  | `/api/v1/deploys?app=` | session | list deploys |
| `GET`  | `/api/v1/deploys/:id` | session | deploy + snapshots |
| `GET`  | `/api/v1/apps` | session | list apps |
| `GET`  | `/api/v1/versions` / `/metrics` | session | latency dashboard |
| `GET/POST` | `/api/v1/apps/:id/alerts` | session | alert configs |
| `PATCH/DELETE` | `/api/v1/alerts/:id` | session | |
| `POST` | `/api/v1/alerts/:id/test` | session | send test notification |

## Configuration

| Env | Purpose |
|---|---|
| `DATABASE_URL` | Postgres |
| `SIGNOZ_URL` / `SIGNOZ_API_KEY` | Analyzer queries |
| `SIGNOZ_UI_URL` | Optional deep links |
| `PUBLIC_BASE_URL` | Links in Slack messages |
| `ALLOW_LOCAL_WEBHOOKS` | Allow localhost webhook URLs (dev default true) |
| `ANALYZER_POLL_INTERVAL` | Job claim interval (default `5s`) |
| `CLOCK_SKEW_MAX` | Marker timestamp tolerance (default `24h`) |
| `SEED_APP` / `SEED_API_KEY` | Seeded app + key |

Demo PromQL templates query `wardn_demo_latency_ms` / `wardn_demo_error_rate`
(from sample-app OTLP). Swap `metric_definitions.promql_template` for real APM
queries in production.

## Layout

```
main.go api/ store/ config/   backend
metrics/ analyzer/ alert/     SigNoz client, worker, notifiers
frontend/                     Dashboards + Deploys + Alerting
demo/sample-app/              emitter (+ optional OTLP)
demo/deploy-marker.sh         fire a marker locally
deploy/recipes/               CI curl + ArgoCD Notifications
deploy/helm/ deploy/kind/
docs/
```
