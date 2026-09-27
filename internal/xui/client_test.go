package xui_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mrjvadi/tgcreator/internal/workflow"
	"github.com/mrjvadi/tgcreator/internal/xui"
	"github.com/mrjvadi/tgcreator/internal/xui/xuifake"
)

func connect(t *testing.T, typ string) (*xui.Client, *xuifake.Panel) {
	t.Helper()
	p := xuifake.New(typ)
	t.Cleanup(p.Close)
	c, err := xui.New(workflow.VPNPanel{Type: typ, URL: p.URL, Username: "admin", Password: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	return c, p
}

func TestClientLifecycle(t *testing.T) {
	for _, typ := range []string{"3x-ui", "x-ui"} {
		t.Run(typ, func(t *testing.T) {
			ctx := context.Background()
			c, p := connect(t, typ)
			ins, err := c.Inbounds(ctx)
			if err != nil || len(ins) != 2 {
				t.Fatalf("inbounds: %v %d", err, len(ins))
			}
			in, err := c.Inbound(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			cl := xui.NewClient(in, xui.Spec{Email: "tg42", TotalBytes: 5 * xui.GB, Days: 30, TgID: "42", Flow: "xtls-rprx-vision"}, typ, now)
			if err := c.AddClient(ctx, 1, cl); err != nil {
				t.Fatal(err)
			}
			if err := c.AddClient(ctx, 1, xui.NewClient(in, xui.Spec{Email: "tg42"}, typ, now)); !errors.Is(err, xui.ErrExists) {
				t.Fatalf("duplicate: %v", err)
			}
			stored, _ := p.Client("tg42")
			if typ == "x-ui" && stored["tgId"] != "42" || typ == "3x-ui" && stored["tgId"] != float64(42) {
				t.Fatalf("tgId typed wrong for %s: %#v", typ, stored["tgId"])
			}
			p.SetUsage("tg42", xui.GB, xui.GB/2)
			f, err := c.FindClient(ctx, "tg42")
			if err != nil {
				t.Fatal(err)
			}
			if f.Traffic.Up+f.Traffic.Down != xui.GB*3/2 || f.Client["totalGB"] != float64(5*xui.GB) {
				t.Fatalf("found: %+v %v", f.Traffic, f.Client)
			}
			if sub := xui.SubLink(c.SubBase(ctx), stored["subId"].(string)); sub != "https://sub.example.com:2096/sub/"+stored["subId"].(string) {
				t.Fatalf("sub link %v", sub)
			}
			link := xui.Links(f.Inbound, f.Client, "vpn.example.com")[0]
			for _, want := range []string{"vless://" + stored["id"].(string) + "@vpn.example.com:443", "security=reality", "pbk=PUBKEY", "sid=ab12", "sni=www.speedtest.net", "flow=xtls-rprx-vision", "#Reality-tg42"} {
				if !strings.Contains(link, want) {
					t.Fatalf("link %s lacks %s", link, want)
				}
			}

			// Renew: +10 days, +5 GB, reset usage.
			upd := map[string]any{}
			for k, v := range f.Client {
				upd[k] = v
			}
			upd["expiryTime"] = xui.Extend(int64(f.Client["expiryTime"].(float64)), 10, now)
			upd["totalGB"] = f.Client["totalGB"].(float64) + float64(5*xui.GB)
			if err := c.UpdateClient(ctx, f, upd); err != nil {
				t.Fatal(err)
			}
			if err := c.ResetTraffic(ctx, 1, "tg42"); err != nil {
				t.Fatal(err)
			}
			f, _ = c.FindClient(ctx, "tg42")
			if f.Traffic.Total != 10*xui.GB || f.Traffic.Up+f.Traffic.Down != 0 || (f.Traffic.ExpiryTime-now.UnixMilli())/xui.DayMs != 40 {
				t.Fatalf("after renew: %+v", f.Traffic)
			}

			// A restarted panel forgets the session: the client logs in again.
			p.ExpireSessions()
			if _, err := c.Onlines(ctx); err != nil {
				t.Fatal(err)
			}
			if p.Logins != 2 {
				t.Fatalf("logins = %d, want 2", p.Logins)
			}
			if err := c.DeleteClient(ctx, f); err != nil {
				t.Fatal(err)
			}
			if _, err := c.FindClient(ctx, "tg42"); !errors.Is(err, xui.ErrNotFound) {
				t.Fatalf("after delete: %v", err)
			}
		})
	}
}

func TestConcurrentLoginOnce(t *testing.T) {
	c, p := connect(t, "3x-ui")
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Inbounds(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if p.Logins != 1 {
		t.Fatalf("50 parallel calls logged in %d times", p.Logins)
	}
}

func TestWrongPassword(t *testing.T) {
	p := xuifake.New("3x-ui")
	defer p.Close()
	c, _ := xui.New(workflow.VPNPanel{URL: p.URL, Username: "admin", Password: "nope"})
	_, err := c.Inbounds(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Wrong username") {
		t.Fatalf("err = %v", err)
	}
}

func TestVmessLink(t *testing.T) {
	c, _ := connect(t, "3x-ui")
	f, err := c.FindClient(context.Background(), "seed2")
	if err != nil {
		t.Fatal(err)
	}
	links := xui.Links(f.Inbound, f.Client, "1.2.3.4")
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(links[0], "vmess://"))
	var o map[string]any
	_ = json.Unmarshal(raw, &o)
	want := map[string]any{"add": "1.2.3.4", "port": float64(8443), "net": "ws", "path": "/ws", "host": "cdn.example.com", "tls": "tls", "sni": "cdn.example.com", "alpn": "h2,http/1.1", "fp": "chrome", "ps": "WS-seed2", "id": "22222222-2222-4222-8222-222222222222"}
	for k, v := range want {
		if o[k] != v {
			t.Fatalf("vmess %s = %v, want %v (%v)", k, o[k], v, o)
		}
	}
}

func TestExpiry(t *testing.T) {
	now := time.UnixMilli(1_000_000_000_000)
	if xui.ExpiryFor(0, false, now) != 0 || xui.ExpiryFor(2, true, now) != -2*xui.DayMs || xui.ExpiryFor(1, false, now) != now.UnixMilli()+xui.DayMs {
		t.Fatal("ExpiryFor")
	}
	if xui.Extend(0, 5, now) != 0 {
		t.Fatal("unlimited must stay unlimited")
	}
	if xui.Extend(-xui.DayMs, 5, now) != -6*xui.DayMs {
		t.Fatal("pending expiry must grow")
	}
	if xui.Extend(now.UnixMilli()-xui.DayMs, 5, now) != now.UnixMilli()+5*xui.DayMs {
		t.Fatal("expired restarts from now")
	}
	if xui.Extend(now.UnixMilli()+xui.DayMs, 5, now) != now.UnixMilli()+6*xui.DayMs {
		t.Fatal("active adds to the end")
	}
}

func TestTOTP(t *testing.T) {
	// RFC 6238 test vector (SHA1, T=59): 94287082 → last 6 digits.
	code, err := xui.TOTP("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time.Unix(59, 0))
	if err != nil || code != "287082" {
		t.Fatalf("TOTP = %s %v", code, err)
	}
}

func TestFlowOnlyWhereXrayAllowsIt(t *testing.T) {
	c, _ := connect(t, "3x-ui")
	ctx := context.Background()
	reality, _ := c.Inbound(ctx, 1)
	ws, _ := c.Inbound(ctx, 2)
	if got := xui.NewClient(reality, xui.Spec{Email: "a", Flow: "xtls-rprx-vision"}, "3x-ui", time.Now())["flow"]; got != "xtls-rprx-vision" {
		t.Fatalf("reality flow = %v", got)
	}
	ws.Protocol = "vless" // VLESS over websocket: no flow
	if got := xui.NewClient(ws, xui.Spec{Email: "b", Flow: "xtls-rprx-vision"}, "3x-ui", time.Now())["flow"]; got != "" {
		t.Fatalf("ws flow = %v", got)
	}
}
