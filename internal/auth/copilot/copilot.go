package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	GitHubClientID  = "Iv1.b507a08c87ecfe98"
	deviceCodeURL   = "https://github.com/login/device/code"
	tokenURL        = "https://github.com/login/oauth/access_token"
	userInfoURL     = "https://api.github.com/user"
	copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
	defaultScope    = "read:user"

	defaultPollInterval = 5 * time.Second
	maxPollDuration     = 15 * time.Minute
)

// Auth handles GitHub Copilot OAuth device flow and token exchange.
type Auth struct {
	client *http.Client
}

// NewCopilotAuth creates a Copilot auth service.
func NewCopilotAuth(cfg *config.Config) *Auth {
	return NewCopilotAuthWithProxyURL(cfg, "")
}

// NewCopilotAuthWithProxyURL creates a Copilot auth service with an auth-level proxy override.
func NewCopilotAuthWithProxyURL(cfg *config.Config, proxyURL string) *Auth {
	client := &http.Client{Timeout: 30 * time.Second}
	effectiveProxyURL := strings.TrimSpace(proxyURL)
	var sdkCfg config.SDKConfig
	if cfg != nil {
		sdkCfg = cfg.SDKConfig
		if effectiveProxyURL == "" {
			effectiveProxyURL = strings.TrimSpace(cfg.ProxyURL)
		}
	}
	sdkCfg.ProxyURL = effectiveProxyURL
	client = util.SetProxy(&sdkCfg, client)
	return &Auth{client: client}
}

// RequestHeaders returns the headers used by VS Code Copilot Chat compatible requests.
func RequestHeaders() map[string]string {
	return map[string]string{
		"Accept":                 "application/json",
		"Copilot-Integration-Id": "vscode-chat",
		"Editor-Plugin-Version":  "copilot-chat/0.35.0",
		"Editor-Version":         "vscode/1.85.1",
		"OpenAI-Intent":          "conversation-panel",
		"User-Agent":             "GitHubCopilotChat/0.35.0",
		"X-GitHub-Api-Version":   "2025-04-01",
		"X-Initiator":            "user",
		"X-Requested-With":       "XMLHttpRequest",
	}
}

func requestHeadersAsAny() map[string]any {
	headers := RequestHeaders()
	out := make(map[string]any, len(headers))
	for key, value := range headers {
		out[key] = value
	}
	return out
}

// RequestDeviceCode starts GitHub's OAuth device authorization flow.
func (a *Auth) RequestDeviceCode(ctx context.Context) (*DeviceCodeResponse, error) {
	form := url.Values{}
	form.Set("client_id", GitHubClientID)
	form.Set("scope", defaultScope)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("copilot: create device code request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", RequestHeaders()["User-Agent"])

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: device code request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("copilot device code: close body error: %v", errClose)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read device code response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: device code request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var out DeviceCodeResponse
	if err = json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("copilot: parse device code response: %w", err)
	}
	if out.VerificationURIComplete == "" {
		out.VerificationURIComplete = out.VerificationURI
	}
	return &out, nil
}

// WaitForAuthorization polls GitHub until the user approves the device code.
func (a *Auth) WaitForAuthorization(ctx context.Context, deviceCode *DeviceCodeResponse) (*AuthBundle, error) {
	githubToken, err := a.PollForGitHubToken(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	copilotToken, err := a.ExchangeForCopilotToken(ctx, githubToken.AccessToken)
	if err != nil {
		return nil, err
	}
	user, err := a.FetchUserInfo(ctx, githubToken.AccessToken)
	if err != nil {
		log.Warnf("copilot: failed to fetch GitHub user info: %v", err)
	}
	return &AuthBundle{
		GitHubToken:  githubToken,
		CopilotToken: copilotToken,
		User:         user,
	}, nil
}

// PollForGitHubToken exchanges the approved device code for a GitHub OAuth token.
func (a *Auth) PollForGitHubToken(ctx context.Context, deviceCode *DeviceCodeResponse) (*GitHubTokenData, error) {
	if deviceCode == nil {
		return nil, fmt.Errorf("copilot: device code is nil")
	}
	if strings.TrimSpace(deviceCode.DeviceCode) == "" {
		return nil, fmt.Errorf("copilot: device code is empty")
	}

	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < defaultPollInterval {
		interval = defaultPollInterval
	}
	deadline := time.Now().Add(maxPollDuration)
	if deviceCode.ExpiresIn > 0 {
		codeDeadline := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)
		if codeDeadline.Before(deadline) {
			deadline = codeDeadline
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("copilot: context cancelled: %w", ctx.Err())
		case <-time.After(interval):
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("copilot: device code expired")
		}

		token, pollErr, shouldContinue, slowDown := a.exchangeDeviceCode(ctx, deviceCode.DeviceCode)
		if token != nil {
			return token, nil
		}
		if slowDown {
			interval += 5 * time.Second
		}
		if shouldContinue {
			continue
		}
		return nil, pollErr
	}
}

