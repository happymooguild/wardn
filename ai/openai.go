package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultOpenAIModel is the second-adapter default. Override via AI_MODEL.
const DefaultOpenAIModel = "gpt-4o"

// OpenAI is the second adapter, proving the Provider abstraction holds. Plain
// net/http against the chat-completions API - no extra SDK dependency for what
// is one request shape.
type OpenAI struct {
	name    string
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

func NewOpenAI(apiKey, model, baseURL string, timeout time.Duration) *OpenAI {
	if model == "" {
		model = DefaultOpenAIModel
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAI{
		name:    "openai",
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

func (o *OpenAI) Name() string { return o.name }

type openAIRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	MaxTokens      int64           `json:"max_completion_tokens,omitempty"`
	ResponseFormat any             `json:"response_format,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (o *OpenAI) Analyze(ctx context.Context, req Request) (Result, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	body := openAIRequest{
		Model:     o.model,
		MaxTokens: maxTokens,
		Messages: []openAIMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.Prompt},
		},
		ResponseFormat: map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "wardn_root_cause",
				"strict": true,
				"schema": VerdictSchema(),
			},
		},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	res, err := o.http.Do(httpReq)
	if err != nil {
		return Result{}, fmt.Errorf("openai: %w", err)
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		err := fmt.Errorf("openai: HTTP %d: %s", res.StatusCode, truncate(string(respBody), 300))
		if permanentStatus(res.StatusCode) {
			return Result{}, &ErrPermanent{Err: err}
		}
		return Result{}, err
	}

	var parsed openAIResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Result{}, fmt.Errorf("openai: decode: %w", err)
	}
	if parsed.Error != nil {
		return Result{}, fmt.Errorf("openai: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return Result{}, fmt.Errorf("openai: no choices returned")
	}

	choice := parsed.Choices[0]
	if choice.Message.Refusal != "" {
		return Result{}, &ErrRefused{Explanation: choice.Message.Refusal}
	}

	verdict, err := parseVerdict(choice.Message.Content)
	if err != nil {
		return Result{}, fmt.Errorf("openai: %w", err)
	}

	return Result{
		Verdict:      verdict,
		Model:        parsed.Model,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		Raw:          json.RawMessage(choice.Message.Content),
	}, nil
}
