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

// ConvertOpenAIResponsesRequestToKiro converts an /v1/responses request body
// into a Kiro request body. The /v1/responses → /v1/chat/completions step is
// delegated to the canonical openai-responses converter so any future fix
// there is automatically inherited; the second step is the same converter the
// /v1/chat/completions handler already uses for Kiro.
func ConvertOpenAIResponsesRequestToKiro(modelName string, rawJSON []byte, stream bool) []byte {
	chat := openairesponses.ConvertOpenAIResponsesRequestToOpenAIChatCompletions(modelName, rawJSON, stream)
	return kiroopenai.ConvertOpenAIRequestToKiro(modelName, chat, stream)
}

// ConvertKiroStreamToOpenAIResponses converts a single Kiro stream chunk into
// zero or more OpenAI Responses SSE events. The shared param pointer carries
// the openai-responses framer state (sequence_number, response.id, output
// item bookkeeping, etc.) across calls; the kiro→openai-chat step is stateless
// per chunk so we can reuse the same param verbatim for the second hop.
func ConvertKiroStreamToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) [][]byte {
	chatChunks := kiroopenai.ConvertKiroStreamToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, param)
	if len(chatChunks) == 0 {
		return nil
	}

	out := make([][]byte, 0, len(chatChunks)*2)
	for _, chunk := range chatChunks {
		if len(chunk) == 0 {
			continue
		}
		events := openairesponses.ConvertOpenAIChatCompletionsResponseToOpenAIResponses(ctx, modelName, originalRequestRawJSON, requestRawJSON, chunk, param)
		out = append(out, events...)
	}
	return out
}

// ConvertKiroNonStreamToOpenAIResponses chains the two non-stream converters.
// Kiro non-stream output is shaped as an OpenAI Chat Completions JSON, then
// rewrapped into the OpenAI Responses object the /v1/responses handler expects.
func ConvertKiroNonStreamToOpenAIResponses(ctx context.Context, modelName string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []byte {
	chat := kiroopenai.ConvertKiroNonStreamToOpenAI(ctx, modelName, originalRequestRawJSON, requestRawJSON, rawJSON, param)
	return openairesponses.ConvertOpenAIChatCompletionsResponseToOpenAIResponsesNonStream(ctx, modelName, originalRequestRawJSON, requestRawJSON, chat, param)
}
