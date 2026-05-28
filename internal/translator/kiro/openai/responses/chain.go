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

	kiroopenai "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/kiro/openai"
	openairesponses "github.com/router-for-me/CLIProxyAPI/v7/internal/translator/openai/openai/responses"
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

// ConvertOpenAIResponsesRequestToKiro converts an /v1/responses request body
// into a Kiro request body. The /v1/responses → /v1/chat/completions step is
// delegated to the canonical openai-responses converter so any future fix
// there is automatically inherited; the second step is the same converter the
// /v1/chat/completions handler already uses for Kiro.
func ConvertOpenAIResponsesRequestToKiro(modelName string, rawJSON []byte, stream bool) []byte {
	chat := openairesponses.ConvertOpenAIResponsesRequestToOpenAIChatCompletions(modelName, rawJSON, stream)
	return kiroopenai.ConvertOpenAIRequestToKiro(modelName, chat, stream)
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
func ConvertKiroStreamToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	st := streamState(param)

	chatChunks := kiroopenai.ConvertKiroStreamToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, &st.kiroParam)
	if len(chatChunks) == 0 {
		return nil
	}

	out := make([][]byte, 0, len(chatChunks)*2)
	for _, chunk := range chatChunks {
		if len(chunk) == 0 {
			continue
		}
		events := openairesponses.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, chunk, &st.responseParam)
		out = append(out, events...)
	}
	return out
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
	return openairesponses.ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, chat, &st.responseParam)
}
