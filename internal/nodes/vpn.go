//go:build !no_vpn

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/vpn"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// Output names of the VPN nodes besides main.
const (
	outNotFound = "notfound"
	outExists   = "exists"
)

func vpnPanelName(params map[string]any) string {
	if s := tmpl.ToString(params["panel"]); s != "" {
		return s
	}
	return "main"
}

func requireVPN(params map[string]any) []string { return []string{"vpn:" + vpnPanelName(params)} }

func openVPN(_ context.Context, e *engine.Engine, name string) (any, io.Closer, error) {
	cfg, ok := e.WF.Services.VPN[name]
	if !ok {
		return nil, nil, fmt.Errorf("services.vpn.%s is not configured", name)
	}
	p, err := vpn.New(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("vpn panel %q: %w", name, err)
	}
	// Logging in is lazy: a panel that is down at start must not keep the
	// bot from serving everything else.
	return p, nil, nil
}

var (
	pVPNPanel = engine.Param{Name: "panel", Label: "پنل", Type: "text", Placeholder: "main", Help: "نام پنل در تنظیمات ← سرویس‌ها"}
	pVPNUser  = engine.Param{Name: "username", Label: "نام کاربر", Type: "text", Required: true, Placeholder: "tg{{ from.id }}", Help: "در X-UI همان ایمیل کلاینت است"}
)

