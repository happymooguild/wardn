// Package api is the HTTP surface of the backend, built on Gin.
package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"wardn/ai"
	"wardn/alert"
	"wardn/secret"
	"wardn/store"
)

const sessionName = "wardn_session"

type API struct {
	st           *store.Store
	alerts       *alert.Engine
	clockSkewMax time.Duration
	ai           *ai.Resolver
	box          *secret.Box // nil when WARDN_SECRET_KEY is unset
}

type Options struct {
	SessionSecret string
	ClockSkewMax  time.Duration
	Alerts        *alert.Engine
	AI            *ai.Resolver
	SecretBox     *secret.Box
}

// New wires the router.
func New(st *store.Store, opts Options) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	if opts.ClockSkewMax <= 0 {
		opts.ClockSkewMax = 24 * time.Hour
	}
	a := &API{
		st:           st,
		alerts:       opts.Alerts,
		clockSkewMax: opts.ClockSkewMax,
		ai:           opts.AI,
		box:          opts.SecretBox,
	}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	cookieStore := cookie.NewStore([]byte(opts.SessionSecret))
	cookieStore.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		MaxAge:   8 * 60 * 60,
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions(sessionName, cookieStore))

	r.GET("/healthz", a.health)

	v1 := r.Group("/api/v1")
	{
		v1.POST("/auth/login", a.login)
		v1.POST("/auth/logout", a.logout)
		v1.GET("/auth/me", a.me)

		v1.POST("/metrics", a.ingest)
		v1.POST("/deployments", a.createDeployment)

		authed := v1.Group("")
		authed.Use(a.requireAuth)
		{
			authed.GET("/versions", a.versions)
			authed.GET("/metrics", a.series)
			authed.GET("/apps", a.apps)
			authed.GET("/metric-definitions", a.listMetricDefinitions)
			authed.GET("/deploys", a.listDeploys)
			authed.GET("/deploys/:id", a.getDeploy)
			authed.GET("/apps/:id/alerts", a.listAlerts)
			authed.POST("/apps/:id/alerts", a.createAlert)
			authed.PATCH("/alerts/:id", a.updateAlert)
			authed.DELETE("/alerts/:id", a.deleteAlert)
			authed.POST("/alerts/:id/test", a.testAlert)
			authed.GET("/apps/:id/alert-deliveries", a.listDeliveries)

			// AI reasoning (design-doc §8).
			authed.PATCH("/apps/:id", a.patchApp)
			authed.POST("/deploys/:id/analyze", a.analyzeDeploy)
			authed.GET("/deploys/:id/analyses", a.listAnalyses)
			authed.GET("/analyses/:id", a.getAnalysis)
			authed.GET("/ai/provider", a.getAIProvider)
			authed.PUT("/ai/provider", a.putAIProvider)
			authed.DELETE("/ai/provider", a.deleteAIProvider)
			authed.POST("/ai/provider/test", a.testAIProvider)
		}
	}
	return r
}

