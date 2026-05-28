package guideline

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

const sampleContent = "PROJECT_GUIDELINE_TOKEN_X1Y2Z3"

// helper: assert that body contains the marker token; fail with snippet on miss.
func mustContain(t *testing.T, body []byte, marker string, ctx string) {
	t.Helper()
	if !strings.Contains(string(body), marker) {
		t.Fatalf("%s: body missing marker %q.\nbody=%s", ctx, marker, string(body))
	}
}

func TestInject_NoOpOnEmpty(t *testing.T) {
	if got := Inject(FormatClaude, nil, sampleContent, PositionPrepend); got != nil {
		t.Fatalf("expected nil body to pass through unchanged, got %s", string(got))
	}
	in := []byte(`{"system":"hi"}`)
	if got := Inject(FormatClaude, in, "", PositionPrepend); string(got) != string(in) {
		t.Fatalf("expected empty content to be a no-op")
	}
	if got := Inject("unknown-format", in, sampleContent, PositionPrepend); string(got) != string(in) {
		t.Fatalf("expected unknown format to be a no-op")
	}
}

// ---- Claude ----

func TestInjectClaude_NoSystem(t *testing.T) {
	in := []byte(`{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hi"}]}`)
	out := Inject(FormatClaude, in, sampleContent, PositionPrepend)
	mustContain(t, out, sampleContent, "claude/no-system")
	if gjson.GetBytes(out, "system").String() != sampleContent {
		t.Fatalf("expected system to be exactly the content; got %q", gjson.GetBytes(out, "system").String())
	}
}

func TestInjectClaude_StringSystem_Prepend(t *testing.T) {
	in := []byte(`{"system":"you are claude.","messages":[]}`)
	out := Inject(FormatClaude, in, sampleContent, PositionPrepend)
	got := gjson.GetBytes(out, "system").String()
	if !strings.HasPrefix(got, sampleContent) {
		t.Fatalf("prepend should place content at the start; got %q", got)
	}
	mustContain(t, []byte(got), "you are claude.", "claude/string-prepend preserves existing")
}

func TestInjectClaude_StringSystem_Append(t *testing.T) {
	in := []byte(`{"system":"you are claude.","messages":[]}`)
	out := Inject(FormatClaude, in, sampleContent, PositionAppend)
	got := gjson.GetBytes(out, "system").String()
	if !strings.HasSuffix(got, sampleContent) {
		t.Fatalf("append should place content at the end; got %q", got)
	}
}

func TestInjectClaude_StringSystem_Replace(t *testing.T) {
	in := []byte(`{"system":"you are claude.","messages":[]}`)
	out := Inject(FormatClaude, in, sampleContent, PositionReplace)
	got := gjson.GetBytes(out, "system").String()
	if got != sampleContent {
		t.Fatalf("replace should overwrite; got %q", got)
	}
}

func TestInjectClaude_ArraySystem_Prepend(t *testing.T) {
	in := []byte(`{"system":[{"type":"text","text":"you are claude."}],"messages":[]}`)
	out := Inject(FormatClaude, in, sampleContent, PositionPrepend)
	arr := gjson.GetBytes(out, "system").Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 system blocks, got %d (%s)", len(arr), string(out))
	}
	if arr[0].Get("text").String() != sampleContent {
		t.Fatalf("expected new block at index 0; got %q", arr[0].Get("text").String())
	}
	if arr[1].Get("text").String() != "you are claude." {
		t.Fatalf("expected existing block at index 1 preserved; got %q", arr[1].Get("text").String())
	}
}

func TestInjectClaude_ArraySystem_Append(t *testing.T) {
	in := []byte(`{"system":[{"type":"text","text":"a","cache_control":{"type":"ephemeral"}}],"messages":[]}`)
	out := Inject(FormatClaude, in, sampleContent, PositionAppend)
	arr := gjson.GetBytes(out, "system").Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(arr))
	}
	if arr[0].Get("cache_control.type").String() != "ephemeral" {
		t.Fatalf("expected existing cache_control preserved; out=%s", string(out))
	}
	if arr[1].Get("text").String() != sampleContent {
		t.Fatalf("expected appended block at index 1; got %q", arr[1].Get("text").String())
	}
}

