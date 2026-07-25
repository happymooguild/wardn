package ai

import (
	"fmt"
	"time"
)

// Kinds are the provider types the UI offers and the DB constraint allows.
const (
	KindAnthropic = "anthropic"
	KindOpenAI    = "openai"
)

// Kinds lists supported providers, for validation and the settings dropdown.
func Kinds() []string { return []string{KindAnthropic, KindOpenAI} }

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
	default:
		return nil, fmt.Errorf("unknown AI provider %q (want one of %v)", kind, Kinds())
	}
}
