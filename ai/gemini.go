package ai

import (
	"strings"
	"time"
)

// DefaultGeminiModel is the Gemini adapter default. Override via AI_MODEL.
const DefaultGeminiModel = "gemini-2.5-flash"

// geminiOpenAIBase is Google's OpenAI-compatible endpoint. Gemini speaks the
// same chat-completions + json_schema shape as our OpenAI adapter, so we reuse
// it rather than add a third transport.
const geminiOpenAIBase = "https://generativelanguage.googleapis.com/v1beta/openai"

// NewGemini returns an OpenAI-compatible adapter pointed at Gemini, reporting
// its own provider name.
func NewGemini(apiKey, model, baseURL string, timeout time.Duration) *OpenAI {
	if model == "" {
		model = DefaultGeminiModel
	}
	if baseURL == "" {
		baseURL = geminiOpenAIBase
	}
	o := NewOpenAI(apiKey, model, baseURL, timeout)
	o.name = "gemini"
	o.baseURL = strings.TrimRight(baseURL, "/")
	return o
}
