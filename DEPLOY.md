# CLIProxyAPI Deployment Guide

> Production safety: do not deploy, replace, restart, reload, `kickstart`, `bootout/bootstrap`, or otherwise touch the running production `cli-proxy-api` binary/process unless the user explicitly asks for that action. For UI/API work, build and verify artifacts first, then wait for explicit approval before applying them to the live service.

## Quick Start

### Build & Run

```bash
# Build binary
go build -o cli-proxy-api ./cmd/server

# Run with migrated local 9router config
./cli-proxy-api --config /Users/justin/.cli-proxy-api/config.9router.yaml

# Run with options
./cli-proxy-api --config config.yaml       # Fresh/default repo config
./cli-proxy-api --tui                    # Terminal UI mode
./cli-proxy-api --standalone             # Standalone mode
./cli-proxy-api --local-model            # Disable remote model updates
./cli-proxy-api --oauth-callback-port 8080
```

### Configuration

1. **Create config file**
   ```bash
   cp config.example.yaml config.yaml
   ```

   For the local 9router migration, use `/Users/justin/.cli-proxy-api/config.9router.yaml`.
   It contains real migrated API keys and should stay outside the tracked repository config.

2. **Set management password** in the active config file:
   ```yaml
   remote-management:
     secret-key: "your-password-here"  # Will be auto-hashed on startup
   ```

3. **Environment variables** (optional)
   - Copy `.env.example` to `.env`
   - Configure postgres/git/object store if needed
   - Configure vision middleware (e.g. `CAPTION_MODEL`) if using text-only models with images
   - For local file-based storage (default), no env vars required

## Auto-Start on macOS (launchd)

### Setup

1. **Build binary to project directory**
   ```bash
   go build -o cli-proxy-api ./cmd/server
   ```

2. **Create launchd plist** at `~/Library/LaunchAgents/com.vibelab.cliproxyapi.plist`:
   ```xml
   <?xml version="1.0" encoding="UTF-8"?>
   <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
   <plist version="1.0">
   <dict>
       <key>Label</key>
       <string>com.vibelab.cliproxyapi</string>
       
       <key>ProgramArguments</key>
       <array>
           <string>/Users/justin/Dev/VibeLab/CLIProxy/CLIProxyAPI/cli-proxy-api</string>
           <string>--config</string>
           <string>/Users/justin/.cli-proxy-api/config.9router.yaml</string>
       </array>
       
       <key>WorkingDirectory</key>
       <string>/Users/justin/Dev/VibeLab/CLIProxy/CLIProxyAPI</string>
       
       <key>RunAtLoad</key>
       <true/>
       
       <key>KeepAlive</key>
       <true/>
       
       <key>StandardOutPath</key>
       <string>/Users/justin/Dev/VibeLab/CLIProxy/CLIProxyAPI/logs/stdout.log</string>
       
       <key>StandardErrorPath</key>
       <string>/Users/justin/Dev/VibeLab/CLIProxy/CLIProxyAPI/logs/stderr.log</string>
       
       <key>EnvironmentVariables</key>
       <dict>
           <key>PATH</key>
           <string>/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
       </dict>
   </dict>
   </plist>
   ```

3. **Create logs directory**
   ```bash
   mkdir -p logs
   ```

4. **Load and start service**
   ```bash
   launchctl load ~/Library/LaunchAgents/com.vibelab.cliproxyapi.plist
   launchctl start com.vibelab.cliproxyapi
   ```

### Service Management

```bash
# Check status
launchctl list | grep cliproxyapi

# Start service
launchctl start com.vibelab.cliproxyapi

# Stop service
launchctl stop com.vibelab.cliproxyapi

# Restart service
launchctl stop com.vibelab.cliproxyapi && launchctl start com.vibelab.cliproxyapi

# Unload (disable auto-start)
launchctl unload ~/Library/LaunchAgents/com.vibelab.cliproxyapi.plist

# Reload after plist changes
launchctl unload ~/Library/LaunchAgents/com.vibelab.cliproxyapi.plist
launchctl load ~/Library/LaunchAgents/com.vibelab.cliproxyapi.plist
```

