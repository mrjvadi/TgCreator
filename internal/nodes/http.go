package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

func init() {
	engine.Register(engine.NodeType{
		Name:        "http.request",
		Description: "Call an external API. Params: method, url, headers, query, body (object = JSON), timeout. Output: {status, body}. Non-2xx goes to the \"error\" output when connected.",
		New:         newHTTPNode,
	})
}

type httpNode struct{ params tmpl.Value }

func newHTTPNode(b *engine.Build, n workflow.Node) (any, error) {
	if tmpl.ToString(n.Params["url"]) == "" {
		return nil, errors.New("url is required")
	}
	p, err := b.Compile(n.Params)
	if err != nil {
		return nil, err
	}
	return &httpNode{params: p}, nil
}

func (h *httpNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(h.params)
	if err != nil {
		return engine.Result{}, err
	}
	p := cloneMap(v)
	method := strings.ToUpper(tmpl.ToString(p["method"]))
	if method == "" {
		method = http.MethodGet
	}
	u, err := url.Parse(tmpl.ToString(p["url"]))
	if err != nil {
		return engine.Result{}, err
	}
	if q, ok := p["query"].(map[string]any); ok {
		vals := u.Query()
		for k, val := range q {
			vals.Set(k, tmpl.ToString(val))
		}
		u.RawQuery = vals.Encode()
	}
	var body io.Reader
	isJSON := false
	switch b := p["body"].(type) {
	case nil:
	case string:
		body = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return engine.Result{}, err
		}
		body, isJSON = bytes.NewReader(raw), true
	}
	ctx := x.Ctx
	if t := tmpl.ToString(p["timeout"]); t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return engine.Result{}, err
		}
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return engine.Result{}, err
	}
	if isJSON {
		req.Header.Set("Content-Type", "application/json")
	}
	if hs, ok := p["headers"].(map[string]any); ok {
		for k, val := range hs {
			req.Header.Set(k, tmpl.ToString(val))
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return engine.Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return engine.Result{}, err
	}
	var parsed any
	if json.Unmarshal(raw, &parsed) != nil {
		parsed = string(raw)
	}
	data := map[string]any{"status": float64(resp.StatusCode), "body": parsed}
	if resp.StatusCode >= 300 {
		return engine.Result{}, errors.New("http " + resp.Status + ": " + truncate(string(raw), 300))
	}
	return engine.Result{Output: engine.Main, Data: data}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
