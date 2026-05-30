package chat_completions

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestToGemini_VideoBase64InImageUrl(t *testing.T) {
	inputJSON := []byte(`{
		"model": "gemini-3.5-flash",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Analyze this video"},
					{"type": "image_url", "image_url": {"url": "data:video/mp4;base64,AAAA"}}
				]
			}
		]
	}`)

	output := ConvertOpenAIRequestToGemini("gemini-3.5-flash", inputJSON, false)

	// Verify that the contents are parsed properly and inlineData is set
	parts := gjson.GetBytes(output, "contents.0.parts").Array()
	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	if got := parts[0].Get("text").String(); got != "Analyze this video" {
		t.Errorf("Expected first part to be text, got '%s'", got)
	}

	inlineData := parts[1].Get("inlineData")
	if !inlineData.Exists() {
		t.Fatal("Expected inlineData to exist")
	}

	if got := inlineData.Get("mime_type").String(); got != "video/mp4" {
		t.Errorf("Expected mime_type 'video/mp4', got '%s'", got)
	}

	if got := inlineData.Get("data").String(); got != "AAAA" {
		t.Errorf("Expected data 'AAAA', got '%s'", got)
	}
}

func TestConvertOpenAIRequestToGemini_VideoBase64InVideoUrlObject(t *testing.T) {
	inputJSON := []byte(`{
		"model": "gemini-3.5-flash",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Check this video out"},
					{"type": "video_url", "video_url": {"url": "data:video/webm;base64,BBBB"}}
				]
			}
		]
	}`)

	output := ConvertOpenAIRequestToGemini("gemini-3.5-flash", inputJSON, false)

	parts := gjson.GetBytes(output, "contents.0.parts").Array()
	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	inlineData := parts[1].Get("inlineData")
	if !inlineData.Exists() {
		t.Fatal("Expected inlineData to exist")
	}

	if got := inlineData.Get("mime_type").String(); got != "video/webm" {
		t.Errorf("Expected mime_type 'video/webm', got '%s'", got)
	}

	if got := inlineData.Get("data").String(); got != "BBBB" {
		t.Errorf("Expected data 'BBBB', got '%s'", got)
	}
}

func TestConvertOpenAIRequestToGemini_VideoBase64InVideoUrlString(t *testing.T) {
	inputJSON := []byte(`{
		"model": "gemini-3.5-flash",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Look at this"},
					{"type": "video_url", "video_url": "data:video/quicktime;base64,CCCC"}
				]
			}
		]
	}`)

	output := ConvertOpenAIRequestToGemini("gemini-3.5-flash", inputJSON, false)

	parts := gjson.GetBytes(output, "contents.0.parts").Array()
	if len(parts) != 2 {
		t.Fatalf("Expected 2 parts, got %d", len(parts))
	}

	inlineData := parts[1].Get("inlineData")
	if !inlineData.Exists() {
		t.Fatal("Expected inlineData to exist")
	}

	if got := inlineData.Get("mime_type").String(); got != "video/quicktime" {
		t.Errorf("Expected mime_type 'video/quicktime', got '%s'", got)
	}

	if got := inlineData.Get("data").String(); got != "CCCC" {
		t.Errorf("Expected data 'CCCC', got '%s'", got)
	}
}

func TestConvertOpenAIRequestToGemini_VideoBase64IgnoresMalformedURLsAndParsesParams(t *testing.T) {
	inputJSON := []byte(`{
		"model": "gemini-3.5-flash",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Analyze only the valid inline video"},
					{"type": "video_url", "video_url": {"url": "https://example.com/video.mp4?token=a;b=c"}},
					{"type": "video_url", "video_url": {"url": "data:video/mp4;base64"}},
					{"type": "video_url", "video_url": {"url": "data:video/mp4;base64,"}},
					{"type": "video_url", "video_url": {"url": "data:video/mp4;codecs=avc1;base64,DDDD"}}
				]
			}
		]
	}`)

	output := ConvertOpenAIRequestToGemini("gemini-3.5-flash", inputJSON, false)

	parts := gjson.GetBytes(output, "contents.0.parts").Array()
	if len(parts) != 2 {
		t.Fatalf("Expected only text plus the valid inline video, got %d parts: %s", len(parts), gjson.GetBytes(output, "contents.0.parts").Raw)
	}

	inlineData := parts[1].Get("inlineData")
	if !inlineData.Exists() {
		t.Fatal("Expected valid video_url to produce inlineData")
	}

	if got := inlineData.Get("mime_type").String(); got != "video/mp4" {
		t.Errorf("Expected mime_type 'video/mp4', got '%s'", got)
	}

	if got := inlineData.Get("data").String(); got != "DDDD" {
		t.Errorf("Expected data 'DDDD', got '%s'", got)
	}
}
