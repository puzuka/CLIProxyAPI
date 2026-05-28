package management

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	kiroauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kiro"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// kiroDevicePollInterval is the default interval between token-poll attempts when
// the upstream does not return one. Mirrors the CLI flow.
const kiroDevicePollInterval = 5 * time.Second

// RequestKiroToken kicks off an AWS Builder ID device-code OAuth flow for the Kiro
// (Amazon Q Developer / CodeWhisperer) provider and returns the verification URL
// plus the user code immediately so the management UI can render them.
//
// A background goroutine polls SSO OIDC CreateToken until the user completes the
// browser challenge, then persists the resulting credentials as a coreauth.Auth
// record. The frontend tracks completion via GetAuthStatus(state).
//
// This handler exposes only the Builder ID device-code path. Other Kiro flows
// (Google OAuth via kiro:// protocol, IAM Identity Center SSO, importing an
// existing Kiro IDE / kiro-cli cache) require either an OS-level URL handler or
// a Start URL the user must already know — they are available via the
// command-line subcommands (-kiro-login, -kiro-idc-login, -kiro-import).
func (h *Handler) RequestKiroToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	log.Info("Initializing Kiro authentication (AWS Builder ID device-code flow)…")

	state := fmt.Sprintf("kiro-%d", time.Now().UnixNano())
	client := kiroauth.NewSSOOIDCClient(h.cfg)

	// Step 1: Register a fresh OIDC client. The client_id/client_secret pair must be
	// kept around alongside the access/refresh tokens so the watcher can refresh
	// later without re-running the OAuth dance.
	regResp, err := client.RegisterClient(ctx)
	if err != nil {
		log.Errorf("kiro: RegisterClient failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register Kiro OIDC client"})
		return
	}

	// Step 2: Start device authorization. The verification URL we return to the
	// caller already embeds the user_code as a query parameter (verification_uri_complete).
	authResp, err := client.StartDeviceAuthorization(ctx, regResp.ClientID, regResp.ClientSecret)
	if err != nil {
		log.Errorf("kiro: StartDeviceAuthorization failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start Kiro device authorization"})
		return
	}

	authURL := strings.TrimSpace(authResp.VerificationURIComplete)
	if authURL == "" {
		authURL = authResp.VerificationURI
	}
	userCode := authResp.UserCode

	RegisterOAuthSession(state, "kiro")

	// Step 3: Background poller — wait until the user approves on the browser
	// and CreateToken returns successfully.
	go func() {
		interval := kiroDevicePollInterval
		if authResp.Interval > 0 {
			interval = time.Duration(authResp.Interval) * time.Second
		}
		expiresAt := time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)
		if authResp.ExpiresIn <= 0 {
			// Fall back to a sane upper bound rather than spinning forever.
			expiresAt = time.Now().Add(15 * time.Minute)
		}

		var tokenResp *kiroauth.CreateTokenResponse
		for time.Now().Before(expiresAt) {
			time.Sleep(interval)

			resp, errToken := client.CreateToken(ctx, regResp.ClientID, regResp.ClientSecret, authResp.DeviceCode)
			if errToken == nil {
				tokenResp = resp
				break
			}

			if errors.Is(errToken, kiroauth.ErrAuthorizationPending) {
				continue
			}
			if errors.Is(errToken, kiroauth.ErrSlowDown) {
				interval += 5 * time.Second
				continue
			}

			log.Errorf("kiro: device-code token poll failed: %v", errToken)
			SetOAuthSessionError(state, "Authentication failed")
			return
		}

		if tokenResp == nil {
			log.Warn("kiro: device-code authorization timed out before user completion")
			SetOAuthSessionError(state, "Authorization timed out before completion")
			return
		}

		// Best-effort enrichment — same calls used by the CLI flow.
		email := kiroauth.FetchUserEmailWithFallback(ctx, h.cfg, tokenResp.AccessToken, regResp.ClientID, tokenResp.RefreshToken)

		expiresAtRFC := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)

		storage := &kiroauth.KiroTokenStorage{
			Type:         "kiro",
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ExpiresAt:    expiresAtRFC,
			AuthMethod:   "builder-id",
			Provider:     "AWS",
			LastRefresh:  time.Now().Format(time.RFC3339),
			ClientID:     regResp.ClientID,
			ClientSecret: regResp.ClientSecret,
			Region:       "us-east-1",
			Email:        email,
		}

		metadata := map[string]any{
			"type":          "kiro",
			"auth_method":   storage.AuthMethod,
			"provider":      storage.Provider,
			"client_id":     storage.ClientID,
			"client_secret": storage.ClientSecret,
			"region":        storage.Region,
			"timestamp":     time.Now().UnixMilli(),
			"expired":       expiresAtRFC,
		}
		if email != "" {
			metadata["email"] = email
		}

		fileName := fmt.Sprintf("kiro-%d.json", time.Now().UnixMilli())
		label := "Kiro User"
		if email != "" {
			label = email
		}

		record := &coreauth.Auth{
			ID:       fileName,
			Provider: "kiro",
			FileName: fileName,
			Label:    label,
			Storage:  storage,
			Metadata: metadata,
		}

		savedPath, errSave := h.saveTokenRecord(ctx, record)
		if errSave != nil {
			log.Errorf("kiro: failed to save authentication tokens: %v", errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		log.Infof("kiro: authentication successful, token saved to %s", savedPath)
		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("kiro")
	}()

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"url":       authURL,
		"state":     state,
		"user_code": userCode,
	})
}
