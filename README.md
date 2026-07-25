# wardn

**Did that deploy make things worse?** wardn is deploy-aware observability: it
detects when a new version goes live and compares the metrics that matter from
before and after.

This repo currently holds the **walking skeleton** — a thin end-to-end slice that
everything else is built on. See [`docs/design-doc.md`](docs/design-doc.md) for the
full design and [`docs/plan.md`](docs/plan.md) for the staged build plan.

---

## What the skeleton does

```
  ┌────────────┐   POST /api/v1/metrics    ┌────────────┐        ┌────────────┐
  │ sample-app │ ────(Bearer API key)────► │  backend   │ ─────► │  Postgres  │
  │ (emitter)  │      latency_ms sample     │   (Go)     │  SQL   │            │
  └────────────┘                            └────────────┘        └────────────┘
                                                  ▲
                          GET /api/v1/metrics      │  (read series)
  ┌────────────┐   /api proxied by nginx           │
  │  frontend  │ ──────────────────────────────────┘
  │  (React)   │   live latency dashboard
  └────────────┘
```

- **sample-app** — stands in for a real service. Every few seconds it POSTs a
  synthetic `latency_ms` sample tagged with its `APP_VERSION`, authenticated with an
  API key from a mounted Secret. Set `REGRESSED=true` to simulate a bad deploy.
- **backend** (Go + **Gin**, at the repo root) — ingests metrics (API-key auth, scoped
  per app), stores them per-version in Postgres, serves them back, and computes
  p50/p90/p95/p99 **per version**. Handles **login** (username/password → cookie session,
  bcrypt-hashed). Seeds an app + key, an admin user, and synthetic multi-version history
  on first boot.
- **Postgres** — the only datastore. Holds `apps`, `users`, and `metrics` (each sample
  carries a `version`).
- **frontend** (React + Vite) — a **login screen**, then the dashboard, styled from the
  *Wardn Dashboards* design: a **version-comparison chart** (one clickable point per
  version, regressions in red), percentile tiles, and a per-version latency drill-down.
  nginx serves it and proxies `/api`.

**Login:** the dashboard requires a sign-in. A dev admin is seeded on first boot —
**`admin` / `admin@12345`** (override with `SEED_ADMIN_USER` / `SEED_ADMIN_PASS`). Later,
the real Helm chart will set the initial admin by exec-ing into the pod.

> This is deliberately the smallest thing that works. SigNoz, the analyzer, the AI
> layer, auth/RBAC, alerting — all come in the later stages in `docs/plan.md`.

---

## Run it — fast path (Docker Compose, no Kubernetes)

Best for iterating on code.

```bash
docker compose up --build          # build + start all four components
open http://localhost:8088         # the dashboard (live within a few seconds)
docker compose down -v             # stop everything and wipe the db volume
```

## Run it — the real thing (kind + Helm)

Best for exercising the deployment path.

```bash
./deploy/kind/setup.sh             # kind cluster → build images → load → helm install
open http://localhost:8088         # dashboard (via the kind NodePort mapping)

./deploy/kind/regress.sh on        # simulate a bad deploy — watch the latency line climb
./deploy/kind/regress.sh off       # back to healthy
./deploy/kind/teardown.sh          # delete the cluster
```

Prereqs for this path: `docker`, `kind`, `kubectl`, `helm`.

---

## The regression demo

The whole point in one gesture:

- **Compose:** set `REGRESSED: "true"` on `wardn-sample-app` in `docker-compose.yml`,
  then `docker compose up -d --build wardn-sample-app`.
- **kind/Helm:** `./deploy/kind/regress.sh on`.

Either way the emitter starts adding ~140ms to every sample, and the dashboard's
latency line visibly jumps. That's the "did this deploy make it worse?" moment the
whole product is built around.

---

## API (skeleton)

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `GET`  | `/healthz` | — | liveness |
| `POST` | `/api/v1/auth/login` | — | `{username, password}` → sets session cookie |
| `POST` | `/api/v1/auth/logout` | session | clear the session |
| `GET`  | `/api/v1/auth/me` | session | current user (401 if not signed in) |
| `POST` | `/api/v1/metrics` | `Bearer <api-key>` | ingest one sample `{app, metric, version, value, timestamp?}` |
| `GET`  | `/api/v1/versions?app=&metric=` | session | per-version percentiles (p50/p90/p95/p99) — the version chart |
| `GET`  | `/api/v1/metrics?app=&version=&metric=` | session | raw samples for one version — the drill-down |
| `GET`  | `/api/v1/apps` | session | list registered apps |

Two auth models: dashboard reads need a **login session** (a human in a browser); ingest
needs a **per-app API key** (a service). Ingest stays key-gated regardless of login.

Read endpoints are open for now — the dashboard is unauthenticated until the
auth/RBAC stage. Ingest is always key-gated and scoped to the key's app.

---

## Layout

```
main.go           Go backend entrypoint (the backend is the repo root)
config/ store/    backend packages: config + Postgres storage
api/              backend package: HTTP API
Dockerfile        backend image (build context = repo root)
frontend/         React + Vite dashboard (components/, styles.css) + nginx
demo/             demo scaffolding (fake data for the skeleton)
  seed/           historical backfill — run in-process by the backend
  sample-app/     live metric emitter (its own module)
deploy/
  helm/wardn/     Helm chart: backend, frontend, postgres, sample-app
  kind/           kind config + setup.sh / regress.sh / teardown.sh
docker-compose.yml  fast local loop (mirrors the chart)
docs/             design-doc.md, plan.md, todo.md
```

## Configuration

Defaults live in [`deploy/helm/wardn/values.yaml`](deploy/helm/wardn/values.yaml)
(Helm) and `docker-compose.yml` (Compose). The seeded app is `checkout-service`
with key `wardn_dev_key_checkout` — fine for local dev, swap before anything real.