func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *API) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	u, err := a.st.UserByUsername(c, req.Username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
			return
		}
		log.Printf("login: lookup user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	if !checkPassword(u.PasswordHash, req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}
	sess := sessions.Default(c)
	sess.Set("username", u.Username)
	sess.Set("role", u.Role)
	if err := sess.Save(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": gin.H{"username": u.Username, "role": u.Role}})
}

func (a *API) logout(c *gin.Context) {
	sess := sessions.Default(c)
	sess.Clear()
	_ = sess.Save()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (a *API) me(c *gin.Context) {
	sess := sessions.Default(c)
	username := sess.Get("username")
	if username == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": gin.H{"username": username, "role": sess.Get("role")}})
}

func (a *API) requireAuth(c *gin.Context) {
	sess := sessions.Default(c)
	if sess.Get("username") == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	c.Next()
}

type ingestReq struct {
	App       string  `json:"app"`
	Metric    string  `json:"metric"`
	Version   string  `json:"version"`
	Value     float64 `json:"value"`
	Timestamp string  `json:"timestamp"`
}

func (a *API) ingest(c *gin.Context) {
	key := bearer(c.GetHeader("Authorization"))
	if key == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return
	}
	app, err := a.st.AppByAPIKeyHash(c, HashKey(key))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	var req ingestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	if req.App != "" && req.App != app.Name {
		c.JSON(http.StatusForbidden, gin.H{"error": "api key not valid for app " + req.App})
		return
	}
	metric := req.Metric
	if metric == "" {
		metric = "latency_ms"
	}
	version := req.Version
	if version == "" {
		version = "untagged"
	}
	ts := time.Now().UTC()
	if req.Timestamp != "" {
		parsed, err := time.Parse(time.RFC3339, req.Timestamp)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "timestamp must be RFC3339"})
			return
		}
		ts = parsed
	}
	if err := a.st.InsertMetric(c, app.ID, metric, version, req.Value, ts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not store metric"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

type deployReq struct {
	App         string `json:"app"`
	Version     string `json:"version"`
	Environment string `json:"environment"`
	Timestamp   string `json:"timestamp"`
	Source      string `json:"source"`
}

func (a *API) createDeployment(c *gin.Context) {
	key := bearer(c.GetHeader("Authorization"))
	if key == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing bearer token"})
		return
	}
	app, err := a.st.AppByAPIKeyHash(c, HashKey(key))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	var req deployReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	if req.App == "" || req.Version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app and version are required"})
		return
	}
	if req.App != app.Name {
		c.JSON(http.StatusForbidden, gin.H{"error": "api key not valid for app " + req.App})
		return
	}
	env := req.Environment
	if env == "" {
		env = app.Environment
		if env == "" {
			env = "production"
		}
	}
	if env != app.Environment {
		c.JSON(http.StatusForbidden, gin.H{"error": "api key not valid for environment " + env})
		return
	}
	source := req.Source
	if source == "" {
		source = "ci"
	}
	switch source {
	case "ci", "argocd", "flux", "manual":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "source must be ci|argocd|flux|manual"})
		return
	}

	var deployedAt time.Time
	if req.Timestamp == "" {
		deployedAt = time.Now().UTC()
	} else {
		parsed, err := time.Parse(time.RFC3339, req.Timestamp)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "timestamp must be RFC3339"})
			return
		}
		deployedAt = parsed.UTC()
	}
	skew := time.Since(deployedAt)
	if skew < 0 {
		skew = -skew
	}
	if skew > a.clockSkewMax {
		c.JSON(http.StatusBadRequest, gin.H{"error": "timestamp too far from server time"})
		return
	}

	sum := sha256.Sum256([]byte(store.FormatIdempotencyKey(app.ID, req.Version, deployedAt)))
	idem := hex.EncodeToString(sum[:])

	result, err := a.st.CreateDeploy(c, app, req.Version, env, source, deployedAt, idem)
	if err != nil {
		log.Printf("create deployment: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record deployment"})
		return
	}

	body := gin.H{
		"id":               result.Event.ID,
		"app":              app.Name,
		"version":          result.Event.Version,
		"previous_version": result.Event.PreviousVersion,
		"environment":      result.Event.Environment,
		"deployed_at":      result.Event.DeployedAt.UTC().Format(time.RFC3339),
		"status":           result.Event.Status,
		"source":           result.Event.Source,
	}
	if result.Scheduled != nil {
		body["analysis_scheduled_at"] = result.Scheduled.UTC().Format(time.RFC3339)
	}
	if result.Created {
		c.JSON(http.StatusCreated, body)
		return
	}
	c.JSON(http.StatusOK, body)
}

func (a *API) versions(c *gin.Context) {
	app := c.Query("app")
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app query param is required"})
		return
	}
	metric := c.DefaultQuery("metric", "latency_ms")
	since := rangeSince(c.DefaultQuery("range", "1d"))

	stats, err := a.st.VersionsWithStats(c, app, metric, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read versions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"app": app, "metric": metric, "versions": stats})
}

func (a *API) series(c *gin.Context) {
	app := c.Query("app")
	version := c.Query("version")
	if app == "" || version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app and version query params are required"})
		return
	}
	metric := c.DefaultQuery("metric", "latency_ms")
	since := rangeSince(c.DefaultQuery("range", "1d"))

	points, err := a.st.VersionSeries(c, app, metric, version, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read series"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"app": app, "metric": metric, "version": version, "points": points})
}

