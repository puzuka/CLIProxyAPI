package guideline

import (
	log "github.com/sirupsen/logrus"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// ApplyFromConfig is a convenience wrapper that resolves the effective
// content/position/enabled values from a config.SDKConfig and runs Inject.
// Handlers can call this directly:
//
//	rawJSON = guideline.ApplyFromConfig(guideline.FormatOpenAIResponses, rawJSON, h.Cfg)
//
// When cfg is nil, when injection is explicitly disabled, or when the
// resolved content is empty, the body is returned unchanged.
func ApplyFromConfig(format string, body []byte, cfg *config.SDKConfig) []byte {
	if cfg == nil {
		out := Inject(format, body, DefaultAgentHarnessKitGuideline, PositionPrepend)
		log.Debugf("guideline-injected format=%s in_len=%d out_len=%d (nil config)", format, len(body), len(out))
		return out
	}
	gi := cfg.GuidelineInjection
	if !gi.IsEnabled() {
		log.Debugf("guideline-skipped format=%s reason=disabled", format)
		return body
	}
	content := gi.Content
	if content == "" {
		content = DefaultAgentHarnessKitGuideline
	}
	out := Inject(format, body, content, gi.EffectivePosition())
	log.Debugf("guideline-injected format=%s in_len=%d out_len=%d position=%s", format, len(body), len(out), gi.EffectivePosition())
	return out
}
