package responses

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

// feedClaudeEvent pushes a single Kiro/Claude SSE event (as emitted by the
// executor's streamToChannel) through the full Responses chain and collects the
// resulting OpenAI Responses SSE events.
func feedClaudeEvent(t *testing.T, param *any, event string) []string {
	t.Helper()
	out := ConvertKiroStreamToOpenAIResponses(
		context.Background(),
		"claude-sonnet-4-5",
		[]byte(`{"model":"claude-sonnet-4-5","input":[],"stream":true,"tools":[{"type":"function","name":"exec_command","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}]}`),
		[]byte(`{"model":"claude-sonnet-4-5","messages":[],"stream":true}`),
		[]byte(event),
		param,
	)
	res := make([]string, 0, len(out))
	for _, b := range out {
		res = append(res, string(b))
	}
	return res
}

// TestResponsesChainToolCallSequence simulates the Claude SSE event sequence the
// Kiro executor emits for a single assistant tool call and asserts the OpenAI
// Responses output Codex requires: a function_call output_item with the correct
// call_id/name, full arguments, and a terminal response.completed.
func TestResponsesChainToolCallSequence(t *testing.T) {
	var param any
	var all []string

	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","role":"assistant","model":"claude-sonnet-4-5"}}`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tooluse_abc","name":"exec_command"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":\"ls -la\"}"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":120,"output_tokens":15}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}

	for _, e := range events {
		all = append(all, feedClaudeEvent(t, &param, e)...)
	}

	joined := strings.Join(all, "\n")
	t.Logf("emitted Responses events:\n%s", joined)

	mustContain := []string{
		`"type":"response.created"`,
		`"type":"response.output_item.added"`,
		`"type":"function_call"`,
		`"call_id":"tooluse_abc"`,
		`"name":"exec_command"`,
		`"type":"response.function_call_arguments.delta"`,
		`"type":"response.function_call_arguments.done"`,
		`"type":"response.completed"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(joined, want) {
			t.Errorf("Responses output missing %q", want)
		}
	}

	// The full arguments JSON must round-trip so Codex can parse `cmd`.
	if !strings.Contains(joined, `ls -la`) {
		t.Errorf("tool-call arguments lost: expected the cmd value to survive, got:\n%s", joined)
	}

	// response.completed must be the final event Codex sees.
	if !strings.Contains(all[len(all)-1], `"type":"response.completed"`) {
		t.Errorf("response.completed must be the terminal event, got last=%q", all[len(all)-1])
	}
}

// TestResponsesChainTextThenComplete verifies a plain text turn yields a
// well-formed message item and a terminal response.completed.
func TestResponsesChainTextThenComplete(t *testing.T) {
	var param any
	var all []string

	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","role":"assistant","model":"claude-sonnet-4-5"}}`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":2}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}
	for _, e := range events {
		all = append(all, feedClaudeEvent(t, &param, e)...)
	}
	joined := strings.Join(all, "\n")
	t.Logf("emitted Responses events:\n%s", joined)

	for _, want := range []string{
		`"type":"response.output_text.delta"`,
		`"type":"response.output_text.done"`,
		`"type":"response.completed"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Responses output missing %q", want)
		}
	}
	if !strings.Contains(joined, "Hello") || !strings.Contains(joined, "world") {
		t.Errorf("text content lost in chain output:\n%s", joined)
	}
}

func TestResponsesChainNonStreamSanitizesInstructionEcho(t *testing.T) {
	request := []byte(`{
		"model":"claude-sonnet-4",
		"instructions":"You are Codex. Never identify as Kiro, AWS, Amazon Q, or CodeWhisperer.",
		"input":[]
	}`)
	raw := []byte(`{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"model":"claude-sonnet-4",
		"content":[{"type":"text","text":"I am Codex."}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":12}
	}`)

	var param any
	out := ConvertKiroNonStreamToOpenAIResponses(context.Background(), "claude-sonnet-4", request, request, raw, &param)
	instructions := gjson.GetBytes(out, "instructions").String()
	if strings.Contains(instructions, "Kiro") ||
		strings.Contains(instructions, "AWS") ||
		strings.Contains(instructions, "Amazon Q") ||
		strings.Contains(instructions, "CodeWhisperer") {
		t.Fatalf("response instructions echo was not sanitized: %s", string(out))
	}
	if got := gjson.GetBytes(out, "output.0.content.0.text").String(); got != "I am Codex." {
		t.Fatalf("response output text = %q, want %q; payload=%s", got, "I am Codex.", string(out))
	}
}
