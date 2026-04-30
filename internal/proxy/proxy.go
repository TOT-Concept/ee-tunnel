// Package proxy forwards inbound tunnel requests to a local Ollama instance.
//
// Each Request frame received over the WebSocket is replayed as an HTTP call
// against the configured Ollama base URL (default http://localhost:11434).
// Streaming responses (e.g. /api/chat with stream=true) are chunked into
// ResponseChunk frames so the server-side httpx transport receives them
// incrementally — which preserves SSE/streaming UX for end users.
package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/TOT-Concept/ee-tunnel/internal/framing"
)

const (
	defaultOllamaURL = "http://localhost:11434"
	chunkSize        = 16 * 1024 // 16 KB per ResponseChunk
)

// FrameSender is the minimal interface the proxy uses to push response frames
// back over the multiplexer. It's satisfied by *tunnel.Conn.
type FrameSender interface {
	Send(ctx context.Context, frame framing.Frame) error
}

// Forwarder executes Ollama requests on behalf of the server.
type Forwarder struct {
	OllamaBaseURL string
	HTTP          *http.Client
	Logger        *log.Logger
}

func New(ollamaURL string, logger *log.Logger) *Forwarder {
	if ollamaURL == "" {
		ollamaURL = defaultOllamaURL
	}
	return &Forwarder{
		OllamaBaseURL: strings.TrimRight(ollamaURL, "/"),
		HTTP: &http.Client{
			// No global timeout: streaming responses can run for minutes.
			// We rely on context cancellation from the outer tunnel pump.
			Timeout: 0,
		},
		Logger: logger,
	}
}

// Handle executes one Request frame and emits the matching ResponseStart /
// ResponseChunk / ResponseEnd (or Error) frames via sender.
func (f *Forwarder) Handle(ctx context.Context, req framing.Request, sender FrameSender) {
	if err := f.handle(ctx, req, sender); err != nil {
		_ = sender.Send(ctx, framing.ErrorFrame{
			ID:      req.ID,
			Code:    "forward_failed",
			Message: err.Error(),
		})
	}
}

func (f *Forwarder) handle(ctx context.Context, req framing.Request, sender FrameSender) error {
	body, err := base64.StdEncoding.DecodeString(req.BodyB64)
	if err != nil {
		return fmt.Errorf("decode body_b64: %w", err)
	}

	// Path on the Request frame is the full path+query as seen by the server.
	url := f.OllamaBaseURL + req.Path
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	for k, v := range req.Headers {
		// Skip Host — Go computes it from the URL automatically.
		if strings.EqualFold(k, "host") {
			continue
		}
		httpReq.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := f.HTTP.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama call: %w", err)
	}
	defer resp.Body.Close()

	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	if err := sender.Send(ctx, framing.ResponseStart{
		ID:      req.ID,
		Status:  resp.StatusCode,
		Headers: headers,
	}); err != nil {
		return fmt.Errorf("send response_start: %w", err)
	}

	buf := make([]byte, chunkSize)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			chunk := framing.ResponseChunk{
				ID:      req.ID,
				DataB64: base64.StdEncoding.EncodeToString(buf[:n]),
			}
			if err := sender.Send(ctx, chunk); err != nil {
				return fmt.Errorf("send chunk: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read body: %w", readErr)
		}
	}

	if err := sender.Send(ctx, framing.ResponseEnd{ID: req.ID}); err != nil {
		return fmt.Errorf("send response_end: %w", err)
	}

	if f.Logger != nil {
		f.Logger.Printf("forwarded %s %s -> %d (%s)",
			req.Method, req.Path, resp.StatusCode, time.Since(start).Round(time.Millisecond))
	}
	return nil
}
