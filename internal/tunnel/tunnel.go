// Package tunnel manages the WebSocket data plane: dial, hello, frame pump,
// reconnect with exponential backoff.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/TOT-Concept/ee-tunnel/internal/framing"
)

const (
	agentName       = "ee-tunnel/0.1.0"
	maxFrameSize    = 64 * 1024 * 1024 // must accept large response chunks for big bodies
	pingInterval    = 30 * time.Second
	dialTimeout     = 15 * time.Second
)

// Conn wraps a single connected WebSocket. Callers send frames via Send() and
// read frames via Recv(). The connection is single-reader, multi-writer
// internally — Send is goroutine-safe via a mutex.
type Conn struct {
	ws  *websocket.Conn
	mu  sync.Mutex
	log *log.Logger
}

// Dial establishes the WSS connection, sends a Hello frame, and returns a Conn
// ready for use. The access token is sent in the Authorization header.
func Dial(ctx context.Context, wsURL, accessToken string, logger *log.Logger) (*Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("User-Agent", agentName)

	ws, _, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("ws dial %s: %w", wsURL, err)
	}
	ws.SetReadLimit(maxFrameSize)

	c := &Conn{ws: ws, log: logger}

	hello := framing.Hello{
		Version: framing.ProtocolVersion,
		Agent:   agentName,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
	}
	if err := c.Send(ctx, hello); err != nil {
		_ = ws.Close(websocket.StatusInternalError, "hello failed")
		return nil, fmt.Errorf("send hello: %w", err)
	}
	return c, nil
}

// Send marshals and writes a frame. Goroutine-safe.
func (c *Conn) Send(ctx context.Context, f framing.Frame) error {
	data, err := framing.Encode(f)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("ws write: %w", err)
	}
	return nil
}

// Recv reads one frame. Should be called from a single goroutine.
func (c *Conn) Recv(ctx context.Context) (framing.Frame, error) {
	mt, data, err := c.ws.Read(ctx)
	if err != nil {
		return nil, err
	}
	if mt != websocket.MessageText {
		return nil, fmt.Errorf("unexpected ws message type %v", mt)
	}
	return framing.Decode(data)
}

// Close shuts down the WebSocket cleanly.
func (c *Conn) Close(code websocket.StatusCode, reason string) error {
	return c.ws.Close(code, reason)
}

// Pinger fires periodic Ping frames to keep the connection alive through proxies.
// Run in its own goroutine; exits when ctx is cancelled or the send fails.
func (c *Conn) Pinger(ctx context.Context) error {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := c.Send(ctx, framing.Ping{}); err != nil {
				return err
			}
		}
	}
}

// IsClosed returns true for errors that mean "the connection is gone, please
// reconnect" rather than "this single op failed".
func IsClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	var ce websocket.CloseError
	return errors.As(err, &ce)
}
