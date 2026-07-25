package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"wardn/ai"
	"wardn/secret"
)

// analyzeDeploy queues an AI root-cause pass for a deploy ("Ask AI").
//
// Async by design: an Opus call over this payload runs for tens of seconds —
// long enough to trip proxy timeouts and to feel broken behind a spinner. The
// client polls the returned analysis.
func (a *API) analyzeDeploy(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if _, err := a.st.GetDeploy(c, id); errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load deploy"})
		return
	}

	if a.ai == nil || !a.ai.Available(c) {
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error": "no AI provider configured — add one under AI Settings",
		})
		return
	}

	// Collapse repeat clicks onto the in-flight run rather than fanning out
	// into duplicate (billable) model calls.
	open, err := a.st.HasOpenAnalysis(c, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not check analyses"})
		return
	}
	if open {
		existing, err := a.st.PendingAnalysis(c, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load analysis"})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"analysis": existing, "already_running": true})
		return
	}

	analysis, err := a.st.CreateAnalysis(c, id, "manual")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not queue analysis"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"analysis": analysis})
}

func (a *API) listAnalyses(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	items, err := a.st.ListAnalyses(c, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load analyses"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"analyses": items})
}

func (a *API) getAnalysis(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	analysis, err := a.st.GetAnalysis(c, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load analysis"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"analysis": analysis})
}

// getAIProvider reports the configured provider. It never returns the key —
// only the last four characters, so an operator can tell which key is
// installed without the API handing it back.
func (a *API) getAIProvider(c *gin.Context) {
	resp := gin.H{
		"configured":     false,
		"source":         "",
		"kinds":          ai.Kinds(),
		"can_store_keys": a.box != nil,
		"default_models": gin.H{
			ai.KindAnthropic: ai.DefaultAnthropicModel,
			ai.KindOpenAI:    ai.DefaultOpenAIModel,
		},
	}

	provider, err := a.st.EnabledAIProvider(c)
	if err == nil {
		resp["configured"] = true
		resp["source"] = "database"
		resp["provider"] = provider
		c.JSON(http.StatusOK, resp)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load provider"})
		return
	}

	// Nothing stored — report the env fallback if one exists, so the settings
	// page can explain why AI works without a row in the table.
	if a.ai != nil {
		if _, cfg, err := a.ai.Resolve(c); err == nil {
			resp["configured"] = true
			resp["source"] = cfg.Source
			resp["provider"] = gin.H{"kind": cfg.Kind, "model": cfg.Model, "key_last4": "env"}
		}
	}
	c.JSON(http.StatusOK, resp)
}

type aiProviderReq struct {
	Kind    string `json:"kind"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

func (a *API) putAIProvider(c *gin.Context) {
	var req aiProviderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Model = strings.TrimSpace(req.Model)
	req.BaseURL = strings.TrimSpace(req.BaseURL)

	if !ai.ValidKind(req.Kind) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind must be one of " + strings.Join(ai.Kinds(), ", ")})
		return
	}
	if req.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api_key is required"})
		return
	}
	if a.box == nil {
		// Fail closed: without an encryption key we will not write a provider
		// credential to Postgres in the clear.
		c.JSON(http.StatusPreconditionFailed, gin.H{
			"error": "WARDN_SECRET_KEY is not set, so API keys cannot be stored. " +
				"Set it and restart, or supply the key via ANTHROPIC_API_KEY / OPENAI_API_KEY.",
		})
		return
	}
	if req.Model == "" {
		req.Model = ai.DefaultModel(req.Kind)
	}

	enc, err := a.box.Seal(req.APIKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not encrypt key"})
		return
	}

	provider, err := a.st.UpsertAIProvider(c, req.Kind, req.Model, req.BaseURL, enc, secret.Last4(req.APIKey))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save provider"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider": provider})
}

func (a *API) deleteAIProvider(c *gin.Context) {
	if err := a.st.DeleteAIProvider(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete provider"})
		return
	}
	c.Status(http.StatusNoContent)
}

// testAIProvider does a cheap round-trip so a bad key surfaces at setup time
// rather than on the first regression at 3am. Mirrors the alert test endpoint.
func (a *API) testAIProvider(c *gin.Context) {
	if a.ai == nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": ai.ErrNotConfigured.Error()})
		return
	}
	provider, cfg, err := a.ai.Resolve(c)
	if err != nil {
		c.JSON(http.StatusPreconditionFailed, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	_, err = provider.Analyze(ctx, ai.Request{
		System: "You are validating an API credential. Reply using the required schema.",
		Prompt: "Connectivity check from wardn. Set summary and likely_cause to \"ok\", " +
			"confidence to \"high\", and leave the arrays empty.",
		MaxTokens: 256,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "provider": cfg.Kind, "model": cfg.Model, "source": cfg.Source})
}

type appPatchReq struct {
	AIEnabled *bool `json:"ai_enabled"`
}

// patchApp toggles per-app settings. Only the AI opt-in for now; the design
// doc gates automatic analysis on it.
func (a *API) patchApp(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req appPatchReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.AIEnabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
		return
	}
	if err := a.st.SetAppAIEnabled(c, id, *req.AIEnabled); errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update app"})
		return
	}

	app, err := a.st.AppByID(c, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load app"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"app": app})
}