### Update Code

The management UI is bundled into the Go binary. After pulling backend or UI-related changes, rebuild and restart the service; only updating files on disk will not affect the running process.

```bash
# 1. Pull the latest code
git pull --ff-only origin main

# 2. Build into a temporary binary first
go build -o cli-proxy-api.new ./cmd/server

# 3. Keep a rollback copy, then atomically replace the runtime binary
cp -p cli-proxy-api /private/tmp/cli-proxy-api.previous.$(date +%Y%m%d-%H%M%S)
mv cli-proxy-api.new cli-proxy-api

# 4. Reload launchd so plist path/config changes are picked up
launchctl bootout gui/$(id -u)/com.vibelab.cliproxyapi || true
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.vibelab.cliproxyapi.plist
launchctl kickstart -k gui/$(id -u)/com.vibelab.cliproxyapi

# 5. Smoke test the new runtime
launchctl print gui/$(id -u)/com.vibelab.cliproxyapi
lsof -nP -iTCP:8317 -sTCP:LISTEN
curl -sS http://localhost:8317/healthz
curl -sS -o /tmp/cliproxy-management.html -w "%{http_code} %{size_download} %{content_type}\n" http://localhost:8317/management.html
curl -sS -o /tmp/cliproxy-usage-analytics.html -w "%{http_code} %{size_download} %{content_type}\n" http://localhost:8317/usage-analytics.html
```

If `launchctl print` shows an old `program`, `arguments`, `working directory`, or log path, update `~/Library/LaunchAgents/com.vibelab.cliproxyapi.plist` before restarting. The expected local project path is `/Users/justin/Dev/VibeLab/CLIProxy/CLIProxyAPI`.

## Monitoring

### Logs

```bash
# View real-time logs
tail -f logs/stdout.log
tail -f logs/stderr.log

# View recent logs
tail -50 logs/stdout.log

# Search logs
grep "error" logs/stdout.log
grep "management" logs/stdout.log
```

### Health Check

```bash
# Check if service is running
curl http://localhost:8317/

# Expected response:
# {"endpoints":["POST /v1/chat/completions","POST /v1/completions","GET /v1/models"],"message":"CLI Proxy API Server"}

# Health endpoint
curl http://localhost:8317/healthz
```

### Process Monitoring

```bash
# Check if port is in use
lsof -ti:8317

# Check process details
ps aux | grep cli-proxy-api

# Check launchd service status
launchctl list | grep cliproxyapi
# Output format: PID  Status  Label
# Example: 39195  0  com.vibelab.cliproxyapi
```

## Endpoints

### Main API Server
- **Base URL**: `http://localhost:8317`
- **Port**: 8317 (configurable in the active config file)

### Core API Endpoints

#### OpenAI Compatible
- `POST /v1/chat/completions` - Chat completions
- `POST /v1/completions` - Text completions
- `GET /v1/models` - List available models

#### Health & Status
- `GET /` - Server info and available endpoints
- `GET /healthz` - Health check endpoint
- `HEAD /healthz` - Health check (HEAD request)

### Management Panel

#### Web UI
- **URL**: `http://localhost:8317/management.html`
- **Password**: Set in the active config file under `remote-management.secret-key`
- **Features**:
  - View/edit configuration
  - Monitor usage statistics
  - Manage auth files
  - View logs
  - API testing tools

### End-user Usage Portal

- **URL**: `http://localhost:8317/usage`
- **Direct key URL**: `http://localhost:8317/usage/<api-key>`
- **Compatibility alias**: `/usages` redirects to `/usage`
- **Requires**: the API key must be valid for the proxy, and `usage-statistics-enabled: true` must be set to collect token/request history.
- **Scope**: read-only usage, daily token chart, request counts, and sanitized recent request metadata for the provided API key.

#### Management API Endpoints
Base path: `/v0/management`

**Configuration**
- `GET /v0/management/config` - Get current config (JSON)
- `GET /v0/management/config.yaml` - Get config file (YAML)
- `PUT /v0/management/config.yaml` - Update config file

