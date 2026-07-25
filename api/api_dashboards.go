package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"wardn/store"
)

// backfillSettle mirrors the analyzer's after-window settle so a newly created
// dashboard reads the same window the analyzer would have for each past deploy.
const backfillSettle = 15 * time.Second

func (a *API) listDashboards(c *gin.Context) {
	ds, err := a.st.ListDashboards(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list dashboards"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"dashboards": ds})
}

type createDashboardReq struct {
	Name         string `json:"name"`
	SignozMetric string `json:"signoz_metric"`
	Kind         string `json:"kind"`
	Unit         string `json:"unit"`
	Decimals     int    `json:"decimals"`
}

func (a *API) createDashboard(c *gin.Context) {
	var req createDashboardReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.SignozMetric = strings.TrimSpace(req.SignozMetric)
	if len(req.Name) < 2 || len(req.Name) > 60 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 2–60 characters"})
		return
	}
	if req.SignozMetric == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a SigNoz metric is required"})
		return
	}
	if req.Kind != "single" && req.Kind != "percentiles" {
		req.Kind = "single"
	}
	if req.Decimals < 0 || req.Decimals > 4 {
		req.Decimals = 0
	}

	d, err := a.st.CreateDashboard(c, req.Name, req.SignozMetric, req.Kind, req.Unit, req.Decimals)
	if errors.Is(err, store.ErrDashboardExists) {
		c.JSON(http.StatusConflict, gin.H{"error": "a dashboard with a similar name already exists"})
		return
	}
	if err != nil {
		log.Printf("create dashboard: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create dashboard"})
		return
	}

	// Populate it from SigNoz for existing deploys so it isn't empty on open.
	filled := a.backfillDashboard(c, d)

	c.JSON(http.StatusCreated, gin.H{"dashboard": d, "backfilled_versions": filled})
}

func (a *API) deleteDashboard(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := a.st.DeleteDashboard(c, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// backfillDashboard pulls the dashboard's metric from SigNoz for each recent
// deploy's after-window (version-filtered, like the analyzer) and stores it
// under the dashboard's key. Best-effort; returns how many versions got data.
func (a *API) backfillDashboard(c *gin.Context, d store.Dashboard) int {
	if a.metrics == nil {
		return 0
	}
	deploys, err := a.st.RecentDeploysForBackfill(c, 25)
	if err != nil {
		log.Printf("backfill %s: list deploys: %v", d.MetricKey, err)
		return 0
	}
	filled := 0
	for _, dep := range deploys {
		window := time.Duration(dep.WindowSeconds) * time.Second
		if window <= 0 {
			window = 2 * time.Minute
		}
		start := dep.DeployedAt.UTC().Add(backfillSettle)
		end := start.Add(window)
		promql := fmt.Sprintf(`%s{service_name=%q,version=%q}`, d.SignozMetric, dep.SignozServiceName, dep.Version)
		series, err := a.metrics.QuerySeries(c, promql, start, end, 5)
		if err != nil || len(series.Points) == 0 {
			continue
		}
		samples := make([]store.Sample, 0, len(series.Points))
		for _, p := range series.Points {
			samples = append(samples, store.Sample{Version: dep.Version, Value: p.V, TS: p.T})
		}
		if err := a.st.InsertSamples(c, dep.AppID, d.MetricKey, samples); err != nil {
			log.Printf("backfill %s: store: %v", d.MetricKey, err)
			continue
		}
		filled++
	}
	log.Printf("backfill %s (%s): populated %d version(s)", d.MetricKey, d.SignozMetric, filled)
	return filled
}
