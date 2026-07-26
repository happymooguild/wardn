# AI reasoning layer - design

Implements design-doc §8 ("AI reasoning") and plan S1.3.

> **Status: built.** Everything below is implemented and verified end to end against
> Docker Compose (migration, provider config, queue → poll → verdict, per-app opt-in).
> Deltas from the original design are marked **[as-built]**. The one thing still
> unverified is the SigNoz logs/traces query shape - see §4.

**Scope.** When a deploy regresses (or a user clicks **Ask AI** on any deploy), gather the
before/after metrics plus a *bounded* sample of SigNoz logs and traces for the same
windows, send them to an LLM, store a structured verdict, and show it on the deploy
detail page. Provider is pluggable - Claude first, OpenAI as a second adapter - with the
API key configured through the UI, not baked into the image.

---

## 1. What exists vs. what this adds

```
                                    already built
  ┌────────────────────────────────────────────────────────────────┐
  │ marker → analysis_jobs → analyzer → PromQL before/after         │
  │                            │                                    │
  │                            └── status=regressed ──► alert engine │
  └────────────────────────────┬───────────────────────────────────┘
                               │ this design
  ┌────────────────────────────▼───────────────────────────────────┐
  │ ai job ──► telemetry.Logs()/Traces()  ─┐                        │
  │            metric_snapshots (existing) ─┼─► bounder ─► prompt    │
  │                                         │                       │
  │            ai.Provider (claude|openai) ◄┘                       │
  │                  │                                              │
  │                  └──► analyses table ──► GET /deploys/:id       │
  │                                          "Ask AI" panel          │
  └────────────────────────────────────────────────────────────────┘
```

Five new pieces: a **provider abstraction + credential store**, a **logs/traces client**,
a **context bounder**, an **`analyses` table + job kind**, and the **API + UI surface**.

---

## 2. Provider abstraction

Mirrors [`metrics.MetricsProvider`](../metrics/provider.go). New package `ai/`.

```go
// ai/provider.go
package ai

// Request is provider-neutral. Everything the model needs is already
// rendered by the bounder - providers do transport, not assembly.
type Request struct {
    System   string
    Prompt   string
    MaxTokens int
}

type Result struct {
    Verdict          Verdict // parsed structured output
    Model            string
    InputTokens      int
    OutputTokens     int
    Raw              json.RawMessage
}

// Verdict is the schema the model is constrained to produce.
type Verdict struct {
    Summary      string   `json:"summary"`       // one sentence
    LikelyCause  string   `json:"likely_cause"`  // the actual answer
    Confidence   string   `json:"confidence"`    // low | medium | high
    Evidence     []string `json:"evidence"`      // quoted log lines / span names
    Suggested    []string `json:"suggested_next_steps"`
}

type Provider interface {
    Name() string // "anthropic" | "openai"
    Analyze(ctx context.Context, req Request) (Result, error)
}
```

One file per provider - `ai/anthropic.go`, `ai/openai.go` - plus `ai/registry.go`:

```go
func New(kind, apiKey, model, baseURL string) (Provider, error)
```

### Anthropic adapter (`ai/anthropic.go`)

Use the official Go SDK: `go get github.com/anthropics/anthropic-sdk-go`.

- **Model default: `claude-opus-5`.** Config-overridable. 1M context, 128K max output.
- **Do not set `temperature` / `top_p` / `top_k`** - removed on Opus 5, they return 400.
- **Do not set `budget_tokens`** - also removed. Thinking is on by default on Opus 5;
  control depth with `output_config.effort` (`medium` is a good starting point here - the
  analysis is bounded and well-specified, not open-ended agentic work).
- **Structured output** via `output_config.format` with a `json_schema` matching `Verdict`.
  If the Go SDK binding for `output_config` isn't available on the pinned version, fall
  back to a **strict tool** (`strict: true`, `additionalProperties: false`) and read the
  `tool_use.input` - do not ask for JSON in prose and regex it out.
