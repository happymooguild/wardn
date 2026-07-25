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
                         │                   ├── regressed? ──► Slack / webhook
                         │                   │
                         └──logs + traces──► AI root cause ──► "Ask AI" panel
                                             (Claude / OpenAI)
  sample-app ──POST /metrics──► Postgres ◄── Dashboard (latency by version)
```

- **Marker API** — `POST /api/v1/deployments` (per-app API key). CI curl recipe and
  ArgoCD Notifications ConfigMap live under [`deploy/recipes/`](deploy/recipes/).
- **Analyzer** — in-process worker waits for the after-window, queries SigNoz
  (`POST /api/v5/query_range` PromQL), writes snapshots, sets deploy status.
- **Alerting** — Slack + generic webhook; UI under **Alerting**; fires on `regressed`.
- **AI root cause** — **Ask AI** on any deploy explains *why*, reasoning over the
  before/after metrics plus a bounded sample of SigNoz error logs and slow traces.
  Pluggable provider (Claude or OpenAI), configured under **AI Settings**; optional
  auto-run on regression per service.
- **Deploys page** — list + before/after snapshot cards + the Ask AI panel.
- **Dashboard** — latency-by-version from Postgres ingest, with a **time-range
  selector** (Last 1 day … All time), version chart, percentile tiles, and drill-down.
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

## AI root cause

Open **AI Settings**, pick a provider, paste an API key, hit **Test**. Then open any
deploy and click **Ask AI**.

```bash
# Or skip the UI entirely — the env fallback works the same way SIGNOZ_API_KEY does:
export ANTHROPIC_API_KEY=sk-ant-...
docker compose up --build
```

- **Model** defaults to `claude-opus-5`; override with `AI_MODEL`. `openai` is the
  second adapter.
- **Keys are write-only over the API** — `GET /api/v1/ai/provider` returns only the last
  four characters. Storing a key from the UI needs `WARDN_SECRET_KEY` (AES-GCM at rest);
  without it wardn refuses to persist one rather than writing it in the clear, and you
  use the env fallback instead.
- **Context is bounded** ([`ai/context.go`](ai/context.go)): identical log lines are
  collapsed with a count, then the top 20 error logs and 10 slowest traces from the
  after-window go in, plus a smaller baseline sample from before. Roughly 15K input
  tokens — about $0.10 per analysis on Opus 5. Every verdict records what was kept vs.
  dropped, so a thin answer is distinguishable from a thin signal.
- **Degrades rather than fails**: if SigNoz logs/traces are unreachable, the analysis
  still runs on metrics alone and the model is told the log evidence was missing.
- **Auto-run** on regression is per-service and off by default (AI Settings → Automatic
  analysis).

> The SigNoz **logs and traces** query shape in
> [`metrics/signoz_telemetry.go`](metrics/signoz_telemetry.go) is written against the
> documented v5 API but has not been verified against a live instance — the parser is
> shape-tolerant and failures degrade to metrics-only, but confirm it before relying on
> it. See [`docs/ai-layer-design.md`](docs/ai-layer-design.md) §4.

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
| `PATCH` | `/api/v1/apps/:id` | session | toggle `ai_enabled` |
| `POST` | `/api/v1/deploys/:id/analyze` | session | queue an AI root-cause pass (202) |
| `GET`  | `/api/v1/deploys/:id/analyses` | session | analysis history for a deploy |
| `GET`  | `/api/v1/analyses/:id` | session | poll one analysis |
| `GET/PUT/DELETE` | `/api/v1/ai/provider` | session | AI credentials (key is write-only) |
| `POST` | `/api/v1/ai/provider/test` | session | verify the configured provider |

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
| `SEED_APPS` | Seeded services, `"name:key,name:key"` |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` | AI credential fallback when none is set in the UI |
| `AI_PROVIDER` / `AI_MODEL` | `anthropic` (default) or `openai`; model defaults to `claude-opus-5` |
| `AI_API_KEY` / `AI_BASE_URL` | Provider-agnostic key (used by the Helm chart) and optional proxy |
| `AI_TIMEOUT` / `AI_MAX_CONTEXT_CHARS` | Per-call timeout (`120s`) and prompt ceiling (`60000`) |
| `WARDN_SECRET_KEY` | Encrypts UI-stored AI keys. Unset ⇒ the UI can't save keys |

Demo PromQL templates query `wardn_demo_latency_ms` / `wardn_demo_error_rate`
(from sample-app OTLP). Swap `metric_definitions.promql_template` for real APM
queries in production.

## Layout

```
main.go api/ store/ config/   backend
metrics/ analyzer/ alert/     SigNoz client, worker, notifiers
ai/ secret/                   LLM providers + prompt bounding, credential encryption
frontend/                     Dashboards + Deploys + Alerting + AI Settings
demo/sample-app/              emitter (+ optional OTLP)
demo/deploy-marker.sh         fire a marker locally
deploy/recipes/               CI curl + ArgoCD Notifications
deploy/helm/ deploy/kind/
docs/
```
## Configuration

Defaults live in [`deploy/helm/wardn/values.yaml`](deploy/helm/wardn/values.yaml)
(Helm) and `docker-compose.yml` (Compose). Two services are seeded out of the box —
`checkout-service` and `payments-service` — each with its own API key and a live
emitter, so the dashboard's app selector has something to switch between. Add or
change them via `SEED_APPS` (`"name:key,name:key"`) + a matching sample-app; keys are
dev-only, swap before anything real.
