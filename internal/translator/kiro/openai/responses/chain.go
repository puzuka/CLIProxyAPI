// Package responses bridges OpenAI Responses API requests to Kiro by chaining
// the existing OpenaiResponse → OpenAI Chat Completions converter with the
// existing OpenAI Chat Completions → Kiro converter. The reverse direction
// (Kiro stream → OpenAI Responses events) is composed the same way.
//
// This avoids duplicating ~1200 lines of OpenAI Responses parsing/serialising
// logic that already lives under internal/translator/openai/openai/responses
// for every new provider, at the cost of a small extra pass per chunk.
package responses

import (
	"context"
	"encoding/json"
	"strings"

	kiroopenai "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/kiro/openai"
	openairesponses "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/responses"
	"github.com/tidwall/gjson"
)

// chainStreamState keeps the per-stage stream state that each chained
// converter expects in its `param *any` slot. The Kiro→OpenAI converter
// stashes a *openai.OpenAIStreamState; the OpenAI→OpenaiResponse converter
// stashes a *responses.oaiToResponsesState. Sharing one cell would cause
// "interface conversion" panics on the second hop.
type chainStreamState struct {
	kiroParam     any
	responseParam any
}

// ConvertOpenAIResponsesRequestToKiro is intentionally a no-op pass-through.
//
// The other Kiro translators (kiro/openai.ConvertOpenAIRequestToKiro,
// kiro/claude.ConvertClaudeRequestToKiro) already follow this pattern: the
// request side just returns the inbound body unchanged, and the actual
// conversion to Kiro shape happens later in the executor via
// kiro/openai.BuildKiroPayloadFromOpenAI / kiro/claude.BuildKiroPayload,
// which need runtime values (origin, profileArn, agentic flags, etc.) the
// translator pipeline does not have access to.
//
// Returning a transformed body here caused a double-convert in
// internal/runtime/executor/kiro_executor.go's openai-response branch:
// the translator turned Responses → ChatCompletions, then the executor
// turned the ChatCompletions output as if it were still Responses, which
// produced an empty messages array and dropped every user/assistant turn.
// See the proxy-side dump in TEMP-DEBUG-KIRO-PAYLOAD where conversationId
// was stable but history was 0 and currentMessage carried only the system
// prompt header.
func ConvertOpenAIResponsesRequestToKiro(modelName string, rawJSON []byte, stream bool) []byte {
	return rawJSON
}

// streamState resolves (or initialises) the chained stream-state envelope
// stored in `param`. The pipeline guarantees the same `param` pointer is
// reused for every chunk in a single response, so we can keep both inner
// stage states alive between calls without leaking across responses.
func streamState(param *any) *chainStreamState {
	if param == nil {
		// Defensive: pipeline always supplies a non-nil pointer in practice,
		// but if it doesn't we fall back to a fresh state for this chunk.
		var local any
		param = &local
	}
	if existing, ok := (*param).(*chainStreamState); ok && existing != nil {
		return existing
	}
	state := &chainStreamState{}
	*param = state
	return state
}

func sanitizeOriginalResponsesRequestForKiro(rawJSON []byte) []byte {
	var root map[string]any
	if err := json.Unmarshal(rawJSON, &root); err != nil {
		return rawJSON
	}

	instructions, ok := root["instructions"].(string)
	if !ok {
		return rawJSON
	}

	root["instructions"] = sanitizeKiroResponseMetadataText(instructions)
	out, err := json.Marshal(root)
	if err != nil {
		return rawJSON
	}
	return out
}

func sanitizeKiroResponseMetadataText(text string) string {
	replacer := strings.NewReplacer(
		"prompt-injection", "instruction-conflict",
		"prompt injection", "instruction conflict",
		"Prompt-injection", "Instruction-conflict",
		"Prompt injection", "Instruction conflict",
		"Kiro", "upstream provider",
		"Amazon Q", "upstream provider",
		"CodeWhisperer", "upstream provider",
		"AWS", "upstream provider",
	)
	return replacer.Replace(text)
}