- **Handle `stop_reason: "refusal"` before reading `content`.** Opus 5 runs safety
  classifiers; a declined request returns HTTP 200 with an empty/partial `content`, so
  `content[0]` panics. Store it as an `analyses.status = 'refused'` rather than a crash.
  Optionally opt into server-side `fallbacks: "default"` (beta header
  `server-side-fallback-2026-07-01`) so a refusal is re-served by Opus 4.8 automatically.
- `max_tokens`: 4096 is plenty for a verdict. Non-streaming is fine at that size.

### OpenAI adapter (`ai/openai.go`)

Separate file, separate SDK (or plain `net/http` against `/v1/chat/completions` with
`response_format: json_schema`). Same `Verdict` shape out. Keeping it in its own file
means neither adapter's quirks leak into the other.

### Cost per analysis

With the bounding in §4 (~12-18K input tokens, ~1K output):

| Model | $/1M in | $/1M out | ≈ per analysis |
|---|---|---|---|
| `claude-opus-5` | $5 | $25 | **~$0.10** |
| `claude-sonnet-5` | $3 ($2 intro to 2026-08-31) | $15 ($10 intro) | ~$0.06 |
| `claude-haiku-4-5` | $1 | $5 | ~$0.02 |

Cheap enough that per-deploy auto-analysis on regression is affordable; the opt-in gate
in §6 is about noise and blast radius, not cost.

---

## 3. Credential plumbing - the "plug in an API key" mechanism

This is the part the team called out. The pattern already exists in the repo:
[`alert_configs`](../store/store_alerts.go) stores `channel_type` + `channel_config` JSONB
per app. Do the same shape, one level up.

```sql
CREATE TABLE IF NOT EXISTS ai_providers (
    id            BIGSERIAL PRIMARY KEY,
    kind          TEXT NOT NULL CHECK (kind IN ('anthropic','openai')),
    display_name  TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL,            -- e.g. claude-opus-5
    base_url      TEXT NOT NULL DEFAULT '', -- for proxies / Azure / self-hosted
    api_key_enc   BYTEA NOT NULL,           -- AES-GCM, key from WARDN_SECRET_KEY
    key_last4     TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS ai_providers_one_enabled
  ON ai_providers (enabled) WHERE enabled;   -- exactly one active provider
```

**Resolution order** at job time:
1. The enabled row in `ai_providers` (UI-configured).
2. Env fallback: `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` + `AI_PROVIDER` + `AI_MODEL`
   - so Compose and Helm can run without touching the UI, matching how `SIGNOZ_API_KEY`
   works today.
3. Neither → AI features report "not configured" (same posture as the analyzer without
   SigNoz), UI shows a setup prompt instead of a broken button.

**Key handling rules:**
- Keys are **write-only over the API**. `GET` returns `kind`, `model`, `key_last4`,
  `enabled` - never the key. `POST`/`PATCH` accept it.
- Encrypt at rest with AES-GCM using a 32-byte `WARDN_SECRET_KEY` from env.
  *Note the existing precedent:* `alert_configs.channel_config` currently stores Slack
  tokens and webhook URLs in plaintext JSONB. Encrypting here is a step up, not
  consistency - worth deciding as a team whether to backfill alerting too. If you'd
  rather not add crypto for the hackathon, env-only (option 2) is the honest fallback;
  plaintext-in-Postgres for a provider key is the option I'd avoid.
- `POST /api/v1/ai/provider/test` does a 1-token round-trip so misconfiguration surfaces
  at setup time, not on the first regression. Mirrors `POST /api/v1/alerts/:id/test`.

---

## 4. Telemetry fetch + context bounding

[`metrics/signoz.go`](../metrics/signoz.go) only implements `Query` (PromQL metrics). The
AI layer's actual input - logs and traces - has no client.

New interface, deliberately **separate** from `MetricsProvider` so the analyzer's
contract doesn't change:

