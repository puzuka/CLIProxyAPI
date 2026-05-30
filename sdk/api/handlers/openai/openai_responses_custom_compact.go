package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

// customCompactSystemPrompt is the system prompt sent to the LLM for context
// compaction. It instructs the model to produce a structured execution handoff
// that preserves the critical state needed for the coding agent to resume.
const customCompactSystemPrompt = `You are a context compaction assistant. Your job is to create an execution handoff for a coding agent that was interrupted mid-task.

CRITICAL REQUIREMENTS:
- Use EXACTLY these section headers (in English):
  * Current task:
  * User intent:
  * Repo / location:
  * Current state:
  * Important files:
  * Changes already made:
  * Known verification:
  * Unfinished work:
  * Next action:
  * Do not do:

- Be SPECIFIC and EXECUTABLE
- Include file paths, line numbers, command outputs
- "Next action" must be a single clear command or edit
- If you cannot determine the task, say "RECOVERY REQUIRED" explicitly

Quality rules:
- Preserve intent, not transcript
- Include only files directly relevant to the active task, each with a reason
- State "Unknown" explicitly instead of inventing
- Do not truncate mid-sentence
- Deduplicate file paths (prefer repo-relative)
- Make "Next action" a single executable step, not a category list
- Preserve tool/function call context that is relevant to the active task
- If the conversation includes developer/system instructions, note the key constraints (not the full text)`

// defaultFunctionArgMaxLen is the default maximum character length for function
// call arguments before truncation. Real compact payloads contain function calls
// with arguments up to 25K+ chars (e.g. apply_patch); a 500-char cap loses
// almost all the meaningful content.
const defaultFunctionArgMaxLen = 4000

// defaultFunctionOutputMaxLen is the default maximum character length for
// function call output before truncation.
const defaultFunctionOutputMaxLen = 4000

// requiredHandoffSections are the section headers that must appear in a valid
// custom compact response. Used for output validation.
var requiredHandoffSections = []string{
	"Current task:",
	"User intent:",
	"Repo / location:",
	"Current state:",
	"Important files:",
	"Changes already made:",
	"Known verification:",
	"Unfinished work:",
	"Next action:",
	"Do not do:",
}