func (a *Auth) exchangeDeviceCode(ctx context.Context, deviceCode string) (*GitHubTokenData, error, bool, bool) {
	form := url.Values{}
	form.Set("client_id", GitHubClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("copilot: create token request: %w", err), false, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", RequestHeaders()["User-Agent"])

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: token request failed: %w", err), false, false
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("copilot token exchange: close body error: %v", errClose)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read token response: %w", err), false, false
	}

	var tokenResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		TokenType        string `json:"token_type"`
		Scope            string `json:"scope"`
		ExpiresIn        int    `json:"expires_in"`
	}
	if err = json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("copilot: parse token response: %w", err), false, false
	}

	if tokenResp.Error != "" {
		switch tokenResp.Error {
		case "authorization_pending":
			return nil, nil, true, false
		case "slow_down":
			return nil, nil, true, true
		case "expired_token":
			return nil, fmt.Errorf("copilot: device code expired"), false, false
		case "access_denied":
			return nil, fmt.Errorf("copilot: access denied by user"), false, false
		default:
			return nil, fmt.Errorf("copilot: OAuth error: %s - %s", tokenResp.Error, tokenResp.ErrorDescription), false, false
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: token request failed with status %d: %s", resp.StatusCode, string(body)), false, false
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, fmt.Errorf("copilot: empty access token in response"), false, false
	}
	return &GitHubTokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Scope:        tokenResp.Scope,
		ExpiresIn:    tokenResp.ExpiresIn,
	}, nil, false, false
}

// ExchangeForCopilotToken exchanges a GitHub OAuth token for the internal Copilot token.
func (a *Auth) ExchangeForCopilotToken(ctx context.Context, githubAccessToken string) (*CopilotTokenInfo, error) {
	githubAccessToken = strings.TrimSpace(githubAccessToken)
	if githubAccessToken == "" {
		return nil, fmt.Errorf("copilot: GitHub access token is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("copilot: create Copilot token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+githubAccessToken)
	for key, value := range RequestHeaders() {
		req.Header.Set(key, value)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: Copilot token request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("copilot token: close body error: %v", errClose)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read Copilot token response: %w", err)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("copilot: GitHub account does not have Copilot access")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: Copilot token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var info CopilotTokenInfo
	if err = json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("copilot: parse Copilot token response: %w", err)
	}
	if strings.TrimSpace(info.Token) == "" {
		return nil, fmt.Errorf("copilot: empty Copilot token in response")
	}
	return &info, nil
}

// RefreshCopilotToken refreshes the internal Copilot token using the stored GitHub token.
func (a *Auth) RefreshCopilotToken(ctx context.Context, githubAccessToken string) (*CopilotTokenInfo, error) {
	return a.ExchangeForCopilotToken(ctx, githubAccessToken)
}

// FetchUserInfo reads the GitHub profile for display and auth-file naming.
func (a *Auth) FetchUserInfo(ctx context.Context, githubAccessToken string) (*UserInfo, error) {
	githubAccessToken = strings.TrimSpace(githubAccessToken)
	if githubAccessToken == "" {
		return nil, fmt.Errorf("copilot: GitHub access token is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("copilot: create GitHub user request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+githubAccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", RequestHeaders()["User-Agent"])
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: GitHub user request failed: %w", err)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("copilot GitHub user: close body error: %v", errClose)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read GitHub user response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: GitHub user request failed with status %d: %s", resp.StatusCode, string(body))
	}
	var user UserInfo
	if err = json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("copilot: parse GitHub user response: %w", err)
	}
	return &user, nil
}

// APIEndpointFromToken extracts the proxy endpoint embedded in some Copilot tokens.
func APIEndpointFromToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return DefaultAPIEndpoint
	}
	const marker = "proxy-ep="
	idx := strings.Index(token, marker)
	if idx < 0 {
		return DefaultAPIEndpoint
	}
	raw := token[idx+len(marker):]
	if semi := strings.Index(raw, ";"); semi >= 0 {
		raw = raw[:semi]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultAPIEndpoint
	}
	decoded, err := url.QueryUnescape(raw)
	if err == nil && strings.TrimSpace(decoded) != "" {
		raw = decoded
	}
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if raw == "" {
		return DefaultAPIEndpoint
	}
	if strings.HasPrefix(raw, "api.") {
		return "https://" + strings.TrimRight(raw, "/")
	}
	return "https://api." + strings.TrimRight(raw, "/")
}
