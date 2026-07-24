# wardn — Build Plan

Companion to [`design-doc.md`](./design-doc.md). Deferred features and improvements
live in [`todo.md`](./todo.md).

## How to read this plan

- **Stages are sequential milestones.** Each stage ends at something you can run,
  test, and demo. Don't start a stage until the previous one's "Definition of done"
  is green.
- **Sessions are the ~4 parallel work-chunks *inside* a stage — one per person.**
  They're sized so four people can work at once with minimal blocking. A stage is
  done only when its sessions converge and the checkpoint passes.
- **Everyone is a backend dev. Frontend is shared, never one person's job.**
  Each dev owns a backend area *and* builds the UI for the feature they built, on a
  shared React shell that one person scaffolds once. Roles carry across stages so each
  person keeps their context:

| Dev | Backend area | Frontend share |
|---|---|---|
| **1 — Platform** | docker-compose, SigNoz, sample app, the spike; later Helm, self-obs, GitOps recipe | lightest — infra + the blocking spike is a full plate |
| **2 — Core** | data layer, Marker API, auth + RBAC, app registration | login + app-registration screens |
| **3 — Analysis** | metrics provider, analyzer, AI reasoning, alert engine | the "Ask AI" panel + alert query-builder |
| **4 — Shell + data views** | scaffolds the React app (tokens, routing, API client, shared chart/table components), then core data views | the shared shell + before/after + timeline |

> **Frontend principle:** Dev 4 stands up the shell and the shared component kit
> *first* (day one, in parallel with the spike) so nobody is blocked and the UI stays
> consistent. After that, **each person wires their own feature's screens** against it —
> Go devs writing React against a shared kit, pairing where it's unfamiliar. The big FE
> chunks (dashboard builder in Stage 2, alert query-builder in Stage 3) are **split
> across two people**, never assigned whole to one. Rebalance freely — roles are a
> default, not a cage.

## Committed decisions (from the brainstorm)

- **Stack:** Go backend, React dashboard (new app, reusing `wardn-docs/src/styles/tokens.css`).
- **Storage:** **Postgres only** — config, relational data, and deploy telemetry
  (snapshots in `JSONB`). No ClickHouse; the raw time series lives in SigNoz already.
  (SigNoz runs its *own* ClickHouse internally — that's SigNoz's business, not ours.)
- **Metrics + logs + traces:** SigNoz. The AI layer needs its logs/traces, so SigNoz is mandatory.
- **Demo mechanism:** an **instrumented sample app** with `v1` (healthy) and `v2`
  (deliberately regressed) + a script that flips the version and fires the marker.
  No real cluster required on stage.
- **Before/after window:** a **short, configurable knob** (~1–2 min) so it fills live.
- **Auth:** username + password; `admin` + `user` roles **plus per-user capability
  grants** ("can edit dashboards", "can edit alerts"); everyone watches dashboards.
- **AI "explain why":** in the live demo.

## Critical path & the one thing that must happen first

```
Day-one spike (Dev A) ──► unblocks the metrics provider (Dev C) ──► analyzer ──► AI
```