func init() {
	engine.RegisterService("vpn", openVPN)

	engine.Describe("vpn.create_user", engine.Meta{
		Label: "ساخت کاربر VPN", Category: "vpn", Icon: "user-plus", Summary: "کاربر جدید در پنل می‌سازد و لینک اشتراک را می‌دهد",
		Outputs: []string{engine.Main, outExists},
		Params: []engine.Param{
			pVPNPanel,
			{Name: "inbound", Label: "اینباند / گروه", Type: "text", Placeholder: "1",
				Help: "X-UI: شمارهٔ اینباند (اجباری) · مرزبان: تگ اینباندها · پاسارگاد: شمارهٔ گروه‌ها · مرزنشین: شمارهٔ سرویس‌ها · رمناویو: UUID اسکوادها — چندتا با ویرگول؛ خالی = همه"},
			{Name: "username", Label: "نام کاربر", Type: "text", Placeholder: "tg{{ from.id }}", Help: "باید یکتا باشد؛ خالی = تصادفی. اگر تکراری باشد خروجی «تکراری» اجرا می‌شود"},
			{Name: "traffic_gb", Label: "حجم (گیگابایت)", Type: "number", Default: 0, Help: "۰ = نامحدود"},
			{Name: "days", Label: "مدت (روز)", Type: "number", Default: 30, Help: "۰ = بدون انقضا"},
			{Name: "start_after_first_use", Label: "شروع مدت از اولین اتصال", Type: "bool", Help: "رمناویو پشتیبانی نمی‌کند و از همین حالا حساب می‌کند"},
			{Name: "limit_ip", Label: "محدودیت IP / دستگاه", Type: "number", Default: 0, Help: "X-UI: تعداد IP · پاسارگاد و رمناویو: تعداد دستگاه · ۰ = نامحدود"},
			{Name: "flow", Label: "Flow", Type: "select", Options: []string{"", "xtls-rprx-vision", "xtls-rprx-vision-udp443"}, Help: "X-UI و مرزبان؛ فقط VLESS روی TCP با TLS یا Reality"},
			{Name: "tg_id", Label: "آیدی تلگرام", Type: "text", Default: "{{ from.id }}", Help: "برای «اکانت‌های من». پنل‌هایی که فیلدش را ندارند آن را در یادداشت نگه می‌دارند"},
			{Name: "note", Label: "یادداشت", Type: "text", Placeholder: "{{ from.first_name }}"},
			{Name: "sub_id", Label: "شناسهٔ اشتراک (X-UI)", Type: "text", Help: "خالی = تصادفی"},
			{Name: "disabled", Label: "ساخت به‌صورت غیرفعال", Type: "bool", Help: "مثلاً تا تأیید پرداخت"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.create_user",
		Description: "Create a user on a VPN panel (X-UI, Marzban, PasarGuard, Marzneshin, Remnawave, Hiddify). Output: user info (sub_link, link, used_gb, expiry_jalali...); \"exists\" when the name is taken.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnCreate),
	})

	engine.Describe("vpn.get_user", engine.Meta{
		Label: "اطلاعات کاربر VPN", Category: "vpn", Icon: "user-search", Summary: "مصرف، حجم باقی‌مانده، انقضا و لینک اشتراک",
		Outputs: []string{engine.Main, outNotFound},
		Params:  []engine.Param{pVPNPanel, pVPNUser},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.get_user",
		Description: "Look a user up. Output: used_gb, remaining_gb, days_left, expiry_jalali, active, sub_link...; \"notfound\" when missing.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnGet, "username"),
	})

	engine.Describe("vpn.update_user", engine.Meta{
		Label: "تمدید / ویرایش کاربر VPN", Category: "vpn", Icon: "user-cog", Summary: "افزودن روز و حجم، صفر کردن مصرف، فعال/غیرفعال",
		Outputs: []string{engine.Main, outNotFound},
		Params: []engine.Param{
			pVPNPanel, pVPNUser,
			{Name: "add_days", Label: "افزودن روز", Type: "number", Help: "اگر منقضی شده باشد از امروز حساب می‌شود"},
			{Name: "add_gb", Label: "افزودن حجم (گیگ)", Type: "number", Help: "روی حجم نامحدود اثری ندارد"},
			{Name: "set_days", Label: "تنظیم مدت از امروز (روز)", Type: "number", Help: "خالی = بدون تغییر، ۰ = بدون انقضا"},
			{Name: "set_gb", Label: "تنظیم حجم کل (گیگ)", Type: "number", Help: "خالی = بدون تغییر، ۰ = نامحدود"},
			{Name: "reset_traffic", Label: "صفر کردن مصرف", Type: "bool"},
			{Name: "enable", Label: "وضعیت", Type: "select", Options: []string{"", "true", "false"}, Help: "خالی = بدون تغییر (کاربری که پنل به‌خاطر اتمام حجم یا زمان متوقف کرده، با تمدید دوباره فعال می‌شود)"},
			{Name: "limit_ip", Label: "محدودیت IP / دستگاه", Type: "number", Help: "خالی = بدون تغییر"},
			{Name: "note", Label: "یادداشت", Type: "text", Help: "خالی = بدون تغییر"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.update_user",
		Description: "Renew or edit a user. Params: username, add_days, add_gb, set_days, set_gb, reset_traffic, enable, limit_ip, note. Output: the updated user; \"notfound\" when missing.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnUpdate, "username"),
	})

	engine.Describe("vpn.delete_user", engine.Meta{
		Label: "حذف کاربر VPN", Category: "vpn", Icon: "user-x", Summary: "کاربر را از پنل حذف می‌کند",
		Outputs: []string{engine.Main, outNotFound},
		Params:  []engine.Param{pVPNPanel, pVPNUser},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.delete_user",
		Description: "Delete a user. Output: {username}; \"notfound\" when missing.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnDelete, "username"),
	})

	engine.Describe("vpn.find_users", engine.Meta{
		Label: "جستجوی کاربران VPN", Category: "vpn", Icon: "users", Summary: "همهٔ کاربرهای یک آیدی تلگرام یا یک عبارت",
		Outputs: []string{engine.Main, outNotFound},
		Params: []engine.Param{
			pVPNPanel,
			{Name: "tg_id", Label: "آیدی تلگرام", Type: "text", Placeholder: "{{ from.id }}"},
			{Name: "search", Label: "بخشی از نام یا یادداشت", Type: "text"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.find_users",
		Description: "List users by tg_id and/or search text. Output: {users, count}; \"notfound\" when none.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnFind),
	})

	engine.Describe("vpn.list_groups", engine.Meta{
		Label: "اینباندها و گروه‌ها", Category: "vpn", Icon: "server", Summary: "اینباندها، گروه‌ها، سرویس‌ها یا اسکوادهای پنل",
		Params: []engine.Param{pVPNPanel},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.list_groups",
		Description: "What users can be attached to (inbounds, groups, services, squads). Output: {groups: [{id, name, protocol, port, users, enabled}], count}.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnGroups),
	})

	engine.Describe("vpn.onlines", engine.Meta{
		Label: "کاربران آنلاین", Category: "vpn", Icon: "activity", Summary: "کاربرانی که همین حالا وصل‌اند",
		Params: []engine.Param{pVPNPanel},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.onlines",
		Description: "Connected users. Output: {users, count}.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnOnlines),
	})

	engine.Describe("vpn.api", engine.Meta{
		Label: "درخواست دلخواه به پنل", Category: "vpn", Icon: "server-cog", Summary: "هر مسیر API پنل با همان احراز هویت",
		Params: []engine.Param{
			pVPNPanel,
			{Name: "method", Label: "متد", Type: "select", Options: []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, Default: "GET"},
			{Name: "path", Label: "مسیر", Type: "text", Required: true, Placeholder: "/api/system", Help: "X-UI: نسبت به آدرس پنل · بقیه: مسیر API"},
			{Name: "body", Label: "بدنه (JSON)", Type: "json"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "vpn.api",
		Description: "Call any panel API path with the panel's auth. Params: panel, method, path, body. Output: the reply.",
		Requires:    requireVPN,
		New:         newVPNNode(vpnRaw, "path"),
	})
}

type vpnOp func(x *engine.Exec, p vpn.Panel, params map[string]any) (engine.Result, error)

type vpnNode struct {
	panel  string
	params tmpl.Value
	op     vpnOp
	p      vpn.Panel
}

func newVPNNode(op vpnOp, required ...string) func(*engine.Build, workflow.Node) (any, error) {
	return func(b *engine.Build, n workflow.Node) (any, error) {
		for _, r := range required {
			if isBlank(n.Params[r]) {
				return nil, fmt.Errorf("%s is required", r)
			}
		}
		p, err := b.Compile(n.Params)
		if err != nil {
			return nil, err
		}
		return &vpnNode{panel: vpnPanelName(n.Params), params: p, op: op}, nil
	}
}

func isBlank(v any) bool {
	s, isStr := v.(string)
	return v == nil || isStr && strings.TrimSpace(s) == ""
}

func (n *vpnNode) Init(e *engine.Engine) error {
	p, ok := e.Service("vpn:" + n.panel).(vpn.Panel)
	if !ok {
		return fmt.Errorf("vpn panel %q not available", n.panel)
	}
	n.p = p
	return nil
}

func (n *vpnNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(n.params)
	if err != nil {
		return engine.Result{}, err
	}
	return n.op(x, n.p, cloneMap(v))
}

// vpnNum reads an optional number param; ok is false when it is empty.
func vpnNum(p map[string]any, key string) (float64, bool, error) {
	switch v := p[key].(type) {
	case nil:
		return 0, false, nil
	case float64:
		return v, true, nil
	case int:
		return float64(v), true, nil
	case int64:
		return float64(v), true, nil
	case bool:
		return 0, false, fmt.Errorf("%s: expected a number", key)
	}
	s := strings.TrimSpace(tmpl.ToString(p[key]))
	if s == "" {
		return 0, false, nil
	}
	f, err := strconv.ParseFloat(faDigits.Replace(s), 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s: %q is not a number", key, s)
	}
	return f, true, nil
}

// faDigits lets users type Persian/Arabic digits into number fields.
var faDigits = strings.NewReplacer("۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4", "۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
	"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4", "٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9")

func gbBytes(gb float64) int64 { return int64(math.Round(gb * float64(vpn.GB))) }

func splitList(v any) []string {
	var out []string
	switch t := v.(type) {
	case []any:
		for _, x := range t {
			if s := strings.TrimSpace(tmpl.ToString(x)); s != "" {
				out = append(out, s)
			}
		}
	default:
		for _, s := range strings.FieldsFunc(tmpl.ToString(v), func(r rune) bool { return r == ',' || r == '،' || r == '\n' }) {
			if s = strings.TrimSpace(s); s != "" { // Marzban tags contain spaces
				out = append(out, faDigits.Replace(s))
			}
		}
	}
	return out
}

func vpnCreate(x *engine.Exec, p vpn.Panel, prm map[string]any) (engine.Result, error) {
	gb, _, err := vpnNum(prm, "traffic_gb")
	if err != nil {
		return engine.Result{}, err
	}
	days, _, err := vpnNum(prm, "days")
	if err != nil {
		return engine.Result{}, err
	}
	limit, _, err := vpnNum(prm, "limit_ip")
	if err != nil {
		return engine.Result{}, err
	}
	name := strings.TrimSpace(tmpl.ToString(prm["username"]))
	if name == "" {
		name = "u" + strconv.FormatInt(time.Now().UnixNano()%1e10, 36)
	}
	groups := splitList(prm["inbound"])
	if g, ok := prm["inbound_id"]; ok && len(groups) == 0 { // older workflows
		groups = splitList(g)
	}
	a, err := p.Create(x.Ctx, vpn.Spec{
		Username:      name,
		TotalBytes:    gbBytes(gb),
		Days:          days,
		AfterFirstUse: tmpl.Truthy(prm["start_after_first_use"]),
		LimitIP:       int(limit),
		Flow:          tmpl.ToString(prm["flow"]),
		TgID:          tmpl.ToString(prm["tg_id"]),
		Note:          tmpl.ToString(prm["note"]),
		Groups:        groups,
		Disabled:      tmpl.Truthy(prm["disabled"]),
		SubID:         strings.TrimSpace(tmpl.ToString(prm["sub_id"])),
	})
	if errors.Is(err, vpn.ErrExists) {
		return engine.Result{Output: outExists, Data: map[string]any{"username": name}}, nil
	}
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: a.Info(time.Now())}, nil
}

func userResult(a *vpn.Account, err error, name string) (engine.Result, error) {
	if errors.Is(err, vpn.ErrNotFound) {
		return engine.Result{Output: outNotFound, Data: map[string]any{"username": name}}, nil
	}
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: a.Info(time.Now())}, nil
}

func vpnGet(x *engine.Exec, p vpn.Panel, prm map[string]any) (engine.Result, error) {
	name := strings.TrimSpace(tmpl.ToString(prm["username"]))
	a, err := p.Get(x.Ctx, name)
	return userResult(a, err, name)
}

func vpnUpdate(x *engine.Exec, p vpn.Panel, prm map[string]any) (engine.Result, error) {
	name := strings.TrimSpace(tmpl.ToString(prm["username"]))
	var ch vpn.Change
	if d, ok, err := vpnNum(prm, "set_days"); err != nil {
		return engine.Result{}, err
	} else if ok {
		ch.SetDays = &d
	}
	if d, _, err := vpnNum(prm, "add_days"); err != nil {
		return engine.Result{}, err
	} else {
		ch.AddDays = d
	}
	if g, ok, err := vpnNum(prm, "set_gb"); err != nil {
		return engine.Result{}, err
	} else if ok {
		b := gbBytes(g)
		ch.SetBytes = &b
	}
	if g, _, err := vpnNum(prm, "add_gb"); err != nil {
		return engine.Result{}, err
	} else {
		ch.AddBytes = gbBytes(g)
	}
	if l, ok, err := vpnNum(prm, "limit_ip"); err != nil {
		return engine.Result{}, err
	} else if ok {
		n := int(l)
		ch.LimitIP = &n
	}
	ch.ResetUsage = tmpl.Truthy(prm["reset_traffic"])
	switch tmpl.ToString(prm["enable"]) {
	case "true":
		ch.Enable = ptrTo(true)
	case "false":
		ch.Enable = ptrTo(false)
	}
	if s := tmpl.ToString(prm["note"]); s != "" {
		ch.Note = &s
	}
	a, err := p.Update(x.Ctx, name, ch)
	return userResult(a, err, name)
}

func ptrTo[T any](v T) *T { return &v }

func vpnDelete(x *engine.Exec, p vpn.Panel, prm map[string]any) (engine.Result, error) {
	name := strings.TrimSpace(tmpl.ToString(prm["username"]))
	err := p.Delete(x.Ctx, name)
	if errors.Is(err, vpn.ErrNotFound) {
		return engine.Result{Output: outNotFound, Data: map[string]any{"username": name}}, nil
	}
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: map[string]any{"username": name}}, nil
}

func vpnFind(x *engine.Exec, p vpn.Panel, prm map[string]any) (engine.Result, error) {
	q := vpn.Query{TgID: strings.TrimSpace(tmpl.ToString(prm["tg_id"])), Search: strings.TrimSpace(tmpl.ToString(prm["search"]))}
	list, err := p.Find(x.Ctx, q)
	if err != nil {
		return engine.Result{}, err
	}
	now := time.Now()
	users := []any{}
	for _, a := range list {
		if q.Match(a) {
			users = append(users, a.Info(now))
		}
	}
	out := engine.Main
	if len(users) == 0 {
		out = outNotFound
	}
	return engine.Result{Output: out, Data: map[string]any{"users": users, "count": float64(len(users))}}, nil
}

func vpnGroups(x *engine.Exec, p vpn.Panel, _ map[string]any) (engine.Result, error) {
	groups, err := p.Groups(x.Ctx)
	if err != nil {
		return engine.Result{}, err
	}
	list := make([]any, len(groups))
	for i, g := range groups {
		list[i] = map[string]any{"id": g.ID, "name": g.Name, "protocol": g.Protocol, "port": float64(g.Port), "users": float64(g.Users), "enabled": g.Enabled}
	}
	return engine.Result{Output: engine.Main, Data: map[string]any{"groups": list, "count": float64(len(list))}}, nil
}

func vpnOnlines(x *engine.Exec, p vpn.Panel, _ map[string]any) (engine.Result, error) {
	names, err := p.Onlines(x.Ctx)
	if err != nil {
		return engine.Result{}, err
	}
	list := make([]any, len(names))
	for i, n := range names {
		list[i] = n
	}
	return engine.Result{Output: engine.Main, Data: map[string]any{"users": list, "count": float64(len(list))}}, nil
}

func vpnRaw(x *engine.Exec, p vpn.Panel, prm map[string]any) (engine.Result, error) {
	method := strings.ToUpper(tmpl.ToString(prm["method"]))
	if method == "" {
		method = http.MethodGet
	}
	var body any
	if b := prm["body"]; b != nil && b != "" {
		body = b
	}
	raw, err := p.Call(x.Ctx, method, tmpl.ToString(prm["path"]), body)
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: decodeJSON(raw)}, nil
}

func decodeJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	return v
}
