// Package ai reasons over a deploy's before/after metrics, logs, and traces to
// explain *why* a regression happened. Providers are pluggable (Claude, OpenAI);
// the prompt and the output schema are shared across all of them.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Verdict is the structured answer every provider is constrained to produce.
type Verdict struct {
	Summary     string   `json:"summary"`
	LikelyCause string   `json:"likely_cause"`
	Confidence  string   `json:"confidence"` // low | medium | high
	Evidence    []string `json:"evidence"`
	Suggested   []string `json:"suggested_next_steps"`
}

// Request is provider-neutral: the bounder has already rendered everything the
// model needs, so providers do transport, not assembly.
type Request struct {
	System    string
	Prompt    string
	MaxTokens int64
}

// Result carries the parsed verdict plus what it cost.
type Result struct {
	Verdict      Verdict
	Model        string
	InputTokens  int
	OutputTokens int
	Raw          json.RawMessage
}

type Provider interface {
	// Name is the provider kind: "anthropic" or "openai".
	Name() string
	// Analyze returns a verdict, or ErrRefused if the provider's safety layer
	// declined the request.
	Analyze(ctx context.Context, req Request) (Result, error)
}

// ErrPermanent marks a failure that retrying cannot fix - a bad API key, a
// revoked credential, an unknown model. Without this the job machinery would
// burn its full retry budget re-sending a request the provider has already
// rejected on its merits.
type ErrPermanent struct{ Err error }

func (e *ErrPermanent) Error() string { return e.Err.Error() }
func (e *ErrPermanent) Unwrap() error { return e.Err }

// permanentStatus reports whether an HTTP status means "this request will never
// succeed as written". 429 and 5xx are excluded: those are worth retrying.
func permanentStatus(code int) bool {
	switch code {
	case 400, 401, 403, 404, 413, 422:
		return true
	}
	return false
}

// ErrRefused means the model's safety classifiers declined the request. It is a
// terminal outcome, not a transport failure - retrying the same payload will
// fail the same way, so the worker records it rather than re-queueing.
type ErrRefused struct {
	Category    string
	Explanation string
}

func (e *ErrRefused) Error() string {
	if e.Explanation != "" {
		return fmt.Sprintf("model refused the request (%s): %s", e.Category, e.Explanation)
	}
	if e.Category != "" {
		return fmt.Sprintf("model refused the request (%s)", e.Category)
	}
	return "model refused the request"
}

// VerdictSchema is the JSON Schema handed to the provider's structured-output
// mode. Kept in one place so both adapters constrain the model identically.
func VerdictSchema() map[string]any {
	// NB: no maxItems - Anthropic's output_config.format.schema rejects it on
	// arrays. The cap is stated in the description instead, which the model honors.
	strArray := func(desc string, maxItems int) map[string]any {
		return map[string]any{
			"type":        "array",
			"description": fmt.Sprintf("%s (at most %d).", desc, maxItems),
			"items":       map[string]any{"type": "string"},
		}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "One sentence: what changed after the deploy.",
			},
			"likely_cause": map[string]any{
				"type":        "string",
				"description": "The most probable cause of the regression, grounded in the supplied logs and traces. Say so plainly if the evidence is insufficient.",
			},
			"confidence": map[string]any{
				"type":        "string",
				"enum":        []string{"low", "medium", "high"},
				"description": "How well the supplied evidence supports the stated cause.",
			},
			"evidence":             strArray("Quoted log lines or span names that support the cause. Quote verbatim from the input; do not invent.", 6),
			"suggested_next_steps": strArray("Concrete next actions for the on-call engineer.", 4),
		},
		"required":             []string{"summary", "likely_cause", "confidence", "evidence", "suggested_next_steps"},
		"additionalProperties": false,
	}
}

// parseVerdict decodes and sanity-checks a model's JSON answer.
func parseVerdict(raw string) (Verdict, error) {
	var v Verdict
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return v, fmt.Errorf("model returned an empty response")
	}
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return v, fmt.Errorf("decode verdict: %w (got %s)", err, truncate(trimmed, 200))
	}
	// Some prompts (e.g. a version comparison with no captured logs/traces) leave
	// likely_cause empty and put everything in summary. Fall back rather than fail.
	if v.LikelyCause == "" {
		v.LikelyCause = v.Summary
	}
	if v.LikelyCause == "" {
		return v, fmt.Errorf("model returned an empty verdict")
	}
	switch v.Confidence {
	case "low", "medium", "high":
	default:
		v.Confidence = "low"
	}
	return v, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