**Debug & Logging**
- `GET /v0/management/debug` - Get debug status
- `PUT /v0/management/debug` - Enable/disable debug mode
- `GET /v0/management/logging-to-file` - Get logging status
- `PUT /v0/management/logging-to-file` - Enable/disable file logging
- `GET /v0/management/logs-max-total-size-mb` - Get log size limit
- `PUT /v0/management/logs-max-total-size-mb` - Set log size limit

**Auth Management**
- `GET /v0/management/auth-files` - List auth files
- `DELETE /v0/management/auth-files?name=<filename>` - Delete auth file

**Usage & Statistics**
- `GET /v0/management/usage-queue?count=<n>` - Get recent usage data

**System Info**
- `GET /v0/management/latest-version` - Check for updates

**Authentication**: All management endpoints require the management key in header:
```bash
curl -H "X-Management-Key: your-password" http://localhost:8317/v0/management/config
```

### WebSocket API
- `WS /v1/ws` - WebSocket API (requires auth if `ws-auth: true`)

### Gemini CLI Endpoints
- `/v1internal:*` - Gemini CLI internal endpoints (disabled by default)
- Enable in the active config file: `enable-gemini-cli-endpoint: true`

## Troubleshooting

### Port Already in Use

```bash
# Find process using port 8317
lsof -ti:8317

# Kill the process
kill <PID>

# Or kill directly
kill $(lsof -ti:8317)
```

### Service Won't Start

```bash
# Check logs for errors
tail -50 logs/stderr.log

# Common issues:
# 1. Port conflict - kill other process on port 8317
# 2. Permission denied - check file permissions
# 3. Config error - validate the active config file syntax
```

### Management Panel 404

- Correct URL is `/management.html` not `/v0/management`
- Ensure `secret-key` is set in the active config file
- Check logs: `grep management logs/stdout.log`

### Config Changes Not Applied

```bash
# Config is hot-reloaded automatically, but you can force restart:
launchctl stop com.vibelab.cliproxyapi
launchctl start com.vibelab.cliproxyapi
```

### Postgres Permission Errors

If you see `/var/lib/cliproxy: permission denied`:
- Comment out postgres config in `.env`:
  ```bash
  # PGSTORE_DSN=...
  # PGSTORE_SCHEMA=...
  # PGSTORE_LOCAL_PATH=...
  ```
- Default file-based storage works without postgres

## Storage Locations

- **Auth directory**: `~/.cli-proxy-api/` (configurable via `auth-dir` in config)
- **Logs**: `./logs/` (stdout.log, stderr.log)
- **Config**: `./config.yaml` by default; local 9router migration uses `~/.cli-proxy-api/config.9router.yaml`
- **Environment**: `./.env`

## Vision Middleware

CLIProxyAPI includes an in-process vision middleware that automatically describes images for text-only models. It intercepts image payloads and calls a vision model (default: `kimi-k2.6`) via a loopback request, passing the text description to the upstream model instead.

Configure it in your `.env` file:
```env
VISION_DISABLE=0                  # Set to 1 to completely disable the vision middleware
CAPTION_MODEL=gpt-5.5             # The vision model used to describe images
CAPTION_MAX_TOKENS=1000           # Max tokens for the image description
```

## Security Notes

1. **Management password**: Always set a strong password in the active config file
2. **Remote access**: By default, management API only accepts localhost connections
   - To allow remote access: set `allow-remote: true` in config
3. **API keys**: Configure in the active config file under `api-keys`
4. **TLS**: Enable HTTPS in config for production:
   ```yaml
   tls:
     enable: true
     cert: "/path/to/cert.pem"
     key: "/path/to/key.pem"
   ```

## Production Deployment

For production environments:

1. **Enable TLS** in config
2. **Set strong management password**
3. **Configure proper logging**:
   ```yaml
   logging-to-file: true
   logs-max-total-size-mb: 1000
   ```