```go
// metrics/telemetry.go
type LogRecord struct {
    Timestamp  time.Time
    Severity   string
    Body       string
    Attributes map[string]string
}

type SpanRecord struct {
    TraceID    string
    Name       string
    DurationMs float64
    StatusCode string
    Service    string
    Timestamp  time.Time
}

type TelemetryProvider interface {
    Logs(ctx context.Context, service, env string, start, end time.Time, opts LogOpts) ([]LogRecord, error)
    Traces(ctx context.Context, service, env string, start, end time.Time, opts TraceOpts) ([]SpanRecord, error)
}
```

Implemented on `SignozProvider` against the same `POST /api/v5/query_range` endpoint the
metrics client already uses, with `requestType: "raw"` and a builder query on the `logs`
and `traces` signals.

> **Verify before building on this.** The design doc's §7 warning applies verbatim: the
> SigNoz annotation feature turned out not to exist, and the PromQL compatibility needed a
> day-one spike. Confirm the v5 raw-query shape for logs and traces against the actual
> SigNoz instance before writing the marshalling code. Budget a short spike, same as
> `S1.1`.

### Bounding ([`docs/todo.md:47-49`](todo.md#L47-L49))

Dumping a window of logs and traces will blow the context and the bill. The bounder is
its own testable unit, `ai/context.go`:

| Input | Bound | Why |
|---|---|---|
| Error logs, after-window | top **20** by severity then recency | the regression's fingerprint |
| Error logs, before-window | top **5** | baseline - is this error *new*? |
| Slowest traces, after-window | top **10** spans by duration | latency regressions |
| Slowest traces, before-window | top **5** | same baseline logic |
| Each log body | truncate to **2000 chars** | one stack trace ≠ the whole budget |
| Metric series | downsample to **20 points** per window | snapshots hold 120; the model doesn't need them |

Plus a hard total-character ceiling (~60K chars ≈ 15K tokens) with proportional trimming
if the caps are still exceeded - a single pathological log line shouldn't be able to
break the budget. Log what was dropped so a thin analysis is explainable rather than
mysterious.

**Dedup before truncating.** N thousand copies of the same stack trace should collapse to
one entry with a count - that's the highest-signal-per-token transformation available and
it's plain Go, no model involved.

---

## 5. Storage

```sql
CREATE TABLE IF NOT EXISTS analyses (
    id               BIGSERIAL PRIMARY KEY,
    deploy_event_id  BIGINT NOT NULL REFERENCES deploy_events(id) ON DELETE CASCADE,
    status           TEXT NOT NULL CHECK (status IN
                       ('pending','running','succeeded','failed','refused')),
    trigger          TEXT NOT NULL CHECK (trigger IN ('auto','manual')),
    provider         TEXT NOT NULL DEFAULT '',
    model            TEXT NOT NULL DEFAULT '',
    summary          TEXT,
    likely_cause     TEXT,
    confidence       TEXT,
    evidence         JSONB NOT NULL DEFAULT '[]',
    suggested_steps  JSONB NOT NULL DEFAULT '[]',
    context_stats    JSONB NOT NULL DEFAULT '{}',  -- what the bounder kept/dropped
    input_tokens     INT,
    output_tokens    INT,
    error            TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS analyses_deploy ON analyses (deploy_event_id, created_at DESC);
```

History is kept - re-running "Ask AI" appends a row; the UI shows the latest and lets you
expand prior runs. `context_stats` is what makes a disappointing answer debuggable: it
records how many logs/traces were available vs. sent.

**Note on naming.** `analysis_jobs`, `AnalysisDelaySeconds`, and `status='pending_analysis'`
already exist and mean the *statistical* delta, not AI. Keep the new work under `analyses`
/ `ai_*` so the two don't blur. This is the single easiest thing to misread in the
codebase right now.

---

## 6. Job flow

Reuse the existing worker rather than adding a second loop:

```sql
ALTER TABLE analysis_jobs ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'metrics';
```

