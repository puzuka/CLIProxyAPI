package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/interfaces"
)

// SetSSEHeaders applies common response headers for server-sent event streams.
func SetSSEHeaders(c *gin.Context) {
	if c == nil {
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("X-Accel-Buffering", "no")
}

type StreamForwardOptions struct {
	// KeepAliveInterval overrides the configured streaming keep-alive interval.
	// If nil, the configured default is used. If set to <= 0, keep-alives are disabled.
	KeepAliveInterval *time.Duration

	// WriteChunk writes a single data chunk to the response body. It should not flush.
	WriteChunk func(chunk []byte)

	// WriteTerminalError writes an error payload to the response body when streaming fails
	// after headers have already been committed. It should not flush.
	WriteTerminalError func(errMsg *interfaces.ErrorMessage)

	// WriteDone optionally writes a terminal marker when the upstream data channel closes
	// without an error (e.g. OpenAI's `[DONE]`). It should not flush.
	WriteDone func()

	// WriteKeepAlive optionally writes a keep-alive heartbeat. It should not flush.
	// When nil, a standard SSE comment heartbeat is used.
	WriteKeepAlive func()
}

// StreamBootstrapKeepAlive commits SSE headers and heartbeats while a streaming
// request is waiting for the first upstream payload.
type StreamBootstrapKeepAlive struct {
	c              *gin.Context
	flusher        http.Flusher
	ticker         *time.Ticker
	tickerC        <-chan time.Time
	committed      bool
	writeHeaders   func()
	writeKeepAlive func()
}

func (h *BaseAPIHandler) NewStreamBootstrapKeepAlive(c *gin.Context, flusher http.Flusher, writeHeaders func(), writeKeepAlive func()) *StreamBootstrapKeepAlive {
	interval := time.Duration(0)
	if h != nil {
		interval = StreamingKeepAliveInterval(h.Cfg)
	}
	return newStreamBootstrapKeepAlive(c, flusher, interval, writeHeaders, writeKeepAlive)
}

func newStreamBootstrapKeepAlive(c *gin.Context, flusher http.Flusher, interval time.Duration, writeHeaders func(), writeKeepAlive func()) *StreamBootstrapKeepAlive {
	if writeHeaders == nil {
		writeHeaders = func() {}
	}
	if writeKeepAlive == nil {
		writeKeepAlive = func() {
			if c != nil {
				_, _ = c.Writer.Write([]byte(": keep-alive\n\n"))
			}
		}
	}
	b := &StreamBootstrapKeepAlive{
		c:              c,
		flusher:        flusher,
		writeHeaders:   writeHeaders,
		writeKeepAlive: writeKeepAlive,
	}
	if interval > 0 && c != nil && flusher != nil {
		b.ticker = time.NewTicker(interval)
		b.tickerC = b.ticker.C
	}
	return b
}

func (b *StreamBootstrapKeepAlive) C() <-chan time.Time {
	if b == nil {
		return nil
	}
	return b.tickerC
}

func (b *StreamBootstrapKeepAlive) Committed() bool {
	return b != nil && b.committed
}

func (b *StreamBootstrapKeepAlive) Commit() {
	if b == nil || b.committed {
		return
	}
	b.writeHeaders()
	b.committed = true
}

func (b *StreamBootstrapKeepAlive) WriteKeepAlive() {
	if b == nil {
		return
	}
	b.Commit()
	b.writeKeepAlive()
	if b.flusher != nil {
		b.flusher.Flush()
	}
}

func (b *StreamBootstrapKeepAlive) Stop() {
	if b == nil || b.ticker == nil {
		return
	}
	b.ticker.Stop()
	b.ticker = nil
	b.tickerC = nil
}

func (h *BaseAPIHandler) ForwardStream(c *gin.Context, flusher http.Flusher, cancel func(error), data <-chan []byte, errs <-chan *interfaces.ErrorMessage, opts StreamForwardOptions) {
	if c == nil {
		return
	}
	if cancel == nil {
		return
	}

	writeChunk := opts.WriteChunk
	if writeChunk == nil {
		writeChunk = func([]byte) {}
	}

	writeKeepAlive := opts.WriteKeepAlive
	if writeKeepAlive == nil {
		writeKeepAlive = func() {
			_, _ = c.Writer.Write([]byte(": keep-alive\n\n"))
		}
	}

	keepAliveInterval := StreamingKeepAliveInterval(h.Cfg)
	if opts.KeepAliveInterval != nil {
		keepAliveInterval = *opts.KeepAliveInterval
	}
	var keepAlive *time.Ticker
	var keepAliveC <-chan time.Time
	if keepAliveInterval > 0 {
		keepAlive = time.NewTicker(keepAliveInterval)
		defer keepAlive.Stop()
		keepAliveC = keepAlive.C
	}

	var terminalErr *interfaces.ErrorMessage
	for {
		select {
		case <-c.Request.Context().Done():
			cancel(c.Request.Context().Err())
			return
		case chunk, ok := <-data:
			if !ok {
				// Prefer surfacing a terminal error if one is pending.
				if terminalErr == nil {
					select {
					case errMsg, ok := <-errs:
						if ok && errMsg != nil {
							terminalErr = errMsg
						}
					default:
					}
				}
				if terminalErr != nil {
					if opts.WriteTerminalError != nil {
						opts.WriteTerminalError(terminalErr)
					}
					flusher.Flush()
					cancel(terminalErr.Error)
					return
				}
				if opts.WriteDone != nil {
					opts.WriteDone()
				}
				flusher.Flush()
				cancel(nil)
				return
			}
			writeChunk(chunk)
			flusher.Flush()
		case errMsg, ok := <-errs:
			if !ok {
				continue
			}
			if errMsg != nil {
				terminalErr = errMsg
				if opts.WriteTerminalError != nil {
					opts.WriteTerminalError(errMsg)
					flusher.Flush()
				}
			}
			var execErr error
			if errMsg != nil {
				execErr = errMsg.Error
			}
			cancel(execErr)
			return
		case <-keepAliveC:
			writeKeepAlive()
			flusher.Flush()
		}
	}
}
