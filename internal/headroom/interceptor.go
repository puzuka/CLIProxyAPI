// Package headroom provides integration with Headroom context compression proxy.
package headroom

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	log "github.com/sirupsen/logrus"
)

// Interceptor wraps auth manager execution and routes requests through Headroom
// when configured.
type Interceptor struct {
	cfg     *config.Config
	manager *cliproxyauth.Manager
}

// NewInterceptor creates a new Headroom interceptor.
func NewInterceptor(cfg *config.Config, manager *cliproxyauth.Manager) *Interceptor {
	return &Interceptor{
		cfg:     cfg,
		manager: manager,
	}
}

// Execute routes the request through Headroom if configured, otherwise falls back
// to the standard manager execution path.
func (i *Interceptor) Execute(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	if i.shouldRouteThroughHeadroom(req.Model) {
		return i.executeViaHeadroom(ctx, providers, req, opts)
	}
	return i.manager.Execute(ctx, providers, req, opts)
}

// ExecuteStream routes streaming requests through Headroom if configured.
func (i *Interceptor) ExecuteStream(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	if i.shouldRouteThroughHeadroom(req.Model) {
		return i.executeStreamViaHeadroom(ctx, providers, req, opts)
	}
	return i.manager.ExecuteStream(ctx, providers, req, opts)
}

// shouldRouteThroughHeadroom checks if the request should go through Headroom.
func (i *Interceptor) shouldRouteThroughHeadroom(model string) bool {
	return i.cfg.Headroom.ShouldRouteModel(model)
}

// executeViaHeadroom routes a non-streaming request through Headroom proxy.
func (i *Interceptor) executeViaHeadroom(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	log.WithFields(log.Fields{
		"model":    req.Model,
		"endpoint": i.cfg.Headroom.Endpoint,
	}).Debug("Routing request through Headroom context compression")

	// Create an HTTP client with appropriate timeout
	client := &http.Client{
		Timeout: time.Duration(i.cfg.Headroom.Timeout) * time.Second,
	}

	// Construct the Headroom proxy URL
	headroomURL, err := url.Parse(i.cfg.Headroom.Endpoint)
	if err != nil {
		log.WithError(err).Error("Invalid Headroom endpoint URL")
		return cliproxyexecutor.Response{}, fmt.Errorf("invalid headroom endpoint: %w", err)
	}

	// Headroom proxy is OpenAI-compatible, so we forward to /v1/chat/completions
	headroomURL.Path = "/v1/chat/completions"

	// Build the HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", headroomURL.String(), nil)
	if err != nil {
		return cliproxyexecutor.Response{}, fmt.Errorf("create headroom request: %w", err)
	}

	// Set headers - Headroom needs the API key to forward to the actual provider
	httpReq.Header.Set("Content-Type", "application/json")

	// Copy original headers if present
	if opts.Headers != nil {
		for k, v := range opts.Headers {
			httpReq.Header[k] = v
		}
	}

	// Set the request body from the payload
	httpReq.Body = http.NoBody
	if len(req.Payload) > 0 {
		httpReq.Body = &bytesReadCloser{bytes: req.Payload}
		httpReq.ContentLength = int64(len(req.Payload))
	}

	// Execute the request
	resp, err := client.Do(httpReq)
	if err != nil {
		log.WithError(err).Error("Headroom proxy request failed")
		return cliproxyexecutor.Response{}, fmt.Errorf("headroom request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	var buf []byte
	if resp.ContentLength > 0 {
		buf = make([]byte, resp.ContentLength)
		_, err = resp.Body.Read(buf)
	} else {
		buf, err = io.ReadAll(resp.Body)
	}
	if err != nil && err != io.EOF {
		return cliproxyexecutor.Response{}, fmt.Errorf("read headroom response: %w", err)
	}

	// Check for HTTP errors
	if resp.StatusCode >= 400 {
		log.WithFields(log.Fields{
			"status": resp.StatusCode,
			"body":   string(buf),
		}).Error("Headroom proxy returned error")
		return cliproxyexecutor.Response{}, &headroomError{
			statusCode: resp.StatusCode,
			message:    string(buf),
		}
	}

	return cliproxyexecutor.Response{
		Payload: buf,
		Headers: resp.Header,
	}, nil
}

// executeStreamViaHeadroom routes a streaming request through Headroom proxy.
func (i *Interceptor) executeStreamViaHeadroom(ctx context.Context, providers []string, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	log.WithFields(log.Fields{
		"model":    req.Model,
		"endpoint": i.cfg.Headroom.Endpoint,
	}).Debug("Routing streaming request through Headroom context compression")

	// For now, fall back to standard execution for streaming
	// A full implementation would need to handle SSE streaming through Headroom
	return i.manager.ExecuteStream(ctx, providers, req, opts)
}

// bytesReadCloser wraps a byte slice as an io.ReadCloser
type bytesReadCloser struct {
	bytes []byte
	pos   int
}

func (b *bytesReadCloser) Read(p []byte) (int, error) {
	if b.pos >= len(b.bytes) {
		return 0, io.EOF
	}
	n := copy(p, b.bytes[b.pos:])
	b.pos += n
	return n, nil
}

func (b *bytesReadCloser) Close() error {
	return nil
}

// headroomError implements StatusError interface
type headroomError struct {
	statusCode int
	message    string
}

func (e *headroomError) Error() string {
	return fmt.Sprintf("headroom proxy error (status %d): %s", e.statusCode, e.message)
}

func (e *headroomError) StatusCode() int {
	return e.statusCode
}
