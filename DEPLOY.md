# CLIProxyAPI Deployment Guide

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
           <string>/Users/justin/Dev/VibeLab/CLIProxyAPI/cli-proxy-api</string>
           <string>--config</string>
           <string>/Users/justin/.cli-proxy-api/config.9router.yaml</string>
       </array>
       
       <key>WorkingDirectory</key>
       <string>/Users/justin/Dev/VibeLab/CLIProxyAPI</string>
       
       <key>RunAtLoad</key>
       <true/>
       
       <key>KeepAlive</key>
       <true/>
       
       <key>StandardOutPath</key>
       <string>/Users/justin/Dev/VibeLab/CLIProxyAPI/logs/stdout.log</string>
       
       <key>StandardErrorPath</key>
       <string>/Users/justin/Dev/VibeLab/CLIProxyAPI/logs/stderr.log</string>
       
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

```bash
# 1. Build new binary
go build -o cli-proxy-api ./cmd/server

# 2. Restart service
launchctl stop com.vibelab.cliproxyapi
launchctl start com.vibelab.cliproxyapi
```

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
   ```
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