`ClaimJob` ([`store/store_deploys.go:246`](../store/store_deploys.go#L246)) returns the job
row already, so the worker dispatches on `job.Kind`. Locking, `attempts`, `maxAttempts`,
and reschedule all come for free.

**Auto path** - in [`analyzer.go`](../analyzer/analyzer.go) where it currently does:

```go
if status == "regressed" && w.Alerts != nil {
    w.Alerts.NotifyRegression(ctx, app, deploy, snapshots)
}
```

add, gated on the app opting in:

```go
if status == "regressed" && app.AIEnabled {
    _ = w.Store.EnqueueAIJob(ctx, deploy.ID, "auto")
}
```

Per design-doc §50, AI runs on regression **and app opted in** - so:

```sql
ALTER TABLE apps ADD COLUMN IF NOT EXISTS ai_enabled BOOLEAN NOT NULL DEFAULT false;
```

**Manual path** - `POST /api/v1/deploys/:id/analyze` inserts `analyses(status='pending',
trigger='manual')` + an `ai` job and returns `202` with the analysis ID. The UI polls.
Async rather than synchronous because an Opus call on this payload runs tens of seconds -
long enough to hit proxy timeouts and to feel broken behind a spinner.

**Worker step** (`ai/worker.go` or a `processAI` method on the existing worker):
1. Load deploy + app + `metric_snapshots`.
2. Resolve provider (§3). Not configured → `failed` with an actionable message.
3. Fetch logs + traces for both windows (§4). SigNoz down → `failed`, don't hang the job.
4. Bound + render prompt (§4).
5. Call `Provider.Analyze`. Refusal → `refused`.
6. Write the `analyses` row, complete the job.

---

## 7. API surface

| Method | Path | Auth | Purpose |
|---|---|---|---|
| `POST` | `/api/v1/deploys/:id/analyze` | session | enqueue an analysis → `202 {analysis_id}` |
| `GET` | `/api/v1/deploys/:id/analyses` | session | list analyses for a deploy (latest first) |
| `GET` | `/api/v1/analyses/:id` | session | poll one analysis |
| `GET` | `/api/v1/ai/provider` | session | current config - **never returns the key** |
| `PUT` | `/api/v1/ai/provider` | session | set kind / model / key / base_url |
| `POST` | `/api/v1/ai/provider/test` | session | 1-token round-trip validation |
| `PATCH` | `/api/v1/apps/:id` | session | toggle `ai_enabled` per app |

Registered in the `authed` group in [`api/api.go:70-84`](../api/api.go#L70-L84). Also fold
the latest analysis into `GET /api/v1/deploys/:id` so the detail page renders in one
round-trip.

---

## 8. Frontend

- **`frontend/src/components/AskAI.jsx`** - panel on the deploy detail view in
  [`Deploys.jsx`](../frontend/src/components/Deploys.jsx). States: not-configured (link to
  settings) / idle (button) / running (poll) / verdict / failed / refused. Render
  `likely_cause` prominently, `evidence` as quoted lines, `confidence` as a chip.
- **`frontend/src/components/AISettings.jsx`** - provider dropdown, model, key (masked,
  showing `key_last4` once saved), **Test** button. Same interaction shape as
  [`Alerting.jsx`](../frontend/src/components/Alerting.jsx), which is the closest existing
  precedent.
- Sidebar entry next to **Alerting**.

---

## 9. Config

| Env | Purpose |
|---|---|
| `AI_PROVIDER` | `anthropic` \| `openai` - fallback when no DB row |
| `AI_MODEL` | default `claude-opus-5` |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` | fallback credential |
| `AI_BASE_URL` | optional proxy / gateway |
| `WARDN_SECRET_KEY` | 32 bytes, AES-GCM for `api_key_enc` |
| `AI_MAX_CONTEXT_CHARS` | bounder ceiling, default 60000 |
| `AI_TIMEOUT` | per-call timeout, default `120s` |

Added to [`config/config.go`](../config/config.go), `docker-compose.yml`, and
[`values.yaml`](../deploy/helm/wardn/values.yaml) alongside the SigNoz keys.

---

## 10. Build order

Sized so the demo-critical path lands first.

| # | Step | Depends on | Notes |
|---|---|---|---|
| 1 | **SigNoz logs/traces spike** | - | blocking, same posture as the PromQL spike. Confirm the v5 raw-query shape before anything else |
| 2 | `ai.Provider` + `ai/anthropic.go` + registry | - | parallelizable with 1 |
| 3 | `ai_providers` table, config resolution, `PUT`/`test` endpoints | 2 | this is the "plug in a key" ask |
| 4 | `TelemetryProvider` on `SignozProvider` | 1 | |
| 5 | `ai/context.go` bounder + unit tests | 4 | pure functions, easy to test against fixtures |
| 6 | `analyses` table, `analysis_jobs.kind`, worker dispatch | 3, 5 | |
| 7 | `POST /deploys/:id/analyze` + polling endpoints | 6 | |
| 8 | `AskAI.jsx` + `AISettings.jsx` | 3, 7 | |
| 9 | `apps.ai_enabled` + auto-trigger on `regressed` | 6 | last - manual "Ask AI" satisfies the Stage 1 DoD on its own |
| 10 | `ai/openai.go` | 2 | second adapter proves the abstraction |

Steps 1-8 close the Stage 1 definition of done in
[`plan.md:107-110`](plan.md#L107-L110): *"…and 'Ask AI' returns a plausible cause."*

---

## 11. As-built deltas

Things that changed once the code met reality:

- **`trigger` → `trigger_source`, `error` → `error_message`.** Both original names are
  SQL keywords. `error_message` also matches the existing `alert_deliveries` column.
  The JSON field names are unchanged (`trigger`, `error`), so the API is as designed.
- **`AI_API_KEY` added.** The design only had `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`,
  but the Helm chart needs one provider-agnostic key to inject from its Secret.
- **Permanent-vs-retryable error classification** (`ai.ErrPermanent`). Not in the
  original design, and it was a real defect: running it with a bad key showed a 401
  being retried five times over 25 seconds before surfacing. Auth and request-shape
  failures (400/401/403/404/413/422) now fail once; 429 and 5xx still retry.
- **Telemetry degrades instead of failing.** If SigNoz is unreachable the analysis runs
  on metrics alone and the prompt says so explicitly, rather than letting the model
  infer the service was clean. Verified - with SigNoz unset the prompt renders at ~600
  chars with an explicit "Unavailable" section.
- **Repeat-click dedup.** `POST /analyze` returns the in-flight analysis with
  `already_running: true` rather than starting a second billable call.
- **Stale-analysis sweeper.** A worker that dies mid-call would otherwise leave a row
  polling forever; the analyzer fails anything pending for over 15 minutes.
- **One provider, not per-app** - as designed. `UpsertAIProvider` deletes prior rows in
  the same transaction rather than accumulating dead credentials.

## 12. Open questions

1. **A real Claude API key** - the only thing blocking a live verdict.
   [`plan.md:196-198`](plan.md#L196-L198) still lists this as unconfirmed. Everything
   else is wired; verification so far used a deliberately invalid key, which proved the
   request reaches `api.anthropic.com` and the failure path is clean, but not that a
   verdict renders.
2. **The SigNoz logs/traces spike** (§4) - still owed. Until then AI analysis runs on
   metrics alone.
3. **Should `alert_configs` also encrypt?** Provider keys are now AES-GCM at rest while
   Slack tokens next door are still plaintext JSONB. Inconsistent; worth a decision.
4. **`refused` handling** - currently surfaced plainly in the UI. Server-side
   `fallbacks: "default"` would auto-retry a declined request on Opus 4.8 for one extra
   request parameter, if that turns out to matter.
5. **Multi-provider abstraction cost** - the `Verdict` schema is the contract, and it
   holds for both adapters today. If OpenAI's structured-output behavior diverges enough
   to need its own prompt, move prompts into the adapters.
