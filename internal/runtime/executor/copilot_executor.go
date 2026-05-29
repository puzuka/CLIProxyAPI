package executor

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor/helps"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// CopilotExecutor adapts GitHub Copilot OAuth credentials to the OpenAI-compatible API.
type CopilotExecutor struct {
	cfg    *config.Config
	compat *OpenAICompatExecutor
}

// NewCopilotExecutor creates an executor for GitHub Copilot.
func NewCopilotExecutor(cfg *config.Config) *CopilotExecutor {
	return &CopilotExecutor{
		cfg:    cfg,
		compat: NewOpenAICompatExecutor(copilot.ProviderKey, cfg),
	}
}

func (e *CopilotExecutor) Identifier() string {
	return copilot.ProviderKey
}

func (e *CopilotExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.compat.Execute(ctx, normalizeCopilotAuth(auth), req, opts)
}

func (e *CopilotExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return e.compat.ExecuteStream(ctx, normalizeCopilotAuth(auth), req, opts)
}

func (e *CopilotExecutor) CountTokens(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return e.compat.CountTokens(ctx, normalizeCopilotAuth(auth), req, opts)
}

func (e *CopilotExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	return e.compat.HttpRequest(ctx, normalizeCopilotAuth(auth), req)
}

func (e *CopilotExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if refreshed, handled, err := helps.RefreshAuthViaHome(ctx, e.cfg, auth); handled {
		return refreshed, err
	}
	if auth == nil {
		return nil, nil
	}
	githubToken := metadataString(auth.Metadata, "github_access_token")
	if githubToken == "" {
		return auth, nil
	}
	authSvc := copilot.NewCopilotAuthWithProxyURL(e.cfg, auth.ProxyURL)
	info, err := authSvc.RefreshCopilotToken(ctx, githubToken)
	if err != nil {
		return auth, err
	}

	updated := auth.Clone()
	if updated.Metadata == nil {
		updated.Metadata = make(map[string]any)
	}
	now := time.Now().UTC()
	updated.Metadata["access_token"] = info.Token
	updated.Metadata["copilot_token"] = info.Token
	updated.Metadata["copilot_api_endpoint"] = info.APIEndpoint()
	updated.Metadata["copilot_token_expires_at"] = info.ExpiresAt
	updated.Metadata["copilot_token_refresh_in"] = info.RefreshIn
	updated.Metadata["headers"] = copilotHeadersAsAny()
	updated.Metadata["last_refresh"] = now.Format(time.RFC3339)
	if info.ExpiresAt > 0 {
		updated.Metadata["expired"] = time.Unix(info.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	updated.LastRefreshedAt = now
	return normalizeCopilotAuth(updated), nil
}

func normalizeCopilotAuth(auth *cliproxyauth.Auth) *cliproxyauth.Auth {
	if auth == nil {
		return nil
	}
	out := auth.Clone()
	if out.Attributes == nil {
		out.Attributes = make(map[string]string)
	}
	out.Attributes["auth_kind"] = "oauth"
	token := metadataString(out.Metadata, "access_token")
	if token == "" {
		token = metadataString(out.Metadata, "copilot_token")
	}
	if token != "" {
		out.Attributes["api_key"] = token
	}
	baseURL := metadataString(out.Metadata, "copilot_api_endpoint")
	if baseURL == "" {
		baseURL = out.Attributes["base_url"]
	}
	if baseURL == "" {
		baseURL = copilot.DefaultAPIEndpoint
	}
	out.Attributes["base_url"] = strings.TrimRight(baseURL, "/")
	for key, value := range copilot.RequestHeaders() {
		out.Attributes["header:"+key] = value
	}
	return out
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func copilotHeadersAsAny() map[string]any {
	headers := copilot.RequestHeaders()
	out := make(map[string]any, len(headers))
	for key, value := range headers {
		out[key] = value
	}
	return out
}