// ---- OpenAI Chat ----

func TestInjectOpenAIChat_NoSystem(t *testing.T) {
	in := []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`)
	out := Inject(FormatOpenAIChat, in, sampleContent, PositionPrepend)
	first := gjson.GetBytes(out, "messages.0")
	if first.Get("role").String() != "system" {
		t.Fatalf("expected new system message at index 0; got role=%q", first.Get("role").String())
	}
	if first.Get("content").String() != sampleContent {
		t.Fatalf("expected content to be exact; got %q", first.Get("content").String())
	}
	if gjson.GetBytes(out, "messages.1.role").String() != "user" {
		t.Fatalf("expected original user message preserved at index 1; out=%s", string(out))
	}
}

func TestInjectOpenAIChat_StringSystem_Prepend(t *testing.T) {
	in := []byte(`{"messages":[{"role":"system","content":"original sys"},{"role":"user","content":"hi"}]}`)
	out := Inject(FormatOpenAIChat, in, sampleContent, PositionPrepend)
	got := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.HasPrefix(got, sampleContent) {
		t.Fatalf("prepend should place content first; got %q", got)
	}
	if !strings.Contains(got, "original sys") {
		t.Fatalf("prepend should preserve original; got %q", got)
	}
}

func TestInjectOpenAIChat_DeveloperRole(t *testing.T) {
	in := []byte(`{"messages":[{"role":"developer","content":"dev sys"},{"role":"user","content":"hi"}]}`)
	out := Inject(FormatOpenAIChat, in, sampleContent, PositionPrepend)
	if gjson.GetBytes(out, "messages.0.role").String() != "developer" {
		t.Fatalf("expected developer role preserved; got %q", gjson.GetBytes(out, "messages.0.role").String())
	}
	got := gjson.GetBytes(out, "messages.0.content").String()
	if !strings.HasPrefix(got, sampleContent) || !strings.Contains(got, "dev sys") {
		t.Fatalf("developer role should be merged like system; got %q", got)
	}
}

func TestInjectOpenAIChat_ContentArray_Prepend(t *testing.T) {
	in := []byte(`{"messages":[{"role":"system","content":[{"type":"text","text":"a"}]},{"role":"user","content":"hi"}]}`)
	out := Inject(FormatOpenAIChat, in, sampleContent, PositionPrepend)
	arr := gjson.GetBytes(out, "messages.0.content").Array()
	if len(arr) != 2 {
		t.Fatalf("expected 2 blocks; got %d (%s)", len(arr), string(out))
	}
	if arr[0].Get("text").String() != sampleContent {
		t.Fatalf("expected new block at index 0; got %q", arr[0].Get("text").String())
	}
}

// ---- OpenAI Responses ----

func TestInjectOpenAIResponses_NoInstructions(t *testing.T) {
	in := []byte(`{"model":"gpt-5.5","input":[{"role":"user","content":"hi"}]}`)
	out := Inject(FormatOpenAIResponses, in, sampleContent, PositionPrepend)
	if got := gjson.GetBytes(out, "instructions").String(); got != sampleContent {
		t.Fatalf("expected instructions to be set; got %q", got)
	}
}

func TestInjectOpenAIResponses_StringInstructions_Prepend(t *testing.T) {
	in := []byte(`{"instructions":"original","input":[]}`)
	out := Inject(FormatOpenAIResponses, in, sampleContent, PositionPrepend)
	got := gjson.GetBytes(out, "instructions").String()
	if !strings.HasPrefix(got, sampleContent) || !strings.Contains(got, "original") {
		t.Fatalf("prepend failed; got %q", got)
	}
}

func TestInjectOpenAIResponses_StringInstructions_Append(t *testing.T) {
	in := []byte(`{"instructions":"original","input":[]}`)
	out := Inject(FormatOpenAIResponses, in, sampleContent, PositionAppend)
	got := gjson.GetBytes(out, "instructions").String()
	if !strings.HasSuffix(got, sampleContent) || !strings.Contains(got, "original") {
		t.Fatalf("append failed; got %q", got)
	}
}

// ---- Gemini ----

func TestInjectGemini_NoSystemInstruction(t *testing.T) {
	in := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	out := Inject(FormatGemini, in, sampleContent, PositionPrepend)
	got := gjson.GetBytes(out, "systemInstruction.parts.0.text").String()
	if got != sampleContent {
		t.Fatalf("expected systemInstruction inserted; got %q (out=%s)", got, string(out))
	}
}

func TestInjectGemini_TextPart_Prepend(t *testing.T) {
	in := []byte(`{"systemInstruction":{"parts":[{"text":"original"}]},"contents":[]}`)
	out := Inject(FormatGemini, in, sampleContent, PositionPrepend)
	got := gjson.GetBytes(out, "systemInstruction.parts.0.text").String()
	if !strings.HasPrefix(got, sampleContent) || !strings.Contains(got, "original") {
		t.Fatalf("expected merged text; got %q", got)
	}
}

func TestInjectGemini_SnakeCaseKey(t *testing.T) {
	// Some Gemini clients use snake_case `system_instruction` (REST default).
	in := []byte(`{"system_instruction":{"parts":[{"text":"snake"}]},"contents":[]}`)
	out := Inject(FormatGemini, in, sampleContent, PositionPrepend)
	got := gjson.GetBytes(out, "system_instruction.parts.0.text").String()
	if !strings.HasPrefix(got, sampleContent) || !strings.Contains(got, "snake") {
		t.Fatalf("expected snake_case key handled; got %q (out=%s)", got, string(out))
	}
}

func TestInjectGemini_NoTextPart_Append(t *testing.T) {
	// systemInstruction has only a non-text part (e.g. inline_data) — injector
	// should append a new text part rather than overwrite.
	in := []byte(`{"systemInstruction":{"parts":[{"inline_data":{"mime_type":"image/png","data":"AAAA"}}]},"contents":[]}`)
	out := Inject(FormatGemini, in, sampleContent, PositionAppend)
	parts := gjson.GetBytes(out, "systemInstruction.parts").Array()
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts; got %d (out=%s)", len(parts), string(out))
	}
	if parts[1].Get("text").String() != sampleContent {
		t.Fatalf("expected new text part appended; got %q", parts[1].Get("text").String())
	}
}

// ---- ApplyFromConfig integration ----

func TestApplyFromConfig_DefaultOn_NilConfig(t *testing.T) {
	in := []byte(`{"input":[],"instructions":"orig"}`)
	out := ApplyFromConfig(FormatOpenAIResponses, in, nil)
	got := gjson.GetBytes(out, "instructions").String()
	if !strings.Contains(got, "agent-harness-kit") {
		t.Fatalf("expected default content to mention agent-harness-kit; got %q", got)
	}
	if !strings.Contains(got, "orig") {
		t.Fatalf("expected original instructions preserved; got %q", got)
	}
}

func TestMergeText(t *testing.T) {
	cases := []struct {
		existing, addition, position, want string
	}{
		{"", "new", PositionPrepend, "new"},
		{"a", "b", PositionPrepend, "b\n\na"},
		{"a", "b", PositionAppend, "a\n\nb"},
		{"a", "b", PositionReplace, "b"},
		{"a", "b", "", "b\n\na"}, // unknown defaults to prepend in injector callers
	}
	for _, c := range cases {
		got := MergeText(c.existing, c.addition, c.position)
		if got != c.want {
			t.Errorf("MergeText(%q,%q,%q)=%q; want %q", c.existing, c.addition, c.position, got, c.want)
		}
	}
}
