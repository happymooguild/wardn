// The "Ask AI" panel on a deploy's detail view (design-doc §8).
//
// The analysis is queued server-side and polled, because the model call runs
// for tens of seconds — long enough that a synchronous request would look hung.
import { useEffect, useRef, useState } from 'react'
import { requestAnalysis, fetchAnalysis } from '../api.js'

const POLL_MS = 3000

export default function AskAI({ deployId, initial, onAuthError }) {
  const [analysis, setAnalysis] = useState(initial ?? null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const timer = useRef(null)

  // A new deploy selection resets the panel to whatever that deploy already has.
  useEffect(() => {
    setAnalysis(initial ?? null)
    setError('')
  }, [deployId, initial?.id, initial?.status])

  const pending = analysis?.status === 'pending' || analysis?.status === 'running'

  // Poll only while something is in flight, then stop.
  useEffect(() => {
    clearInterval(timer.current)
    if (!pending || !analysis?.id) return

    let alive = true
    const tick = async () => {
      try {
        const next = await fetchAnalysis(analysis.id)
        if (alive) setAnalysis(next)
      } catch (e) {
        if (alive) onAuthError?.(e)
      }
    }
    timer.current = setInterval(tick, POLL_MS)
    return () => {
      alive = false
      clearInterval(timer.current)
    }
  }, [pending, analysis?.id])

  async function run() {
    setBusy(true)
    setError('')
    try {
      setAnalysis(await requestAnalysis(deployId))
    } catch (e) {
      setError(String(e.message || e))
      onAuthError?.(e)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div className="section-label" style={{ marginTop: 18 }}>
        AI ROOT CAUSE
        <span className="section-hint">
          reasons over before/after metrics, logs and traces
        </span>
      </div>

      <div className="ai-panel">
        <div className="ai-head">
          <button className="ghost-btn" type="button" onClick={run} disabled={busy || pending}>
            {pending ? 'Analyzing…' : analysis ? 'Re-run analysis' : 'Ask AI'}
          </button>
          {analysis?.model && (
            <span className="muted mono" style={{ fontSize: 12 }}>
              {analysis.provider} · {analysis.model}
            </span>
          )}
        </div>

        {error && <div className="ai-error">{error}</div>}

        {!analysis && !error && (
          <div className="muted" style={{ fontSize: 13 }}>
            No analysis yet. Ask AI to explain why this deploy behaved the way it did.
          </div>
        )}

        {pending && (
          <div className="muted" style={{ fontSize: 13 }}>
            Gathering logs and traces for both windows, then reasoning over them. This
            usually takes under a minute.
          </div>
        )}

        {analysis?.status === 'failed' && <div className="ai-error">{analysis.error}</div>}

        {analysis?.status === 'refused' && (
          <div className="ai-error">
            The model declined to answer this request. {analysis.error}
          </div>
        )}

        {analysis?.status === 'succeeded' && <Verdict analysis={analysis} />}
      </div>
    </>
  )
}

function Verdict({ analysis }) {
  const evidence = asArray(analysis.evidence)
  const steps = asArray(analysis.suggested_steps)
  const stats = asObject(analysis.context_stats)

  return (
    <div className="ai-verdict">
      <div className="ai-cause-row">
        <span className={`badge conf-${analysis.confidence}`}>{analysis.confidence} confidence</span>
        {analysis.summary && <span className="ai-summary">{analysis.summary}</span>}
      </div>

      <p className="ai-cause">{analysis.likely_cause}</p>

      {evidence.length > 0 && (
        <>
          <div className="ai-sub">Evidence</div>
          <ul className="ai-list mono">
            {evidence.map((e, i) => (
              <li key={i}>{e}</li>
            ))}
          </ul>
        </>
      )}

      {steps.length > 0 && (
        <>
          <div className="ai-sub">Suggested next steps</div>
          <ul className="ai-list">
            {steps.map((s, i) => (
              <li key={i}>{s}</li>
            ))}
          </ul>
        </>
      )}

      {/* What the model actually saw. Without this a thin answer is
          indistinguishable from a thin signal. */}
      <div className="ai-stats muted">
        {stats.telemetry_missing
          ? 'Logs and traces were unavailable — this verdict is based on metrics alone.'
          : `Saw ${stats.logs_sent_after ?? 0} of ${stats.logs_available_after ?? 0} error logs and ` +
            `${stats.traces_sent_after ?? 0} of ${stats.traces_available_after ?? 0} traces from the after-window.`}
        {stats.ceiling_hit ? ' Context ceiling hit — the sample was trimmed further.' : ''}
        {analysis.input_tokens != null &&
          ` ${analysis.input_tokens} in / ${analysis.output_tokens} out tokens.`}
      </div>
    </div>
  )
}

// evidence / suggested_steps / context_stats arrive as JSONB. Depending on the
// driver they can surface as a parsed value or as a JSON string, so normalize.
function asArray(v) {
  if (Array.isArray(v)) return v
  if (typeof v === 'string') {
    try {
      const parsed = JSON.parse(v)
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  }
  return []
}

function asObject(v) {
  if (v && typeof v === 'object' && !Array.isArray(v)) return v
  if (typeof v === 'string') {
    try {
      const parsed = JSON.parse(v)
      return parsed && typeof parsed === 'object' ? parsed : {}
    } catch {
      return {}
    }
  }
  return {}
}
