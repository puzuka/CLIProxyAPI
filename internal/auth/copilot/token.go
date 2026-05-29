package copilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
)

const (
	ProviderKey        = "copilot"
	DefaultAPIEndpoint = "https://api.individual.githubcopilot.com"
)

// DeviceCodeResponse is GitHub's OAuth 2.0 device authorization response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// GitHubTokenData is the OAuth token returned by GitHub after device approval.
type GitHubTokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
}

// UserInfo is the GitHub account profile associated with the OAuth token.
type UserInfo struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// CopilotTokenInfo is the internal Copilot token response used for model calls.
type CopilotTokenInfo struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	RefreshIn int64  `json:"refresh_in,omitempty"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints,omitempty"`
}

// APIEndpoint returns the OpenAI-compatible Copilot API endpoint.
func (i *CopilotTokenInfo) APIEndpoint() string {
	if i == nil {
		return DefaultAPIEndpoint
	}
	if endpoint := strings.TrimSpace(i.Endpoints.API); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	return APIEndpointFromToken(i.Token)
}

// AuthBundle combines the GitHub OAuth token, Copilot token, and user profile.
type AuthBundle struct {
	GitHubToken  *GitHubTokenData
	CopilotToken *CopilotTokenInfo
	User         *UserInfo
}

// TokenStorage stores GitHub Copilot credentials on disk.
type TokenStorage struct {
	AccessToken           string         `json:"access_token"`
	GitHubAccessToken     string         `json:"github_access_token"`
	GitHubRefreshToken    string         `json:"github_refresh_token,omitempty"`
	TokenType             string         `json:"token_type,omitempty"`
	Scope                 string         `json:"scope,omitempty"`
	Expired               string         `json:"expired,omitempty"`
	CopilotToken          string         `json:"copilot_token"`
	CopilotTokenExpiresAt int64          `json:"copilot_token_expires_at,omitempty"`
	CopilotTokenRefreshIn int64          `json:"copilot_token_refresh_in,omitempty"`
	CopilotAPIEndpoint    string         `json:"copilot_api_endpoint"`
	GitHubUserID          int64          `json:"github_user_id,omitempty"`
	GitHubLogin           string         `json:"github_login,omitempty"`
	GitHubName            string         `json:"github_name,omitempty"`
	Email                 string         `json:"email,omitempty"`
	Headers               map[string]any `json:"headers,omitempty"`
	Type                  string         `json:"type"`

	Metadata map[string]any `json:"-"`
}

func (ts *TokenStorage) SetMetadata(meta map[string]any) {
	ts.Metadata = meta
}

// NewTokenStorage creates a persistable Copilot token record from an auth bundle.
func NewTokenStorage(bundle *AuthBundle) *TokenStorage {
	storage := &TokenStorage{
		Type:               ProviderKey,
		CopilotAPIEndpoint: DefaultAPIEndpoint,
		Headers:            requestHeadersAsAny(),
	}
	if bundle == nil {
		return storage
	}
	if bundle.GitHubToken != nil {
		storage.GitHubAccessToken = strings.TrimSpace(bundle.GitHubToken.AccessToken)
		storage.GitHubRefreshToken = strings.TrimSpace(bundle.GitHubToken.RefreshToken)
		storage.TokenType = strings.TrimSpace(bundle.GitHubToken.TokenType)
		storage.Scope = strings.TrimSpace(bundle.GitHubToken.Scope)
	}
	if bundle.CopilotToken != nil {
		storage.AccessToken = strings.TrimSpace(bundle.CopilotToken.Token)
		storage.CopilotToken = storage.AccessToken
		storage.CopilotTokenExpiresAt = bundle.CopilotToken.ExpiresAt
		storage.CopilotTokenRefreshIn = bundle.CopilotToken.RefreshIn
		storage.CopilotAPIEndpoint = bundle.CopilotToken.APIEndpoint()
		if bundle.CopilotToken.ExpiresAt > 0 {
			storage.Expired = time.Unix(bundle.CopilotToken.ExpiresAt, 0).UTC().Format(time.RFC3339)
		}
	}
	if bundle.User != nil {
		storage.GitHubUserID = bundle.User.ID
		storage.GitHubLogin = strings.TrimSpace(bundle.User.Login)
		storage.GitHubName = strings.TrimSpace(bundle.User.Name)
		storage.Email = strings.TrimSpace(bundle.User.Email)
	}
	return storage
}

// SaveTokenToFile writes the Copilot auth file.
func (ts *TokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = ProviderKey
	if ts.CopilotAPIEndpoint == "" {
		ts.CopilotAPIEndpoint = DefaultAPIEndpoint
	}
	if len(ts.Headers) == 0 {
		ts.Headers = requestHeadersAsAny()
	}

	if err := os.MkdirAll(filepath.Dir(authFilePath), 0o700); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	f, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("failed to create token file: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := misc.MergeMetadata(ts, ts.Metadata)
	if err != nil {
		return fmt.Errorf("failed to merge metadata: %w", err)
	}

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to write token to file: %w", err)
	}
	return nil
}

// CredentialFileName returns the filename for a GitHub Copilot auth record.
func CredentialFileName(login string, userID int64) string {
	login = sanitizeFileSegment(login)
	if login != "" {
		return fmt.Sprintf("copilot-%s.json", login)
	}
	if userID > 0 {
		return fmt.Sprintf("copilot-%d.json", userID)
	}
	return fmt.Sprintf("copilot-%d.json", time.Now().UnixMilli())
}

func sanitizeFileSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '@' || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
