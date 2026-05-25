(function () {
  'use strict';

  if (window.__cpaUsageAnalyticsExtensionLoaded) return;
  window.__cpaUsageAnalyticsExtensionLoaded = true;

  var API_PREFIX = '/v0/management';
  var STORAGE_KEY_AUTH = 'cli-proxy-auth';
  var ENC_PREFIX = 'enc::v1::';
  var SECRET_SALT = 'cli-proxy-api-webui::secure-storage';
  var PERIODS = ['today', '24h', '7d', '30d', '60d'];
  var isPageMode =
    window.__CPA_USAGE_ANALYTICS_MODE__ === 'page' ||
    /\/usage-analytics\.html$/i.test(window.location.pathname);

  var state = {
    tab: 'overview',
    period: 'today',
    chartMetric: 'tokens',
    status: 'idle',
    error: '',
    auth: readAuth(),
    snapshot: null,
    details: null,
    filters: { provider: '', model: '', apiKey: '', endpoint: '', status: '' },
    page: 1,
    controller: null,
    detailReloadTimer: 0,
    mountTimer: 0,
    pageObserver: null,
    menuMounted: false,
    navigationBound: false,
    root: null,
    host: null,
    previousMainContent: null,
    previousMainChildren: null,
    started: false,
  };

  function normalizeApiBase(input) {
    var base = String(input || '').trim();
    if (!base) return '';
    base = base.replace(/\/?v0\/management\/?$/i, '');
    base = base.replace(/\/+$/i, '');
    if (!/^https?:\/\//i.test(base)) base = 'http://' + base;
    return base;
  }

  function detectApiBase() {
    try {
      return normalizeApiBase(window.location.protocol + '//' + window.location.host);
    } catch (error) {
      return 'http://localhost:8317';
    }
  }

  function apiBase() {
    return normalizeApiBase(state.auth.apiBase || detectApiBase()) + API_PREFIX;
  }

  function authHeaders(extra) {
    var headers = extra || {};
    if (state.auth.managementKey) headers.Authorization = 'Bearer ' + state.auth.managementKey;
    return headers;
  }

  function encodeText(text) {
    return new TextEncoder().encode(text);
  }

  function decodeText(bytes) {
    return new TextDecoder().decode(bytes);
  }

  function keyBytes() {
    return encodeText(SECRET_SALT + '|' + window.location.host + '|' + navigator.userAgent);
  }

  function xorBytes(data, key) {
    var out = new Uint8Array(data.length);
    for (var i = 0; i < data.length; i += 1) out[i] = data[i] ^ key[i % key.length];
    return out;
  }

  function fromBase64(value) {
    var binary = atob(value);
    var bytes = new Uint8Array(binary.length);
    for (var i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    return bytes;
  }

  function deobfuscate(value) {
    if (!value || value.indexOf(ENC_PREFIX) !== 0) return value;
    try {
      return decodeText(xorBytes(fromBase64(value.slice(ENC_PREFIX.length)), keyBytes()));
    } catch (error) {
      return value;
    }
  }

  function readStored(key) {
    try {
      if (!window.localStorage) return null;
      var raw = window.localStorage.getItem(key);
      if (raw === null) return null;
      var value = deobfuscate(raw);
      try {
        return JSON.parse(value);
      } catch (error) {
        return value;
      }
    } catch (error) {
      return null;
    }
  }

  function readSession(key) {
    try {
      return window.sessionStorage ? window.sessionStorage.getItem(key) || '' : '';
    } catch (error) {
      return '';
    }
  }

  function writeSession(key, value) {
    try {
      if (window.sessionStorage) window.sessionStorage.setItem(key, value);
    } catch (error) {
      // Session fallback is best-effort only.
    }
  }

  function readAuth() {
    var persisted = readStored(STORAGE_KEY_AUTH);
    var persistedState = persisted && typeof persisted === 'object' ? persisted.state || persisted : {};
    var legacyBase = readStored('apiBase') || readStored('apiUrl');
    var legacyKey = readStored('managementKey');
    return {
      apiBase: normalizeApiBase(
        readSession('cpa-usage-analytics-api-base') ||
          persistedState.apiBase ||
          legacyBase ||
          detectApiBase()
      ),
      managementKey: String(
        readSession('cpa-usage-analytics-management-key') ||
          persistedState.managementKey ||
          legacyKey ||
          ''
      ).trim(),
    };
  }

  function saveSessionAuth(apiBaseValue, keyValue) {
    var normalizedBase = normalizeApiBase(apiBaseValue || detectApiBase());
    var trimmedKey = String(keyValue || '').trim();
    state.auth = { apiBase: normalizedBase, managementKey: trimmedKey };
    writeSession('cpa-usage-analytics-api-base', normalizedBase);
    writeSession('cpa-usage-analytics-management-key', trimmedKey);
  }

  function formatNumber(value) {
    return Number(value || 0).toLocaleString();
  }

  function formatMoney(value) {
    return '$' + Number(value || 0).toFixed(4);
  }

  function formatCompactNumber(value) {
    value = Number(value || 0);
    if (value >= 1000000) return (value / 1000000).toFixed(value >= 10000000 ? 0 : 1).replace(/\.0$/, '') + 'M';
    if (value >= 1000) return (value / 1000).toFixed(value >= 10000 ? 0 : 1).replace(/\.0$/, '') + 'k';
    return String(Math.round(value));
  }

  function formatDateTime(value) {
    if (!value) return '-';
    var date = new Date(value);
    return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString();
  }

  function age(value) {
    if (!value) return '-';
    var then = new Date(value).getTime();
    if (!then) return '-';
    var seconds = Math.max(0, Math.floor((Date.now() - then) / 1000));
    if (seconds < 60) return seconds + 's';
    if (seconds < 3600) return Math.floor(seconds / 60) + 'm';
    if (seconds < 86400) return Math.floor(seconds / 3600) + 'h';
    return Math.floor(seconds / 86400) + 'd';
  }

  function escapeHtml(value) {
    return String(value == null ? '' : value)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function getErrorMessage(error) {
    return error && error.message ? error.message : String(error || 'Request failed');
  }

  function isAbortLikeError(error) {
    var message = getErrorMessage(error);
    return (error && error.name === 'AbortError') ||
      (error && error.code === 'ERR_CANCELED') ||
      /(^|\s)abort(ed)?(\s|$)/i.test(message) ||
      /BodyStreamBuffer was aborted/i.test(message);
  }

  function toQuery(values) {
    var query = new URLSearchParams();
    Object.keys(values || {}).forEach(function (key) {
      var value = values[key];
      if (value !== undefined && value !== null && value !== '') query.set(key, String(value));
    });
    var encoded = query.toString();
    return encoded ? '?' + encoded : '';
  }

  async function request(path) {
    if (!state.auth.managementKey) throw new Error('Enter the management key to load usage analytics.');
    var response = await fetch(apiBase() + path, {
      headers: authHeaders({ Accept: 'application/json' }),
    });
    if (response.status === 401) throw new Error('Management key was rejected.');
    if (!response.ok) throw new Error('Request failed: HTTP ' + response.status);
    return response.json();
  }

  function parseStreamEvent(text) {
    var lines = text.split(/\r?\n/);
    var data = [];
    for (var i = 0; i < lines.length; i += 1) {
      if (lines[i].indexOf('data:') === 0) data.push(lines[i].slice(5).trimStart());
    }
    if (!data.length) return null;
    return JSON.parse(data.join('\n'));
  }

  async function loadSnapshot() {
    state.snapshot = await request('/usage-analytics/stats' + toQuery({ period: state.period }));
    state.error = '';
    renderPage();
  }

  async function loadDetails() {
    state.details = await request('/usage-analytics/request-details' + toQuery({
      page: state.page,
      page_size: 25,
      provider: state.filters.provider,
      model: state.filters.model,
      api_key: state.filters.apiKey,
      endpoint: state.filters.endpoint,
      status: state.filters.status,
    }));
    state.error = '';
    renderPage();
  }

  function scheduleDetailsReload() {
    if (state.tab !== 'details') return;
    window.clearTimeout(state.detailReloadTimer);
    state.detailReloadTimer = window.setTimeout(function () {
      loadDetails().catch(function (error) {
        state.error = getErrorMessage(error);
        renderPage();
      });
    }, 250);
  }

  async function startStream() {
    if (state.controller) state.controller.abort();
    if (!state.auth.managementKey) {
      state.status = 'idle';
      renderPage();
      return;
    }

    var controller = new AbortController();
    state.controller = controller;
    state.status = 'connecting';
    state.error = '';
    renderPage();

    try {
      var response = await fetch(apiBase() + '/usage-analytics/stream' + toQuery({ period: state.period }), {
        headers: authHeaders({ Accept: 'text/event-stream' }),
        signal: controller.signal,
      });
      if (controller.signal.aborted || state.controller !== controller) return;
      if (response.status === 401) throw new Error('Management key was rejected.');
      if (!response.ok) throw new Error('Realtime stream failed: HTTP ' + response.status);
      if (!response.body) throw new Error('Realtime stream is unavailable.');

      state.status = 'connected';
      state.error = '';
      renderPage();

      var reader = response.body.getReader();
      var decoder = new TextDecoder();
      var buffer = '';
      while (!controller.signal.aborted && state.controller === controller) {
        var result = await reader.read();
        if (result.done) break;
        buffer += decoder.decode(result.value, { stream: true });
        var index = buffer.search(/\r?\n\r?\n/);
        while (index !== -1) {
          var eventText = buffer.slice(0, index);
          var separatorLength = buffer.startsWith('\r\n\r\n', index) ? 4 : 2;
          buffer = buffer.slice(index + separatorLength);
          var snapshot = parseStreamEvent(eventText);
          if (snapshot && state.controller === controller && !controller.signal.aborted) {
            state.snapshot = snapshot;
            state.error = '';
            renderPage();
            scheduleDetailsReload();
          }
          index = buffer.search(/\r?\n\r?\n/);
        }
      }
    } catch (error) {
      if (controller.signal.aborted || state.controller !== controller) return;
      if (isAbortLikeError(error)) {
        window.setTimeout(function () {
          if (state.controller === controller && !controller.signal.aborted) startStream();
        }, 3000);
        return;
      }
      state.status = 'error';
      state.error = getErrorMessage(error);
      renderPage();
      window.setTimeout(startStream, 3000);
    }
  }

  function stat(label, value, hint) {
    return '<div class="stat"><span>' + escapeHtml(label) + '</span><strong>' +
      escapeHtml(value) + '</strong><small>' + escapeHtml(hint || '') + '</small></div>';
  }

  function renderChart(snapshot) {
    var series = snapshot && snapshot.series ? snapshot.series : [];
    if (!series.length) return '<div class="empty">No usage in this period</div>';
    var metric = state.chartMetric === 'cost' ? 'cost' : 'tokens';
    var metricLabel = metric === 'cost' ? 'Cost' : 'Tokens';
    var metricValue = function (point) {
      return metric === 'cost' ? Number(point.cost_usd || 0) : Number(point.tokens || 0);
    };
    var formatMetric = function (value) {
      return metric === 'cost' ? formatMoney(value) : formatCompactNumber(value);
    };
    var formatTooltipMetric = function (value) {
      return metric === 'cost' ? formatMoney(value) : formatNumber(value) + ' tokens';
    };
    var values = series.map(metricValue);
    var rawMax = Math.max.apply(null, values.concat([0]));
    var scaleMax = rawMax > 0 ? rawMax : 1;
    var labelStep = Math.max(1, Math.ceil(series.length / 8));
    var grid = [100, 75, 50, 25, 0].map(function (percent) {
      var value = rawMax > 0 ? scaleMax * (percent / 100) : 0;
      return '<span class="chart-grid-line" style="top:' + (100 - percent) + '%"><em>' +
        escapeHtml(formatMetric(value)) + '</em></span>';
    }).join('');
    var points = series.map(function (point, index) {
      var value = values[index] || 0;
      return {
        x: series.length === 1 ? 50 : (index / (series.length - 1)) * 100,
        y: rawMax > 0 ? 100 - ((value / scaleMax) * 100) : 100,
        value: value,
        label: point.label || point.date || '',
      };
    });
    var pathPoints = points.map(function (point) {
      return point.x.toFixed(3) + ',' + point.y.toFixed(3);
    }).join(' ');
    var markers = points.map(function (point) {
      var tooltip = point.label + ' - ' + formatTooltipMetric(point.value);
      return '<span class="chart-point" data-tooltip="' + escapeHtml(tooltip) + '" aria-label="' +
        escapeHtml(tooltip) + '" tabindex="0" style="left:' + point.x.toFixed(3) + '%;top:' +
        point.y.toFixed(3) + '%"></span>';
    }).join('');
    var labels = series.map(function (point, index) {
      var label = point.label || point.date || '';
      var visible = index === 0 || index === series.length - 1 || index % labelStep === 0;
      return '<span>' + (visible ? escapeHtml(label) : '&nbsp;') + '</span>';
    }).join('');
    return '<div class="chart-shell">' +
      '<div class="chart-topline"><div class="chart-legend"><span class="legend-item"><i class="legend-dot token"></i>' +
      escapeHtml(metricLabel) + '</span></div><div class="chart-metric-toggle" role="group" aria-label="Chart metric">' +
      '<button data-chart-metric="tokens" class="' + (metric === 'tokens' ? 'active' : '') + '">Token</button>' +
      '<button data-chart-metric="cost" class="' + (metric === 'cost' ? 'active' : '') + '">Cost</button></div></div>' +
      '<div class="chart-frame"><div class="chart-y-title">' + escapeHtml(metricLabel) + '</div><div class="chart-y-axis">Scale</div>' +
      '<div class="chart-plot">' + grid + '<svg class="chart-line-svg" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">' +
      '<polyline class="chart-line" points="' + pathPoints + '"></polyline></svg><div class="chart-points">' + markers + '</div></div></div>' +
      '<div class="chart-x-axis">' + labels + '</div><div class="chart-x-title">Time bucket</div></div>';
  }

  function renderGroups(title, rows, nameField) {
    rows = rows || [];
    if (!rows.length) return '<section class="panel"><h3>' + escapeHtml(title) + '</h3><div class="empty">No data</div></section>';
    return '<section class="panel"><h3>' + escapeHtml(title) + '</h3><table><thead><tr><th>Name</th><th>Requests</th><th>Input</th><th>Output</th><th>Cost</th></tr></thead><tbody>' +
      rows.slice(0, 12).map(function (row) {
        return '<tr><td>' + escapeHtml(row[nameField] || row.key || 'unknown') +
          '</td><td>' + formatNumber(row.requests) +
          '</td><td>' + formatNumber(row.input_tokens) +
          '</td><td>' + formatNumber(row.output_tokens) +
          '</td><td>' + formatMoney(row.cost_usd) + '</td></tr>';
      }).join('') + '</tbody></table></section>';
  }

  function renderRecent(snapshot) {
    var requests = snapshot && snapshot.recent_requests ? snapshot.recent_requests : [];
    if (!requests.length) return '<div class="empty">No recent requests</div>';
    return '<div class="recent-table"><table><thead><tr><th>Time</th><th>Provider</th><th>Model</th><th>API Key</th><th>Endpoint</th><th>Status</th><th>Tokens</th><th>Cost</th><th>Age</th></tr></thead><tbody>' +
      requests.slice(0, 20).map(function (request) {
        var status = request.failed ? 'failed' : 'ok';
        return '<tr><td>' + escapeHtml(formatDateTime(request.time)) +
          '</td><td>' + escapeHtml(request.provider || '-') +
          '</td><td><strong class="recent-model">' + escapeHtml(request.model || 'unknown') + '</strong></td>' +
          '<td>' + escapeHtml(request.api_key_label || '-') +
          '</td><td class="recent-endpoint">' + escapeHtml(request.endpoint || '-') +
          '</td><td><span class="pill ' + (request.failed ? 'fail' : 'ok') + '">' + escapeHtml(status) + '</span></td>' +
          '<td>' + formatNumber(request.total_tokens) +
          '</td><td>' + formatMoney(request.cost_usd) +
          '</td><td class="recent-age">' + escapeHtml(age(request.time)) + '</td></tr>';
      }).join('') + '</tbody></table></div>';
  }

  function renderDetails() {
    var payload = state.details || {};
    var rows = payload.details || [];
    var pagination = payload.pagination || {};
    var totals = payload.totals || {};
    var totalTokens = totals.tokens || {};
    var detailStats = '<div class="stats detail-stats">' +
      stat('Requests', formatNumber(totals.requests), formatNumber(totals.success) + ' ok / ' + formatNumber(totals.failed) + ' failed') +
      stat('Input Tokens', formatNumber(totalTokens.input_tokens), formatNumber(totalTokens.cached_tokens) + ' cached') +
      stat('Output Tokens', formatNumber(totalTokens.output_tokens), formatNumber(totalTokens.reasoning_tokens) + ' reasoning') +
      stat('Total Tokens', formatNumber(totalTokens.total_tokens), 'filtered details') +
      stat('Estimated Cost', formatMoney(totals.cost_usd), 'filtered details') +
      '</div>';
    var body = rows.length ? rows.map(function (row) {
      var tokens = row.tokens || {};
      var latency = row.latency || {};
      return '<tr><td>' + escapeHtml(formatDateTime(row.timestamp)) +
        '</td><td>' + escapeHtml(row.provider || '-') +
        '</td><td>' + escapeHtml(row.model || '-') +
        '</td><td>' + escapeHtml(row.api_key_label || '-') +
        '</td><td><span class="pill ' + (row.failed ? 'fail' : 'ok') + '">' + escapeHtml(row.status || '-') +
        '</span></td><td>' + formatNumber(tokens.total_tokens) +
        '</td><td>' + formatNumber(latency.total) + ' ms</td></tr>';
    }).join('') : '<tr><td colspan="7"><div class="empty">No matching details</div></td></tr>';

    return '<section class="panel full"><h3>Request Details</h3><div class="filters">' +
      '<input data-field="provider" placeholder="Provider" value="' + escapeHtml(state.filters.provider) + '">' +
      '<input data-field="model" placeholder="Model" value="' + escapeHtml(state.filters.model) + '">' +
      '<input data-field="apiKey" placeholder="API Key" value="' + escapeHtml(state.filters.apiKey) + '">' +
      '<input data-field="endpoint" placeholder="Endpoint" value="' + escapeHtml(state.filters.endpoint) + '">' +
      '<select data-field="status"><option value="">Any status</option><option value="success"' +
      (state.filters.status === 'success' ? ' selected' : '') + '>Success</option><option value="failed"' +
      (state.filters.status === 'failed' ? ' selected' : '') + '>Failed</option></select>' +
      '<button data-action="apply-filters">Apply</button></div>' +
      detailStats +
      '<table><thead><tr><th>Time</th><th>Provider</th><th>Model</th><th>API Key</th><th>Status</th><th>Tokens</th><th>Latency</th></tr></thead><tbody>' +
      body + '</tbody></table>' +
      '<div class="pager"><button data-action="prev-page"' + (pagination.has_prev ? '' : ' disabled') +
      '>Prev</button><span>Page ' + formatNumber(pagination.page || state.page) + ' of ' +
      formatNumber(pagination.total_pages || 0) + ' / ' + formatNumber(pagination.total_items || 0) +
      ' items</span><button data-action="next-page"' + (pagination.has_next ? '' : ' disabled') +
      '>Next</button></div></section>';
  }

  function renderAuthBox() {
    return '<section class="auth-box"><strong>Management authorization required</strong>' +
      '<p>Use the same Management API key as the Management Center login. Remembered logins are detected automatically.</p>' +
      '<label>API base<input data-auth="apiBase" value="' + escapeHtml(state.auth.apiBase || detectApiBase()) + '"></label>' +
      '<label>Management key<input data-auth="managementKey" type="password" value="' + escapeHtml(state.auth.managementKey) + '"></label>' +
      '<button data-action="save-auth">Connect</button></section>';
  }

  function renderOverview(snapshot) {
    var totals = snapshot && snapshot.totals ? snapshot.totals : {};
    var tokens = totals.tokens || {};
    return '<div class="stats">' +
      stat('Requests', formatNumber(totals.requests), formatNumber(totals.success) + ' ok / ' + formatNumber(totals.failed) + ' failed') +
      stat('Input Tokens', formatNumber(tokens.input_tokens), formatNumber(tokens.cached_tokens) + ' cached') +
      stat('Output Tokens', formatNumber(tokens.output_tokens), formatNumber(tokens.reasoning_tokens) + ' reasoning') +
      stat('Total Tokens', formatNumber(tokens.total_tokens), snapshot ? snapshot.period : state.period) +
      stat('Estimated Cost', formatMoney(totals.cost_usd), snapshot ? snapshot.period : state.period) +
      '</div><section class="panel full"><h3>Usage Chart</h3>' + renderChart(snapshot) + '</section>' +
      '<div class="grid">' +
      renderGroups('By Provider', snapshot && snapshot.by_provider, 'provider') +
      renderGroups('By Model', snapshot && snapshot.by_model, 'model') +
      renderGroups('By Account', snapshot && snapshot.by_account, 'account_label') +
      renderGroups('By API Key', snapshot && snapshot.by_api_key, 'api_key_label') +
      renderGroups('By Endpoint', snapshot && snapshot.by_endpoint, 'endpoint') +
      '</div><section class="panel full"><h3>Recent Requests</h3>' + renderRecent(snapshot) + '</section>';
  }

  function renderPage() {
    if (!state.root) return;
    var snapshot = state.snapshot;
    var statusText = state.status === 'connected' ? 'Realtime connected' :
      state.status === 'connecting' ? 'Connecting realtime...' :
        state.status === 'error' ? 'Realtime disconnected' : 'Ready';

    state.root.innerHTML = '<style>' + pageStyles() + '</style><div class="usage-page">' +
      '<header><div><span class="eyebrow">Management extension</span><h1>Usage & Analytics</h1>' +
      '<p>Request volume, token flow, provider activity, and captured request details.</p></div>' +
      '<div class="header-actions"><span class="status ' + state.status + '">' + escapeHtml(statusText) + '</span>' +
      '<button data-action="refresh">Refresh</button></div></header>' +
      '<div class="toolbar"><div class="segments">' + PERIODS.map(function (period) {
        return '<button data-period="' + period + '" class="' + (state.period === period ? 'active' : '') + '">' + period + '</button>';
      }).join('') + '</div><div class="tabs"><button data-tab="overview" class="' + (state.tab === 'overview' ? 'active' : '') + '">Overview</button>' +
      '<button data-tab="details" class="' + (state.tab === 'details' ? 'active' : '') + '">Details</button></div></div>' +
      (state.error ? '<div class="error">' + escapeHtml(state.error) + '</div>' : '') +
      (!state.auth.managementKey ? renderAuthBox() : '') +
      (snapshot && snapshot.usage_statistics_enabled === false ? '<div class="warning">Usage statistics are disabled. Enable usage-statistics-enabled in Config Panel to collect new requests.</div>' : '') +
      (state.tab === 'overview' ? renderOverview(snapshot) : renderDetails()) +
      '</div>';
    bindPageEvents();
  }

  function bindPageEvents() {
    var root = state.root;
    root.querySelectorAll('[data-action]').forEach(function (node) {
      node.addEventListener('click', function () {
        var action = node.getAttribute('data-action');
        if (action === 'refresh') {
          loadSnapshot().catch(showError);
          if (state.tab === 'details') loadDetails().catch(showError);
          startStream();
        }
        if (action === 'save-auth') {
          saveSessionAuth(
            root.querySelector('[data-auth="apiBase"]').value,
            root.querySelector('[data-auth="managementKey"]').value
          );
          loadSnapshot().catch(showError);
          startStream();
        }
        if (action === 'apply-filters') {
          state.filters.provider = root.querySelector('[data-field="provider"]').value;
          state.filters.model = root.querySelector('[data-field="model"]').value;
          state.filters.apiKey = root.querySelector('[data-field="apiKey"]').value;
          state.filters.endpoint = root.querySelector('[data-field="endpoint"]').value;
          state.filters.status = root.querySelector('[data-field="status"]').value;
          state.page = 1;
          loadDetails().catch(showError);
        }
        if (action === 'prev-page') {
          state.page = Math.max(1, state.page - 1);
          loadDetails().catch(showError);
        }
        if (action === 'next-page') {
          state.page += 1;
          loadDetails().catch(showError);
        }
      });
    });

    root.querySelectorAll('[data-period]').forEach(function (node) {
      node.addEventListener('click', function () {
        state.period = node.getAttribute('data-period');
        state.snapshot = null;
        state.details = null;
        state.page = 1;
        renderPage();
        loadSnapshot().catch(showError);
        startStream();
        if (state.tab === 'details') loadDetails().catch(showError);
      });
    });

    root.querySelectorAll('[data-tab]').forEach(function (node) {
      node.addEventListener('click', function () {
        state.tab = node.getAttribute('data-tab');
        renderPage();
        if (state.tab === 'details' && !state.details) loadDetails().catch(showError);
      });
    });

    root.querySelectorAll('[data-chart-metric]').forEach(function (node) {
      node.addEventListener('click', function () {
        state.chartMetric = node.getAttribute('data-chart-metric') === 'cost' ? 'cost' : 'tokens';
        renderPage();
      });
    });
  }

  function showError(error) {
    state.error = getErrorMessage(error);
    renderPage();
  }

  function usageSidebarIconSVG() {
    return '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 19V5"/><path d="M4 19h16"/><path d="M8 16v-5"/><path d="M12 16V8"/><path d="M16 16v-8"/><path d="M20 16v-3"/></svg>';
  }

  function stripActiveClassNames(node) {
    if (!node) return;
    if (node.classList) {
      Array.prototype.slice.call(node.classList).forEach(function (className) {
        if (/active/i.test(className)) node.classList.remove(className);
      });
      return;
    }
    var classNameValue = typeof node.className === 'string' ? node.className : (node.getAttribute ? node.getAttribute('class') : '');
    if (!classNameValue) return;
    var cleanClassName = String(classNameValue)
      .split(/\s+/)
      .filter(function (className) { return className && !/active/i.test(className); })
      .join(' ');
    if (node.setAttribute) node.setAttribute('class', cleanClassName);
  }

  function updateUsageSidebarIcon(link) {
    var icon = link.querySelector('svg');
    if (icon) {
      icon.outerHTML = usageSidebarIconSVG();
      return;
    }
    link.insertAdjacentHTML('afterbegin', '<span class="cpa-usage-icon" aria-hidden="true">' + usageSidebarIconSVG() + '</span>');
  }

  function updateUsageSidebarLabel(link) {
    var updated = false;
    Array.prototype.slice.call(link.childNodes).forEach(function (node) {
      if (node.nodeType === Node.TEXT_NODE && String(node.textContent || '').trim()) {
        node.textContent = 'Usage & Analytics';
        updated = true;
      }
    });
    var labels = Array.prototype.slice.call(link.querySelectorAll('span')).filter(function (node) {
      return !node.querySelector('svg') && String(node.textContent || '').trim();
    });
    if (labels.length) {
      labels[labels.length - 1].textContent = 'Usage & Analytics';
      updated = true;
    }
    if (!updated) {
      var label = document.createElement('span');
      label.textContent = 'Usage & Analytics';
      link.appendChild(label);
    }
  }

  function injectSidebarLink() {
    if (document.querySelector('[data-cpa-usage-nav="true"]')) return true;
    var anchors = Array.prototype.slice.call(document.querySelectorAll('a[href]'));
    var template = anchors.find(function (anchor) {
      var href = anchor.getAttribute('href') || '';
      var text = anchor.textContent || '';
      return /#\/quota|\/quota\b/i.test(href) || /Quota|配额|配額/i.test(text);
    }) || anchors.find(function (anchor) {
      var href = anchor.getAttribute('href') || '';
      return /#\/config|\/config\b|#\/system|\/system\b/i.test(href);
    });
    if (!template) return false;

    var item = template.closest('li') || template;
    var clone = item.cloneNode(false);
    var link = template.cloneNode(true);
    link.setAttribute('href', '/usage-analytics.html');
    link.setAttribute('data-cpa-usage-nav', 'true');
    link.removeAttribute('aria-current');
    stripActiveClassNames(link);
    stripActiveClassNames(clone);
    Array.prototype.slice.call(link.querySelectorAll('[class]')).forEach(stripActiveClassNames);
    updateUsageSidebarIcon(link);
    updateUsageSidebarLabel(link);
    link.addEventListener('click', function (event) {
      event.preventDefault();
      event.stopPropagation();
      openUsagePage();
    }, true);
    if (item === template) {
      template.insertAdjacentElement('afterend', link);
    } else {
      clone.appendChild(link);
      item.insertAdjacentElement('afterend', clone);
    }
    syncSidebarActiveState();
    return true;
  }

  function syncSidebarActiveState() {
    if (!isPageMode) return;
    var usageLink = document.querySelector('[data-cpa-usage-nav="true"]');
    if (!usageLink) return;
    document.querySelectorAll('.nav-item.active, a.active').forEach(function (node) {
      if (node !== usageLink) node.classList.remove('active');
    });
    usageLink.classList.add('active');
    usageLink.setAttribute('aria-current', 'page');
  }

  function mountManagementMenuExtension() {
    if (state.menuMounted) {
      injectSidebarLink();
      syncSidebarActiveState();
      return;
    }
    state.menuMounted = true;
    var style = document.createElement('style');
    style.textContent = '.cpa-usage-icon{display:inline-flex;align-items:center;justify-content:center;width:28px;height:28px;flex:0 0 auto;border:1px solid color-mix(in srgb,currentColor 16%,transparent);border-radius:8px;background:linear-gradient(135deg,color-mix(in srgb,currentColor 16%,transparent),color-mix(in srgb,currentColor 5%,transparent));box-shadow:inset 0 1px 0 rgba(255,255,255,.12)}.cpa-usage-icon svg,[data-cpa-usage-nav="true"] svg{width:16px;height:16px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}';
    document.head.appendChild(style);
    injectSidebarLink();
    var observer = new MutationObserver(function () {
      injectSidebarLink();
      syncSidebarActiveState();
    });
    observer.observe(document.body, { childList: true, subtree: true });
  }

  function openUsagePage() {
    isPageMode = true;
    try {
      if (!/\/usage-analytics\.html$/i.test(window.location.pathname)) {
        window.history.pushState({ cpaUsageAnalytics: true }, '', '/usage-analytics.html');
      }
    } catch (error) {
      window.location.assign('/usage-analytics.html');
      return;
    }
    mountUsagePage();
  }

  function leaveUsagePage(activeLink) {
    if (!isPageMode) return;
    isPageMode = false;
    if (state.previousMainContent && state.host && state.previousMainContent.contains(state.host)) {
      state.previousMainContent.replaceChildren.apply(
        state.previousMainContent,
        state.previousMainChildren || []
      );
    }
    state.host = null;
    state.root = null;
    state.previousMainContent = null;
    state.previousMainChildren = null;
    if (state.controller) {
      state.controller.abort();
      state.controller = null;
    }
    var usageLink = document.querySelector('[data-cpa-usage-nav="true"]');
    if (usageLink) {
      usageLink.classList.remove('active');
      usageLink.removeAttribute('aria-current');
    }
    if (activeLink && activeLink.classList) {
      activeLink.classList.add('active');
      activeLink.setAttribute('aria-current', 'page');
    }
  }

  function bindPageNavigation() {
    if (state.navigationBound) return;
    state.navigationBound = true;
    document.addEventListener('click', function (event) {
      if (!isPageMode) return;
      var target = event.target;
      var link = target && target.closest ? target.closest('a[href]') : null;
      if (!link || link.getAttribute('data-cpa-usage-nav') === 'true') return;
      var href = link.getAttribute('href') || '';
      if (href.indexOf('#/') === 0) {
        event.preventDefault();
        event.stopPropagation();
        leaveUsagePage(link);
        try {
          window.history.pushState(null, '', '/management.html' + href);
          window.dispatchEvent(new HashChangeEvent('hashchange'));
          window.dispatchEvent(new PopStateEvent('popstate', { state: null }));
        } catch (error) {
          window.location.assign('/management.html' + href);
        }
        return;
      }
      if (/\/management\.html/i.test(href)) leaveUsagePage(link);
    }, true);
    window.addEventListener('popstate', function () {
      if (/\/usage-analytics\.html$/i.test(window.location.pathname)) {
        isPageMode = true;
        mountUsagePage();
      } else {
        leaveUsagePage();
      }
    });
  }

  function mountUsagePage() {
    if (!isPageMode) return;
    mountManagementMenuExtension();
    bindPageNavigation();
    mountUsagePageInManagementShell();
    if (!state.pageObserver) {
      state.pageObserver = new MutationObserver(function () {
        window.clearTimeout(state.mountTimer);
        state.mountTimer = window.setTimeout(mountUsagePageInManagementShell, 0);
      });
      state.pageObserver.observe(document.body, { childList: true, subtree: true });
    }
  }

  function mountUsagePageInManagementShell() {
    if (!isPageMode) return false;
    var shell = document.querySelector('.app-shell');
    if (!shell || !shell.querySelector('.sidebar')) return false;
    var content = shell.querySelector('.content > main.main-content');
    if (!content) return false;
    var host = document.getElementById('cpa-usage-analytics-page');
    if (!host || !content.contains(host)) {
      state.previousMainContent = content;
      state.previousMainChildren = Array.prototype.slice.call(content.childNodes);
      host = document.createElement('div');
      host.id = 'cpa-usage-analytics-page';
      content.replaceChildren(host);
    }
    mountUsageRoot(host);
    syncSidebarActiveState();
    return true;
  }

  function mountUsageRoot(host) {
    var shadow = host.shadowRoot || host.attachShadow({ mode: 'open' });
    if (state.host === host && state.root === shadow) {
      syncSidebarActiveState();
      return;
    }
    state.host = host;
    state.root = shadow;
    renderPage();
    if (!state.started && state.auth.managementKey) {
      state.started = true;
      loadSnapshot().catch(showError);
      startStream();
    }
  }

  function pageStyles() {
    return [
      ':host{all:initial;display:block;width:100%;color-scheme:light dark;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:var(--text-primary,#172033)}',
      '*{box-sizing:border-box}',
      'button,input,select{font:inherit}',
      '.usage-page{width:100%;min-width:0;display:flex;flex-direction:column;gap:16px;color:var(--text-primary,#172033)}',
      'header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px}',
      'h1{margin:2px 0 6px;font-size:28px;line-height:1.1;color:var(--text-primary,#111827);letter-spacing:0}',
      'p{margin:0;color:var(--text-secondary,#64748b)}',
      '.eyebrow{color:var(--primary-color,#2563eb);font-size:11px;text-transform:uppercase;letter-spacing:.08em;font-weight:800}',
      '.header-actions{display:flex;align-items:center;gap:10px}',
      'button{border:1px solid var(--border-color,#d1d5db);background:var(--bg-secondary,#fff);color:var(--text-primary,#172033);border-radius:8px;padding:8px 11px;cursor:pointer}',
      'button:hover:not(:disabled){background:var(--bg-hover,var(--bg-tertiary,#f3f4f6));border-color:var(--border-hover,var(--border-color,#d1d5db))}',
      'button:disabled{opacity:.6;cursor:not-allowed}',
      '.toolbar{display:flex;justify-content:space-between;gap:12px;flex-wrap:wrap}',
      '.segments,.tabs{display:flex;gap:7px;flex-wrap:wrap}',
      '.segments button.active,.tabs button.active{background:var(--primary-color,#172033);border-color:var(--primary-color,#172033);color:var(--primary-contrast,#fff)}',
      '.status{border:1px solid var(--border-color,#d1d5db);border-radius:999px;background:var(--bg-tertiary,#e2e8f0);color:var(--text-secondary,#334155);padding:6px 10px;font-size:12px}',
      '.status.connected{background:var(--success-badge-bg,#dcfce7);border-color:var(--success-badge-border,#86efac);color:var(--success-badge-text,#166534)}',
      '.status.connecting{background:color-mix(in srgb,var(--primary-color,#2563eb) 14%,transparent);border-color:color-mix(in srgb,var(--primary-color,#2563eb) 34%,transparent);color:var(--primary-active,var(--primary-color,#1d4ed8))}',
      '.status.error{background:var(--failure-badge-bg,#fee2e2);border-color:var(--failure-badge-border,#fecaca);color:var(--failure-badge-text,#991b1b)}',
      '.error,.warning,.auth-box,.panel,.stat{border:1px solid var(--border-color,#e5e7eb);background:var(--bg-primary,#fff);border-radius:12px;box-shadow:var(--shadow,none)}',
      '.error,.warning{padding:12px 14px}',
      '.error{color:var(--failure-badge-text,#991b1b);background:var(--failure-badge-bg,#fef2f2);border-color:var(--failure-badge-border,#fecaca)}',
      '.warning{color:var(--warning-text,#92400e);background:var(--warning-bg,#fffbeb);border-color:var(--warning-border,#fde68a)}',
      '.auth-box{padding:16px;display:grid;gap:12px;max-width:720px}',
      '.auth-box label{display:grid;gap:6px;font-size:12px;color:var(--text-secondary,#334155)}',
      '.auth-box input,.filters input,.filters select{width:100%;border:1px solid var(--border-color,#d1d5db);border-radius:8px;padding:9px 10px;background:var(--bg-secondary,#fff);color:var(--text-primary,#172033)}',
      '.auth-box input:focus,.filters input:focus,.filters select:focus{border-color:var(--primary-color,#2563eb);outline:none;box-shadow:0 0 0 3px color-mix(in srgb,var(--primary-color,#2563eb) 20%,transparent)}',
      '.stats{display:grid;grid-template-columns:repeat(auto-fit,minmax(170px,1fr));gap:12px}',
      '.detail-stats{margin:0 0 12px}',
      '.stat{padding:15px}',
      '.stat span{display:block;color:var(--text-secondary,#64748b);font-size:12px}',
      '.stat strong{display:block;margin-top:5px;font-size:26px;line-height:1.15;color:var(--text-primary,#172033)}',
      '.stat small{color:var(--text-secondary,#64748b)}',
      '.grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}',
      '.panel{padding:16px;min-width:0}',
      '.panel.full{width:100%}',
      'h3{margin:0 0 12px;font-size:15px;color:var(--text-primary,#334155);letter-spacing:0}',
      '.chart-shell{display:grid;gap:8px}',
      '.chart-topline{display:flex;align-items:center;justify-content:space-between;gap:10px;flex-wrap:wrap}',
      '.chart-legend{display:flex;align-items:center;gap:14px;color:var(--text-secondary,#64748b);font-size:12px}',
      '.legend-item{display:inline-flex;align-items:center;gap:6px}',
      '.legend-dot{width:9px;height:9px;border-radius:999px;display:inline-block}',
      '.legend-dot.token{background:var(--primary-color,#2563eb)}',
      '.chart-metric-toggle{display:inline-flex;border:1px solid var(--border-color,#d1d5db);border-radius:8px;background:var(--bg-secondary,#fff);overflow:hidden}',
      '.chart-metric-toggle button{border:0;border-radius:0;background:transparent;padding:6px 10px;font-size:12px;color:var(--text-secondary,#64748b)}',
      '.chart-metric-toggle button.active{background:var(--primary-color,#172033);color:var(--primary-contrast,#fff)}',
      '.chart-frame{display:grid;grid-template-columns:18px 42px minmax(0,1fr);gap:8px;min-height:250px}',
      '.chart-y-title{writing-mode:vertical-rl;transform:rotate(180deg);align-self:center;justify-self:center;color:var(--text-secondary,#64748b);font-size:11px;font-weight:800;text-transform:uppercase;letter-spacing:.08em}',
      '.chart-y-axis{align-self:start;color:var(--text-tertiary,var(--text-secondary,#64748b));font-size:11px}',
      '.chart-plot{position:relative;min-width:0;border-left:1px solid var(--border-color,#e5e7eb);border-bottom:1px solid var(--border-color,#e5e7eb);background:linear-gradient(to bottom,color-mix(in srgb,var(--border-color,#e5e7eb) 50%,transparent) 1px,transparent 1px);background-size:100% 25%;overflow:visible}',
      '.chart-grid-line{position:absolute;left:0;right:0;border-top:1px solid color-mix(in srgb,var(--border-color,#e5e7eb) 62%,transparent);height:0}',
      '.chart-grid-line em{position:absolute;left:-42px;top:-7px;width:34px;text-align:right;color:var(--text-tertiary,var(--text-secondary,#64748b));font-style:normal;font-size:11px}',
      '.chart-line-svg{position:absolute;inset:0;width:100%;height:100%;overflow:visible}',
      '.chart-line{fill:none;stroke:var(--primary-color,#2563eb);stroke-width:2.5;stroke-linecap:round;stroke-linejoin:round;vector-effect:non-scaling-stroke}',
      '.chart-points{position:absolute;inset:0}',
      '.chart-point{position:absolute;width:9px;height:9px;border:2px solid var(--bg-primary,#fff);border-radius:999px;background:var(--primary-color,#2563eb);box-shadow:0 0 0 1px color-mix(in srgb,var(--primary-color,#2563eb) 40%,var(--border-color,#e5e7eb));cursor:default;outline:none;transform:translate(-50%,-50%);z-index:2}',
      '.chart-point::after{content:attr(data-tooltip);position:absolute;left:50%;bottom:calc(100% + 8px);min-width:max-content;padding:6px 8px;border:1px solid var(--border-color,#d1d5db);border-radius:8px;background:var(--bg-primary,#fff);box-shadow:0 8px 24px rgba(15,23,42,.18);color:var(--text-primary,#172033);font-size:12px;font-weight:700;line-height:1.2;pointer-events:none;white-space:nowrap;opacity:0;transform:translate(-50%,0);z-index:20}',
      '.chart-point::before{content:"";position:absolute;left:50%;bottom:calc(100% + 3px);border:5px solid transparent;border-top-color:var(--border-color,#d1d5db);pointer-events:none;opacity:0;transform:translateX(-50%);z-index:19}',
      '.chart-point:hover::after,.chart-point:focus-visible::after,.chart-point:hover::before,.chart-point:focus-visible::before{opacity:1}',
      '.chart-x-axis{display:flex;gap:5px;margin-left:68px;color:var(--text-tertiary,var(--text-secondary,#64748b));font-size:11px}',
      '.chart-x-axis span{flex:1;min-width:8px;text-align:center;white-space:nowrap;overflow:hidden}',
      '.chart-x-title{text-align:center;color:var(--text-secondary,#64748b);font-size:11px;font-weight:800;text-transform:uppercase;letter-spacing:.08em}',
      '.empty{padding:28px;text-align:center;color:var(--text-secondary,#64748b);font-size:13px}',
      'table{width:100%;border-collapse:collapse;font-size:13px}',
      'th,td{text-align:left;border-bottom:1px solid var(--border-color,#e5e7eb);padding:9px;vertical-align:top;color:var(--text-primary,#172033)}',
      'th{color:var(--text-secondary,#64748b);font-weight:800}',
      '.recent-table{overflow:auto;border:1px solid var(--border-color,#e5e7eb);border-radius:10px;background:var(--bg-secondary,#fff)}',
      '.recent-table table{min-width:920px}',
      '.recent-table tr:last-child td{border-bottom:0}',
      '.recent-model{font-size:13px;color:var(--text-primary,#172033)}',
      '.recent-endpoint{max-width:420px;word-break:break-word}',
      '.recent-age{white-space:nowrap;color:var(--text-secondary,#64748b)}',
      '.filters{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:8px;margin-bottom:12px}',
      '.pager{display:flex;justify-content:space-between;align-items:center;gap:8px;padding-top:12px;color:var(--text-secondary,#64748b);font-size:12px}',
      '.pill{display:inline-flex;border:1px solid var(--border-color,#d1d5db);border-radius:999px;padding:2px 7px;background:var(--bg-tertiary,#e2e8f0);color:var(--text-secondary,#334155)}',
      '.pill.ok{background:var(--success-badge-bg,#dcfce7);border-color:var(--success-badge-border,#86efac);color:var(--success-badge-text,#166534)}',
      '.pill.fail{background:var(--failure-badge-bg,#fee2e2);border-color:var(--failure-badge-border,#fecaca);color:var(--failure-badge-text,#991b1b)}',
      '@media(max-width:900px){.stats,.grid,.filters{grid-template-columns:1fr}header{flex-direction:column}.recent-table{overflow-x:auto}}',
    ].join('');
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', isPageMode ? mountUsagePage : mountManagementMenuExtension, { once: true });
  } else if (isPageMode) {
    mountUsagePage();
  } else {
    mountManagementMenuExtension();
  }
})();
