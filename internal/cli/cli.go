// Package cli implements the ee-tunnel command-line interface.
//
// Subcommands:
//
//	ee-tunnel pair <refresh-token> [--server URL] [--ollama URL] [--label NAME]
//	ee-tunnel                 (default: connect and run)
//	ee-tunnel status
//	ee-tunnel disconnect
//	ee-tunnel version
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/TOT-Concept/ee-tunnel/internal/auth"
	"github.com/TOT-Concept/ee-tunnel/internal/config"
	"github.com/TOT-Concept/ee-tunnel/internal/framing"
	"github.com/TOT-Concept/ee-tunnel/internal/proxy"
	"github.com/TOT-Concept/ee-tunnel/internal/tunnel"
)

const Version = "0.1.0"

// Run dispatches to the appropriate subcommand. Returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	logger := log.New(stderr, "", log.LstdFlags)

	if len(args) == 0 {
		return cmdConnect(stdout, stderr, logger)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "pair":
		return cmdPair(rest, stdout, stderr)
	case "status":
		return cmdStatus(stdout, stderr)
	case "disconnect":
		return cmdDisconnect(stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "ee-tunnel", Version)
		return 0
	case "help", "--help", "-h":
		printHelp(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		printHelp(stderr)
		return 2
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `ee-tunnel %s — open-source tunnel CLI for Entity Enricher

Source:   https://github.com/TOT-Concept/ee-tunnel
License:  MIT

Usage:
  ee-tunnel pair --server URL                  Pair via the browser (recommended)
  ee-tunnel pair --server URL <refresh-token>  Pair with an existing token
  ee-tunnel                                     Connect and serve (default)
  ee-tunnel status                              Show pairing state
  ee-tunnel disconnect                          Forget local credentials
  ee-tunnel version                             Print version

Pair flags:
  --server URL    Server base URL (e.g. https://entityenricher.ai)
  --ollama URL    Local Ollama base URL (default: http://localhost:11434)
  --label NAME    Label, only used for display in 'status'

Environment:
  EE_TUNNEL_OLLAMA_URL    Override the Ollama URL at connect time
`, Version)
}

func cmdPair(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	server := fs.String("server", "", "Server base URL (e.g. https://entityenricher.ai)")
	ollama := fs.String("ollama", "http://localhost:11434", "Local Ollama base URL")
	label := fs.String("label", "", "Label for this device (display only)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	serverURL := strings.TrimSpace(*server)
	if serverURL == "" {
		fmt.Fprintln(stderr, "pair: --server is required (e.g. --server https://entityenricher.ai)")
		return 2
	}

	c := auth.New(serverURL)

	// Branching pairing modes:
	//   ee-tunnel pair --server URL              → browser device-code flow
	//   ee-tunnel pair --server URL <refresh>    → direct paste of token
	if fs.NArg() == 0 {
		return runDeviceCodePair(c, *ollama, *label, stdout, stderr)
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "pair: too many arguments")
		return 2
	}
	return runManualPair(c, fs.Arg(0), *ollama, *label, stdout, stderr)
}

func runManualPair(c *auth.Client, refreshToken, ollama, label string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := c.Exchange(ctx, refreshToken); err != nil {
		fmt.Fprintf(stderr, "pair: failed to validate refresh token: %v\n", err)
		return 1
	}
	return persistPairing(c, refreshToken, ollama, label, stdout, stderr)
}