4. **Enable usage statistics**:
   ```yaml
   usage-statistics-enabled: true
   usage-store:
     type: postgres
     dsn: postgresql://user:pass@localhost:5432/cliproxy
     schema: ""
     events-table: usage_events
     hourly-rollup-table: usage_rollups_hourly
     daily-rollup-table: usage_rollups_daily
     rollups-enabled: true
     rollup-query-min-events: 50000
   ```
   The usage analytics store persists append-only events for the current Usage & Analytics screen and maintains hourly/daily rollups for larger datasets. `USAGESTORE_DSN` and related `USAGESTORE_*` environment variables can override the YAML values. When `PGSTORE_DSN` is set and no explicit usage store DSN is configured, the same Postgres DSN is reused for usage analytics.
5. **Configure external storage** (postgres/git/object store) for high availability
6. **Set up monitoring** and alerting on health endpoints
7. **Use systemd** (Linux) or launchd (macOS) for auto-restart
8. **Configure firewall** to restrict access to management endpoints

## Performance Tuning

```yaml
# Reduce memory usage under high concurrency
commercial-mode: true

# Adjust retry behavior
request-retry: 3
max-retry-credentials: 5
max-retry-interval: 30

# Session affinity for consistent routing
routing:
  session-affinity: true
  session-affinity-ttl: "1h"

# Streaming keep-alives
streaming:
  keepalive-seconds: 15
  bootstrap-retries: 1
```

## Context Compaction

Codex CLI sends `POST /v1/responses/compact` to compress conversation context when a session runs long. Without compact routing configured, only Codex-native models handle this endpoint and other providers return `501 Not Implemented`. The shipped `config.example.yaml` enables a two-stage fallback by default: try Codex compact first when a Codex auth is available, then fall back to LLM-based custom compact if Codex compact is unavailable or fails.

### Option A: Compact Fallback (route through Codex)

Rewrites the model field so the request is forwarded to the Codex compact endpoint (`chatgpt.com/backend-api/codex/responses/compact`). Requires at least one active Codex auth.

```yaml
compact-fallback:
  enabled: true
  model: "gpt-5.5"                           # any compact-capable model served by a codex provider
  applies-to-providers: ["openai-compatibility"]  # or ["*"] for all non-codex
  trigger-log: true                           # optional: log compact I/O to logs/
```

| Field | Description |
|---|---|
| `enabled` | Toggle. Zero-value default is `false` when omitted; the shipped default config sets it to `true`. |
| `model` | Codex-capable substitute model (e.g. `gpt-5.5`). Any compact-capable model is valid as long as it resolves to an active Codex auth. |
| `applies-to-providers` | Provider identifiers that trigger the fallback. `["*"]` or `[]` matches every non-Codex provider. |
| `trigger-log` | When `true`, each compact-fallback call writes a private JSON log file (`logs/compact-*.log`, mode `0600`) containing the request input and response output. The write happens in a background goroutine and never affects compact speed or correctness. Zero-value default is `false` when omitted; the shipped default config sets it to `true`. |

**When to use:** You have Codex credentials and want compact to be handled by OpenAI's native compaction service regardless of which model the client is using.

**Behavior:**
1. Client requests compact for a non-Codex model (e.g. `deepseek-v4-pro`)
2. Proxy rewrites the model to the configured Codex compact model (for example `gpt-5.5`), strips provider-specific reasoning items
3. Request is forwarded to the Codex executor which calls the upstream compact endpoint
4. Response is returned to the client verbatim
5. If Codex auth is missing or the Codex compact call fails, the handler falls through to custom compact when `custom-compact.enabled: true`
6. If `trigger-log: true`, a background goroutine writes the request input and response output to `logs/compact-<timestamp>.log`

### Option B: Custom Compact (LLM-based secondary fallback)

When compact-fallback is disabled, unavailable, or fails, the proxy can perform compaction locally by calling any model registered in CLIProxy via `/chat/completions`. The proxy extracts the conversation, sends it to the LLM with a structured summarization prompt, validates the output, and wraps the result in the Responses API compact format.

```yaml
compact-fallback:
  enabled: true           # custom compact is used if this is unavailable or fails

custom-compact:
  enabled: true
  # model omitted: use the original requested model via /chat/completions
  # model: "deepseek-v4-pro" # optional: force any model registered in CLIProxy
  max-tokens: 4096            # optional, default 4096
  temperature: 0.2            # optional, default 0.2
  max-retries: 1              # optional, default 1
  trigger-log: true           # optional: log compact I/O to logs/
```

