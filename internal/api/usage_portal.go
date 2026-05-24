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
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, usagePortalHTML)
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
	value = strings.ToLower(strings.TrimSpace(value))
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

const usagePortalHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Usage Portal</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f8fb;
      --panel: #ffffff;
      --panel-soft: #f9fafb;
      --border: #dde3ea;
      --border-strong: #c8d2dd;
      --text: #111827;
      --muted: #667085;
      --faint: #98a2b3;
      --accent: #0f766e;
      --accent-strong: #115e59;
      --accent-soft: #dff7f3;
      --warn: #ea580c;
      --danger: #dc2626;
      --shadow: 0 14px 36px rgba(15, 23, 42, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: var(--bg);
      color: var(--text);
    }
    button, input { font: inherit; }
    button { cursor: pointer; }
    .shell {
      width: min(1120px, calc(100vw - 32px));
      margin: 48px auto;
    }
    .center {
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 24px;
    }
    .login {
      width: min(520px, 100%);
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 8px;
      box-shadow: var(--shadow);
      padding: 28px;
    }
    .icon {
      width: 48px;
      height: 48px;
      border-radius: 8px;
      display: inline-grid;
      place-items: center;
      color: var(--accent);
      background: var(--accent-soft);
      margin-bottom: 14px;
    }
    h1 {
      margin: 0;
      font-size: 30px;
      line-height: 1.15;
      letter-spacing: 0;
    }
    h2 {
      margin: 0;
      font-size: 16px;
      line-height: 1.35;
    }
    p {
      margin: 8px 0 0;
      color: var(--muted);
      line-height: 1.45;
    }
    label {
      display: block;
      margin: 22px 0 8px;
      font-weight: 650;
      font-size: 14px;
    }
    input {
      width: 100%;
      height: 44px;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--panel-soft);
      color: var(--text);
      padding: 0 13px;
      outline: none;
    }
    input:focus {
      border-color: var(--accent);
      box-shadow: 0 0 0 3px rgba(15, 118, 110, 0.16);
    }
    .primary {
      width: 100%;
      height: 44px;
      border: 0;
      border-radius: 8px;
      margin-top: 14px;
      color: #fff;
      background: var(--accent);
      font-weight: 750;
    }
    .primary:hover { background: var(--accent-strong); }
    .hint {
      margin-top: 18px;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--panel-soft);
      color: var(--muted);
      padding: 12px;
      font-size: 13px;
    }
    .hero, .card, .chart, .table-wrap {
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 8px;
      box-shadow: var(--shadow);
    }
    .hero {
      padding: 24px;
      margin-bottom: 24px;
    }
    .hero-top {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 18px;
    }
    .badge-row {
      display: flex;
      align-items: center;
      gap: 10px;
      flex-wrap: wrap;
      margin-bottom: 12px;
    }
    .badge {
      min-height: 34px;
      border-radius: 8px;
      display: inline-flex;
      align-items: center;
      gap: 8px;
      border: 1px solid var(--border);
      padding: 0 12px;
      color: var(--muted);
      font-weight: 650;
      font-size: 13px;
      background: var(--panel-soft);
    }
    .badge.ok {
      color: #047857;
      border-color: #b7ebd8;
      background: #e9fbf4;
    }
    .actions {
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
      justify-content: flex-end;
    }
    .btn {
      height: 36px;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--panel-soft);
      color: var(--text);
      padding: 0 12px;
      font-weight: 700;
    }
    .btn:hover { border-color: var(--border-strong); }
    .divider {
      height: 1px;
      background: var(--border);
      margin: 20px 0;
    }
    .range {
      display: inline-flex;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: var(--panel-soft);
      overflow: hidden;
    }
    .range button {
      border: 0;
      background: transparent;
      color: var(--muted);
      padding: 9px 14px;
      font-weight: 750;
      font-size: 13px;
    }
    .range button.active {
      background: var(--accent);
      color: #fff;
    }
    .meta-row {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      flex-wrap: wrap;
    }
    .grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 16px;
      margin-bottom: 24px;
    }
    .card {
      min-height: 140px;
      padding: 18px;
      display: flex;
      flex-direction: column;
      justify-content: space-between;
    }
    .card-label {
      color: var(--muted);
      font-size: 14px;
      font-weight: 650;
    }
    .card-value {
      font-size: 28px;
      font-weight: 850;
      letter-spacing: 0;
      line-height: 1.1;
      overflow-wrap: anywhere;
    }
    .card-sub {
      color: var(--muted);
      font-size: 13px;
    }
    .chart {
      padding: 24px;
      margin-bottom: 24px;
    }
    .chart-head, .table-head {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 16px;
      margin-bottom: 18px;
    }
    .chart-box {
      width: 100%;
      height: 280px;
      border-radius: 8px;
      background: linear-gradient(#fff, #fbfcfd);
      border: 1px solid var(--border);
      overflow: hidden;
    }
    svg { display: block; width: 100%; height: 100%; }
    .table-wrap { overflow: hidden; }
    .table-head { padding: 20px 24px 0; }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 13px;
    }
    th {
      text-align: left;
      color: var(--muted);
      background: var(--panel-soft);
      border-top: 1px solid var(--border);
      border-bottom: 1px solid var(--border);
      padding: 12px 16px;
      font-weight: 800;
    }
    td {
      border-bottom: 1px solid var(--border);
      padding: 13px 16px;
      vertical-align: middle;
    }
    .mono { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
    .status {
      display: inline-flex;
      align-items: center;
      border-radius: 999px;
      padding: 4px 9px;
      font-weight: 800;
      font-size: 12px;
    }
    .status.ok { color: #047857; background: #e9fbf4; }
    .status.err { color: var(--danger); background: #fee2e2; }
    .empty {
      color: var(--muted);
      text-align: center;
      padding: 32px 16px 40px;
    }
    .error {
      color: var(--danger);
      background: #fff1f2;
      border: 1px solid #fecdd3;
      border-radius: 8px;
      padding: 12px;
      margin-top: 14px;
      display: none;
    }
    .hidden { display: none !important; }
    @media (max-width: 900px) {
      .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .hero-top { flex-direction: column; }
      .actions { justify-content: flex-start; }
    }
    @media (max-width: 640px) {
      .shell { width: min(100vw - 20px, 1120px); margin: 16px auto; }
      .grid { grid-template-columns: 1fr; }
      .hero, .chart { padding: 16px; }
      .table-head { padding: 16px 16px 0; }
      .table-scroll { overflow-x: auto; }
      table { min-width: 780px; }
    }
  </style>
</head>
<body>
  <main id="login" class="center hidden">
    <section class="login">
      <div class="icon" aria-hidden="true">
        <svg width="25" height="25" viewBox="0 0 24 24" fill="none">
          <path d="M4 19V5M4 19H20M8 16L11 12L14 14L20 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
      </div>
      <h1>Usage Portal</h1>
      <p>Enter your API key to view token usage, request volume, and recent activity.</p>
      <form id="key-form">
        <label for="api-key">API key</label>
        <input id="api-key" autocomplete="off" spellcheck="false" placeholder="Paste API key or /usage/... link" />
        <button class="primary" type="submit">View Usage</button>
      </form>
      <div id="login-error" class="error"></div>
      <div class="hint">This page is read-only. The API key only unlocks usage data for that key.</div>
    </section>
  </main>

  <main id="dashboard" class="shell hidden">
    <section class="hero">
      <div class="hero-top">
        <div>
          <div class="badge-row">
            <span class="badge">Read-only usage portal</span>
            <span id="active-badge" class="badge ok">Active</span>
            <span id="stats-badge" class="badge">Usage stats</span>
          </div>
          <h1 id="key-title">API key</h1>
          <p>Token usage, request volume, and sanitized request activity for this API key.</p>
        </div>
        <div class="actions">
          <button id="refresh" class="btn" type="button">Refresh</button>
          <button id="other-key" class="btn" type="button">Other Key</button>
        </div>
      </div>
      <div class="divider"></div>
      <div class="meta-row">
        <p id="updated">Last updated -</p>
        <div class="range" role="tablist" aria-label="Usage range">
          <button type="button" data-window="1d">Today</button>
          <button type="button" data-window="7d">7D</button>
          <button type="button" data-window="30d">30D</button>
          <button type="button" data-window="60d">60D</button>
        </div>
      </div>
    </section>

    <section class="grid">
      <article class="card">
        <div class="card-label">Tokens</div>
        <div>
          <div id="tokens" class="card-value">0</div>
          <div id="tokens-sub" class="card-sub">Total tokens in selected range</div>
        </div>
      </article>
      <article class="card">
        <div class="card-label">Requests</div>
        <div>
          <div id="requests" class="card-value">0</div>
          <div id="requests-sub" class="card-sub">Successful and failed requests</div>
        </div>
      </article>
      <article class="card">
        <div class="card-label">Cache</div>
        <div>
          <div id="cache" class="card-value">0</div>
          <div class="card-sub">Cached tokens</div>
        </div>
      </article>
      <article class="card">
        <div class="card-label">Errors</div>
        <div>
          <div id="errors" class="card-value">0</div>
          <div id="errors-sub" class="card-sub">Failed requests</div>
        </div>
      </article>
    </section>

    <section class="chart">
      <div class="chart-head">
        <div>
          <h2>Usage chart</h2>
          <p>Daily token trend for this key.</p>
        </div>
      </div>
      <div id="chart" class="chart-box"></div>
    </section>

    <section class="table-wrap">
      <div class="table-head">
        <div>
          <h2>Recent requests</h2>
          <p>Sanitized request metadata for this API key.</p>
        </div>
      </div>
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Model</th>
              <th>Endpoint</th>
              <th>Input</th>
              <th>Cache</th>
              <th>Output</th>
              <th>Total</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody id="recent"></tbody>
        </table>
      </div>
      <div id="empty" class="empty hidden">No usage records for this range yet.</div>
    </section>
  </main>

  <script>
    const state = { key: "", window: "7d" };
    const fmt = new Intl.NumberFormat();

    function keyFromPath() {
      const parts = window.location.pathname.split("/").filter(Boolean);
      const usageIndex = parts.indexOf("usage");
      if (usageIndex === -1 || parts.length <= usageIndex + 1) return "";
      try { return decodeURIComponent(parts[usageIndex + 1]); } catch { return parts[usageIndex + 1]; }
    }

    function normalizeKeyInput(value) {
      const trimmed = String(value || "").trim();
      if (!trimmed) return "";
      try {
        const parsed = new URL(trimmed);
        const parts = parsed.pathname.split("/").filter(Boolean);
        const usageIndex = parts.indexOf("usage");
        if (usageIndex !== -1 && parts.length > usageIndex + 1) {
          return decodeURIComponent(parts[usageIndex + 1]);
        }
      } catch {}
      return trimmed;
    }

    function setMode(mode) {
      document.getElementById("login").classList.toggle("hidden", mode !== "login");
      document.getElementById("dashboard").classList.toggle("hidden", mode !== "dashboard");
    }

    function showLoginError(message) {
      const el = document.getElementById("login-error");
      el.textContent = message || "";
      el.style.display = message ? "block" : "none";
    }

    function openKey(key) {
      key = normalizeKeyInput(key);
      if (!key) return;
      window.history.pushState({}, "", "/usage/" + encodeURIComponent(key));
      state.key = key;
      load();
    }

    async function load() {
      if (!state.key) {
        setMode("login");
        return;
      }
      setMode("dashboard");
      const res = await fetch("/usage/" + encodeURIComponent(state.key) + "/data?window=" + encodeURIComponent(state.window), { cache: "no-store" });
      if (!res.ok) {
        const payload = await safeJson(res);
        setMode("login");
        document.getElementById("api-key").value = state.key;
        showLoginError(payload.error || "Unable to load usage for this API key.");
        return;
      }
      const data = await res.json();
      render(data);
    }

    async function safeJson(res) {
      try { return await res.json(); } catch { return {}; }
    }

    function render(data) {
      document.getElementById("key-title").textContent = data.key_label || "API key";
      document.getElementById("active-badge").textContent = data.active ? "Active" : "Inactive";
      document.getElementById("stats-badge").textContent = data.usage_statistics_enabled ? "Stats enabled" : "Stats disabled";
      document.getElementById("updated").textContent = "Last updated " + (data.updated_at ? new Date(data.updated_at).toLocaleString() : "never");

      const totals = data.totals || {};
      const tokens = totals.tokens || {};
      document.getElementById("tokens").textContent = compact(tokens.total_tokens || 0);
      document.getElementById("requests").textContent = fmt.format(totals.requests || 0);
      document.getElementById("cache").textContent = compact(tokens.cached_tokens || 0);
      document.getElementById("errors").textContent = fmt.format(totals.failed || 0);
      document.getElementById("requests-sub").textContent = fmt.format(totals.success || 0) + " ok, " + fmt.format(totals.failed || 0) + " failed";
      document.getElementById("errors-sub").textContent = data.usage_statistics_enabled ? "Failed requests" : "Enable usage-statistics-enabled";

      document.querySelectorAll("[data-window]").forEach(btn => {
        btn.classList.toggle("active", btn.dataset.window === state.window);
      });
      renderChart(data.series || []);
      renderRecent(data.recent_requests || []);
    }

    function compact(value) {
      value = Number(value || 0);
      if (value >= 1000000000) return (value / 1000000000).toFixed(1).replace(/\.0$/, "") + "B";
      if (value >= 1000000) return (value / 1000000).toFixed(1).replace(/\.0$/, "") + "M";
      if (value >= 1000) return (value / 1000).toFixed(1).replace(/\.0$/, "") + "K";
      return fmt.format(value);
    }

    function renderChart(series) {
      const width = 900;
      const height = 260;
      const pad = 28;
      const values = series.map(point => Number(point.tokens?.total_tokens || 0));
      const max = Math.max(1, ...values);
      const step = series.length > 1 ? (width - pad * 2) / (series.length - 1) : 0;
      const points = values.map((value, i) => {
        const x = pad + step * i;
        const y = height - pad - (value / max) * (height - pad * 2);
        return [x, y];
      });
      const line = points.map(p => p.join(",")).join(" ");
      const area = points.length ? pad + "," + (height - pad) + " " + line + " " + (width - pad) + "," + (height - pad) : "";
      const labels = series.map((point, i) => {
        if (series.length > 12 && i % Math.ceil(series.length / 8) !== 0 && i !== series.length - 1) return "";
        const x = pad + step * i;
        return '<text x="' + x + '" y="' + (height - 8) + '" text-anchor="middle" fill="#667085" font-size="12">' + escapeHtml(shortDate(point.date)) + '</text>';
      }).join("");
      document.getElementById("chart").innerHTML =
        '<svg viewBox="0 0 ' + width + ' ' + height + '" preserveAspectRatio="none">' +
        '<defs><linearGradient id="usageFill" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stop-color="#0f766e" stop-opacity="0.22"/><stop offset="1" stop-color="#0f766e" stop-opacity="0"/></linearGradient></defs>' +
        '<g stroke="#e5e7eb" stroke-width="1">' +
        '<line x1="' + pad + '" y1="' + (height - pad) + '" x2="' + (width - pad) + '" y2="' + (height - pad) + '"/>' +
        '<line x1="' + pad + '" y1="' + pad + '" x2="' + pad + '" y2="' + (height - pad) + '"/>' +
        '</g>' +
        '<polygon points="' + area + '" fill="url(#usageFill)"/>' +
        '<polyline points="' + line + '" fill="none" stroke="#0f766e" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/>' +
        labels +
        '</svg>';
    }

    function shortDate(value) {
      const parts = String(value || "").split("-");
      if (parts.length !== 3) return value || "";
      return parts[1] + "/" + parts[2];
    }

    function renderRecent(rows) {
      const tbody = document.getElementById("recent");
      const empty = document.getElementById("empty");
      tbody.innerHTML = rows.map(row => {
        const statusClass = row.failed ? "err" : "ok";
        const status = row.failed ? (row.status_code || "err") : "ok";
        return "<tr>" +
          "<td>" + escapeHtml(new Date(row.time).toLocaleString()) + "</td>" +
          "<td class=\"mono\">" + escapeHtml(row.model || "-") + "</td>" +
          "<td>" + escapeHtml(row.endpoint || "-") + "</td>" +
          "<td class=\"mono\">" + fmt.format(row.input_tokens || 0) + "</td>" +
          "<td class=\"mono\">" + fmt.format(row.cached_tokens || 0) + "</td>" +
          "<td class=\"mono\">" + fmt.format(row.output_tokens || 0) + "</td>" +
          "<td class=\"mono\">" + fmt.format(row.total_tokens || 0) + "</td>" +
          "<td><span class=\"status " + statusClass + "\">" + escapeHtml(String(status)) + "</span></td>" +
        "</tr>";
      }).join("");
      empty.classList.toggle("hidden", rows.length > 0);
    }

    function escapeHtml(value) {
      return String(value ?? "").replace(/[&<>"']/g, ch => ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        "\"": "&quot;",
        "'": "&#39;"
      }[ch]));
    }

    document.getElementById("key-form").addEventListener("submit", event => {
      event.preventDefault();
      showLoginError("");
      openKey(document.getElementById("api-key").value);
    });
    document.getElementById("refresh").addEventListener("click", () => load());
    document.getElementById("other-key").addEventListener("click", () => {
      state.key = "";
      window.history.pushState({}, "", "/usage");
      document.getElementById("api-key").value = "";
      showLoginError("");
      setMode("login");
    });
    document.querySelectorAll("[data-window]").forEach(btn => {
      btn.addEventListener("click", () => {
        state.window = btn.dataset.window;
        load();
      });
    });
    window.addEventListener("popstate", () => {
      state.key = keyFromPath();
      load();
    });

    state.key = keyFromPath();
    if (state.key) load(); else setMode("login");
  </script>
</body>
</html>`