// customCompactRequest is the /chat/completions payload sent to the LLM.
type customCompactRequest struct {
	Model       string                 `json:"model"`
	Messages    []customCompactMessage `json:"messages"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Temperature float64                `json:"temperature"`
	Stream      bool                   `json:"stream"`
}

type customCompactMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// compactionSummaryResponse matches the real Codex compact response format:
//   - object: "response.compaction"
//   - output array: preserved developer/user messages + final compaction_summary item
//
// This is the format returned by the Codex native compact endpoint
// (chatgpt.com/backend-api/codex/responses/compact).
type compactionSummaryResponse struct {
	ID        string        `json:"id"`
	Object    string        `json:"object"`
	CreatedAt int64         `json:"created_at"`
	Output    []interface{} `json:"output"`
}

// compactionOutputMessage is a preserved developer/user message in the compact
// output array. These messages carry system instructions, AGENTS.md, skills,
// plugins, memory, and the original user request.
type compactionOutputMessage struct {
	ID      string                  `json:"id"`
	Type    string                  `json:"type"`
	Status  string                  `json:"status"`
	Content []compactionContentPart `json:"content"`
	Role    string                  `json:"role"`
}

// compactionSummaryItem is the final item in the compact output array that
// contains the LLM-generated compact summary text. The Codex client expects
// the field to be named "encrypted_content" (even though the custom compact
// path stores plain text, not ciphertext). Omitting this field causes the
// client to fail with: missing field `encrypted_content`.
type compactionSummaryItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	EncryptedContent string `json:"encrypted_content"`
}

type compactionContentPart struct {
	Type        string   `json:"type"`
	Text        string   `json:"text"`
	Annotations []string `json:"annotations,omitempty"`
}

// buildCompactResponseJSON creates a compact response that matches the real
// Codex compact output format (response.compaction). It preserves developer
// and user messages from the original input and appends the LLM-generated
// compaction summary as the final output item.
//
// Output format:
//
//	{
//	  "id": "resp_compact_<timestamp>",
//	  "object": "response.compaction",
//	  "created_at": <unix_timestamp>,
//	  "output": [
//	    { preserved developer messages... },
//	    { preserved user messages... },
//	    { "type": "compaction_summary", "encrypted_content": "<summary_text>" }
//	  ]
//	}
func buildCompactResponseJSON(summaryText string, rawJSON []byte) ([]byte, error) {
	now := time.Now()
	respID := fmt.Sprintf("resp_compact_%d", now.UnixMilli())

	// Collect preserved developer/user messages from the original input
	var outputItems []interface{}
	input := gjson.GetBytes(rawJSON, "input")
	if input.IsArray() {
		msgIdx := 0
		for _, item := range input.Array() {
			itemType := item.Get("type").String()
			role := item.Get("role").String()
			if itemType == "message" && (role == "developer" || role == "user") {
				// Preserve this message in the compact output
				msg := compactionOutputMessage{
					ID:     fmt.Sprintf("msg_compact_%d", msgIdx),
					Type:   "message",
					Status: "completed",
					Role:   role,
				}
				content := item.Get("content")
				if content.IsArray() {
					for _, part := range content.Array() {
						partType := part.Get("type").String()
						if partType == "input_text" || partType == "text" {
							msg.Content = append(msg.Content, compactionContentPart{
								Type: "input_text",
								Text: part.Get("text").String(),
							})
						}
					}
				} else if content.Type == gjson.String {
					msg.Content = append(msg.Content, compactionContentPart{
						Type: "input_text",
						Text: content.String(),
					})
				}
				if len(msg.Content) > 0 {
					outputItems = append(outputItems, msg)
					msgIdx++
				}
			}
		}
	}

	// Append the compaction summary as the final item.
	// The Codex client deserializes "encrypted_content" (a plain string) on
	// compaction_summary items. Using "content" (an array) causes:
	//   missing field `encrypted_content` at line 1 column NNNNN
	summary := compactionSummaryItem{
		ID:               fmt.Sprintf("cmp_compact_%d", now.UnixMilli()),
		Type:             "compaction_summary",
		EncryptedContent: summaryText,
	}
	outputItems = append(outputItems, summary)

	resp := compactionSummaryResponse{
		ID:        respID,
		Object:    "response.compaction",
		CreatedAt: now.Unix(),
		Output:    outputItems,
	}
	return json.Marshal(resp)
}

// extractConversationForCompact extracts the conversation content from a
// compact request and formats it as a text block suitable for LLM
// summarization. It processes:
//   - top-level "instructions" field (system instructions the agent was given)
//   - top-level "tools" array (tool names for context)
//   - input array: user/assistant messages, function/custom tool calls and outputs
//   - skips reasoning items (provider-specific signatures)
//
// Function call arguments and outputs are truncated at configurable limits
// (default 4000 chars) to preserve meaningful content from large payloads
// like apply_patch calls.
func extractConversationForCompact(rawJSON []byte) string {
	var sb strings.Builder

	// Extract top-level instructions (developer/system prompt context)
	instructions := gjson.GetBytes(rawJSON, "instructions")
	if instructions.Exists() && instructions.Type == gjson.String {
		instText := instructions.String()
		if len(instText) > 0 {
			// Include a truncated version of instructions for context
			sb.WriteString("[System instructions summary]\n")
			if len(instText) > 2000 {
				sb.WriteString(instText[:2000])
				sb.WriteString("\n...(truncated)\n\n")
			} else {
				sb.WriteString(instText)
				sb.WriteString("\n\n")
			}
		}
	}

	// Extract tool names for context
	tools := gjson.GetBytes(rawJSON, "tools")
	if tools.IsArray() && len(tools.Array()) > 0 {
		sb.WriteString("[Available tools: ")
		var toolNames []string
		for _, tool := range tools.Array() {
			name := tool.Get("name").String()
			if name == "" {
				// OpenAI function calling format: tools[].function.name
				name = tool.Get("function.name").String()
			}
			if name != "" {
				toolNames = append(toolNames, name)
			}
		}
		sb.WriteString(strings.Join(toolNames, ", "))
		sb.WriteString("]\n\n")
	}

	input := gjson.GetBytes(rawJSON, "input")
	if !input.Exists() {
		return sb.String()
	}

	// Handle input as a string (simple text input)
	if input.Type == gjson.String {
		sb.WriteString("User: ")
		sb.WriteString(input.String())
		return sb.String()
	}

	// Handle input as an array of items
	if !input.IsArray() {
		return sb.String()
	}

	for _, item := range input.Array() {
		itemType := item.Get("type").String()
		role := item.Get("role").String()

		// Skip reasoning items - they carry provider-specific signatures
		if itemType == "reasoning" {
			continue
		}

		if itemType == "message" {
			// Extract text content from message items
			content := item.Get("content")
			if content.Type == gjson.String {
				sb.WriteString(capitalizeFirst(role))
				sb.WriteString(": ")
				sb.WriteString(content.String())
				sb.WriteString("\n\n")
				continue
			}
			if content.IsArray() {
				for _, part := range content.Array() {
					partType := part.Get("type").String()
					if partType == "input_text" || partType == "output_text" || partType == "text" {
						text := part.Get("text").String()
						if text != "" {
							sb.WriteString(capitalizeFirst(role))
							sb.WriteString(": ")
							sb.WriteString(text)
							sb.WriteString("\n\n")
						}
					}
				}
			}
			continue
		}

		// Handle other item types with content. Codex custom tools (for
		// example apply_patch) use custom_tool_call with a free-form input
		// string instead of function_call.arguments.
		if itemType == "function_call" || itemType == "custom_tool_call" {
			name := item.Get("name").String()
			args := item.Get("arguments").String()
			if args == "" {
				args = item.Get("input").String()
			}
			label := "Function call"
			if itemType == "custom_tool_call" {
				label = "Custom tool call"
			}
			sb.WriteString(fmt.Sprintf("%s: %s(%s)\n\n", label, name, truncateText(args, defaultFunctionArgMaxLen)))
			continue
		}

		if itemType == "function_call_output" || itemType == "custom_tool_call_output" {
			output := item.Get("output").String()
			label := "Function output"
			if itemType == "custom_tool_call_output" {
				label = "Custom tool output"
			}
			sb.WriteString(fmt.Sprintf("%s: %s\n\n", label, truncateText(output, defaultFunctionOutputMaxLen)))
			continue
		}
	}

	return sb.String()
}

// capitalizeFirst returns the string with its first character uppercased.
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// truncateText truncates text to maxLen characters, appending "..." if truncated.
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "...(truncated)"
}

// buildCustomCompactUserPrompt builds the user message for the LLM compaction
// call, including the extracted conversation and any additional context.
func buildCustomCompactUserPrompt(conversation string) string {
	var sb strings.Builder
	sb.WriteString("Create a compact execution handoff from this conversation context.\n\n")
	sb.WriteString("Conversation:\n")
	sb.WriteString(conversation)
	return sb.String()
}

// extractChatCompletionText extracts the assistant's text response from a
// /chat/completions response payload.
func extractChatCompletionText(payload []byte) string {
	// Standard chat completion format: choices[0].message.content
	content := gjson.GetBytes(payload, "choices.0.message.content")
	if content.Exists() && content.Type == gjson.String {
		return content.String()
	}
	return ""
}

// validateCustomCompactOutput checks whether the LLM compaction output
// contains all required handoff sections and meets minimum quality.
func validateCustomCompactOutput(text string) (ok bool, missing []string) {
	for _, section := range requiredHandoffSections {
		if !strings.Contains(text, section) {
			missing = append(missing, section)
		}
	}
	if len(text) < 200 {
		return false, missing
	}
	return len(missing) == 0, missing
}

// shouldApplyCustomCompact reports whether the custom compact path should be
// used for this request. It returns true when:
//   - custom-compact.enabled is true
//   - the requested model's providers match the custom compact criteria
//     (i.e., the model is NOT already served by a Codex provider that handles
//     compact natively)
//
// Custom compact is intentionally allowed even when compact-fallback is
// enabled. The main handler tries Codex compact fallback first; if that
// fallback is unavailable or fails, this path becomes the secondary fallback.
func shouldApplyCustomCompact(h *OpenAIResponsesAPIHandler, modelName string) bool {
	if h == nil || h.Cfg == nil {
		return false
	}
	cc := h.Cfg.CustomCompact
	if !cc.Enabled {
		return false
	}
	// Skip if the original model is already served by a Codex provider
	// (native compact works without any custom handling)
	originalProviders := util.GetProviderName(modelName)
	if util.InArray(originalProviders, "codex") {
		return false
	}
	return true
}

func customCompactModelForRequest(h *OpenAIResponsesAPIHandler, modelName string) string {
	if h == nil || h.Cfg == nil {
		return strings.TrimSpace(modelName)
	}
	if configured := strings.TrimSpace(h.Cfg.CustomCompact.Model); configured != "" {
		return configured
	}
	return strings.TrimSpace(modelName)
}

// executeCustomCompact performs LLM-based context compaction:
//  1. Extracts conversation from the compact request input (including instructions and tools)
//  2. Builds a system + user prompt for the LLM
//  3. Calls /chat/completions via ExecuteInternalWithAuthManager with the configured model
//  4. Validates the output and retries if needed
//  5. Returns the result wrapped in the Codex response.compaction format with
//     preserved developer/user messages and a compaction_summary item
func executeCustomCompact(h *OpenAIResponsesAPIHandler, cliCtx context.Context, modelName string, rawJSON []byte) ([]byte, error) {
	cc := h.Cfg.CustomCompact
	compactModel := customCompactModelForRequest(h, modelName)
	if compactModel == "" {
		return nil, fmt.Errorf("custom compact: no model configured and request model is empty")
	}
	maxRetries := cc.EffectiveMaxRetries()
	maxTokens := cc.EffectiveMaxTokens()
	temperature := cc.EffectiveTemperature()

	conversation := extractConversationForCompact(rawJSON)
	if conversation == "" {
		log.Warn("custom compact: no conversation content extracted from input")
		conversation = "No conversation content available."
	}

	log.Infof("custom compact: using model %s for compaction (original model: %s, conversation_len: %d)", compactModel, modelName, len(conversation))

	var lastText string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		userPrompt := buildCustomCompactUserPrompt(conversation)
		if attempt > 0 && lastText != "" {
			_, missing := validateCustomCompactOutput(lastText)
			userPrompt = fmt.Sprintf("Previous compact attempt was incomplete. Missing sections: %v\nPlease include ALL required sections.\n\n%s", missing, userPrompt)
		}

		chatReq := customCompactRequest{
			Model: compactModel,
			Messages: []customCompactMessage{
				{Role: "system", Content: customCompactSystemPrompt},
				{Role: "user", Content: userPrompt},
			},
			MaxTokens:   maxTokens,
			Temperature: temperature,
			Stream:      false,
		}

		chatPayload, err := json.Marshal(chatReq)
		if err != nil {
			return nil, fmt.Errorf("custom compact: marshal chat request: %w", err)
		}

		// Use the OpenAI chat handler type for the /chat/completions call.
		// The Compact handler already checked the user's API-key policy against
		// the requested model; the custom compact model is operator-owned
		// infrastructure and should not be blocked by the caller's model allow-list.
		resp, _, errMsg := h.ExecuteInternalWithAuthManager(
			cliCtx,
			"openai",
			compactModel,
			chatPayload,
			"", // empty alt = /chat/completions path
		)
		if errMsg != nil {
			log.Warnf("custom compact attempt %d failed: %s", attempt, errMsg.Error)
			if attempt == maxRetries {
				return nil, fmt.Errorf("custom compact: LLM call failed after %d attempts: %s", attempt+1, errMsg.Error)
			}
			continue
		}

		text := extractChatCompletionText(resp)
		if text == "" {
			log.Warnf("custom compact attempt %d: empty response from LLM", attempt)
			lastText = ""
			if attempt == maxRetries {
				return nil, fmt.Errorf("custom compact: empty LLM response after %d attempts", attempt+1)
			}
			continue
		}

		lastText = text
		ok, missing := validateCustomCompactOutput(text)
		if ok || attempt == maxRetries {
			if !ok {
				log.Warnf("custom compact: accepting incomplete output after %d attempts (missing: %v)", attempt+1, missing)
			} else {
				log.Infof("custom compact: successful compaction on attempt %d, output length=%d", attempt+1, len(text))
			}
			return buildCompactResponseJSON(text, rawJSON)
		}

		log.Infof("custom compact attempt %d: output missing sections %v, retrying", attempt+1, missing)
	}

	// Should not reach here, but safety fallback
	return buildCompactResponseJSON(lastText, rawJSON)
}
