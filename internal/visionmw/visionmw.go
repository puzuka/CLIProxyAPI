// Package visionmw provides a Gin middleware that gives text-only models
// transparent vision support.
//
// For every POST /v1/chat/completions, /v1/responses or /v1/messages request
// containing image content, the middleware:
//
//  1. Decompresses the body (gzip / brotli / zstd are supported, identity is
//     passed through).
//  2. Calls a vision-capable model (default: kimi-k2.6) to describe each
//     image. Descriptions are cached on disk by sha256 of the image data URI
//     so identical images in follow-up turns are not described again.
//  3. Replaces every image part in the request with a text part containing
//     "[Image Description: …]" and forwards the rewritten body upstream.
//     The original "model" field is preserved — the user-chosen model
//     (e.g. deepseek-v4-pro) keeps answering normally, just on text input.
//
// Caption calls are issued as HTTP loopback to the same server. They carry
// the X-Vision-Internal: 1 header which the middleware recognises and skips,
// so they cannot recurse.
//
// Configuration (env vars, all optional):
//
//	CAPTION_MODEL         default kimi-k2.6
//	CAPTION_API_BASE      default http://<host>:<port>/v1 (this server)
//	CAPTION_API_KEY       default first proxy api-key from config
//	CAPTION_PROMPT        default: built-in coding-agent prompt
//	CAPTION_MAX_TOKENS    default 800
//	CAPTION_TIMEOUT       default 90s
//	CAPTION_CACHE_DIR     default $TMPDIR/cli-proxy-api-vision-cache
//	CAPTION_CONCURRENCY   default 4
//	VISION_DISABLE        when "1", middleware is bypassed entirely
package visionmw

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	log "github.com/sirupsen/logrus"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

const (
	internalHeader = "X-Vision-Internal"
	internalValue  = "1"
)

const defaultPrompt = "You are an image-description assistant for a coding agent. " +
	"Describe the image as concretely and faithfully as possible: " +
	"identify text content (transcribe verbatim when readable), UI elements, " +
	"diagrams, code, charts, screenshots, error messages, and spatial layout. " +
	"Avoid speculation; if something is unclear, say so. " +
	"Be thorough but use plain prose; no markdown code fences."

// New returns a Gin middleware configured from cfg + env vars.
// When VISION_DISABLE=1 the returned middleware is a no-op.
func New(cfg *config.Config) gin.HandlerFunc {
	if os.Getenv("VISION_DISABLE") == "1" {
		log.Info("[vision] disabled via VISION_DISABLE=1")
		return func(c *gin.Context) { c.Next() }
	}
	cc := loadConfig(cfg)
	log.Infof("[vision] enabled: model=%s base=%s prompt=%dB max_tokens=%d cache=%s",
		cc.Model, cc.UpstreamBase, len(cc.Prompt), cc.MaxTokens, cc.CacheDir)

	visionPaths := map[string]bool{
		"/v1/chat/completions": true,
		"/v1/responses":        true,
		"/v1/messages":         true,
	}

	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		// Loopback caption calls carry this header; never recurse.
		if c.GetHeader(internalHeader) == internalValue {
			c.Next()
			return
		}
		if !visionPaths[c.Request.URL.Path] {
			c.Next()
			return
		}

		body, err := io.ReadAll(io.LimitReader(c.Request.Body, 50<<20))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "read body: " + err.Error()})
			return
		}
		_ = c.Request.Body.Close()

		ce := c.GetHeader("Content-Encoding")
		decoded, derr := decompress(body, ce)
		if derr != nil {
			log.Warnf("[vision] decompress(%s) on %s failed: %v — passthrough",
				ce, c.Request.URL.Path, derr)
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Request.ContentLength = int64(len(body))
			c.Next()
			return
		}

		var doc map[string]any
		if err := json.Unmarshal(decoded, &doc); err != nil {
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Request.ContentLength = int64(len(body))
			c.Next()
			return
		}

		refs := collectImages(doc)
		if len(refs) == 0 {
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Request.ContentLength = int64(len(body))
			c.Next()
			return
		}

		cached, described := 0, 0
		for _, r := range refs {
			if _, ok := readCache(cc.CacheDir, r.hash); ok {
				cached++
			} else {
				described++
			}
		}

		describeAll(c.Request.Context(), refs, cc)

		newBody, jerr := json.Marshal(doc)
		if jerr != nil {
			log.Warnf("[vision] re-marshal failed on %s: %v — passthrough",
				c.Request.URL.Path, jerr)
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			c.Request.ContentLength = int64(len(body))
			c.Next()
			return
		}

		log.Infof("[vision] %s images=%d cache_hits=%d (decoded %d -> forwarded %d, encoding=%q)",
			c.Request.URL.Path, cached+described, cached, len(decoded), len(newBody), ce)

		c.Request.Header.Del("Content-Encoding")
		c.Request.Body = io.NopCloser(bytes.NewReader(newBody))
		c.Request.ContentLength = int64(len(newBody))
		c.Request.Header.Set("Content-Length", strconv.Itoa(len(newBody)))
		c.Next()
	}
}

