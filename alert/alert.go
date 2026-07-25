// Package alert delivers regression notifications to Slack and generic webhooks.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"wardn/store"
)

type Event struct {
	Deploy      store.DeployEvent     `json:"deploy"`
	App         store.App             `json:"app"`
	Verdict     string                `json:"verdict"`
	Snapshots   []store.MetricSnapshot `json:"snapshots"`
	AnalysisURL string                `json:"analysis_url"`
	IsTest      bool                  `json:"is_test,omitempty"`
}

type Engine struct {
	Store              *store.Store
	HTTPClient         *http.Client
	PublicBaseURL      string
	AllowLocalWebhooks bool
}

func New(st *store.Store, publicBaseURL string, allowLocal bool) *Engine {
	return &Engine{
		Store:              st,
		HTTPClient:         &http.Client{Timeout: 10 * time.Second},
		PublicBaseURL:      strings.TrimRight(publicBaseURL, "/"),
		AllowLocalWebhooks: allowLocal,
	}
}

func (e *Engine) AnalysisURL(deployID int64) string {
	return fmt.Sprintf("%s/#/deploys/%d", e.PublicBaseURL, deployID)
}

// NotifyRegression fires enabled alert configs for a regressed deploy.
func (e *Engine) NotifyRegression(ctx context.Context, app store.App, deploy store.DeployEvent, snapshots []store.MetricSnapshot) {
	configs, err := e.Store.ListEnabledAlertsForApp(ctx, app.ID)
	if err != nil {
		log.Printf("alert: list configs: %v", err)
		return
	}
	event := Event{
		Deploy:      deploy,
		App:         app,
		Verdict:     deploy.Status,
		Snapshots:   snapshots,
		AnalysisURL: e.AnalysisURL(deploy.ID),
	}
	for _, cfg := range configs {
		if cfg.OnVerdict != "" && cfg.OnVerdict != "regressed" && cfg.OnVerdict != deploy.Status {
			continue
		}
		if cfg.MetricKey != nil && *cfg.MetricKey != "" {
			matched := false
			for _, sn := range snapshots {
				if sn.MetricKey == *cfg.MetricKey && sn.Degraded {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		exists, err := e.Store.DeliveryExists(ctx, cfg.ID, deploy.ID)
		if err != nil {
			log.Printf("alert: delivery check: %v", err)
			continue
		}
		if exists {
			continue
		}
		code, sendErr := e.send(ctx, cfg, event)
		status := "sent"
		var errMsg *string
		if sendErr != nil {
			status = "failed"
			m := sendErr.Error()
			errMsg = &m
			log.Printf("alert: send config=%d deploy=%d: %v", cfg.ID, deploy.ID, sendErr)
		}
		if err := e.Store.InsertDelivery(ctx, cfg.ID, deploy.ID, status, code, errMsg); err != nil {
			log.Printf("alert: record delivery: %v", err)
		}
	}
}

// SendTest delivers a synthetic event for one alert config.
func (e *Engine) SendTest(ctx context.Context, cfg store.AlertConfig, app store.App) error {
	event := Event{
		Deploy: store.DeployEvent{
			ID:      0,
			AppName: app.Name,
			Version: "test",
			Status:  "regressed",
			Source:  "manual",
		},
		App:         app,
		Verdict:     "regressed",
		Snapshots:   nil,
		AnalysisURL: e.PublicBaseURL,
		IsTest:      true,
	}
	_, err := e.send(ctx, cfg, event)
	return err
}

func (e *Engine) send(ctx context.Context, cfg store.AlertConfig, event Event) (*int, error) {
	switch cfg.ChannelType {
	case "slack":
		return e.sendSlack(ctx, cfg.ChannelConfig, event)
	case "webhook":
		return e.sendWebhook(ctx, cfg.ChannelConfig, event)
	default:
		return nil, fmt.Errorf("unsupported channel_type %q", cfg.ChannelType)
	}
}

type slackConfig struct {
	WebhookURL string `json:"webhook_url"`
}

func (e *Engine) sendSlack(ctx context.Context, raw json.RawMessage, event Event) (*int, error) {
	var cfg slackConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.WebhookURL == "" {
		return nil, fmt.Errorf("slack: webhook_url required")
	}
	if err := e.validateURL(cfg.WebhookURL); err != nil {
		return nil, err
	}
	text := fmt.Sprintf("wardn: *%s* deploy `%s` → `%s`", event.App.Name, event.Deploy.Version, event.Verdict)
	if event.IsTest {
		text = "[TEST] " + text
	}
	if event.AnalysisURL != "" {
		text += fmt.Sprintf("\n<%s|Open in wardn>", event.AnalysisURL)
	}
	for _, sn := range event.Snapshots {
		if !sn.Degraded {
			continue
		}
		bv, av := "n/a", "n/a"
		if sn.BeforeValue != nil {
			bv = fmt.Sprintf("%.2f", *sn.BeforeValue)
		}
		if sn.AfterValue != nil {
			av = fmt.Sprintf("%.2f", *sn.AfterValue)
		}
		dp := "n/a"
		if sn.DeltaPct != nil {
			dp = fmt.Sprintf("%+.1f%%", *sn.DeltaPct)
		}
		text += fmt.Sprintf("\n• %s: %s → %s (%s)", sn.MetricKey, bv, av, dp)
	}
	payload, _ := json.Marshal(map[string]string{"text": text})
	return e.postJSON(ctx, cfg.WebhookURL, payload)
}

type webhookConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

func (e *Engine) sendWebhook(ctx context.Context, raw json.RawMessage, event Event) (*int, error) {
	var cfg webhookConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("webhook: url required")
	}
	if err := e.validateURL(cfg.URL); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	res, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	code := res.StatusCode
	if code >= 400 {
		return &code, fmt.Errorf("webhook HTTP %d", code)
	}
	return &code, nil
}

func (e *Engine) postJSON(ctx context.Context, target string, payload []byte) (*int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := e.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	code := res.StatusCode
	if code >= 400 {
		return &code, fmt.Errorf("HTTP %d", code)
	}
	return &code, nil
}

func (e *Engine) validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http(s)")
	}
	if e.AllowLocalWebhooks {
		return nil
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("localhost webhooks blocked (set ALLOW_LOCAL_WEBHOOKS=true)")
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate()) {
		return fmt.Errorf("private/link-local webhook URLs blocked")
	}
	return nil
}
