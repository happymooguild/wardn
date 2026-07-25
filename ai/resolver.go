package ai

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"wardn/secret"
	"wardn/store"
)

// ErrNotConfigured means no provider is available from either source. The UI
// treats it as "set this up", not as a failure — the same posture the analyzer
// takes when SigNoz is unset.
var ErrNotConfigured = errors.New("no AI provider configured")

// Credential is the env-supplied fallback, so Compose and Helm can run without
// anyone opening the settings page — mirroring how SIGNOZ_API_KEY works.
type Credential struct {
	Kind    string
	APIKey  string
	Model   string
	BaseURL string
}

// Resolver decides which provider to use for a call.
//
// Order: the UI-configured row in ai_providers, then the env fallback, then
// ErrNotConfigured.
type Resolver struct {
	Store   *store.Store
	Box     *secret.Box // nil when WARDN_SECRET_KEY is unset
	Env     Credential
	Timeout time.Duration
}

// Config describes the resolved provider without exposing the key.
type Config struct {
	Kind   string
	Model  string
	Source string // "database" | "environment"
}

// Resolve returns a ready-to-call provider plus a description of where its
// credential came from.
func (r *Resolver) Resolve(ctx context.Context) (Provider, Config, error) {
	if r.Store != nil && r.Box != nil {
		p, err := r.Store.EnabledAIProvider(ctx)
		switch {
		case err == nil:
			enc, err := r.Store.AIProviderKey(ctx, p.ID)
			if err != nil {
				return nil, Config{}, fmt.Errorf("load stored key: %w", err)
			}
			key, err := r.Box.Open(enc)
			if err != nil {
				return nil, Config{}, fmt.Errorf("stored key unreadable: %w", err)
			}
			provider, err := New(p.Kind, key, p.Model, p.BaseURL, r.Timeout)
			if err != nil {
				return nil, Config{}, err
			}
			return provider, Config{Kind: p.Kind, Model: p.Model, Source: "database"}, nil
		case errors.Is(err, sql.ErrNoRows):
			// fall through to the env fallback
		default:
			return nil, Config{}, err
		}
	}

	if r.Env.APIKey == "" {
		return nil, Config{}, ErrNotConfigured
	}
	kind := r.Env.Kind
	if kind == "" {
		kind = KindAnthropic
	}
	model := r.Env.Model
	if model == "" {
		model = DefaultModel(kind)
	}
	provider, err := New(kind, r.Env.APIKey, model, r.Env.BaseURL, r.Timeout)
	if err != nil {
		return nil, Config{}, err
	}
	return provider, Config{Kind: kind, Model: model, Source: "environment"}, nil
}

// Available reports whether any credential exists, for the UI's setup state.
func (r *Resolver) Available(ctx context.Context) bool {
	_, _, err := r.Resolve(ctx)
	return err == nil
}
