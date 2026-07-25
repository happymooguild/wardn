package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultAnthropicModel is Claude Opus 5 — 1M context, strongest on the kind of
// multi-signal reasoning this feature needs.
const DefaultAnthropicModel = "claude-opus-5"

// Anthropic talks to the Claude API via the official Go SDK.
//
// Opus 5 notes that shape this code:
//   - temperature / top_p / top_k are removed (400 if sent) — we never set them.
//   - budget_tokens is removed; thinking is on by default. Depth is controlled
//     with output_config.effort instead.
//   - a safety-classifier decline is HTTP 200 with stop_reason "refusal" and
//     empty/partial content, so stop_reason is checked before reading content.
type Anthropic struct {
	client anthropic.Client
	model  string
	effort anthropic.OutputConfigEffort
}

func NewAnthropic(apiKey, model, baseURL string, timeout time.Duration) *Anthropic {
	if model == "" {
		model = DefaultAnthropicModel
	}
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{Timeout: timeout}),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Anthropic{
		client: anthropic.NewClient(opts...),
		model:  model,
		// The task is bounded and well-specified — medium is the right point on
		// the cost/quality curve. Raise to high if verdicts read as shallow.
		effort: anthropic.OutputConfigEffortMedium,
	}
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) Analyze(ctx context.Context, req Request) (Result, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: req.System}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: a.effort,
			Format: anthropic.JSONOutputFormatParam{Schema: VerdictSchema()},
		},
	})
	if err != nil {
		// A bad key or an unknown model won't fix itself on retry — classify it
		// so the job fails once, loudly, instead of five times.
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) && permanentStatus(apiErr.StatusCode) {
			return Result{}, &ErrPermanent{Err: fmt.Errorf("anthropic: %w", err)}
		}
		return Result{}, fmt.Errorf("anthropic: %w", err)
	}

	// Check the stop reason before touching content: on a refusal the content
	// array is empty or partial and indexing it would panic.
	if msg.StopReason == anthropic.StopReasonRefusal {
		return Result{}, &ErrRefused{
			Category:    string(msg.StopDetails.Category),
			Explanation: msg.StopDetails.Explanation,
		}
	}

	var text string
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			text += tb.Text
		}
	}
	if text == "" {
		return Result{}, fmt.Errorf("anthropic: no text content (stop_reason %s)", msg.StopReason)
	}

	verdict, err := parseVerdict(text)
	if err != nil {
		return Result{}, fmt.Errorf("anthropic: %w", err)
	}

	return Result{
		Verdict:      verdict,
		Model:        string(msg.Model),
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
		Raw:          json.RawMessage(text),
	}, nil
}
