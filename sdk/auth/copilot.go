package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/browser"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

var copilotRefreshLead = 5 * time.Minute

// CopilotAuthenticator implements GitHub Copilot OAuth device flow login.
type CopilotAuthenticator struct{}

// NewCopilotAuthenticator constructs a Copilot authenticator.
func NewCopilotAuthenticator() Authenticator {
	return &CopilotAuthenticator{}
}

func (CopilotAuthenticator) Provider() string {
	return copilot.ProviderKey
}

func (CopilotAuthenticator) RefreshLead() *time.Duration {
	return &copilotRefreshLead
}

func (a CopilotAuthenticator) Login(ctx context.Context, cfg *config.Config, opts *LoginOptions) (*coreauth.Auth, error) {
	if cfg == nil {
		return nil, fmt.Errorf("cliproxy auth: configuration is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts == nil {
		opts = &LoginOptions{}
	}

	authSvc := copilot.NewCopilotAuth(cfg)
	fmt.Println("Starting GitHub Copilot authentication...")
	deviceCode, err := authSvc.RequestDeviceCode(ctx)
	if err != nil {
		return nil, err
	}

	verificationURL := strings.TrimSpace(deviceCode.VerificationURIComplete)
	if verificationURL == "" {
		verificationURL = strings.TrimSpace(deviceCode.VerificationURI)
	}
	fmt.Printf("\nTo authenticate, please visit:\n%s\n\n", verificationURL)
	if deviceCode.UserCode != "" {
		fmt.Printf("User code: %s\n\n", deviceCode.UserCode)
	}

	if !opts.NoBrowser && verificationURL != "" && browser.IsAvailable() {
		if errOpen := browser.OpenURL(verificationURL); errOpen != nil {
			log.Warnf("Failed to open browser automatically: %v", errOpen)
		} else {
			fmt.Println("Browser opened automatically.")
		}
	}

	fmt.Println("Waiting for GitHub authorization...")
	if deviceCode.ExpiresIn > 0 {
		fmt.Printf("(This will timeout in %d seconds if not authorized)\n", deviceCode.ExpiresIn)
	}

	bundle, err := authSvc.WaitForAuthorization(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	fmt.Println("\nGitHub Copilot authentication successful!")
	return BuildCopilotAuthRecord(bundle)
}

// BuildCopilotAuthRecord creates a runtime auth record from a Copilot auth bundle.
func BuildCopilotAuthRecord(bundle *copilot.AuthBundle) (*coreauth.Auth, error) {
	if bundle == nil || bundle.CopilotToken == nil || strings.TrimSpace(bundle.CopilotToken.Token) == "" {
		return nil, fmt.Errorf("copilot: missing Copilot token")
	}
	tokenStorage := copilot.NewTokenStorage(bundle)
	fileName := copilot.CredentialFileName(tokenStorage.GitHubLogin, tokenStorage.GitHubUserID)
	label := strings.TrimSpace(tokenStorage.GitHubLogin)
	if label == "" {
		label = strings.TrimSpace(tokenStorage.Email)
	}
	if label == "" {
		label = "GitHub Copilot"
	}

	metadata := map[string]any{
		"type":                     copilot.ProviderKey,
		"access_token":             tokenStorage.AccessToken,
		"copilot_token":            tokenStorage.CopilotToken,
		"copilot_api_endpoint":     tokenStorage.CopilotAPIEndpoint,
		"copilot_token_expires_at": tokenStorage.CopilotTokenExpiresAt,
		"copilot_token_refresh_in": tokenStorage.CopilotTokenRefreshIn,
		"github_access_token":      tokenStorage.GitHubAccessToken,
		"github_refresh_token":     tokenStorage.GitHubRefreshToken,
		"github_user_id":           tokenStorage.GitHubUserID,
		"github_login":             tokenStorage.GitHubLogin,
		"github_name":              tokenStorage.GitHubName,
		"email":                    tokenStorage.Email,
		"headers":                  tokenStorage.Headers,
		"timestamp":                time.Now().UnixMilli(),
	}
	if tokenStorage.TokenType != "" {
		metadata["token_type"] = tokenStorage.TokenType
	}
	if tokenStorage.Scope != "" {
		metadata["scope"] = tokenStorage.Scope
	}
	if tokenStorage.Expired != "" {
		metadata["expired"] = tokenStorage.Expired
	}

	attrs := map[string]string{
		"auth_kind": "oauth",
		"api_key":   tokenStorage.AccessToken,
		"base_url":  tokenStorage.CopilotAPIEndpoint,
	}
	applyCopilotHeadersToAttrs(attrs)

	return &coreauth.Auth{
		ID:         fileName,
		Provider:   copilot.ProviderKey,
		FileName:   fileName,
		Label:      label,
		Storage:    tokenStorage,
		Attributes: attrs,
		Metadata:   metadata,
	}, nil
}

func applyCopilotHeadersToAttrs(attrs map[string]string) {
	if attrs == nil {
		return
	}
	for key, value := range copilot.RequestHeaders() {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		attrs["header:"+key] = value
	}
}
