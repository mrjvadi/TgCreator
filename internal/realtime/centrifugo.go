// Package realtime pushes live events to the web panel through Centrifugo.
//
// The Go server publishes with Centrifugo's HTTP API; browsers connect to
// Centrifugo over WebSocket with short-lived JWTs issued by the server:
// a connection token, plus a subscription token per private channel.
package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Config struct {
	APIURL string // e.g. http://centrifugo:8000/api
	APIKey string // http_api.key
	Secret string // client.token.hmac_secret_key
	WSURL  string // public URL for browsers, e.g. ws://localhost:8000/connection/websocket
}

type Centrifugo struct {
	cfg Config
	hc  *http.Client
}

// New returns nil when Centrifugo is not configured; methods on a nil
// *Centrifugo are no-ops so callers need no checks.
func New(cfg Config) *Centrifugo {
	if cfg.APIURL == "" || cfg.Secret == "" {
		return nil
	}
	cfg.APIURL = strings.TrimRight(cfg.APIURL, "/")
	return &Centrifugo{cfg: cfg, hc: &http.Client{Timeout: 5 * time.Second}}
}

// Enabled reports whether realtime delivery is configured.
func (c *Centrifugo) Enabled() bool { return c != nil }

// WSURL is the WebSocket endpoint browsers connect to.
func (c *Centrifugo) WSURL() string {
	if c == nil {
		return ""
	}
	return c.cfg.WSURL
}

// Publish sends data to every subscriber of channel.
func (c *Centrifugo) Publish(ctx context.Context, channel string, data any) error {
	if c == nil {
		return nil
	}
	body, err := json.Marshal(map[string]any{"channel": channel, "data": data})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIURL+"/publish", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.cfg.APIKey)
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("centrifugo publish: %s: %s", resp.Status, raw)
	}
	var r struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &r) == nil && r.Error != nil {
		return fmt.Errorf("centrifugo publish: %d %s", r.Error.Code, r.Error.Message)
	}
	return nil
}

// ConnectionToken authorizes a WebSocket connection for user.
func (c *Centrifugo) ConnectionToken(user string, ttl time.Duration) (string, error) {
	if c == nil {
		return "", errors.New("realtime disabled")
	}
	return c.sign(jwt.MapClaims{"sub": user, "exp": time.Now().Add(ttl).Unix()})
}

// SubscriptionToken authorizes user to subscribe to one channel.
func (c *Centrifugo) SubscriptionToken(user, channel string, ttl time.Duration) (string, error) {
	if c == nil {
		return "", errors.New("realtime disabled")
	}
	return c.sign(jwt.MapClaims{"sub": user, "channel": channel, "exp": time.Now().Add(ttl).Unix()})
}

func (c *Centrifugo) sign(claims jwt.MapClaims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(c.cfg.Secret))
}
