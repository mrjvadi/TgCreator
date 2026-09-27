package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestDisabledIsNoop(t *testing.T) {
	var c *Centrifugo = New(Config{})
	if c.Enabled() || c.Publish(context.Background(), "x", 1) != nil {
		t.Fatal("nil client must be a no-op")
	}
}

func TestPublishAndTokens(t *testing.T) {
	var got struct {
		key  string
		body map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/publish" {
			t.Errorf("path %s", r.URL.Path)
		}
		got.key = r.Header.Get("X-API-Key")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		_, _ = w.Write([]byte(`{"result":{}}`))
	}))
	defer srv.Close()
	c := New(Config{APIURL: srv.URL + "/api/", APIKey: "k", Secret: "s3", WSURL: "ws://x"})
	if err := c.Publish(context.Background(), "test:1", map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if got.key != "k" || got.body["channel"] != "test:1" {
		t.Fatalf("request = %+v", got)
	}

	tok, err := c.SubscriptionToken("u1", "test:1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{}
	if _, err := jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (any, error) { return []byte("s3"), nil }); err != nil {
		t.Fatal(err)
	}
	if claims["channel"] != "test:1" || claims["sub"] != "u1" {
		t.Fatalf("claims = %v", claims)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"code":102,"message":"unknown channel"}}`))
	}))
	defer failing.Close()
	if err := New(Config{APIURL: failing.URL, Secret: "s"}).Publish(context.Background(), "nope:1", 1); err == nil {
		t.Fatal("API errors must be reported")
	}
}
