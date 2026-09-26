package tg

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUploadMultipart(t *testing.T) {
	dir := t.TempDir()
	photo := filepath.Join(dir, "a.jpg")
	if err := os.WriteFile(photo, []byte("JPEGDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		got = map[string]string{"chat_id": r.FormValue("chat_id"), "media": r.FormValue("media")}
		for _, name := range []string{"photo", "file1"} {
			if f, _, err := r.FormFile(name); err == nil {
				b, _ := io.ReadAll(f)
				got[name] = string(b)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
	}))
	defer srv.Close()
	c := New("T", srv.URL)

	if _, err := c.Call(context.Background(), "sendPhoto", map[string]any{"chat_id": -100123.0, "photo": FilePrefix + photo}); err != nil {
		t.Fatal(err)
	}
	if got["chat_id"] != "-100123" || got["photo"] != "JPEGDATA" {
		t.Errorf("sendPhoto form = %#v", got)
	}

	media := []any{map[string]any{"type": "photo", "media": FilePrefix + photo}}
	if _, err := c.Call(context.Background(), "sendMediaGroup", map[string]any{"chat_id": 1, "media": media}); err != nil {
		t.Fatal(err)
	}
	if got["file1"] != "JPEGDATA" || got["media"] != `[{"media":"attach://file1","type":"photo"}]` {
		t.Errorf("sendMediaGroup form = %#v", got)
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`))
	}))
	defer srv.Close()
	_, err := New("T", srv.URL).Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1})
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != 400 {
		t.Fatalf("err = %v", err)
	}
}
