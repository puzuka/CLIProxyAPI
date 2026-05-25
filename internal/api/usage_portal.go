package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usageportal"
)

func (s *Server) applyUsagePortalConfig(cfg *config.Config) {
	if cfg == nil {
		usageportal.SetEnabled(false)
		return
	}
	usageportal.SetEnabled(cfg.UsageStatisticsEnabled)
}

func (s *Server) registerUsagePortalRoutes() {
	s.engine.GET("/usage", s.serveUsagePortal)
	s.engine.HEAD("/usage", s.serveUsagePortal)
	s.engine.GET("/usage/:api_key/data", s.getUsagePortalData)
	s.engine.HEAD("/usage/:api_key/data", s.getUsagePortalData)
	s.engine.GET("/usage/:api_key", s.serveUsagePortal)
	s.engine.HEAD("/usage/:api_key", s.serveUsagePortal)
	s.engine.GET("/usages", redirectUsagePortal("/usage"))
	s.engine.HEAD("/usages", redirectUsagePortal("/usage"))
	s.engine.GET("/usages/:api_key/data", redirectUsagePortalKey("/usage/%s/data"))
	s.engine.HEAD("/usages/:api_key/data", redirectUsagePortalKey("/usage/%s/data"))
	s.engine.GET("/usages/:api_key", redirectUsagePortalKey("/usage/%s"))
	s.engine.HEAD("/usages/:api_key", redirectUsagePortalKey("/usage/%s"))
}

func (s *Server) serveUsagePortal(c *gin.Context) {
	s.serveManagementControlPanel(c)
}

func redirectUsagePortal(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, withRawQuery(c, path))
	}
}

func redirectUsagePortalKey(format string) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := url.PathEscape(strings.TrimSpace(c.Param("api_key")))
		if apiKey == "" {
			c.Redirect(http.StatusTemporaryRedirect, withRawQuery(c, "/usage"))
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, withRawQuery(c, fmt.Sprintf(format, apiKey)))
	}
}

func withRawQuery(c *gin.Context, path string) string {
	if c == nil || c.Request == nil || c.Request.URL == nil || c.Request.URL.RawQuery == "" {
		return path
	}
	return path + "?" + c.Request.URL.RawQuery
}

func (s *Server) getUsagePortalData(c *gin.Context) {
	apiKey := strings.TrimSpace(c.Param("api_key"))
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing API key"})
		return
	}

	principal, ok, statusCode, message := s.authenticateUsagePortalKey(c, apiKey)
	if !ok {
		c.JSON(statusCode, gin.H{"error": message})
		return
	}

	windowDays := usagePortalWindowDays(c.Query("window"))
	snapshot := usageportal.SnapshotForKey(principal, windowDays, true, time.Now())
	c.JSON(http.StatusOK, snapshot)
}

func (s *Server) authenticateUsagePortalKey(c *gin.Context, apiKey string) (string, bool, int, string) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", false, http.StatusBadRequest, "missing API key"
	}
	if s == nil || s.accessManager == nil || len(s.accessManager.Providers()) == 0 {
		return "", false, http.StatusUnauthorized, "API key access is not configured"
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, "http://usage.local/v1/models?key="+url.QueryEscape(apiKey), nil)
	if err != nil {
		return "", false, http.StatusInternalServerError, "failed to validate API key"
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("X-Goog-Api-Key", apiKey)

	result, authErr := s.accessManager.Authenticate(c.Request.Context(), req)
	if authErr != nil {
		statusCode := authErr.HTTPStatusCode()
		if statusCode <= 0 {
			statusCode = http.StatusUnauthorized
		}
		return "", false, statusCode, authErr.Message
	}
	principal := apiKey
	if result != nil && strings.TrimSpace(result.Principal) != "" {
		principal = strings.TrimSpace(result.Principal)
	}
	return principal, true, http.StatusOK, ""
}

func usagePortalWindowDays(value string) int {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimSuffix(value, "d")
	if strings.EqualFold(value, "today") || strings.EqualFold(value, "24h") {
		return 1
	}
	days, err := strconv.Atoi(value)
	if err != nil {
		return 7
	}
	switch days {
	case 1, 7, 30, 60:
		return days
	default:
		return 7
	}
}