func runDeviceCodePair(c *auth.Client, ollama, label string, stdout, stderr io.Writer) int {
	startCtx, startCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startCancel()

	dc, err := c.StartDeviceCode(startCtx, label)
	if err != nil {
		fmt.Fprintf(stderr, "pair: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Open this URL in your browser to confirm pairing:")
	fmt.Fprintln(stdout, "  ", dc.VerificationURIComplete)
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "  Code: %s\n", dc.UserCode)
	fmt.Fprintln(stdout)

	if err := openInBrowser(dc.VerificationURIComplete); err != nil {
		fmt.Fprintln(stdout, "(Could not auto-open the browser. Open the URL above manually.)")
	}

	fmt.Fprintf(stdout, "Waiting for confirmation (expires in %ds)...\n", dc.ExpiresIn)

	interval := time.Duration(dc.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			fmt.Fprintln(stderr, "pair: timed out waiting for confirmation")
			return 1
		}
		time.Sleep(interval)

		pollCtx, pollCancel := context.WithTimeout(context.Background(), 10*time.Second)
		resp, err := c.PollDeviceCode(pollCtx, dc.DeviceCode)
		pollCancel()
		if err != nil {
			// Network blip; keep polling until the deadline.
			fmt.Fprintf(stderr, "  (poll error: %v — retrying)\n", err)
			continue
		}
		switch resp.Status {
		case "pending":
			continue
		case "ok":
			fmt.Fprintln(stdout, "✓ Confirmed.")
			return persistPairing(c, resp.RefreshToken, ollama, label, stdout, stderr)
		case "cancelled":
			fmt.Fprintln(stderr, "pair: the user cancelled the pairing in the browser.")
			return 1
		case "expired":
			fmt.Fprintln(stderr, "pair: the pairing expired. Run 'ee-tunnel pair --server ...' again.")
			return 1
		case "not_found":
			fmt.Fprintln(stderr, "pair: server lost track of the pairing. Try again.")
			return 1
		default:
			fmt.Fprintf(stderr, "pair: unexpected poll status %q\n", resp.Status)
			return 1
		}
	}
}

func persistPairing(c *auth.Client, refreshToken, ollama, label string, stdout, stderr io.Writer) int {
	cfg := &config.Config{
		ServerURL: c.BaseURL,
		Label:     label,
		OllamaURL: ollama,
	}
	if err := config.Save(cfg, refreshToken); err != nil {
		fmt.Fprintf(stderr, "pair: save: %v\n", err)
		return 1
	}
	dir, _ := config.Path()
	fmt.Fprintf(stdout, "✓ Paired with %s\n", c.BaseURL)
	fmt.Fprintf(stdout, "  Credentials stored in %s\n", dir)
	fmt.Fprintf(stdout, "  Run 'ee-tunnel' to connect.\n")
	return 0
}

func cmdStatus(stdout, stderr io.Writer) int {
	cfg, refresh, err := config.Load()
	if errors.Is(err, config.ErrNotPaired) {
		fmt.Fprintln(stdout, "Not paired. Run 'ee-tunnel pair <refresh-token>' to pair this device.")
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "status: %v\n", err)
		return 1
	}
	dir, _ := config.Path()
	fmt.Fprintf(stdout, "ee-tunnel %s\n", Version)
	fmt.Fprintf(stdout, "  state dir   %s\n", dir)
	fmt.Fprintf(stdout, "  server      %s\n", cfg.ServerURL)
	fmt.Fprintf(stdout, "  ollama      %s\n", cfg.OllamaURL)
	if cfg.Label != "" {
		fmt.Fprintf(stdout, "  label       %s\n", cfg.Label)
	}
	fmt.Fprintf(stdout, "  token len   %d (hidden)\n", len(refresh))
	return 0
}

