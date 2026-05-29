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
const copilotModelProbeTimeout = 20 * time.Second

var copilotModelProbeSlots = make(chan struct{}, 8)

// refreshCopilotDynamicModels schedules an asynchronous fetch of the live
// Copilot model list for the given auth and re-registers only models that pass
// a live endpoint-specific probe. Copilot /models can over-report account access,
// so probe success is the source of truth for auth-scoped callable models.
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

		authSvc := copilotauth.NewCopilotAuthWithProxyURL(cfg, proxyURL)
		apiModels, err := authSvc.ListModels(ctx, endpoint, token)
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
		verified := verifyCopilotCallableModels(authSvc, authID, endpoint, token, merged)

		GlobalModelRegistry().RegisterClient(authID, provider, verified)
		if s.coreManager != nil {
			s.coreManager.ReconcileRegistryModelStates(context.Background(), authID)
			s.coreManager.RefreshSchedulerEntry(authID)
		}
		log.Debugf("copilot: refreshed %d/%d callable models from /models for %s", len(verified), len(merged), authID)
	}()
}

func verifyCopilotCallableModels(authSvc *copilotauth.Auth, authID, endpoint, token string, models []*registry.ModelInfo) []*registry.ModelInfo {
	if authSvc == nil || len(models) == 0 {
		return nil
	}
	verified := make([]*registry.ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil || strings.TrimSpace(model.ID) == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), copilotModelProbeTimeout)
		if !acquireCopilotModelProbeSlot(ctx) {
			cancel()
			log.Debugf("copilot: skipped model probe for %s/%s: %v", authID, model.ID, ctx.Err())
			continue
		}
		result, err := probeCopilotModelForSupportedEndpoint(authSvc, ctx, endpoint, token, model)
		releaseCopilotModelProbeSlot()
		cancel()
		if err != nil {
			log.Debugf("copilot: model probe failed for %s/%s: %v", authID, model.ID, err)
			continue
		}
		if result != nil && result.Callable {
			verified = append(verified, model)
			continue
		}
		if result == nil {
			log.Debugf("copilot: model probe returned no result for %s/%s", authID, model.ID)
			continue
		}
		if result.ModelNotSupported {
			log.Debugf("copilot: skipping unsupported model %s for %s after live probe: status=%d code=%s message=%s", model.ID, authID, result.StatusCode, result.ErrorCode, result.ErrorMessage)
			continue
		}
		log.Debugf("copilot: skipping non-callable model %s for %s after live probe: status=%d code=%s message=%s", model.ID, authID, result.StatusCode, result.ErrorCode, result.ErrorMessage)
	}
	return verified
}

func probeCopilotModelForSupportedEndpoint(authSvc *copilotauth.Auth, ctx context.Context, endpoint, token string, model *registry.ModelInfo) (*copilotauth.ModelProbeResult, error) {
	if authSvc == nil || model == nil {
		return nil, nil
	}
	if len(model.SupportedEndpoints) == 0 || registry.CopilotSupportsChatCompletions(model.SupportedEndpoints) {
		return authSvc.ProbeChatCompletionModel(ctx, endpoint, token, model.ID)
	}
	if registry.CopilotSupportsResponses(model.SupportedEndpoints) {
		return authSvc.ProbeResponsesModel(ctx, endpoint, token, model.ID)
	}
	return authSvc.ProbeChatCompletionModel(ctx, endpoint, token, model.ID)
}

func acquireCopilotModelProbeSlot(ctx context.Context) bool {
	select {
	case copilotModelProbeSlots <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func releaseCopilotModelProbeSlot() {
	select {
	case <-copilotModelProbeSlots:
	default:
	}
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