// ConvertKiroStreamToOpenAIResponses converts a single Kiro stream chunk into
// zero or more OpenAI Responses SSE events.
//
// Each chained stage owns its own state cell: kiroParam carries the
// kiro-openai chunk-builder bookkeeping, responseParam carries the openai-
// responses framer state (sequence_number, response.id, output item
// bookkeeping, etc.). Mixing these cells caused panics on the very first
// chunk that triggered the second hop — see the explicit type-assertion
// failure surfaced as "interface conversion: interface {} is
// *openai.OpenAIStreamState, not *responses.oaiToResponsesState".
//
// The kiro-openai converter intentionally never emits a `[DONE]` marker
// (the /v1/chat/completions handler appends one when the upstream channel
// closes). The openai-responses converter, however, only emits the terminal
// `response.completed` event when it sees `[DONE]`. Without the marker,
// /v1/responses clients (Codex Desktop, Antigravity) keep retrying with
// "stream disconnected before completion: stream closed before
// response.completed". So when this hop sees a Chat Completions chunk that
// already carries a non-empty `finish_reason`, we synthesise the missing
// `[DONE]` and feed it through the response framer so it can finalise.
func ConvertKiroStreamToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	st := streamState(param)
	sanitizedOriginalRequestRawJSON := sanitizeOriginalResponsesRequestForKiro(originalRequestRawJSON)
	sanitizedRequestRawJSON := sanitizeOriginalResponsesRequestForKiro(requestRawJSON)

	chatChunks := kiroopenai.ConvertKiroStreamToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, &st.kiroParam)
	if len(chatChunks) == 0 {
		return nil
	}

	out := make([][]byte, 0, len(chatChunks)*2+1)
	sawFinish := false
	for _, chunk := range chatChunks {
		if len(chunk) == 0 {
			continue
		}
		events := openairesponses.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, sanitizedOriginalRequestRawJSON, sanitizedRequestRawJSON, chunk, &st.responseParam)
		out = append(out, events...)

		if !sawFinish && chunkHasFinishReason(chunk) {
			sawFinish = true
		}
	}

	if sawFinish {
		// Push the synthetic [DONE] through the framer so it can flush the
		// pending output_text.done / response.completed envelope. We do this
		// once per stream — the response stage is idempotent against repeated
		// [DONE] but we don't need to spam it.
		doneEvents := openairesponses.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, sanitizedOriginalRequestRawJSON, sanitizedRequestRawJSON, []byte("[DONE]"), &st.responseParam)
		out = append(out, doneEvents...)
	}

	return out
}

// chunkHasFinishReason returns true when a Chat Completions stream chunk
// carries a non-empty `choices[].finish_reason`. Only those chunks signal the
// model is done generating in the Kiro→OpenAI converter; intermediate delta
// chunks have an empty `finish_reason`.
func chunkHasFinishReason(chunk []byte) bool {
	// Strip optional `data:` SSE prefix and any whitespace before parsing.
	body := chunk
	if len(body) > 5 && body[0] == 'd' && body[1] == 'a' && body[2] == 't' && body[3] == 'a' && body[4] == ':' {
		body = body[5:]
		for len(body) > 0 && (body[0] == ' ' || body[0] == '\t') {
			body = body[1:]
		}
	}
	if len(body) == 0 {
		return false
	}

	results := gjson.GetManyBytes(body, "choices.0.finish_reason", "choices.1.finish_reason")
	for _, r := range results {
		if r.Exists() && r.Type == gjson.String && r.String() != "" {
			return true
		}
	}
	return false
}

// ConvertKiroNonStreamToOpenAIResponses chains the two non-stream converters.
// Kiro non-stream output is shaped as an OpenAI Chat Completions JSON, then
// rewrapped into the OpenAI Responses object the /v1/responses handler
// expects. Non-stream calls do not maintain incremental state, so we still
// pass the chained envelope to keep the contract symmetric — the inner
// converters simply ignore an unused state cell.
func ConvertKiroNonStreamToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	st := streamState(param)
	chat := kiroopenai.ConvertKiroNonStreamToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, &st.kiroParam)
	sanitizedOriginalRequestRawJSON := sanitizeOriginalResponsesRequestForKiro(originalRequestRawJSON)
	sanitizedRequestRawJSON := sanitizeOriginalResponsesRequestForKiro(requestRawJSON)
	return openairesponses.ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(ctx, modelName, sanitizedOriginalRequestRawJSON, sanitizedRequestRawJSON, chat, &st.responseParam)
}
