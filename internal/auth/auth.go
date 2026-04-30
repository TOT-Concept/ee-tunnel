// Package auth handles communication with the Entity Enricher tunnel API.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to /api/tunnel/* endpoints on the Entity Enricher server.
type Client struct {
	BaseURL string // e.g. https://entityenricher.ai or http://localhost:18808
	HTTP    *http.Client
}

// New constructs a Client. baseURL must NOT include a trailing slash.
// httpScheme should be set if the input baseURL starts with ws:// or wss://
// (the WSS server URL returned by /api/org-keys/tunnels), in which case it
// will be rewritten to http/https.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: normalizeBaseURL(baseURL),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// normalizeBaseURL converts ws://host → http://host, wss://host → https://host,
// and strips trailing slashes. If the URL has neither a ws nor http scheme, it
// is returned unchanged.
func normalizeBaseURL(raw string) string {
	raw = strings.TrimRight(raw, "/")
	switch {
	case strings.HasPrefix(raw, "wss://"):
		return "https://" + raw[len("wss://"):]
	case strings.HasPrefix(raw, "ws://"):
		return "http://" + raw[len("ws://"):]
	default:
		return raw
	}
}

// WSURL returns the WebSocket URL for the tunnel data plane, derived from BaseURL.
func (c *Client) WSURL() string {
	switch {
	case strings.HasPrefix(c.BaseURL, "https://"):
		return "wss://" + c.BaseURL[len("https://"):] + "/api/tunnel/ws"
	case strings.HasPrefix(c.BaseURL, "http://"):
		return "ws://" + c.BaseURL[len("http://"):] + "/api/tunnel/ws"
	default:
		return c.BaseURL + "/api/tunnel/ws"
	}
}

type ExchangeResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// Exchange swaps a refresh token for a short-lived access token.
// Calls POST /api/tunnel/exchange?refresh_token=...
func (c *Client) Exchange(ctx context.Context, refreshToken string) (*ExchangeResponse, error) {
	u := fmt.Sprintf("%s/api/tunnel/exchange?refresh_token=%s",
		c.BaseURL, url.QueryEscape(refreshToken))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("exchange: server returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out ExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("exchange: decode response: %w", err)
	}
	return &out, nil
}

// === Device-code (RFC 8628-style) pairing ==================================

type DeviceCodeResponse struct {
	DeviceCode             string `json:"device_code"`
	UserCode               string `json:"user_code"`
	VerificationURI        string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn              int    `json:"expires_in"`
	Interval               int    `json:"interval"`
}

type DeviceCodePollResponse struct {
	Status       string `json:"status"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ServerURL    string `json:"server_url,omitempty"`
	TunnelID     string `json:"tunnel_id,omitempty"`
}

// StartDeviceCode initiates a device-code pairing.
func (c *Client) StartDeviceCode(ctx context.Context, labelHint string) (*DeviceCodeResponse, error) {
	body, _ := json.Marshal(map[string]any{"label_hint": labelHint})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/api/tunnel/device-code", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device-code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device-code: server returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("device-code: decode: %w", err)
	}
	return &out, nil
}

// PollDeviceCode polls a pending pairing once. Returns the parsed response;
// callers branch on resp.Status (one of "pending", "ok", "expired",
// "cancelled", "not_found").
func (c *Client) PollDeviceCode(ctx context.Context, deviceCode string) (*DeviceCodePollResponse, error) {
	body, _ := json.Marshal(map[string]string{"device_code": deviceCode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/api/tunnel/poll", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("poll: server returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out DeviceCodePollResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("poll: decode: %w", err)
	}
	return &out, nil
}