func (a *API) apps(c *gin.Context) {
	apps, err := a.st.ListApps(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list apps"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"apps": apps})
}

func (a *API) listMetricDefinitions(c *gin.Context) {
	list, err := a.st.ListMetricDefinitions(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list metrics"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"metrics": list})
}

func (a *API) listDeploys(c *gin.Context) {
	appName := c.Query("app")
	if appName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app query param is required"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	list, err := a.st.ListDeploys(c, appName, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list deploys"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deploys": list})
}

func (a *API) getDeploy(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	dep, err := a.st.GetDeploy(c, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load deploy"})
		return
	}
	snaps, err := a.st.ListSnapshots(c, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load snapshots"})
		return
	}

	body := gin.H{"deploy": dep, "snapshots": snaps}
	// Fold in the latest AI verdict so the detail page renders in one
	// round-trip. Absence is normal, not an error.
	if analysis, err := a.st.LatestAnalysis(c, id); err == nil {
		body["analysis"] = analysis
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("api: load latest analysis for deploy %d: %v", id, err)
	}
	c.JSON(http.StatusOK, body)
}

type alertReq struct {
	MetricKey     *string         `json:"metric_key"`
	ChannelType   string          `json:"channel_type"`
	ChannelConfig json.RawMessage `json:"channel_config"`
	OnVerdict     string          `json:"on_verdict"`
	ThresholdPct  *float64        `json:"threshold_pct"`
	Enabled       *bool           `json:"enabled"`
}

func (a *API) listAlerts(c *gin.Context) {
	appID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid app id"})
		return
	}
	list, err := a.st.ListAlertConfigs(c, appID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list alerts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alerts": list})
}

func (a *API) createAlert(c *gin.Context) {
	appID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid app id"})
		return
	}
	if _, err := a.st.AppByID(c, appID); errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "app not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	var req alertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if req.ChannelType != "slack" && req.ChannelType != "webhook" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_type must be slack or webhook"})
		return
	}
	if len(req.ChannelConfig) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_config required"})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	cfg, err := a.st.CreateAlertConfig(c, appID, req.MetricKey, req.ChannelType, req.ChannelConfig, req.OnVerdict, req.ThresholdPct, enabled)
	if err != nil {
		log.Printf("create alert: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create alert"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"alert": cfg})
}

func (a *API) updateAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	existing, err := a.st.GetAlertConfig(c, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	var req alertReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	channelType := existing.ChannelType
	if req.ChannelType != "" {
		channelType = req.ChannelType
	}
	cfgJSON := existing.ChannelConfig
	if len(req.ChannelConfig) > 0 {
		cfgJSON = req.ChannelConfig
	}
	onVerdict := existing.OnVerdict
	if req.OnVerdict != "" {
		onVerdict = req.OnVerdict
	}
	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	metricKey := existing.MetricKey
	if req.MetricKey != nil {
		metricKey = req.MetricKey
	}
	thresholdPct := existing.ThresholdPct
	if req.ThresholdPct != nil {
		thresholdPct = req.ThresholdPct
	}
	cfg, err := a.st.UpdateAlertConfig(c, id, metricKey, channelType, cfgJSON, onVerdict, thresholdPct, enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update alert"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alert": cfg})
}

func (a *API) deleteAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := a.st.DeleteAlertConfig(c, id); errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (a *API) testAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	cfg, err := a.st.GetAlertConfig(c, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	app, err := a.st.AppByID(c, cfg.AppID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "app lookup failed"})
		return
	}
	if a.alerts == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "alert engine not configured"})
		return
	}
	if err := a.alerts.SendTest(c, cfg, app); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "sent"})
}

func (a *API) listDeliveries(c *gin.Context) {
	appID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid app id"})
		return
	}
	list, err := a.st.ListDeliveries(c, appID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list deliveries"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deliveries": list})
}

func (a *API) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// rangeSince maps a dashboard time-range key to a lower-bound timestamp.
// "all" returns the zero time (no lower bound). Months/years are approximated
// (30d / 365d), which is plenty for a range selector.
func rangeSince(key string) time.Time {
	now := time.Now()
	day := 24 * time.Hour
	switch key {
	case "1d":
		return now.Add(-1 * day)
	case "2d":
		return now.Add(-2 * day)
	case "3d":
		return now.Add(-3 * day)
	case "5d":
		return now.Add(-5 * day)
	case "1w":
		return now.Add(-7 * day)
	case "2w":
		return now.Add(-14 * day)
	case "1mo":
		return now.Add(-30 * day)
	case "2mo":
		return now.Add(-60 * day)
	case "3mo":
		return now.Add(-90 * day)
	case "1y":
		return now.Add(-365 * day)
	case "2y":
		return now.Add(-730 * day)
	case "all":
		return time.Time{}
	default:
		return now.Add(-1 * day)
	}
}

func bearer(h string) string {
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
