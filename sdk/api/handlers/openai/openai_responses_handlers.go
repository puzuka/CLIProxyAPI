// Package openai provides HTTP handlers for OpenAIResponses API endpoints.
// This package implements the OpenAIResponses-compatible API interface, including model listing
// and chat completion functionality. It supports both streaming and non-streaming responses,
// and manages a pool of clients to interact with backend services.
// The handlers translate OpenAIResponses API requests to the appropriate backend format and
// convert responses back to OpenAIResponses-compatible format.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/apikeypolicy"
	. "github.com/router-for-me/CLIProxyAPI/v7/internal/constant"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/guideline"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func writeResponsesSSEChunk(w io.Writer, chunk []byte) {
	if w == nil || len(chunk) == 0 {
		return
	}
	if _, err := w.Write(chunk); err != nil {
		return
	}
	if bytes.HasSuffix(chunk, []byte("\n\n")) || bytes.HasSuffix(chunk, []byte("\r\n\r\n")) {
		return
	}
	suffix := []byte("\n\n")
	if bytes.HasSuffix(chunk, []byte("\r\n")) {
		suffix = []byte("\r\n")
	} else if bytes.HasSuffix(chunk, []byte("\n")) {
		suffix = []byte("\n")
	}
	if _, err := w.Write(suffix); err != nil {
		return
	}
}

type responsesSSEFramer struct {
	pending              []byte
	outputItems          map[int][]byte
	outputOrder          []int
	unindexedOutputItems [][]byte
}

func (f *responsesSSEFramer) WriteChunk(w io.Writer, chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	if responsesSSENeedsLineBreak(f.pending, chunk) {
		f.pending = append(f.pending, '\n')
	}
	f.pending = append(f.pending, chunk...)
	for {
		frameLen := responsesSSEFrameLen(f.pending)
		if frameLen == 0 {
			break
		}
		f.writeFrame(w, f.pending[:frameLen])
		copy(f.pending, f.pending[frameLen:])
		f.pending = f.pending[:len(f.pending)-frameLen]
	}
	if len(bytes.TrimSpace(f.pending)) == 0 {
		f.pending = f.pending[:0]
		return
	}
	if len(f.pending) == 0 || !responsesSSECanEmitWithoutDelimiter(f.pending) {
		return
	}
	f.writeFrame(w, f.pending)
	f.pending = f.pending[:0]
}

func (f *responsesSSEFramer) Flush(w io.Writer) {
	if len(f.pending) == 0 {
		return
	}
	if len(bytes.TrimSpace(f.pending)) == 0 {
		f.pending = f.pending[:0]
		return
	}
	if !responsesSSECanEmitWithoutDelimiter(f.pending) {
		f.pending = f.pending[:0]
		return
	}
	f.writeFrame(w, f.pending)
	f.pending = f.pending[:0]
}

func (f *responsesSSEFramer) writeFrame(w io.Writer, frame []byte) {
	writeResponsesSSEChunk(w, f.repairFrame(frame))
}

func (f *responsesSSEFramer) repairFrame(frame []byte) []byte {
	payload, ok := responsesSSEDataPayload(frame)
	if !ok || len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) || !json.Valid(payload) {
		return frame
	}

	switch gjson.GetBytes(payload, "type").String() {
	case "response.output_item.done":
		f.recordOutputItem(payload)
	case "response.completed":
		repaired := f.repairCompletedPayload(payload)
		if !bytes.Equal(repaired, payload) {
			return responsesSSEFrameWithData(frame, repaired)
		}
	}
	return frame
}

func responsesSSEDataPayload(frame []byte) ([]byte, bool) {
	var payload []byte
	found := false
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		trimmed := bytes.TrimSpace(line)
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(trimmed[len("data:"):])
		if found {
			payload = append(payload, '\n')
		}
		payload = append(payload, data...)
		found = true
	}
	return payload, found
}

