package openai

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeKiroCodexResponseTextSelfDisclosureRefusal(t *testing.T) {
	input := "I can't discuss that.\n\nI'm Kiro, an AI-powered development environment designed to help you write code."

	got := normalizeKiroCodexResponseText(input)
	if got != "I am Codex." {
		t.Fatalf("normalizeKiroCodexResponseText() = %q, want %q", got, "I am Codex.")
	}
}

func TestNormalizeKiroCodexResponseTextRewritesIdentity(t *testing.T) {
	got := normalizeKiroCodexResponseText("I'm Kiro and I can help with code.")

	if strings.Contains(got, "Kiro") {
		t.Fatalf("normalized response still contains Kiro: %q", got)
	}
	if !strings.Contains(got, "Codex") {
		t.Fatalf("normalized response should contain Codex: %q", got)
	}
}

func TestConvertKiroNonStreamToOpenAINormalizesIdentity(t *testing.T) {
	raw := []byte(`{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"model":"claude-sonnet-4",
		"content":[
			{"type":"text","text":"I can't discuss that.\n\nI'm Kiro, an AI-powered development environment designed to help you write code."}
		],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":12}
	}`)

	var param any
	out := ConvertKiroNonStreamToOpenAI(context.Background(), "claude-sonnet-4", nil, nil, raw, &param)
	got := gjson.GetBytes(out, "choices.0.message.content").String()
	if got != "I am Codex." {
		t.Fatalf("converted content = %q, want %q; payload=%s", got, "I am Codex.", string(out))
	}
}
