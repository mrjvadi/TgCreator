package engine

import (
	"fmt"
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
