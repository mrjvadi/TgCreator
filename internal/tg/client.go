// Package tg is a small, generic Telegram Bot API client. It does not model
// individual methods: any method and any parameter the Bot API accepts (now
// or in the future) can be called, which is how the runtime supports every
// Telegram feature without code changes.
package tg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/tmpl"
)

// FilePrefix marks a parameter value as a local file to upload,
// e.g. "file:///data/logo.png".
const FilePrefix = "file://"

type Client struct {
	base string
	hc   *http.Client
}

type APIError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

type response struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// New creates a client. apiURL may point to a self-hosted Bot API server.
func New(token, apiURL string) *Client {
	if apiURL == "" {
		apiURL = "https://api.telegram.org"
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 256,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	return &Client{
		base: strings.TrimRight(apiURL, "/") + "/bot" + token + "/",
		hc:   &http.Client{Transport: tr},
	}
}

// Call invokes a Bot API method and returns the raw "result" field.
// Flood-wait (429) and transient 5xx errors are retried once.
func (c *Client) Call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	res, err := c.call(ctx, method, params)
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.RetryAfter > 0 || apiErr.Code >= 500) {
		wait := time.Duration(apiErr.RetryAfter) * time.Second
		if wait == 0 {
			wait = 500 * time.Millisecond
		}
		if wait > 30*time.Second {
			return nil, err
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return c.call(ctx, method, params)
	}
	return res, err
}

// CallInto is Call plus decoding the result into out.
func (c *Client) CallInto(ctx context.Context, method string, params map[string]any, out any) error {
	raw, err := c.Call(ctx, method, params)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	body, ctype, err := encode(params)
	if err != nil {
		return nil, fmt.Errorf("telegram %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", ctype)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram %s: %w", method, err)
	}
	defer resp.Body.Close()
	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("telegram %s: bad response (%s): %w", method, resp.Status, err)
	}
	if !r.OK {
		return nil, &APIError{Method: method, Code: r.ErrorCode, Description: r.Description, RetryAfter: r.Parameters.RetryAfter}
	}
	return r.Result, nil
}

// encode picks JSON, or multipart when a local file must be uploaded.
func encode(params map[string]any) (io.Reader, string, error) {
	if !needsUpload(params) {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(b), "application/json", nil
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	n := 0
	for k, v := range params {
		if s, ok := v.(string); ok && strings.HasPrefix(s, FilePrefix) {
			if err := addFile(mw, k, s); err != nil {
				return nil, "", err
			}
			continue
		}
		// Files nested inside objects (input media, stickers...) are sent as
		// separate parts referenced by attach://name.
		v, err := attachNested(mw, v, &n)
		if err != nil {
			return nil, "", err
		}
		var field string
		switch t := v.(type) {
		case string:
			field = t
		case map[string]any, []any:
			b, err := json.Marshal(t)
			if err != nil {
				return nil, "", err
			}
			field = string(b)
		default:
			field = tmpl.ToString(t)
		}
		if err := mw.WriteField(k, field); err != nil {
			return nil, "", err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return &buf, mw.FormDataContentType(), nil
}

func needsUpload(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.HasPrefix(t, FilePrefix)
	case map[string]any:
		for _, x := range t {
			if needsUpload(x) {
				return true
			}
		}
	case []any:
		for _, x := range t {
			if needsUpload(x) {
				return true
			}
		}
	}
	return false
}

func attachNested(mw *multipart.Writer, v any, n *int) (any, error) {
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(t, FilePrefix) {
			*n++
			name := "file" + strconv.Itoa(*n)
			if err := addFile(mw, name, t); err != nil {
				return nil, err
			}
			return "attach://" + name, nil
		}
		return t, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			r, err := attachNested(mw, x, n)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			r, err := attachNested(mw, x, n)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}
	return v, nil
}

func addFile(mw *multipart.Writer, field, ref string) error {
	path := strings.TrimPrefix(ref, FilePrefix)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := mw.CreateFormFile(field, filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}