**Hour-one, blocking:** confirm how we query SigNoz for before/after series
(open-question #1). Is its query API genuinely PromQL-compatible, or do we query its
ClickHouse directly with SQL? Everything Dev C builds sits behind the
`MetricsProvider` interface either way, but the *implementation* depends on this
answer. **Nobody on Dev C's analyzer work is truly unblocked until this is settled.**

---

## Stage 1 — Core loop: a deploy is caught, compared, and explained

**The basic product.** End to end: ship `v1`, then a regressed `v2`, and wardn records
the deploy, compares the two seeded metrics before/after, shows it on a dashboard, and
the AI explains the cause. Auth is minimal but real (login, seeded admin, app
registration + key mint). This is the hackathon-demoable slice.

### Sessions

> **Day-one, in parallel:** Dev 1 runs the SigNoz spike; Dev 4 scaffolds the React
> shell + shared component kit. Devs 2 and 3 lock the **API contract** with Dev 4 so
> everyone builds against the same shapes.

- **S1.1 — Platform + sample app + spike (Dev 1)** — *critical path, front-loaded*
  - `docker-compose`: SigNoz (self-contained — its own ClickHouse + OTel collector),
    Postgres (wardn's only store), wardn app skeleton.
  - **The SigNoz query-API spike** → decide PromQL vs ClickHouse-SQL, hand Dev 3 a working query example.
  - Instrumented sample app: `v1` healthy, `v2` with an injected ~300ms hot-path delay
    + raised error rate that emits loggable errors + slow traces. A `deploy.sh` that
    flips version and `POST`s the marker.
  - *Frontend share: none this stage — infra + the blocking spike is a full plate.*

- **S1.2 — Data layer + Marker API + minimal auth (Dev 2)**
  - Postgres schema — `apps`, `users`, `deploy_events`, `metric_snapshots`, `analyses`
    (snapshots as `JSONB`); migrations.
  - `POST /api/v1/deployments`: per-app key auth **scoped to the payload's `app`**,
    idempotency dedupe, validation, clock-skew sanity-check, server-inferred `previous_version`.
  - App registration + **key-shown-once** mint; seeded admin; username/password login (session cookie).
  - **Own UI:** login + app-registration/key-display screens, on Dev 4's shell.

- **S1.3 — Metrics provider + analyzer + AI (Dev 3)** — *depends on S1.1 spike + S1.2 schema*
  - `MetricsProvider` interface + SigNoz implementation (the spike decides its guts).
  - Analyzer: before/after windows around a marker, percentage-delta per metric,
    threshold check. **Runs after the after-window elapses** (see Fork 2), writes `metric_snapshots`.
  - AI reasoning: on threshold cross, gather **bounded** logs/traces for the windows,
    call Claude, store the analysis. `POST .../analyze` for on-demand "Ask AI".
  - **Own UI:** the "Ask AI" panel, on Dev 4's shell.

- **S1.4 — Frontend shell + core data views (Dev 4)**
  - Scaffold the React app (reuse `wardn-docs` design tokens), routing, shared API client,
    and the shared chart/table component kit **everyone else builds on**.
  - Build the **before/after view** (per-metric) and the **all-versions timeline**.
  - Ship a mocked API contract first so Devs 2/3 aren't blocked; wire to real endpoints as they land.

### Definition of done
`deploy.sh v2` → within one window, the dashboard shows p99 + error-rate before/after
with the regression visible, the version timeline updates, and "Ask AI" returns a
plausible cause. No real cluster involved. Two seeded metrics, seeded admin.

---

## Stage 2 — Configurable & multi-user: metric library, dashboards, RBAC

Move from *seeded/hardcoded* to *admin-managed*.

### Sessions
- **S2.1 — Metric library (Dev 3)** — `metric_definitions` CRUD + seed the two Stage-1
  metrics into it; generalize the query path to read from the library. **Own UI:** the
  metric-library admin screen.
- **S2.2 — Full RBAC (Dev 2)** — `roles` + per-user **capability grants** table, user
  management endpoints, permission-enforcing middleware, auth hardening. **Own UI:**
  user-management + capability-grant screens.
- **S2.3 — Dashboard builder, backend + builder core (Dev 4)** — `dashboard_configs`
  CRUD + the builder shell: pick metrics from the library, compose/arrange panels.
- **S2.4 — Dashboard builder, panels + viz (Dev 1)** — *pairs with Dev 4:* the
  deploy-aware panel types (before/after, version timeline) as reusable blocks, viz
  options, and gating every control by the viewer's capabilities ("everyone watches" default).

### Definition of done
An admin creates a metric, grants a user "edit dashboards," that user builds a dashboard
from the library, and a plain user can only watch it.

---

## Stage 3 — Alerting via UI query-builder

### Sessions
- **S3.1 — Alert model + engine (Dev 3)** — `alert_configs` CRUD; evaluate on threshold
  cross / regression found; dedupe + firing history.
- **S3.2 — Channel dispatch (Dev 2)** — Slack incoming-webhook, generic webhook, email
  (SMTP); retries; per-alert routing.
- **S3.3 — Alert query-builder UI, core (Dev 4)** — the builder: pick app + metric +
  condition + threshold + channel.
- **S3.4 — Alert list/history UI + pipeline wiring (Dev 1)** — *pairs with Dev 4:* alert
  list/management + notifications-history view, hook alerting into the analyzer, and demo
  an alert firing end-to-end.

### Definition of done
A regressed `v2` deploy that crosses a UI-defined threshold posts a real Slack/webhook
message, recorded in firing history.

---

## Stage 4 — Production-shape & polish

### Sessions
- **S4.1 — Helm chart (Dev 1)** — one chart: stateless app (1–2 replicas) + Postgres +
  Ingress; config/secrets management.
- **S4.2 — Self-observability (Dev 3)** — wardn emits its own OTel into SigNoz; "the
  watcher is itself observed" demo beat.
- **S4.3 — GitOps recipe (Dev 2)** — the ArgoCD Notifications `ConfigMap`, **field names
  verified against current ArgoCD docs** (open-question #4), tested against a real ArgoCD;
  Flux note. Not load-bearing in the live demo — this is the "and here's the GitOps path" slide.
- **S4.4 — Polish + runbook (Dev 4)** — empty/loading/error states, responsive, docs
  refresh, a **demo-day runbook**, and seed/reset scripts.

### Definition of done
`helm install` brings up the whole thing; wardn appears in SigNoz as its own service;
the ArgoCD recipe is verified; there's a written runbook for the live demo.

---

## Beyond Stage 4 (deferred — see [`todo.md`](./todo.md))

- **Stage 5 — Rollback.** wardn as action-taker: a manual "roll back" button first, then
  the per-alert **auto-rollback** checkbox, both running `kubectl rollout undo` via a
  scoped ServiceAccount. Ships with its guardrails (circuit-breaker, confidence gate,
  dry-run, audit). Direction is decided (design-doc §9); still gets its own design pass
  before build, and the GitOps/self-heal caveat is unresolved.
- Then: OTP verification, AI-created alerts, multi-backend metrics, full SSO/OIDC —
  all tracked in `todo.md`.

---

## Cross-cutting forks (confirm before Stage 1)

1. **Sample-app language** — Go (one language across the repo) vs a tiny Node/Express
   (fastest to write + regress). *Leaning Node for speed of the regressed v2; open.*
2. **Analyzer timing** — the after-window must elapse before comparison, so analysis
   can't finish in the POST handler. *Leaning: in-process delayed job (timer/goroutine)
   for Stage 1, a real scheduler later.*
3. **Session auth** — JWT vs server-side session cookie. *Leaning: session cookie for a
   single-service demo; simpler logout/revocation.*
4. **AI model + context budget** — default to the latest Claude (Opus/Sonnet); logs +
   traces must be **bounded** (top-N error logs + slowest traces) or context blows up.
   *Confirm you have Claude API access + budget.*
