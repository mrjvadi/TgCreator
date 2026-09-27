package engine

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// Issue is a problem found in a workflow, tied to a node when possible.
type Issue struct {
	Level   string `json:"level"` // error, warning
	Node    string `json:"node,omitempty"`
	Message string `json:"message"`
}

// Check reports every problem in a workflow instead of stopping at the
// first one, for the visual builder. A workflow without errors compiles.
func Check(wf *workflow.Workflow) []Issue {
	var out []Issue
	add := func(level, node, format string, a ...any) {
		out = append(out, Issue{Level: level, Node: node, Message: fmt.Sprintf(format, a...)})
	}

	byID := map[string]workflow.Node{}
	triggers := 0
	b := &Build{Workflow: wf, requires: map[string]bool{}}
	isTrigger := map[string]bool{}
	for i, n := range wf.Nodes {
		if n.ID == "" {
			add("error", "", "نود شمارهٔ %d شناسه ندارد", i+1)
			continue
		}
		if _, dup := byID[n.ID]; dup {
			add("error", n.ID, "شناسهٔ تکراری %q", n.ID)
			continue
		}
		byID[n.ID] = n
		nt := lookup(n.Type)
		if nt == nil {
			add("error", n.ID, "نوع ناشناخته %q", n.Type)
			continue
		}
		if n.Params == nil {
			n.Params = map[string]any{}
		}
		regMu.RLock()
		meta := metas[n.Type]
		if strings.HasPrefix(n.Type, "tg.") {
			meta = metas["tg."]
		}
		regMu.RUnlock()
		for _, p := range meta.Params {
			if p.Required && isEmpty(n.Params[p.Name]) && p.Default == nil {
				add("error", n.ID, "«%s» خالی است", p.Label)
			}
		}
		impl, err := nt.New(b, n)
		if err != nil {
			add("error", n.ID, "%v", err)
			continue
		}
		if _, ok := impl.(Trigger); ok {
			isTrigger[n.ID] = true
			if !n.Disabled {
				triggers++
			}
		}
		if nt.Requires != nil && !n.Disabled {
			for _, r := range nt.Requires(n.Params) {
				b.requires[r] = true
			}
		}
	}
	if triggers == 0 {
		add("error", "", "هیچ نود شروعی (trigger) وجود ندارد")
	}

	reached := map[string]bool{}
	for from, outs := range wf.Connections {
		if _, ok := byID[from]; !ok {
			add("error", "", "اتصال از نود ناموجود %q", from)
			continue
		}
		for output, targets := range outs {
			if data, isBtn := strings.CutPrefix(output, ButtonPrefix); isBtn && len(targets) > 0 {
				if list, has := ButtonData(byID[from].Params); !has {
					add("error", from, "خروجی دکمهٔ %q وصل است ولی این نود دکمه ندارد", data)
				} else if !contains(list, data) && !strings.Contains(fmt.Sprint(byID[from].Params["buttons"]), "{{") {
					add("warning", from, "دکمه‌ای با callback_data %q وجود ندارد", data)
				}
			}
			for _, to := range targets {
				if _, ok := byID[to]; !ok {
					add("error", from, "خروجی %q به نود ناموجود %q وصل است", output, to)
					continue
				}
				if isTrigger[to] {
					add("error", to, "نمی‌توان به نود شروع (trigger) وصل شد")
				}
				reached[to] = true
			}
		}
	}
	for _, n := range wf.Nodes {
		if n.ID != "" && !isTrigger[n.ID] && !reached[n.ID] && !n.Disabled && lookup(n.Type) != nil {
			add("warning", n.ID, "به هیچ جا وصل نیست و اجرا نمی‌شود")
		}
	}

	for r := range b.requires {
		kind, name, _ := strings.Cut(r, ":")
		switch kind {
		case "redis":
			if wf.Services.Redis == nil || wf.Services.Redis.URL == "" {
				add("warning", "", "Redis لازم است ولی آدرسش در تنظیمات سرویس‌ها نیست (یا TGC_REDIS_URL)")
			}
		case "db":
			db, ok := wf.Services.Databases[name]
			if !ok {
				add("error", "", "دیتابیس %q استفاده شده ولی در تنظیمات سرویس‌ها تعریف نشده", name)
			} else if db.Driver != "postgres" && db.Driver != "mysql" && db.Driver != "sqlite" {
				add("error", "", "درایور دیتابیس %q نامعتبر است: %q", name, db.Driver)
			}
		case "vpn":
			p, ok := wf.Services.VPN[name]
			switch {
			case !ok:
				add("error", "", "پنل VPN %q استفاده شده ولی در تنظیمات سرویس‌ها تعریف نشده", name)
			case p.URL == "":
				add("error", "", "آدرس پنل VPN %q خالی است", name)
			case p.Type == "remnawave" && p.Token == "" && p.Username == "":
				add("warning", "", "برای پنل رمناویو %q توکن API لازم است (یا %s_TOKEN)", name, workflow.VPNEnv(name))
			case p.Type == "hiddify" && p.Token == "" && !hasAdminUUID(p.URL):
				add("warning", "", "برای پنل هیدیفای %q کلید API (UUID ادمین) لازم است یا لینک کامل پنل ادمین را بدهید", name)
			case p.Type != "remnawave" && p.Type != "hiddify" && (p.Username == "" || p.Password == ""):
				add("warning", "", "نام کاربری یا رمز پنل VPN %q خالی است (یا %s_PASSWORD)", name, workflow.VPNEnv(name))
			}
		}
		if serviceProvider(kind) == nil {
			add("error", "", "ماژول %s در این نسخه از ران‌تایم وجود ندارد", kind)
		}
	}
	if b.state {
		switch wf.Runtime.StateBackend {
		case "", "memory", "redis":
		default:
			add("error", "", "state_backend نامعتبر: %q", wf.Runtime.StateBackend)
		}
	}
	return out
}

// hasAdminUUID reports a Hiddify admin link (https://domain/PATH/<uuid>/...).
func hasAdminUUID(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) > 1 && len(parts[1]) == 36 && strings.Count(parts[1], "-") == 4
}

func isEmpty(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case map[string]any:
		return len(t) == 0
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