func cmdDisconnect(stdout, stderr io.Writer) int {
	if err := config.Clear(); err != nil {
		fmt.Fprintf(stderr, "disconnect: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "✓ Local credentials removed.")
	fmt.Fprintln(stdout, "  Note: revoke the tunnel server-side via the Entity Enricher UI to fully")
	fmt.Fprintln(stdout, "  disable this device's access. 'disconnect' only forgets local state.")
	return 0
}

// cmdConnect runs the data-plane loop until interrupted.
func cmdConnect(stdout, stderr io.Writer, logger *log.Logger) int {
	cfg, refresh, err := config.Load()
	if errors.Is(err, config.ErrNotPaired) {
		fmt.Fprintln(stderr, "ee-tunnel is not paired on this machine.")
		fmt.Fprintln(stderr, "Run 'ee-tunnel pair <refresh-token>' first.")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "connect: %v\n", err)
		return 1
	}

	ollamaURL := cfg.OllamaURL
	if v := os.Getenv("EE_TUNNEL_OLLAMA_URL"); v != "" {
		ollamaURL = v
	}

	logger.Printf("ee-tunnel %s connecting to %s", Version, cfg.ServerURL)
	logger.Printf("  forwarding requests to %s", ollamaURL)

	// Set up Ctrl+C handling.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := runForever(ctx, cfg, refresh, ollamaURL, logger); err != nil &&
		!errors.Is(err, context.Canceled) {
		logger.Printf("fatal: %v", err)
		return 1
	}
	logger.Println("stopped.")
	return 0
}

// runForever keeps the tunnel up: dial, pump, on disconnect back off and redial.
func runForever(
	ctx context.Context,
	cfg *config.Config,
	refreshToken, ollamaURL string,
	logger *log.Logger,
) error {
	authClient := auth.New(cfg.ServerURL)
	forwarder := proxy.New(ollamaURL, logger)
	backoff := newBackoff()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		connected, err := connectAndPump(ctx, authClient, refreshToken, forwarder, logger)
		if connected {
			// A successful connect resets the backoff so a transient blip
			// after hours of uptime doesn't queue up a 30-second wait.
			backoff.reset()
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		if isPermanent(err) {
			return err
		}
		wait := backoff.next()
		logger.Printf("disconnected: %v — retrying in %s", err, wait)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// connectAndPump returns (connected, err): connected is true once the WS dial
// + hello succeed, allowing the caller to reset its backoff.
func connectAndPump(
	ctx context.Context,
	authClient *auth.Client,
	refreshToken string,
	forwarder *proxy.Forwarder,
	logger *log.Logger,
) (bool, error) {
	exchangeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	access, err := authClient.Exchange(exchangeCtx, refreshToken)
	if err != nil {
		// Distinguish 401 (revoked / rotated) — that's permanent.
		if strings.Contains(err.Error(), "401") {
			return false, permanentError{inner: err}
		}
		return false, err
	}

	conn, err := tunnel.Dial(ctx, authClient.WSURL(), access.AccessToken, logger)
	if err != nil {
		return false, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "client exit")

	logger.Printf("connected ✓ tunnel ready")

	pumpCtx, pumpCancel := context.WithCancel(ctx)
	defer pumpCancel()

	// Pinger lives until the pump exits.
	go func() {
		_ = conn.Pinger(pumpCtx)
	}()

	for {
		frame, err := conn.Recv(pumpCtx)
		if err != nil {
			return true, err
		}
		switch f := frame.(type) {
		case framing.Request:
			go forwarder.Handle(pumpCtx, f, conn)
		case framing.Cancel:
			// We rely on context cancellation only at the connection level for
			// v0.1. Per-request cancellation will be wired in v0.2.
			logger.Printf("cancel %s ignored (per-request cancel not yet implemented)", f.ID)
		case framing.Ping:
			_ = conn.Send(pumpCtx, framing.Pong{})
		case framing.Pong:
			// Heartbeat reply; nothing to do.
		case framing.ErrorFrame:
			logger.Printf("server error %s: %s", f.Code, f.Message)
		default:
			logger.Printf("unexpected frame: %T", frame)
		}
	}
}

// === backoff =================================================================

type backoff struct {
	current time.Duration
	max     time.Duration
}

func newBackoff() *backoff {
	return &backoff{current: 1 * time.Second, max: 30 * time.Second}
}

func (b *backoff) next() time.Duration {
	d := b.current
	b.current *= 2
	if b.current > b.max {
		b.current = b.max
	}
	return d
}

func (b *backoff) reset() {
	b.current = 1 * time.Second
}

// === permanent errors ========================================================

type permanentError struct{ inner error }

func (e permanentError) Error() string { return "permanent: " + e.inner.Error() }
func (e permanentError) Unwrap() error { return e.inner }

func isPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}
