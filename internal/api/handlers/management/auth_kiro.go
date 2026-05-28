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

// kiroDefaultRegion is the fallback AWS region used when the caller does not
// supply one. AWS Builder ID is hosted in us-east-1 only; IAM Identity Center
// instances normally live in a region the operator must already know.
const kiroDefaultRegion = "us-east-1"

// kiroAuthMethodBuilderID is the AWS Builder ID device-code flow. This is the
// default and only requires a freshly registered OIDC client.
const kiroAuthMethodBuilderID = "builder-id"

// kiroAuthMethodIDC is the AWS IAM Identity Center (Enterprise SSO) flow. It
// additionally needs a Start URL plus the home region of the IDC instance.
const kiroAuthMethodIDC = "idc"

// RequestKiroToken kicks off a Kiro (Amazon Q Developer / CodeWhisperer) OAuth
// device-code flow over HTTP and returns the verification URL plus user code
// immediately so the management UI can render them. A background goroutine
// polls SSO OIDC CreateToken until the user completes the browser challenge,
// then persists the resulting credentials as a coreauth.Auth record.
//
// Query parameters:
//   - method:    "builder-id" (default) for AWS Builder ID, or "idc" for AWS
//                IAM Identity Center / Enterprise SSO. Anything else is treated
//                as builder-id.
//   - start_url: required when method=idc, e.g. https://my-org.awsapps.com/start.
//   - region:    required when method=idc, e.g. us-east-1. Optional for
//                builder-id (always us-east-1 in practice).
//
// The frontend tracks completion via GET /get-auth-status?state=...
//
// Other Kiro flows — Google OAuth via the kiro:// custom protocol, AWS
// authorization-code flow with localhost callback, and importing an existing
// Kiro IDE / kiro-cli local cache — still require either an OS-level URL
// handler or inputs we cannot prompt over a JSON API, and remain CLI-only.
func (h *Handler) RequestKiroToken(c *gin.Context) {
	ctx := context.Background()
	ctx = PopulateAuthContext(ctx, c)

	method := strings.ToLower(strings.TrimSpace(c.Query("method")))
	if method == "" {
		method = kiroAuthMethodBuilderID
	}

	startURL := strings.TrimSpace(c.Query("start_url"))
	region := strings.TrimSpace(c.Query("region"))
	if region == "" {
		region = kiroDefaultRegion
	}

	switch method {
	case kiroAuthMethodBuilderID:
		// no extra inputs required.
	case kiroAuthMethodIDC:
		if startURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "start_url is required for method=idc"})
			return
		}
		if region == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "region is required for method=idc"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("unknown method %q (expected 'builder-id' or 'idc')", method)})
		return
	}

	log.Infof("Initializing Kiro authentication (method=%s, region=%s, start_url=%q)", method, region, startURL)

	state := fmt.Sprintf("kiro-%d", time.Now().UnixNano())
	client := kiroauth.NewSSOOIDCClient(h.cfg)

	// Step 1: Register an OIDC client. For IDC we need to register against the
	// chosen region/start URL; for Builder ID a regular RegisterClient call
	// (always us-east-1) is enough.
	var (
		regResp *kiroauth.RegisterClientResponse
		err     error
	)
	if method == kiroAuthMethodIDC {
		regResp, err = client.RegisterClientWithRegion(ctx, region)
	} else {
		regResp, err = client.RegisterClient(ctx)
	}
	if err != nil {
		log.Errorf("kiro: RegisterClient (method=%s) failed: %v", method, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register Kiro OIDC client"})
		return
	}

	// Step 2: Start device authorization. For IDC we must pass the Start URL +
	// region so AWS issues a code scoped to the right SSO instance.
	var authResp *kiroauth.StartDeviceAuthResponse
	if method == kiroAuthMethodIDC {
		authResp, err = client.StartDeviceAuthorizationWithIDC(ctx, regResp.ClientID, regResp.ClientSecret, startURL, region)
	} else {
		authResp, err = client.StartDeviceAuthorization(ctx, regResp.ClientID, regResp.ClientSecret)
	}
	if err != nil {
		log.Errorf("kiro: StartDeviceAuthorization (method=%s) failed: %v", method, err)
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
			expiresAt = time.Now().Add(15 * time.Minute)
		}

		var tokenResp *kiroauth.CreateTokenResponse
		for time.Now().Before(expiresAt) {
			time.Sleep(interval)

			var (
				resp     *kiroauth.CreateTokenResponse
				errToken error
			)
			if method == kiroAuthMethodIDC {
				resp, errToken = client.CreateTokenWithRegion(ctx, regResp.ClientID, regResp.ClientSecret, authResp.DeviceCode, region)
			} else {
				resp, errToken = client.CreateToken(ctx, regResp.ClientID, regResp.ClientSecret, authResp.DeviceCode)
			}
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

			log.Errorf("kiro: device-code token poll (method=%s) failed: %v", method, errToken)
			SetOAuthSessionError(state, "Authentication failed")
			return
		}

		if tokenResp == nil {
			log.Warnf("kiro: device-code authorization (method=%s) timed out before user completion", method)
			SetOAuthSessionError(state, "Authorization timed out before completion")
			return
		}

		// Best-effort enrichment.
		email := kiroauth.FetchUserEmailWithFallback(ctx, h.cfg, tokenResp.AccessToken, regResp.ClientID, tokenResp.RefreshToken)

		// IDC accounts have a CodeWhisperer profile ARN that the executor needs;
		// Builder ID accounts do not. The fetcher is best-effort: when it returns
		// empty the token still works, the watcher just cannot pre-pin a profile.
		profileArn := ""
		if method == kiroAuthMethodIDC {
			profileArn = client.FetchProfileArn(ctx, tokenResp.AccessToken, regResp.ClientID, tokenResp.RefreshToken)
		}

		expiresAtRFC := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)

		storage := &kiroauth.KiroTokenStorage{
			Type:         "kiro",
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ProfileArn:   profileArn,
			ExpiresAt:    expiresAtRFC,
			AuthMethod:   method,
			Provider:     "AWS",
			LastRefresh:  time.Now().Format(time.RFC3339),
			ClientID:     regResp.ClientID,
			ClientSecret: regResp.ClientSecret,
			Region:       region,
			StartURL:     startURL,
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
		if startURL != "" {
			metadata["start_url"] = startURL
		}
		if profileArn != "" {
			metadata["profile_arn"] = profileArn
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
			log.Errorf("kiro: failed to save authentication tokens (method=%s): %v", method, errSave)
			SetOAuthSessionError(state, "Failed to save authentication tokens")
			return
		}

		log.Infof("kiro: authentication successful (method=%s), token saved to %s", method, savedPath)
		CompleteOAuthSession(state)
		CompleteOAuthSessionsByProvider("kiro")
	}()

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"url":       authURL,
		"state":     state,
		"user_code": userCode,
		"method":    method,
	})
}
