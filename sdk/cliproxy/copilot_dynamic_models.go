// Package cliproxy — GitHub Copilot dynamic model discovery.
//
// This mirrors the Kiro dynamic model flow (see kiro_dynamic_models.go): after
// the static Copilot catalog is registered, an asynchronous fetch hits the live
// {endpoint}/models API and re-registers the merged result so newly-released
// models surface in /v1/models without a static catalog update. The static
// catalog remains in place when the upstream call fails.
package cliproxy

import (
	"context"
	"strings"
	"time"

	copilotauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// copilotDynamicFetchTimeout caps each /models call to avoid stalling the
// background updater when the upstream is slow or unreachable.
const copilotDynamicFetchTimeout = 8 * time.Second

// refreshCopilotDynamicModels schedules an asynchronous fetch of the live
// Copilot model list for the given auth and re-registers the merged catalog.
// Callers must already have registered the static catalog so /v1/models is not
// empty if the upstream call fails.
func (s *Service) refreshCopilotDynamicModels(a *coreauth.Auth, excluded []string) {
	if a == nil || a.ID == "" || a.Metadata == nil {
		return
	}
	token := metaString(a.Metadata, "copilot_token")
	if token == "" {
		token = metaString(a.Metadata, "access_token")
	}
	if strings.TrimSpace(token) == "" {
		return
	}

	authID := a.ID
	provider := strings.ToLower(strings.TrimSpace(a.Provider))
	if provider == "" {
		provider = copilotauth.ProviderKey
	}
	endpoint := metaString(a.Metadata, "copilot_api_endpoint")
	proxyURL := a.ProxyURL
	excludedCopy := append([]string(nil), excluded...)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Warnf("copilot: dynamic model fetch panicked for %s: %v", authID, r)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), copilotDynamicFetchTimeout)
		defer cancel()

		s.cfgMu.RLock()
		cfg := s.cfg
		s.cfgMu.RUnlock()

		apiModels, err := copilotauth.NewCopilotAuthWithProxyURL(cfg, proxyURL).ListModels(ctx, endpoint, token)
		if err != nil {
			log.Debugf("copilot: ListModels failed for %s: %v", authID, err)
			return
		}
		converted := registry.ConvertCopilotAPIModels(toCopilotRegistryModels(apiModels))
		if len(converted) == 0 {
			return
		}
		merged := registry.MergeCopilotDynamicWithStaticMetadata(converted, registry.GetCopilotModels())
		merged = applyExcludedModels(merged, excludedCopy)
		if len(merged) == 0 {
			return
		}

		GlobalModelRegistry().RegisterClient(authID, provider, merged)
		log.Debugf("copilot: refreshed %d models from /models for %s", len(merged), authID)
	}()
}

// toCopilotRegistryModels maps the copilot auth package wire type into the
// registry-local struct so the registry stays decoupled from the auth package.
func toCopilotRegistryModels(models []*copilotauth.CopilotModel) []*registry.CopilotAPIModel {
	out := make([]*registry.CopilotAPIModel, 0, len(models))
	for _, m := range models {
		if m == nil {
			continue
		}
		out = append(out, &registry.CopilotAPIModel{
			ID:                 m.ID,
			Name:               m.Name,
			Type:               m.Capabilities.Type,
			SupportedEndpoints: append([]string(nil), m.SupportedEndpoints...),
			ContextWindow:      m.Capabilities.Limits.MaxContextWindowTokens,
			MaxOutput:          m.Capabilities.Limits.MaxOutputTokens,
			Vision:             m.Capabilities.Supports.Vision,
			ModelPickerEnabled: m.ModelPickerEnabled,
			PolicyState:        m.Policy.State,
		})
	}
	return out
}