func responsesSSEFrameWithData(frame, payload []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimRight(line, "\r")
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	for _, line := range bytes.Split(payload, []byte("\n")) {
		out.WriteString("data: ")
		out.Write(line)
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func (f *responsesSSEFramer) recordOutputItem(payload []byte) {
	item := gjson.GetBytes(payload, "item")
	if !item.Exists() || !item.IsObject() || item.Get("type").String() == "" {
		return
	}

	if outputIndex := gjson.GetBytes(payload, "output_index"); outputIndex.Exists() {
		index := int(outputIndex.Int())
		if f.outputItems == nil {
			f.outputItems = make(map[int][]byte)
		}
		if _, exists := f.outputItems[index]; !exists {
			f.outputOrder = append(f.outputOrder, index)
		}
		f.outputItems[index] = append([]byte(nil), item.Raw...)
		return
	}

	f.unindexedOutputItems = append(f.unindexedOutputItems, append([]byte(nil), item.Raw...))
}

func (f *responsesSSEFramer) repairCompletedPayload(payload []byte) []byte {
	if len(f.outputOrder) == 0 && len(f.unindexedOutputItems) == 0 {
		return payload
	}
	output := gjson.GetBytes(payload, "response.output")
	if output.Exists() && (!output.IsArray() || len(output.Array()) > 0) {
		return payload
	}

	var outputJSON bytes.Buffer
	outputJSON.WriteByte('[')
	indexes := append([]int(nil), f.outputOrder...)
	sort.Ints(indexes)
	written := 0
	for _, index := range indexes {
		item, ok := f.outputItems[index]
		if !ok {
			continue
		}
		if written > 0 {
			outputJSON.WriteByte(',')
		}
		outputJSON.Write(item)
		written++
	}
	for _, item := range f.unindexedOutputItems {
		if written > 0 {
			outputJSON.WriteByte(',')
		}
		outputJSON.Write(item)
		written++
	}
	outputJSON.WriteByte(']')

	repaired, err := sjson.SetRawBytes(payload, "response.output", outputJSON.Bytes())
	if err != nil {
		return payload
	}
	return repaired
}

func responsesSSEFrameLen(chunk []byte) int {
	if len(chunk) == 0 {
		return 0
	}
	lf := bytes.Index(chunk, []byte("\n\n"))
	crlf := bytes.Index(chunk, []byte("\r\n\r\n"))
	switch {
	case lf < 0:
		if crlf < 0 {
			return 0
		}
		return crlf + 4
	case crlf < 0:
		return lf + 2
	case lf < crlf:
		return lf + 2
	default:
		return crlf + 4
	}
}

func responsesSSENeedsMoreData(chunk []byte) bool {
	trimmed := bytes.TrimSpace(chunk)
	if len(trimmed) == 0 {
		return false
	}
	return responsesSSEHasField(trimmed, []byte("event:")) && !responsesSSEHasField(trimmed, []byte("data:"))
}

func responsesSSEHasField(chunk []byte, prefix []byte) bool {
	s := chunk
	for len(s) > 0 {
		line := s
		if i := bytes.IndexByte(s, '\n'); i >= 0 {
			line = s[:i]
			s = s[i+1:]
		} else {
			s = nil
		}
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func responsesSSECanEmitWithoutDelimiter(chunk []byte) bool {
	trimmed := bytes.TrimSpace(chunk)
	if len(trimmed) == 0 || responsesSSENeedsMoreData(trimmed) || !responsesSSEHasField(trimmed, []byte("data:")) {
		return false
	}
	return responsesSSEDataLinesValid(trimmed)
}

func responsesSSEDataLinesValid(chunk []byte) bool {
	s := chunk
	for len(s) > 0 {
		line := s
		if i := bytes.IndexByte(s, '\n'); i >= 0 {
			line = s[:i]
			s = s[i+1:]
		} else {
			s = nil
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[len("data:"):])
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		if !json.Valid(data) {
			return false
		}
	}
	return true
}

func responsesSSENeedsLineBreak(pending, chunk []byte) bool {
	if len(pending) == 0 || len(chunk) == 0 {
		return false
	}
	if bytes.HasSuffix(pending, []byte("\n")) || bytes.HasSuffix(pending, []byte("\r")) {
		return false
	}
	if chunk[0] == '\n' || chunk[0] == '\r' {
		return false
	}
	trimmed := bytes.TrimLeft(chunk, " \t")
	if len(trimmed) == 0 {
		return false
	}
	for _, prefix := range [][]byte{[]byte("data:"), []byte("event:"), []byte("id:"), []byte("retry:"), []byte(":")} {
		if bytes.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

// OpenAIResponsesAPIHandler contains the handlers for OpenAIResponses API endpoints.
// It holds a pool of clients to interact with the backend service.
type OpenAIResponsesAPIHandler struct {
	*handlers.BaseAPIHandler
}

// NewOpenAIResponsesAPIHandler creates a new OpenAIResponses API handlers instance.
// It takes an BaseAPIHandler instance as input and returns an OpenAIResponsesAPIHandler.
//
// Parameters:
//   - apiHandlers: The base API handlers instance
//
// Returns:
//   - *OpenAIResponsesAPIHandler: A new OpenAIResponses API handlers instance
func NewOpenAIResponsesAPIHandler(apiHandlers *handlers.BaseAPIHandler) *OpenAIResponsesAPIHandler {
	return &OpenAIResponsesAPIHandler{
		BaseAPIHandler: apiHandlers,
	}
}

// HandlerType returns the identifier for this handler implementation.
func (h *OpenAIResponsesAPIHandler) HandlerType() string {
	return OpenaiResponse
}

// Models returns the OpenAIResponses-compatible model metadata supported by this handler.
func (h *OpenAIResponsesAPIHandler) Models() []map[string]any {
	// Get dynamic models from the global registry
	modelRegistry := registry.GetGlobalRegistry()
	return modelRegistry.GetAvailableModels("openai")
}

// OpenAIResponsesModels handles the /v1/models endpoint.
// It returns a list of available AI models with their capabilities
// and specifications in OpenAIResponses-compatible format.
func (h *OpenAIResponsesAPIHandler) OpenAIResponsesModels(c *gin.Context) {
	apiKey := c.GetString("userApiKey")
	models := apikeypolicy.FilterAllowedModels(h.Cfg, apiKey, h.Models())
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

// Responses handles the /v1/responses endpoint.
// It determines whether the request is for a streaming or non-streaming response
// and calls the appropriate handler based on the model provider.
//
// Parameters:
//   - c: The Gin context containing the HTTP request and response
func (h *OpenAIResponsesAPIHandler) Responses(c *gin.Context) {
	rawJSON, err := handlers.ReadRequestBody(c)
	// If data retrieval fails, return a 400 Bad Request error.
	if err != nil {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: fmt.Sprintf("Invalid request: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	// Inject project-level guideline into the `instructions` field. Default
	// ON; operators may opt-out via guideline-injection.enabled: false.
	rawJSON = guideline.ApplyFromConfig(guideline.FormatOpenAIResponses, rawJSON, h.Cfg)

	// Check if the client requested a streaming response.
	streamResult := gjson.GetBytes(rawJSON, "stream")
	if streamResult.Type == gjson.True {
		h.handleStreamingResponse(c, rawJSON)
	} else {
		h.handleNonStreamingResponse(c, rawJSON)
	}

}

func (h *OpenAIResponsesAPIHandler) Compact(c *gin.Context) {
	rawJSON, err := handlers.ReadRequestBody(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: fmt.Sprintf("Invalid request: %v", err),
				Type:    "invalid_request_error",
			},
		})
		return
	}

	// Inject guideline into the compact request as well so the summarizer
	// model sees the same project-level instructions as a regular Responses
	// call. Default ON; opt out via guideline-injection.enabled: false.
	rawJSON = guideline.ApplyFromConfig(guideline.FormatOpenAIResponses, rawJSON, h.Cfg)

	streamResult := gjson.GetBytes(rawJSON, "stream")
	if streamResult.Type == gjson.True {
		c.JSON(http.StatusBadRequest, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: "Streaming not supported for compact responses",
				Type:    "invalid_request_error",
			},
		})
		return
	}
	if streamResult.Exists() {
		if updated, err := sjson.DeleteBytes(rawJSON, "stream"); err == nil {
			rawJSON = updated
		}
	}

	c.Header("Content-Type", "application/json")
	modelName := gjson.GetBytes(rawJSON, "model").String()
	// Enforce API-key policy (allowed-models, providers, quota, status) on the
	// original model the client requested *before* applyCompactModelFallback
	// rewrites it to a system-level fallback (e.g. gpt-5.5). Otherwise an
	// operator who restricts a key to one model family would silently lose
	// compact support, because the policy would be evaluated against the proxy's
	// internal fallback model rather than the model the client actually asked
	// for. executeWithAuthManager skips its duplicate check for alt =
	// "responses/compact" since this pre-check covers it.
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	if errMsg := h.CheckAPIKeyPolicy(cliCtx, h.HandlerType(), modelName); errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cliCancel(errMsg.Error)
		return
	}
	// Capture the original model and input for trigger-log before the
	// fallback rewrites them. Custom compact must also use the original
	// request payload if Codex compact fallback is unavailable or fails.
	originalModelForLog := modelName
	inputForLog := rawJSON

	runCustomCompact := func(reason string) bool {
		if !shouldApplyCustomCompact(h, originalModelForLog) {
			return false
		}
		customCompactModel := customCompactModelForRequest(h, originalModelForLog)
		if reason != "" {
			log.Warnf("%s; falling back to custom compact model %s", reason, customCompactModel)
		}
		customCompactStart := time.Now()
		stopKeepAlive := h.StartNonStreamingKeepAlive(c, cliCtx)
		compactResp, err := executeCustomCompact(h, cliCtx, originalModelForLog, inputForLog)
		stopKeepAlive()
		customCompactDuration := time.Since(customCompactStart)
		if err != nil {
			log.Warnf("custom compact failed: %v", err)
			c.JSON(http.StatusBadGateway, handlers.ErrorResponse{
				Error: handlers.ErrorDetail{
					Message: fmt.Sprintf("Custom compact failed: %v", err),
					Type:    "custom_compact_error",
				},
			})
			cliCancel(err.Error())
			return true
		}
		// Trigger-log: async write of custom compact input/output when enabled.
		asyncCustomCompactTriggerLog(h.Cfg, originalModelForLog, customCompactModel, inputForLog, compactResp, customCompactDuration)
		c.Header("Content-Type", "application/json")
		_, _ = c.Writer.Write(compactResp)
		cliCancel()
		return true
	}

	fallbackApplied := false
	if rewritten, newModel, applied := applyCompactModelFallback(h, modelName, rawJSON); applied {
		rawJSON = rewritten
		modelName = newModel
		fallbackApplied = applied
	}
	if !fallbackApplied {
		reason := ""
		if h.Cfg != nil && h.Cfg.CompactFallback.Enabled {
			reason = fmt.Sprintf("compact fallback unavailable for %s", originalModelForLog)
		}
		if runCustomCompact(reason) {
			return
		}
	}
	compactStart := time.Now()
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, cliCtx)
	resp, upstreamHeaders, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, rawJSON, "responses/compact")
	stopKeepAlive()
	compactDuration := time.Since(compactStart)
	if errMsg != nil {
		if fallbackApplied && runCustomCompact(fmt.Sprintf("compact fallback failed for %s -> %s: %s", originalModelForLog, modelName, errMsg.Error)) {
			return
		}
		h.WriteErrorResponse(c, errMsg)
		cliCancel(errMsg.Error)
		return
	}
	// Trigger-log: async write of compact input/output when enabled and
	// the compact-fallback path was used.
	if fallbackApplied {
		asyncCompactTriggerLog(h.Cfg, originalModelForLog, modelName, inputForLog, resp, compactDuration)
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	_, _ = c.Writer.Write(resp)
	cliCancel()
}

// applyCompactModelFallback rewrites the request payload model field when the
// configured CompactFallback is enabled and the requested model resolves to a
// provider that does not natively support /responses/compact (e.g. third-party
// openai-compatibility upstreams that only expose /chat/completions).
//
// The fallback is only applied when:
//   - h.Cfg.CompactFallback.Enabled is true
//   - h.Cfg.CompactFallback.Model is non-empty
//   - The requested model resolves to a provider that satisfies the whitelist
//     in AppliesToProviders. The whitelist matches by exact provider identifier;
//     the special token "*" or an empty list matches any non-codex provider so
//     custom OpenAI-compat names (e.g. "opencode-go", "9router") are covered
//     without forcing the operator to enumerate every entry.
//   - The fallback model itself resolves to a "codex" provider that is currently
//     registered in the global model registry (i.e. there is at least one Codex
//     auth that can serve it)
//
// When applied, the returned bool is true and the payload's model field is
// replaced in place with the configured fallback model. When skipped, the
// original payload is returned unchanged with applied=false. Errors during the
// rewrite are non-fatal: the original payload is preserved and a warning is
// logged so the caller still gets the upstream's native error rather than a
// proxy-side failure.
func applyCompactModelFallback(h *OpenAIResponsesAPIHandler, modelName string, rawJSON []byte) (rewritten []byte, newModel string, applied bool) {
	if h == nil || h.Cfg == nil {
		return rawJSON, modelName, false
	}
	cfg := h.Cfg.CompactFallback
	if !cfg.Enabled {
		return rawJSON, modelName, false
	}
	fallbackModel := cfg.Model
	if fallbackModel == "" || modelName == "" {
		return rawJSON, modelName, false
	}
	if fallbackModel == modelName {
		// Already using the fallback model; nothing to do.
		return rawJSON, modelName, false
	}

	originalProviders := util.GetProviderName(modelName)
	if len(originalProviders) == 0 {
		return rawJSON, modelName, false
	}

	// Skip if the original model is already served by a Codex provider — the
	// native compact path will work without rewriting.
	if util.InArray(originalProviders, "codex") {
		return rawJSON, modelName, false
	}

	// Decide whether the original providers match the configured whitelist.
	// Empty list or the wildcard "*" means "every non-codex provider", which
	// is the typical operator intent (compact should fall back for any
	// non-Codex upstream that doesn't expose /responses/compact natively).
	if !compactProvidersMatch(originalProviders, cfg.AppliesToProviders) {
		return rawJSON, modelName, false
	}

	// Confirm the fallback model is reachable through a Codex provider in the
	// running registry. If no Codex auth is loaded we deliberately leave the
	// payload untouched so the operator sees the original 404 and can fix
	// their auth setup, rather than masking the misconfiguration.
	fallbackProviders := util.GetProviderName(fallbackModel)
	if !util.InArray(fallbackProviders, "codex") {
		log.Warnf("compact fallback skipped: fallback model %q has no codex provider registered (resolved providers=%v)", fallbackModel, fallbackProviders)
		return rawJSON, modelName, false
	}

	updated, err := sjson.SetBytes(rawJSON, "model", fallbackModel)
	if err != nil {
		log.Warnf("compact fallback skipped: failed to rewrite model field: %v", err)
		return rawJSON, modelName, false
	}
	// Strip provider-specific reasoning items from the input array. The
	// originating provider (e.g. opencode.ai) signs/encodes its reasoning
	// blocks with its own keys; forwarding them to Codex causes a
	// thinking_signature_invalid 400 because chatgpt.com cannot verify
	// signatures it did not produce. Compact is a summarization endpoint —
	// dropping prior reasoning items only loses provider-private state, the
	// conversation messages remain intact for the model to summarize.
	if stripped, removed := stripReasoningItems(updated); removed > 0 {
		log.Infof("compact fallback: stripped %d reasoning item(s) before forwarding to %s", removed, fallbackModel)
		updated = stripped
	}
	log.Infof("compact fallback applied: %s -> %s (matched providers=%v)", modelName, fallbackModel, originalProviders)
	return updated, fallbackModel, true
}

// stripReasoningItems removes every element from the top-level "input" array
// whose "type" is "reasoning". It returns the modified payload and the count
// of removed items. When "input" is missing or not an array the original
// payload is returned unchanged with removed=0.
//
// The caller uses this to sanitize a payload that originated from a different
// provider before forwarding it to a Codex compact endpoint. Codex rejects
// reasoning blocks signed by other providers with a thinking_signature_invalid
// error, so they must be dropped rather than passed through.
func stripReasoningItems(rawJSON []byte) ([]byte, int) {
	input := gjson.GetBytes(rawJSON, "input")
	if !input.IsArray() {
		return rawJSON, 0
	}
	// Collect indices of reasoning items in reverse order so successive
	// deletions remain stable. sjson uses array indices in its path syntax.
	items := input.Array()
	indices := make([]int, 0, len(items))
	for i, item := range items {
		if item.Get("type").String() == "reasoning" {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return rawJSON, 0
	}
	out := rawJSON
	for i := len(indices) - 1; i >= 0; i-- {
		path := fmt.Sprintf("input.%d", indices[i])
		next, err := sjson.DeleteBytes(out, path)
		if err != nil {
			// Abort partial deletion: returning the partially-edited payload
			// would corrupt the array indices. Fall back to the unmodified
			// payload and let the caller log/handle the upstream error.
			return rawJSON, 0
		}
		out = next
	}
	return out, len(indices)
}

// compactProvidersMatch reports whether the given provider identifiers satisfy
// the configured AppliesToProviders whitelist.
//
// Semantics:
//   - Empty whitelist: match (default permissive — covers any non-codex provider,
//     since the codex skip happens earlier in the caller).
//   - Whitelist contains "*": match (explicit wildcard).
//   - Otherwise: case-sensitive exact-name intersection between providers and
//     the whitelist. This deliberately matches the canonical lowercase names
//     used elsewhere in the codebase (e.g. "openai-compatibility", "opencode-go").
func compactProvidersMatch(providers, whitelist []string) bool {
	if len(providers) == 0 {
		return false
	}
	if len(whitelist) == 0 {
		return true
	}
	for _, name := range whitelist {
		if name == "*" {
			return true
		}
	}
	return providerSetIntersects(providers, whitelist)
}

// providerSetIntersects reports whether any element of providers appears in
// the whitelist. Comparison is case-sensitive: provider identifiers in this
// codebase are canonical lowercase strings (e.g. "codex", "openai-compatibility").
func providerSetIntersects(providers, whitelist []string) bool {
	if len(providers) == 0 || len(whitelist) == 0 {
		return false
	}
	wl := make(map[string]struct{}, len(whitelist))
	for _, name := range whitelist {
		if name == "" {
			continue
		}
		wl[name] = struct{}{}
	}
	for _, name := range providers {
		if _, ok := wl[name]; ok {
			return true
		}
	}
	return false
}

// handleNonStreamingResponse handles non-streaming chat completion responses
// for Gemini models. It selects a client from the pool, sends the request, and
// aggregates the response before sending it back to the client in OpenAIResponses format.
//
// Parameters:
//   - c: The Gin context containing the HTTP request and response
//   - rawJSON: The raw JSON bytes of the OpenAIResponses-compatible request
func (h *OpenAIResponsesAPIHandler) handleNonStreamingResponse(c *gin.Context, rawJSON []byte) {
	c.Header("Content-Type", "application/json")

	modelName := gjson.GetBytes(rawJSON, "model").String()
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	stopKeepAlive := h.StartNonStreamingKeepAlive(c, cliCtx)

	resp, upstreamHeaders, errMsg := h.ExecuteWithAuthManager(cliCtx, h.HandlerType(), modelName, rawJSON, "")
	stopKeepAlive()
	if errMsg != nil {
		h.WriteErrorResponse(c, errMsg)
		cliCancel(errMsg.Error)
		return
	}
	handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	_, _ = c.Writer.Write(resp)
	cliCancel()
}

// handleStreamingResponse handles streaming responses for Gemini models.
// It establishes a streaming connection with the backend service and forwards
// the response chunks to the client in real-time using Server-Sent Events.
//
// Parameters:
//   - c: The Gin context containing the HTTP request and response
//   - rawJSON: The raw JSON bytes of the OpenAIResponses-compatible request
func (h *OpenAIResponsesAPIHandler) handleStreamingResponse(c *gin.Context, rawJSON []byte) {
	// Get the http.Flusher interface to manually flush the response.
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, handlers.ErrorResponse{
			Error: handlers.ErrorDetail{
				Message: "Streaming not supported",
				Type:    "server_error",
			},
		})
		return
	}

	// New core execution path
	modelName := gjson.GetBytes(rawJSON, "model").String()
	cliCtx, cliCancel := h.GetContextWithCancel(h, c, context.Background())
	dataChan, upstreamHeaders, errChan := h.ExecuteStreamWithAuthManager(cliCtx, h.HandlerType(), modelName, rawJSON, "")

	framer := &responsesSSEFramer{}
	writeHeaders := func() {
		handlers.SetSSEHeaders(c)
		handlers.WriteUpstreamHeaders(c.Writer.Header(), upstreamHeaders)
	}
	bootstrap := h.NewStreamBootstrapKeepAlive(c, flusher, writeHeaders, nil)
	defer bootstrap.Stop()

	// Peek at the first chunk
	for {
		select {
		case <-c.Request.Context().Done():
			cliCancel(c.Request.Context().Err())
			return
		case errMsg, ok := <-errChan:
			if !ok {
				// Err channel closed cleanly; wait for data channel.
				errChan = nil
				continue
			}
			// Upstream failed immediately. Return JSON unless keep-alive already committed SSE.
			if bootstrap.Committed() {
				writeResponsesStreamError(c, framer, errMsg)
				flusher.Flush()
			} else {
				h.WriteErrorResponse(c, errMsg)
			}
			if errMsg != nil {
				cliCancel(errMsg.Error)
			} else {
				cliCancel(nil)
			}
			return
		case chunk, ok := <-dataChan:
			if !ok {
				// Stream closed without data? Send headers and done.
				bootstrap.Commit()
				_, _ = c.Writer.Write([]byte("\n"))
				flusher.Flush()
				cliCancel(nil)
				return
			}

			// Success! Set headers.
			bootstrap.Commit()

			// Write first chunk logic (matching forwardResponsesStream)
			framer.WriteChunk(c.Writer, chunk)
			flusher.Flush()
			bootstrap.Stop()

			// Continue
			h.forwardResponsesStream(c, flusher, func(err error) { cliCancel(err) }, dataChan, errChan, framer)
			return
		case <-bootstrap.C():
			bootstrap.WriteKeepAlive()
		}
	}
}

func (h *OpenAIResponsesAPIHandler) forwardResponsesStream(c *gin.Context, flusher http.Flusher, cancel func(error), data <-chan []byte, errs <-chan *interfaces.ErrorMessage, framer *responsesSSEFramer) {
	if framer == nil {
		framer = &responsesSSEFramer{}
	}
	h.ForwardStream(c, flusher, cancel, data, errs, handlers.StreamForwardOptions{
		WriteChunk: func(chunk []byte) {
			framer.WriteChunk(c.Writer, chunk)
		},
		WriteTerminalError: func(errMsg *interfaces.ErrorMessage) {
			writeResponsesStreamError(c, framer, errMsg)
		},
		WriteDone: func() {
			framer.Flush(c.Writer)
			_, _ = c.Writer.Write([]byte("\n"))
		},
	})
}

func writeResponsesStreamError(c *gin.Context, framer *responsesSSEFramer, errMsg *interfaces.ErrorMessage) {
	if framer != nil {
		framer.Flush(c.Writer)
	}
	if errMsg == nil {
		return
	}
	status := http.StatusInternalServerError
	if errMsg.StatusCode > 0 {
		status = errMsg.StatusCode
	}
	errText := http.StatusText(status)
	if errMsg.Error != nil && errMsg.Error.Error() != "" {
		errText = errMsg.Error.Error()
	}
	chunk := handlers.BuildOpenAIResponsesStreamErrorChunk(status, errText, 0)
	_, _ = fmt.Fprintf(c.Writer, "\nevent: error\ndata: %s\n\n", string(chunk))
}
