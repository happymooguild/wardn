// Package api is the HTTP surface of the backend, built on Gin.
//
// Endpoints:
//
//	GET  /healthz                          liveness
//	POST /api/v1/auth/login                username/password -> session cookie
//	POST /api/v1/auth/logout               clear the session
//	GET  /api/v1/auth/me                   current user (401 if not signed in)
//	POST /api/v1/metrics                   ingest a sample   (API-key auth, scoped to app)
//	GET  /api/v1/versions?app=&metric=     per-version percentiles   (requires login)
//	GET  /api/v1/metrics?app=&version=     raw samples for a version (requires login)
//	GET  /api/v1/apps                      list registered apps      (requires login)
//
// Two auth models sit side by side: dashboard reads are gated by a login session
// (a human in a browser), while ingest is gated by a per-app API key (a service).
package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"wardn/store"
)

const sessionName = "wardn_session"

type API struct{ st *store.Store }

// New wires the router. sessionSecret signs the login cookie.
func New(st *store.Store, sessionSecret string) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	a := &API{st: st}

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	cookieStore := cookie.NewStore([]byte(sessionSecret))
	cookieStore.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		MaxAge:   8 * 60 * 60, // 8h
		SameSite: http.SameSiteLaxMode,
		// Secure: true — enable once the dashboard is served over HTTPS.
	})
	r.Use(sessions.Sessions(sessionName, cookieStore))

	r.GET("/healthz", a.health)

	v1 := r.Group("/api/v1")
	{
		v1.POST("/auth/login", a.login)
		v1.POST("/auth/logout", a.logout)
		v1.GET("/auth/me", a.me)

		// Service-to-service ingest: API-key auth, not a session.
		v1.POST("/metrics", a.ingest)

		// Dashboard reads: require a signed-in user.
		authed := v1.Group("")
		authed.Use(a.requireAuth)
		{
			authed.GET("/versions", a.versions)
			authed.GET("/metrics", a.series)
			authed.GET("/apps", a.apps)
		}
	}
	return r
}

// HashKey is the one-way transform applied to API keys before they touch the DB.
// SHA-256 is fine: API keys are long and random, so no slow password hash needed.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// HashPassword bcrypt-hashes a user password (used by the admin seeder).
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// ---- auth handlers ----

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
		log.Printf("login: save session: %v", err)
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

// ---- ingest (API-key) ----

type ingestReq struct {
	App       string  `json:"app"`
	Metric    string  `json:"metric"`
	Version   string  `json:"version"`
	Value     float64 `json:"value"`
	Timestamp string  `json:"timestamp"` // optional RFC3339; defaults to now
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
		log.Printf("ingest: lookup app: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	var req ingestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}

	// Key scoping: a key may only post for the app it belongs to.
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
		log.Printf("ingest: insert metric: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not store metric"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

// ---- dashboard reads ----

func (a *API) versions(c *gin.Context) {
	app := c.Query("app")
	if app == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "app query param is required"})
		return
	}
	metric := c.DefaultQuery("metric", "latency_ms")

	stats, err := a.st.VersionsWithStats(c, app, metric)
	if err != nil {
		log.Printf("versions: %v", err)
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

	points, err := a.st.VersionSeries(c, app, metric, version)
	if err != nil {
		log.Printf("series: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read series"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"app": app, "metric": metric, "version": version, "points": points})
}

func (a *API) apps(c *gin.Context) {
	apps, err := a.st.ListApps(c)
	if err != nil {
		log.Printf("apps: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list apps"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"apps": apps})
}

func (a *API) health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// bearer extracts the token from an "Authorization: Bearer <token>" header.
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