| Field | Description |
|---|---|
| `enabled` | Toggle. Default `false`. |
| `model` | Optional. When empty, custom compact uses the original requested model via `/chat/completions`. When set, it can be any model registered in CLIProxy. The LLM call goes through the proxy's own provider system (auth, load balancing, proxy config). |
| `max-tokens` | Maximum tokens for the LLM response. Default `4096`. |
| `temperature` | Sampling temperature. Lower = more deterministic. Default `0.2`. |
| `max-retries` | Retry attempts when the LLM output is missing required sections. Default `1`. |
| `trigger-log` | When `true`, each custom compact call writes a private JSON log file (`logs/compact-*.log`, mode `0600`) containing the request input and response output. The write happens in a background goroutine. Default `false`. |

**When to use:** You do not have Codex credentials, or you want to compact with a specific model (e.g. a local or third-party model) without depending on OpenAI's compact endpoint.

**Behavior:**
1. Client requests compact for any non-Codex model
2. Proxy extracts the full compact payload: top-level `instructions` (system prompt, truncated), `tools` (tool names), and `input` array (messages, function calls with 4K-char truncation, function outputs)
3. Proxy calls `/chat/completions` with the configured model and a structured handoff prompt
4. Output is validated for 10 required sections (Current task, User intent, Next action, etc.)
5. If validation fails, the proxy retries with feedback; on the last attempt the output is accepted as-is
6. The LLM text is wrapped in `response.compaction` format: preserved developer/user messages + `compaction_summary` item (matches Codex native compact output)

### Priority Order

When a compact request arrives, the proxy evaluates in this order:

1. **Codex-native** — the requested model belongs to a Codex provider → use native compact (no rewrite)
2. **Compact fallback** — `compact-fallback.enabled: true` → rewrite model, route to Codex
3. **Custom compact** — `custom-compact.enabled: true` → LLM-based compaction via `/chat/completions` when Codex fallback is disabled, unavailable, or fails
4. **Passthrough** — none of the above → forward to original provider's executor (usually returns `501`)

### Examples

**Use Codex compact for all non-Codex models (with logging):**
```yaml
compact-fallback:
  enabled: true
  model: "gpt-5.5"
  applies-to-providers: ["*"]
  trigger-log: true
```

**Default two-stage fallback (Codex first, then original model via chat):**
```yaml
compact-fallback:
  enabled: true
  model: "gpt-5.5"
  applies-to-providers: ["*"]
  trigger-log: true

custom-compact:
  enabled: true
  trigger-log: true
```

**Use deepseek for compact (no Codex needed, with logging):**
```yaml
compact-fallback:
  enabled: false

custom-compact:
  enabled: true
  model: "deepseek-v4-pro"
  trigger-log: true
```

**Use a specific model with tuned parameters:**
```yaml
compact-fallback:
  enabled: false

custom-compact:
  enabled: true
  model: "qwen-3-coder"
  max-tokens: 8192
  temperature: 0.1
  max-retries: 2
```

**Disable both (compact returns 501 for non-Codex models):**
```yaml
compact-fallback:
  enabled: false

custom-compact:
  enabled: false
```

### Verifying Compact Works

```bash
# Test compact endpoint with a simple request
curl -s -X POST http://localhost:8317/v1/responses/compact \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <your-api-key>" \
  -d '{
    "model": "deepseek-v4-pro",
    "input": [
      {"type":"message","role":"user","content":[{"type":"input_text","text":"Fix the login bug in auth.go"}]},
      {"type":"message","role":"assistant","content":[{"type":"output_text","text":"I found the issue in line 42..."}]}
    ]
  }' | jq .

# Expected: 200 with a response containing output[0].content[0].text
# with structured handoff sections (Current task, User intent, etc.)

# Check logs for compact activity
grep "custom compact" logs/stdout.log
grep "compact fallback" logs/stdout.log
```
