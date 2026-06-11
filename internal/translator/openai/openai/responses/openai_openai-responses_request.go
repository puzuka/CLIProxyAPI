package responses

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIResponsesRequestToOpenAIChatCompletions converts OpenAI responses format to OpenAI chat completions format.
// It transforms the OpenAI responses API format (with instructions and input array) into the standard
// OpenAI chat completions format (with messages array and system content).
//
// The conversion handles:
// 1. Model name and streaming configuration
// 2. Instructions to system message conversion
// 3. Input array to messages array transformation
// 4. Tool definitions and tool choice conversion
// 5. Function calls and function results handling
// 6. Generation parameters mapping (max_tokens, reasoning, etc.)
//
// Parameters:
//   - modelName: The name of the model to use for the request
//   - rawJSON: The raw JSON request data in OpenAI responses format
//   - stream: A boolean indicating if the request is for a streaming response
//
// Returns:
//   - []byte: The transformed request data in OpenAI chat completions format
func ConvertOpenAIResponsesRequestToOpenAIChatCompletions(modelName string, inputRawJSON []byte, stream bool) []byte {
	rawJSON := inputRawJSON
	// Base OpenAI chat completions template with default values
	out := []byte(`{"model":"","messages":[],"stream":false}`)

	root := gjson.ParseBytes(rawJSON)

	// Set model name
	out, _ = sjson.SetBytes(out, "model", modelName)

	// Set stream configuration
	out, _ = sjson.SetBytes(out, "stream", stream)

	// Map generation parameters from responses format to chat completions format
	if maxTokens := root.Get("max_output_tokens"); maxTokens.Exists() {
		out, _ = sjson.SetBytes(out, "max_tokens", maxTokens.Int())
	}

	if parallelToolCalls := root.Get("parallel_tool_calls"); parallelToolCalls.Exists() {
		out, _ = sjson.SetBytes(out, "parallel_tool_calls", parallelToolCalls.Bool())
	}

	// Convert instructions to system message
	if instructions := root.Get("instructions"); instructions.Exists() {
		systemMessage := []byte(`{"role":"system","content":""}`)
		systemMessage, _ = sjson.SetBytes(systemMessage, "content", instructions.String())
		out, _ = sjson.SetRawBytes(out, "messages.-1", systemMessage)
	}

	// Convert input array to messages
	if input := root.Get("input"); input.Exists() && input.IsArray() {
		inputItems := input.Array()
		outputCallIDs := make(map[string]struct{})
		for _, item := range inputItems {
			t := item.Get("type").String()
			if t != "function_call_output" && t != "custom_tool_call_output" {
				continue
			}
			callID := strings.TrimSpace(item.Get("call_id").String())
			if callID == "" {
				continue
			}
			outputCallIDs[callID] = struct{}{}
		}

		pendingToolCalls := make([]interface{}, 0)
		pendingToolCallIDs := make([]string, 0)
		awaitingToolOutputs := make(map[string]struct{})
		deferredMessages := make([][]byte, 0)
		pendingReasoningContent := ""

		flushPendingToolCalls := func() {
			if len(pendingToolCalls) == 0 {
				return
			}
			assistantMessage := []byte(`{"role":"assistant","tool_calls":[]}`)
			assistantMessage, _ = sjson.SetBytes(assistantMessage, "tool_calls", pendingToolCalls)
			if pendingReasoningContent != "" {
				assistantMessage, _ = sjson.SetBytes(assistantMessage, "reasoning_content", pendingReasoningContent)
				pendingReasoningContent = ""
			}
			out, _ = sjson.SetRawBytes(out, "messages.-1", assistantMessage)
			for _, id := range pendingToolCallIDs {
				if strings.TrimSpace(id) == "" {
					continue
				}
				awaitingToolOutputs[id] = struct{}{}
			}
			pendingToolCalls = pendingToolCalls[:0]
			pendingToolCallIDs = pendingToolCallIDs[:0]
		}
		flushDeferredMessages := func() {
			for _, message := range deferredMessages {
				out, _ = sjson.SetRawBytes(out, "messages.-1", message)
			}
			deferredMessages = deferredMessages[:0]
		}
		hasAwaitingToolOutput := func() bool {
			for id := range awaitingToolOutputs {
				if _, ok := outputCallIDs[id]; ok {
					return true
				}
			}
			return false
		}
		appendRegularMessage := func(message []byte) {
			// Keep tool-call adjacency strict for providers that require
			// assistant(tool_calls) -> tool(tool_call_id) with no message in between.
			if hasAwaitingToolOutput() {
				deferredMessages = append(deferredMessages, message)
				return
			}
			out, _ = sjson.SetRawBytes(out, "messages.-1", message)
		}

		for _, item := range inputItems {
			itemType := item.Get("type").String()
			if itemType == "" && item.Get("role").String() != "" {
				itemType = "message"
			}
			if itemType != "function_call" && itemType != "custom_tool_call" {
				flushPendingToolCalls()
			}

			switch itemType {
			case "reasoning":
				if reasoningContent := responsesReasoningContent(item); reasoningContent != "" {
					pendingReasoningContent = reasoningContent
				}

			case "message", "":
				// Handle regular message conversion
				role := item.Get("role").String()
				if role == "developer" {
					role = "user"
				}
				message := []byte(`{"role":"","content":[]}`)
				message, _ = sjson.SetBytes(message, "role", role)

				if content := item.Get("content"); content.Exists() && content.IsArray() {
					var messageContent string
					var toolCalls []interface{}

					content.ForEach(func(_, contentItem gjson.Result) bool {
						contentType := contentItem.Get("type").String()
						if contentType == "" {
							contentType = "input_text"
						}

						switch contentType {
						case "input_text", "output_text":
							text := contentItem.Get("text").String()
							contentPart := []byte(`{"type":"text","text":""}`)
							contentPart, _ = sjson.SetBytes(contentPart, "text", text)
							message, _ = sjson.SetRawBytes(message, "content.-1", contentPart)
						case "input_image":
							imageURL := contentItem.Get("image_url").String()
							contentPart := []byte(`{"type":"image_url","image_url":{"url":""}}`)
							contentPart, _ = sjson.SetBytes(contentPart, "image_url.url", imageURL)
							message, _ = sjson.SetRawBytes(message, "content.-1", contentPart)
						}
						return true
					})

					if messageContent != "" {
						message, _ = sjson.SetBytes(message, "content", messageContent)
					}

					if len(toolCalls) > 0 {
						message, _ = sjson.SetBytes(message, "tool_calls", toolCalls)
					}
				} else if content.Type == gjson.String {
					message, _ = sjson.SetBytes(message, "content", content.String())
				}

				if role == "assistant" {
					if reasoningContent := responsesReasoningContent(item); reasoningContent != "" {
						pendingReasoningContent = reasoningContent
					}
					if pendingReasoningContent != "" {
						message, _ = sjson.SetBytes(message, "reasoning_content", pendingReasoningContent)
					}
				} else if role != "tool" {
					pendingReasoningContent = ""
				}

				appendRegularMessage(message)

			case "function_call":
				// Buffer consecutive function calls and emit them as one assistant message.
				toolCall := []byte(`{"id":"","type":"function","function":{"name":"","arguments":""}}`)
				name := ""

				if callId := item.Get("call_id"); callId.Exists() {
					toolCall, _ = sjson.SetBytes(toolCall, "id", callId.String())
				}

				if nameValue := item.Get("name"); nameValue.Exists() {
					name = nameValue.String()
					toolCall, _ = sjson.SetBytes(toolCall, "function.name", name)
				}

				arguments := "{}"
				if argumentsValue := item.Get("arguments"); argumentsValue.Exists() {
					arguments, _ = normalizeFunctionCallArguments(name, argumentsValue.String())
				}
				toolCall, _ = sjson.SetBytes(toolCall, "function.arguments", arguments)
				pendingToolCalls = append(pendingToolCalls, gjson.ParseBytes(toolCall).Value())
				if callID := strings.TrimSpace(item.Get("call_id").String()); callID != "" {
					pendingToolCallIDs = append(pendingToolCallIDs, callID)
				}

			case "function_call_output":
				// Handle function call output conversion to tool message
				toolMessage := []byte(`{"role":"tool","tool_call_id":"","content":""}`)
				callID := ""

				if callId := item.Get("call_id"); callId.Exists() {
					callID = strings.TrimSpace(callId.String())
					toolMessage, _ = sjson.SetBytes(toolMessage, "tool_call_id", callID)
				}

				if output := item.Get("output"); output.Exists() {
					toolMessage, _ = sjson.SetBytes(toolMessage, "content", output.String())
				}

				out, _ = sjson.SetRawBytes(out, "messages.-1", toolMessage)
				if callID != "" {
					delete(awaitingToolOutputs, callID)
				}
				if len(awaitingToolOutputs) == 0 && len(deferredMessages) > 0 {
					flushDeferredMessages()
				}

			case "custom_tool_call":
				// Codex emits custom tool invocations (e.g. apply_patch) as
				// custom_tool_call with a free-form string `input` instead of the
				// JSON `arguments` used by function_call. Since the request-side
				// converter rewrites custom tools into function tools that take a
				// single string `input`, wrap the raw input back into that shape so
				// the assistant turn round-trips as a normal tool_call.
				toolCall := []byte(`{"id":"","type":"function","function":{"name":"","arguments":""}}`)
				if callId := item.Get("call_id"); callId.Exists() {
					toolCall, _ = sjson.SetBytes(toolCall, "id", callId.String())
				}
				if name := item.Get("name"); name.Exists() {
					toolCall, _ = sjson.SetBytes(toolCall, "function.name", name.String())
				}
				args := []byte(`{"input":""}`)
				args, _ = sjson.SetBytes(args, "input", item.Get("input").String())
				toolCall, _ = sjson.SetBytes(toolCall, "function.arguments", string(args))
				pendingToolCalls = append(pendingToolCalls, gjson.ParseBytes(toolCall).Value())
				if callID := strings.TrimSpace(item.Get("call_id").String()); callID != "" {
					pendingToolCallIDs = append(pendingToolCallIDs, callID)
				}

			case "custom_tool_call_output":
				// Result for a custom tool call -> tool message (same shape as
				// function_call_output, but the payload lives in `output`).
				toolMessage := []byte(`{"role":"tool","tool_call_id":"","content":""}`)
				callID := ""
				if callId := item.Get("call_id"); callId.Exists() {
					callID = strings.TrimSpace(callId.String())
					toolMessage, _ = sjson.SetBytes(toolMessage, "tool_call_id", callID)
				}
				if output := item.Get("output"); output.Exists() {
					toolMessage, _ = sjson.SetBytes(toolMessage, "content", output.String())
				}
				out, _ = sjson.SetRawBytes(out, "messages.-1", toolMessage)
				if callID != "" {
					delete(awaitingToolOutputs, callID)
				}
				if len(awaitingToolOutputs) == 0 && len(deferredMessages) > 0 {
					flushDeferredMessages()
				}
			}

		}
		flushPendingToolCalls()
		flushDeferredMessages()
	} else if input.Type == gjson.String {
		msg := []byte(`{}`)
		msg, _ = sjson.SetBytes(msg, "role", "user")
		msg, _ = sjson.SetBytes(msg, "content", input.String())
		out, _ = sjson.SetRawBytes(out, "messages.-1", msg)
	}

	// Convert tools from responses format to chat completions format
	if tools := root.Get("tools"); tools.Exists() && tools.IsArray() {
		var chatCompletionsTools []interface{}

		tools.ForEach(func(_, tool gjson.Result) bool {
			toolType := tool.Get("type").String()

			// Codex registers free-form tools such as apply_patch as custom tools
			// ({"type":"custom","name":...,"format":{"type":"grammar",...}}). Chat
			// Completions providers (Kiro, Kimi, etc.) only understand function
			// tools, so previously these were dropped and the model never learned
			// the tool existed (e.g. Codex could not edit files via Kiro). Convert
			// them into function tools that take a single free-form string input,
			// preserving the grammar hint in the description so the model still
			// emits a well-formed payload.
			if toolType == "custom" {
				name := tool.Get("name").String()
				if name == "" {
					return true
				}
				description := tool.Get("description").String()
				if def := tool.Get("format.definition"); def.Exists() && def.String() != "" {
					syntax := tool.Get("format.syntax").String()
					if syntax == "" {
						syntax = "grammar"
					}
					description = strings.TrimSpace(description + "\n\nProvide the tool input as a single string in the \"input\" field. It must conform to this " + syntax + " grammar:\n" + def.String())
				}
				function := []byte(`{"name":"","description":"","parameters":{"type":"object","properties":{"input":{"type":"string","description":"The raw free-form input for this tool."}},"required":["input"]}}`)
				function, _ = sjson.SetBytes(function, "name", name)
				function, _ = sjson.SetBytes(function, "description", description)
				chatTool := []byte(`{"type":"function","function":{}}`)
				chatTool, _ = sjson.SetRawBytes(chatTool, "function", function)
				chatCompletionsTools = append(chatCompletionsTools, gjson.ParseBytes(chatTool).Value())
				return true
			}

			// Built-in tools (e.g. {"type":"web_search"}, tool_search,
			// image_generation) have no Chat Completions equivalent. Drop them so
			// downstream providers don't reject the request on an unknown schema.
			if toolType != "" && toolType != "function" && tool.IsObject() {
				return true
			}

			chatTool := []byte(`{"type":"function","function":{}}`)

			// Convert tool structure from responses format to chat completions format
			function := []byte(`{"name":"","description":"","parameters":{}}`)

			if name := tool.Get("name"); name.Exists() {
				function, _ = sjson.SetBytes(function, "name", name.String())
			}

			if description := tool.Get("description"); description.Exists() {
				function, _ = sjson.SetBytes(function, "description", description.String())
			}

			if parameters := tool.Get("parameters"); parameters.Exists() {
				function, _ = sjson.SetRawBytes(function, "parameters", []byte(parameters.Raw))
			}

			chatTool, _ = sjson.SetRawBytes(chatTool, "function", function)
			chatCompletionsTools = append(chatCompletionsTools, gjson.ParseBytes(chatTool).Value())

			return true
		})

		if len(chatCompletionsTools) > 0 {
			out, _ = sjson.SetBytes(out, "tools", chatCompletionsTools)
		}
	}

	if reasoningEffort := root.Get("reasoning.effort"); reasoningEffort.Exists() {
		effort := strings.ToLower(strings.TrimSpace(reasoningEffort.String()))
		if effort != "" {
			out, _ = sjson.SetBytes(out, "reasoning_effort", effort)
		}
	}

	// Convert tool_choice if present
	if toolChoice := root.Get("tool_choice"); toolChoice.Exists() {
		out, _ = sjson.SetBytes(out, "tool_choice", toolChoice.String())
	}

	return out
}

func responsesReasoningContent(item gjson.Result) string {
	if reasoning := item.Get("reasoning_content").String(); strings.TrimSpace(reasoning) != "" {
		return reasoning
	}

	if summary := item.Get("summary"); summary.Exists() && summary.IsArray() {
		parts := make([]string, 0)
		summary.ForEach(func(_, part gjson.Result) bool {
			partType := part.Get("type").String()
			if partType != "" && partType != "summary_text" {
				return true
			}
			if text := part.Get("text").String(); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
			return true
		})
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	if encryptedContent := item.Get("encrypted_content").String(); strings.TrimSpace(encryptedContent) != "" {
		return encryptedContent
	}

	return ""
}