// captionConfig groups everything the caption pipeline needs.
type captionConfig struct {
	UpstreamBase string
	Model        string
	APIKey       string
	Prompt       string
	MaxTokens    int
	CacheDir     string
	Timeout      time.Duration
	Concurrency  int
}

func loadConfig(cfg *config.Config) captionConfig {
	host := cfg.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	defaultBase := "http://" + host + ":" + strconv.Itoa(cfg.Port) + "/v1"

	apiKey := os.Getenv("CAPTION_API_KEY")
	if apiKey == "" && len(cfg.APIKeys) > 0 {
		apiKey = cfg.APIKeys[0]
	}

	timeout, perr := time.ParseDuration(envOr("CAPTION_TIMEOUT", "90s"))
	if perr != nil || timeout <= 0 {
		timeout = 90 * time.Second
	}

	return captionConfig{
		UpstreamBase: envOr("CAPTION_API_BASE", defaultBase),
		Model:        envOr("CAPTION_MODEL", "kimi-k2.6"),
		APIKey:       apiKey,
		Prompt:       envOr("CAPTION_PROMPT", defaultPrompt),
		MaxTokens:    atoi(os.Getenv("CAPTION_MAX_TOKENS"), 800),
		CacheDir:     envOr("CAPTION_CACHE_DIR", filepath.Join(os.TempDir(), "cli-proxy-api-vision-cache")),
		Timeout:      timeout,
		Concurrency:  atoi(os.Getenv("CAPTION_CONCURRENCY"), 4),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func atoi(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// decompress decodes a request body according to the Content-Encoding header.
// Supported: identity (empty), gzip, br (brotli), zstd. Unknown encodings
// return the original bytes and no error so the caller can forward as-is.
func decompress(body []byte, enc string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "", "identity":
		return body, nil
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		return io.ReadAll(gr)
	case "br":
		return io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
	case "zstd":
		zr, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	default:
		return body, nil
	}
}

// =============================================================================
// Image scanning + caption pipeline
// =============================================================================

// imgRef points at a single image part inside a parsed JSON document so we
// can mutate it in place after captioning.
//
// Three shapes are supported:
//
//  1. OpenAI Responses API (Codex CLI):
//     {"input":[ {"role":"user","content":[
//        {"type":"input_image","image_url":"data:image/...","detail":"high"}
//     ]} ]}
//
//  2. OpenAI chat/completions:
//     {"messages":[ {"role":"user","content":[
//        {"type":"image_url","image_url":{"url":"data:image/..."}}
//     ]} ]}
//
//  3. Anthropic messages:
//     {"messages":[ {"role":"user","content":[
//        {"type":"image","source":{"type":"base64","data":"...","media_type":"image/png"}}
//     ]} ]}
type imgRef struct {
	parent  map[string]any
	dataURI string
	hash    string
}

func collectImages(doc map[string]any) []*imgRef {
	var out []*imgRef
	visitArr := func(arr []any) {
		for _, m := range arr {
			mm, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if uri := imageDataURI(mm); uri != "" {
				out = append(out, &imgRef{parent: mm, dataURI: uri})
			}
			if c, ok := mm["content"].([]any); ok {
				for _, p := range c {
					pp, ok := p.(map[string]any)
					if !ok {
						continue
					}
					if uri := imageDataURI(pp); uri != "" {
						out = append(out, &imgRef{parent: pp, dataURI: uri})
					}
				}
			}
		}
	}
	if a, ok := doc["messages"].([]any); ok {
		visitArr(a)
	}
	if a, ok := doc["input"].([]any); ok {
		visitArr(a)
	}
	for _, ref := range out {
		sum := sha256.Sum256([]byte(ref.dataURI))
		ref.hash = hex.EncodeToString(sum[:])
	}
	return out
}

func imageDataURI(part map[string]any) string {
	t, _ := part["type"].(string)
	switch t {
	case "input_image":
		if s, ok := part["image_url"].(string); ok && s != "" {
			return s
		}
		if obj, ok := part["image_url"].(map[string]any); ok {
			if u, _ := obj["url"].(string); u != "" {
				return u
			}
		}
	case "image_url":
		if obj, ok := part["image_url"].(map[string]any); ok {
			if u, _ := obj["url"].(string); u != "" {
				return u
			}
		}
		if s, ok := part["image_url"].(string); ok && s != "" {
			return s
		}
	case "image":
		if src, ok := part["source"].(map[string]any); ok {
			if u, _ := src["url"].(string); u != "" {
				return u
			}
			if data, _ := src["data"].(string); data != "" {
				media, _ := src["media_type"].(string)
				if media == "" {
					media = "image/png"
				}
				return "data:" + media + ";base64," + data
			}
		}
	}
	return ""
}

func replaceWithCaption(part map[string]any, description string) {
	t, _ := part["type"].(string)
	delete(part, "image_url")
	delete(part, "source")
	delete(part, "detail")
	switch t {
	case "input_image":
		part["type"] = "input_text"
		part["text"] = description
	case "image":
		part["type"] = "text"
		part["text"] = description
	default:
		part["type"] = "text"
		part["text"] = description
	}
}

// describeAll captions every image referenced in refs, mutating their parent
// JSON nodes in place. Cached descriptions are reused. Errors for individual
// images become placeholder text so the upstream model still receives valid
// content.
func describeAll(ctx context.Context, refs []*imgRef, cc captionConfig) {
	if len(refs) == 0 {
		return
	}
	type result struct {
		idx  int
		text string
		err  error
	}
	conc := cc.Concurrency
	if conc <= 0 {
		conc = 4
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	results := make([]result, len(refs))
	for i, r := range refs {
		i, r := i, r
		if cached, ok := readCache(cc.CacheDir, r.hash); ok {
			results[i] = result{idx: i, text: cached}
			log.Debugf("[vision] cache hit %s (%d bytes desc)", r.hash[:12], len(cached))
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			text, err := callVision(ctx, r.dataURI, cc)
			if err != nil {
				log.Warnf("[vision] describe %s failed in %s: %v",
					r.hash[:12], time.Since(start), err)
				results[i] = result{idx: i, err: err}
				return
			}
			log.Infof("[vision] described %s in %s (%d chars)",
				r.hash[:12], time.Since(start), len(text))
			results[i] = result{idx: i, text: text}
			writeCache(cc.CacheDir, r.hash, text)
		}()
	}
	wg.Wait()

	for i, r := range refs {
		res := results[i]
		var desc string
		if res.err != nil {
			desc = fmt.Sprintf("[Image Description Unavailable: %v]", sanitizeErr(res.err))
		} else {
			desc = "[Image Description: " + strings.TrimSpace(res.text) + "]"
		}
		replaceWithCaption(r.parent, desc)
	}
}

func sanitizeErr(err error) string {
	s := err.Error()
	if len(s) > 240 {
		s = s[:240] + "..."
	}
	return s
}

// callVision posts a chat/completions describe call to the loopback URL,
// authenticated with the proxy API key, marked with internalHeader so the
// in-process vision middleware passes it through without re-captioning.
func callVision(ctx context.Context, dataURI string, cc captionConfig) (string, error) {
	if cc.UpstreamBase == "" || cc.Model == "" {
		return "", errors.New("vision config not set")
	}
	body := map[string]any{
		"model":      cc.Model,
		"max_tokens": cc.MaxTokens,
		"stream":     false,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": cc.Prompt},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": dataURI, "detail": "high"},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	url := strings.TrimRight(cc.UpstreamBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if cc.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cc.APIKey)
	}
	req.Header.Set(internalHeader, internalValue)

	cli := &http.Client{Timeout: cc.Timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(respBody), 400))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content   any    `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w (raw=%s)", err, truncate(string(respBody), 300))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("empty choices: %s", truncate(string(respBody), 300))
	}
	c := parsed.Choices[0].Message.Content
	text := flattenContent(c)
	if strings.TrimSpace(text) == "" {
		text = parsed.Choices[0].Message.Reasoning
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("empty content: %s", truncate(string(respBody), 300))
	}
	return text, nil
}

func flattenContent(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var b strings.Builder
		for _, p := range x {
			pp, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := pp["text"].(string); t != "" {
				b.WriteString(t)
			}
		}
		return b.String()
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...<truncated>"
}

// --- file cache --------------------------------------------------------------

func cachePath(dir, hash string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, hash+".txt")
}

func readCache(dir, hash string) (string, bool) {
	p := cachePath(dir, hash)
	if p == "" {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func writeCache(dir, hash, value string) {
	p := cachePath(dir, hash)
	if p == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o700)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(value), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}
