package openai

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	sdkconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	log "github.com/sirupsen/logrus"
)

// compactLogDir is the directory where compact trigger-log files are written.
// Resolved relative to the working directory.
const compactLogDir = "logs"

// compactTriggerLogFileMode keeps compact trigger logs private to the runtime
// user because they may contain prompts, tool outputs, and compact summaries.
const compactTriggerLogFileMode = 0o600

// compactLogEntry is the JSON structure written for each compact trigger-log event.
type compactLogEntry struct {
	Timestamp     string          `json:"timestamp"`
	Type          string          `json:"type"`                     // "compact_fallback" or "custom_compact"
	RequestModel  string          `json:"request_model"`            // original model requested by the client
	FallbackModel string          `json:"fallback_model,omitempty"` // the model after rewrite, or custom compact model
	Input         json.RawMessage `json:"input"`                    // compact request body
	Output        json.RawMessage `json:"output"`                   // compact response body
	DurationMs    int64           `json:"duration_ms"`
}

// compactLogDirOnce ensures the log directory is created at most once per process.
var compactLogDirOnce sync.Once

// ensureCompactLogDir creates the compact log directory if it does not exist.
// Errors are logged but never returned — callers must not let directory
// creation failures affect the compact flow.
func ensureCompactLogDir() string {
	dir := compactLogDir
	compactLogDirOnce.Do(func() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Warnf("compact trigger-log: failed to create log directory %s: %v", dir, err)
		}
	})
	return dir
}

// writeCompactTriggerLog writes a compact input/output log entry to a file in
// the logs/ directory. It is designed to be called from a goroutine — it never
// panics and all errors are swallowed (logged at warn level) so the compact
// response path is completely unaffected.
func writeCompactTriggerLog(requestModel, fallbackModel string, input, output []byte, duration time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			log.Warnf("compact trigger-log: recovered panic: %v", r)
		}
	}()

	now := time.Now()
	entry := compactLogEntry{
		Timestamp:     now.Format(time.RFC3339Nano),
		Type:          "compact_fallback",
		RequestModel:  requestModel,
		FallbackModel: fallbackModel,
		Input:         normalizeRawJSON(input),
		Output:        normalizeRawJSON(output),
		DurationMs:    duration.Milliseconds(),
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		log.Warnf("compact trigger-log: marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	dir := ensureCompactLogDir()
	filename := compactTriggerLogFilename(now)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, data, compactTriggerLogFileMode); err != nil {
		log.Warnf("compact trigger-log: write error: %v", err)
	}
}

// normalizeRawJSON returns the input as json.RawMessage if it is valid JSON,
// otherwise wraps it in a JSON string so the log entry is always valid JSON.
func normalizeRawJSON(data []byte) json.RawMessage {
	if len(data) == 0 {
		return json.RawMessage(`null`)
	}
	if json.Valid(data) {
		return json.RawMessage(data)
	}
	// Not valid JSON — encode as a JSON string
	escaped, _ := json.Marshal(string(data))
	return json.RawMessage(escaped)
}

// asyncCompactTriggerLog fires writeCompactTriggerLog in a separate goroutine
// if trigger-log is enabled in the config. This is the only entry point the
// Compact handler should call — it checks the config flag, copies the byte
// slices to avoid races with buffer reuse, and launches the goroutine.
func asyncCompactTriggerLog(cfg *sdkconfig.SDKConfig, requestModel, fallbackModel string, input, output []byte, duration time.Duration) {
	if cfg == nil || !cfg.CompactFallback.TriggerLog {
		return
	}
	// Copy slices so the goroutine owns its data — the caller may reuse
	// the original buffers after returning.
	inputCopy := make([]byte, len(input))
	copy(inputCopy, input)
	outputCopy := make([]byte, len(output))
	copy(outputCopy, output)

	go writeCompactTriggerLog(requestModel, fallbackModel, inputCopy, outputCopy, duration)
}

// writeCustomCompactTriggerLog writes a custom compact input/output log entry
// to a file in the logs/ directory. Same safety guarantees as
// writeCompactTriggerLog: never panics, all errors are swallowed.
func writeCustomCompactTriggerLog(requestModel, compactModel string, input, output []byte, duration time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			log.Warnf("custom compact trigger-log: recovered panic: %v", r)
		}
	}()

	now := time.Now()
	entry := compactLogEntry{
		Timestamp:     now.Format(time.RFC3339Nano),
		Type:          "custom_compact",
		RequestModel:  requestModel,
		FallbackModel: compactModel,
		Input:         normalizeRawJSON(input),
		Output:        normalizeRawJSON(output),
		DurationMs:    duration.Milliseconds(),
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		log.Warnf("custom compact trigger-log: marshal error: %v", err)
		return
	}
	data = append(data, '\n')

	dir := ensureCompactLogDir()
	filename := compactTriggerLogFilename(now)
	path := filepath.Join(dir, filename)

	if err := os.WriteFile(path, data, compactTriggerLogFileMode); err != nil {
		log.Warnf("custom compact trigger-log: write error: %v", err)
	}
}

func compactTriggerLogFilename(t time.Time) string {
	return fmt.Sprintf("compact-%s.log", t.Format("2006-01-02T15-04-05.000000000"))
}

// asyncCustomCompactTriggerLog fires writeCustomCompactTriggerLog in a separate
// goroutine if trigger-log is enabled in the custom-compact config. This is the
// entry point the custom compact path should call.
func asyncCustomCompactTriggerLog(cfg *sdkconfig.SDKConfig, requestModel, compactModel string, input, output []byte, duration time.Duration) {
	if cfg == nil || !cfg.CustomCompact.TriggerLog {
		return
	}
	inputCopy := make([]byte, len(input))
	copy(inputCopy, input)
	outputCopy := make([]byte, len(output))
	copy(outputCopy, output)

	go writeCustomCompactTriggerLog(requestModel, compactModel, inputCopy, outputCopy, duration)
}
