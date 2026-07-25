package ai

import (
	"fmt"
	"time"
)

// Kinds are the provider types the UI offers and the DB constraint allows.
const (
	KindAnthropic = "anthropic"
	KindOpenAI    = "openai"
	KindGemini    = "gemini"
)

// Kinds lists supported providers, for validation and the settings dropdown.
func Kinds() []string { return []string{KindAnthropic, KindOpenAI, KindGemini} }

// ModelsFor returns the curated model choices for a provider's settings
// dropdown. Not exhaustive — the UI also allows a custom model string.
func ModelsFor(kind string) []string {
	switch kind {
	case KindAnthropic:
		return []string{
			"claude-opus-4-8",
			"claude-sonnet-5",
			"claude-haiku-4-5",
			"claude-opus-4-7",
			"claude-fable-5",
		}
	case KindOpenAI:
		return []string{"gpt-4o", "gpt-4o-mini", "gpt-4.1"}
	case KindGemini:
		return []string{"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.0-flash"}
	default:
		return nil
	}
}

func ValidKind(kind string) bool {
	for _, k := range Kinds() {
		if k == kind {
			return true
		}
	}
	return false
}

// DefaultModel is the model used when a provider is configured without one.
func DefaultModel(kind string) string {
	switch kind {
	case KindOpenAI:
		return DefaultOpenAIModel
	case KindGemini:
		return DefaultGeminiModel
	default:
		return DefaultAnthropicModel
	}
}

// New builds a Provider from stored or env-supplied configuration.
func New(kind, apiKey, model, baseURL string, timeout time.Duration) (Provider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured for provider %q", kind)
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	switch kind {
	case KindAnthropic:
		return NewAnthropic(apiKey, model, baseURL, timeout), nil
	case KindOpenAI:
		return NewOpenAI(apiKey, model, baseURL, timeout), nil
	case KindGemini:
		return NewGemini(apiKey, model, baseURL, timeout), nil
	default:
		return nil, fmt.Errorf("unknown AI provider %q (want one of %v)", kind, Kinds())
	}
}
