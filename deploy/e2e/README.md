# wardn — end-to-end cluster

Stands up the **whole product** in a local [kind](https://kind.sigs.k8s.io) cluster
so you can exercise the full loop:

```
  ArgoCD ──sync──►  storefront (demo app) ──OTLP──►  SigNoz
    │                     │                            ▲
    │ on Healthy          │ POST /metrics              │ PromQL (before/after)
    │ (notification)      ▼                            │
    └──deploy marker──►  wardn backend  ──────analyzer─┘
                           │  ▲                         └─► regression? → alert (+ AI, if a key is set)
                           ▼  │ session (admin/admin@12345)
                        Postgres         wardn dashboard  (http://localhost:8088)
```

A **version bump** to the storefront app (via `deploy-version.sh`) is committed to
an in-cluster Gitea repo → ArgoCD syncs it → ArgoCD's notifications controller
POSTs a **deploy marker** to wardn → the analyzer queries SigNoz for the
before/after latency of that service → if it regressed, it records a verdict and
fires alerts.

## Prerequisites

- `docker`, `kind`, `kubectl`, `helm`, `git`, `curl`
- **~6–8 GB of free RAM** — SigNoz runs ClickHouse + an OTel collector. If your
  machine is tight, use the flags below to drop layers.

## Run

```bash
cd deploy/e2e
./up.sh                 # kind + wardn + SigNoz + ArgoCD/Gitea + demo app
# or, lighter:
./up.sh --no-argocd     # skip ArgoCD + Gitea
./up.sh --no-signoz     # skip SigNoz (analyzer no-ops; marker + alert UI still work)
./up.sh --no-build      # reuse already-loaded images on a re-run
```

First run takes a while (image builds + SigNoz + ArgoCD pulls).

## Access

| What | How |
|---|---|
| **wardn dashboard** | http://localhost:8088 — **admin / admin@12345** |
| SigNoz UI | `kubectl -n signoz port-forward svc/<signoz-frontend> 3301:3301` → http://localhost:3301 |
| ArgoCD UI | `kubectl -n argocd port-forward svc/argocd-server 8092:443` → https://localhost:8092 (user `admin`; password below) |
| Gitea | `kubectl -n gitea port-forward svc/gitea 3000:3000` → http://localhost:3000 (`wardn` / `wardn`) |

ArgoCD admin password:
```bash
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d; echo
```

## The end-to-end test

```bash
./deploy-version.sh --regress    # ship a deliberately slow version of storefront
```
Then watch, in the dashboard:
- **Deploys** page — a new marker for `storefront` appears (source `argocd`), status
  `scheduled` → then a verdict once the analysis window elapses.
- **Alerting** page — a regression alert if the p99 jump crosses the threshold.

Ship a healthy one to see it recover:
```bash
./deploy-version.sh
```

## What's wired for you

- **Admin user** `admin` / `admin@12345`, seeded by the backend on boot.
- **Three registered apps**: `checkout-service` + `payments-service` (dashboard demo,
  with live emitters) and `storefront` (observed via SigNoz + ArgoCD; no chart
  emitter — ArgoCD deploys it).
- `storefront`'s `signoz_service_name` is `storefront`, matching the OTLP metrics
  the demo app emits, so the analyzer can find its before/after data.

## Caveats — read before debugging

1. **SigNoz service names vary by chart version.** `up.sh` discovers the OTLP
   collector and query services and wires them via `--set`, falling back to
   defaults if it can't. If the analyzer reports "metrics provider" errors, run
   `kubectl -n signoz get svc` and re-`helm upgrade` wardn with the right
   `backend.signozUrl` (query API, usually port 8080) and
   `sampleApp.otlpEndpoint` (collector, port 4318).
2. **SigNoz PromQL compatibility is exactly what this exercise tests** (design-doc
   open question #1). If the before/after query fails, that's the finding — the
   analyzer's `MetricsProvider` may need SigNoz's native query API instead of
   PromQL. The wiring here lets you see it either way.
3. **AI verdict is off** (no key). The analyzer still runs; the "explain why" step
   is skipped. To enable it, `helm upgrade wardn ... --set backend.aiApiKey=<key>`
   (or set it in the AI Settings UI).

## Teardown

```bash
./teardown.sh          # deletes the whole kind cluster
```
