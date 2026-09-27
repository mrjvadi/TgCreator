//go:build !no_xui

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
	"github.com/mrjvadi/tgcreator/internal/workflow"
	"github.com/mrjvadi/tgcreator/internal/xui"
)

// Output names of the X-UI nodes besides main.
const (
	outNotFound = "notfound"
	outExists   = "exists"
)

func xuiPanel(params map[string]any) string {
	if s := tmpl.ToString(params["panel"]); s != "" {
		return s
	}
	return "main"
}

func requireXUI(params map[string]any) []string { return []string{"xui:" + xuiPanel(params)} }

// xuiConn is the service registered for each panel.
type xuiConn struct {
	*xui.Client
	address string
}

func openXUI(_ context.Context, e *engine.Engine, name string) (any, io.Closer, error) {
	cfg, ok := e.WF.Services.XUI[name]
	if !ok {
		return nil, nil, fmt.Errorf("services.xui.%s is not configured", name)
	}
	c, err := xui.New(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("xui panel %q: %w", name, err)
	}
	addr := cfg.Address
	if addr == "" {
		addr = c.Host()
	}
	// Logging in is lazy: a panel that is down at start must not keep the
	// bot from serving everything else.
	return &xuiConn{Client: c, address: addr}, nil, nil
}

var pPanel = engine.Param{Name: "panel", Label: "پنل", Type: "text", Placeholder: "main", Help: "نام پنل در تنظیمات ← سرویس‌ها"}
var pEmail = engine.Param{Name: "email", Label: "ایمیل (نام کاربر)", Type: "text", Required: true, Placeholder: "tg{{ from.id }}", Help: "شناسهٔ یکتای کاربر در پنل"}

func init() {
	engine.RegisterService("xui", openXUI)

	engine.Describe("xui.add_client", engine.Meta{
		Label: "ساخت کاربر VPN", Category: "xui", Icon: "user-plus", Summary: "کاربر جدید در اینباند می‌سازد و لینک کانفیگ و ساب را می‌دهد",
		Outputs: []string{engine.Main, outExists},
		Params: []engine.Param{
			pPanel,
			{Name: "inbound_id", Label: "شمارهٔ اینباند", Type: "number", Required: true, Default: 1, Help: "ID اینباند در پنل"},
			{Name: "email", Label: "ایمیل (نام کاربر)", Type: "text", Placeholder: "tg{{ from.id }}", Help: "باید یکتا باشد؛ خالی = تصادفی. اگر تکراری باشد خروجی «تکراری» اجرا می‌شود"},
			{Name: "traffic_gb", Label: "حجم (گیگابایت)", Type: "number", Default: 0, Help: "۰ = نامحدود"},
			{Name: "days", Label: "مدت (روز)", Type: "number", Default: 30, Help: "۰ = بدون انقضا"},
			{Name: "start_after_first_use", Label: "شروع مدت از اولین اتصال", Type: "bool"},
			{Name: "limit_ip", Label: "محدودیت تعداد IP", Type: "number", Default: 0, Help: "۰ = نامحدود"},
			{Name: "flow", Label: "Flow", Type: "select", Options: []string{"", "xtls-rprx-vision", "xtls-rprx-vision-udp443"}, Help: "فقط VLESS روی TCP با TLS یا Reality؛ در بقیهٔ اینباندها نادیده گرفته می‌شود"},
			{Name: "tg_id", Label: "آیدی تلگرام", Type: "text", Default: "{{ from.id }}", Help: "برای اعلان‌های ربات خود پنل و جستجو"},
			{Name: "comment", Label: "توضیح", Type: "text", Placeholder: "{{ from.first_name }}"},
			{Name: "sub_id", Label: "شناسهٔ اشتراک (subId)", Type: "text", Help: "خالی = تصادفی. برای یکی کردن ساب چند کاربر، یکسان بگذارید"},
			{Name: "disabled", Label: "ساخت به‌صورت غیرفعال", Type: "bool", Help: "مثلاً تا تأیید پرداخت"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.add_client",
		Description: "Create a client in an X-UI inbound. Params: panel, inbound_id, email, traffic_gb, days, start_after_first_use, limit_ip, flow, tg_id, comment, sub_id, disabled. Output: client info (link, sub_link, ...); \"exists\" when the email is taken.",
		Requires:    requireXUI,
		New:         newXUINode(xuiAdd, "inbound_id"),
	})

	engine.Describe("xui.get_client", engine.Meta{
		Label: "اطلاعات کاربر VPN", Category: "xui", Icon: "user-search", Summary: "مصرف، حجم باقی‌مانده، انقضا و لینک‌ها",
		Outputs: []string{engine.Main, outNotFound},
		Params:  []engine.Param{pPanel, pEmail},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.get_client",
		Description: "Look a client up by email. Output: used_gb, remaining_gb, days_left, expiry_jalali, active, link, sub_link...; \"notfound\" when missing.",
		Requires:    requireXUI,
		New:         newXUINode(xuiGet, "email"),
	})

	engine.Describe("xui.update_client", engine.Meta{
		Label: "تمدید / ویرایش کاربر VPN", Category: "xui", Icon: "user-cog", Summary: "افزودن روز و حجم، صفر کردن مصرف، فعال/غیرفعال",
		Outputs: []string{engine.Main, outNotFound},
		Params: []engine.Param{
			pPanel, pEmail,
			{Name: "add_days", Label: "افزودن روز", Type: "number", Help: "اگر منقضی شده باشد از امروز حساب می‌شود"},
			{Name: "add_gb", Label: "افزودن حجم (گیگ)", Type: "number", Help: "روی حجم نامحدود اثری ندارد"},
			{Name: "set_days", Label: "تنظیم مدت از امروز (روز)", Type: "number", Help: "خالی = بدون تغییر، ۰ = بدون انقضا"},
			{Name: "set_gb", Label: "تنظیم حجم کل (گیگ)", Type: "number", Help: "خالی = بدون تغییر، ۰ = نامحدود"},
			{Name: "reset_traffic", Label: "صفر کردن مصرف", Type: "bool"},
			{Name: "enable", Label: "وضعیت", Type: "select", Options: []string{"", "true", "false"}, Help: "خالی = بدون تغییر (با تمدید خودکار فعال می‌شود)"},
			{Name: "limit_ip", Label: "محدودیت IP", Type: "number", Help: "خالی = بدون تغییر"},
			{Name: "comment", Label: "توضیح", Type: "text", Help: "خالی = بدون تغییر"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.update_client",
		Description: "Renew or edit a client. Params: panel, email, add_days, add_gb, set_days, set_gb, reset_traffic, enable, limit_ip, comment. Output: the updated client info; \"notfound\" when missing.",
		Requires:    requireXUI,
		New:         newXUINode(xuiUpdate, "email"),
	})

	engine.Describe("xui.delete_client", engine.Meta{
		Label: "حذف کاربر VPN", Category: "xui", Icon: "user-x", Summary: "کاربر را از پنل حذف می‌کند",
		Outputs: []string{engine.Main, outNotFound},
		Params:  []engine.Param{pPanel, pEmail},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.delete_client",
		Description: "Delete a client by email. Output: {email, inbound_id}; \"notfound\" when missing.",
		Requires:    requireXUI,
		New:         newXUINode(xuiDelete, "email"),
	})

	engine.Describe("xui.find_clients", engine.Meta{
		Label: "جستجوی کاربران VPN", Category: "xui", Icon: "users", Summary: "همهٔ کاربرهای یک آیدی تلگرام یا یک عبارت",
		Outputs: []string{engine.Main, outNotFound},
		Params: []engine.Param{
			pPanel,
			{Name: "tg_id", Label: "آیدی تلگرام", Type: "text", Placeholder: "{{ from.id }}"},
			{Name: "search", Label: "بخشی از ایمیل یا توضیح", Type: "text"},
			{Name: "inbound_id", Label: "فقط این اینباند", Type: "number"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.find_clients",
		Description: "List clients matching tg_id and/or search text (email, comment). Output: {clients, count}; \"notfound\" when none.",
		Requires:    requireXUI,
		New:         newXUINode(xuiFind),
	})

	engine.Describe("xui.list_inbounds", engine.Meta{
		Label: "لیست اینباندها", Category: "xui", Icon: "server", Summary: "اینباندها با پروتکل، پورت و تعداد کاربر",
		Params: []engine.Param{pPanel},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.list_inbounds",
		Description: "List the panel's inbounds. Output: {inbounds: [{id, remark, protocol, port, enable, clients, used_gb}], count}.",
		Requires:    requireXUI,
		New:         newXUINode(xuiInbounds),
	})

	engine.Describe("xui.onlines", engine.Meta{
		Label: "کاربران آنلاین", Category: "xui", Icon: "activity", Summary: "ایمیل کاربرانی که الان وصل‌اند",
		Params: []engine.Param{pPanel},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.onlines",
		Description: "Connected clients. Output: {emails, count}.",
		Requires:    requireXUI,
		New:         newXUINode(xuiOnlines),
	})

	engine.Describe("xui.api", engine.Meta{
		Label: "درخواست دلخواه به پنل", Category: "xui", Icon: "server-cog", Summary: "هر مسیر API پنل با همان نشست",
		Params: []engine.Param{
			pPanel,
			{Name: "method", Label: "متد", Type: "select", Options: []string{"GET", "POST"}, Default: "GET"},
			{Name: "path", Label: "مسیر", Type: "text", Required: true, Placeholder: "/panel/api/server/status", Help: "نسبت به آدرس پنل"},
			{Name: "body", Label: "بدنه (JSON)", Type: "json"},
		},
	})
	engine.Register(engine.NodeType{
		Name:        "xui.api",
		Description: "Call any panel API path with the shared session. Params: panel, method, path, body. Output: the reply's obj.",
		Requires:    requireXUI,
		New:         newXUINode(xuiRaw, "path"),
	})
}

type xuiOp func(x *engine.Exec, c *xuiConn, p map[string]any) (engine.Result, error)

type xuiNode struct {
	panel  string
	params tmpl.Value
	op     xuiOp
	conn   *xuiConn
}

func newXUINode(op xuiOp, required ...string) func(*engine.Build, workflow.Node) (any, error) {
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
		return &xuiNode{panel: xuiPanel(n.Params), params: p, op: op}, nil
	}
}

func isBlank(v any) bool {
	s, isStr := v.(string)
	return v == nil || isStr && strings.TrimSpace(s) == ""
}

func (n *xuiNode) Init(e *engine.Engine) error {
	c, ok := e.Service("xui:" + n.panel).(*xuiConn)
	if !ok {
		return fmt.Errorf("xui panel %q not available", n.panel)
	}
	n.conn = c
	return nil
}

func (n *xuiNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(n.params)
	if err != nil {
		return engine.Result{}, err
	}
	return n.op(x, n.conn, cloneMap(v))
}

// xuiNum reads an optional number param; ok is false when it is empty.
func xuiNum(p map[string]any, key string) (float64, bool, error) {
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

func gbBytes(gb float64) int64 { return int64(math.Round(gb * float64(xui.GB))) }

func xuiInfo(x *engine.Exec, c *xuiConn, f *xui.Found) map[string]any {
	return xui.Info(f, c.SubBase(x.Ctx), c.address, time.Now())
}

func xuiAdd(x *engine.Exec, c *xuiConn, p map[string]any) (engine.Result, error) {
	id, _, err := xuiNum(p, "inbound_id")
	if err != nil {
		return engine.Result{}, err
	}
	gb, _, err := xuiNum(p, "traffic_gb")
	if err != nil {
		return engine.Result{}, err
	}
	days, _, err := xuiNum(p, "days")
	if err != nil {
		return engine.Result{}, err
	}
	limit, _, err := xuiNum(p, "limit_ip")
	if err != nil {
		return engine.Result{}, err
	}
	email := strings.TrimSpace(tmpl.ToString(p["email"]))
	if email == "" {
		email = xui.RandomString(10)
	}
	in, err := c.Inbound(x.Ctx, int(id))
	if err != nil {
		return engine.Result{}, err
	}
	spec := xui.Spec{
		Email:         email,
		TotalBytes:    gbBytes(gb),
		Days:          days,
		AfterFirstUse: tmpl.Truthy(p["start_after_first_use"]),
		LimitIP:       int(limit),
		Flow:          tmpl.ToString(p["flow"]),
		TgID:          tmpl.ToString(p["tg_id"]),
		Comment:       tmpl.ToString(p["comment"]),
		SubID:         strings.TrimSpace(tmpl.ToString(p["sub_id"])),
		Disabled:      tmpl.Truthy(p["disabled"]),
	}
	client := xui.NewClient(in, spec, c.Type(), time.Now())
	if err := c.AddClient(x.Ctx, in.ID, client); err != nil {
		if errors.Is(err, xui.ErrExists) {
			return engine.Result{Output: outExists, Data: map[string]any{"email": email}}, nil
		}
		return engine.Result{}, err
	}
	f := &xui.Found{Inbound: in, Client: client, Traffic: xui.Traffic{InboundID: in.ID, Email: email, Enable: !spec.Disabled}}
	return engine.Result{Output: engine.Main, Data: xuiInfo(x, c, f)}, nil
}

func xuiLookup(x *engine.Exec, c *xuiConn, p map[string]any) (*xui.Found, *engine.Result, error) {
	email := strings.TrimSpace(tmpl.ToString(p["email"]))
	f, err := c.FindClient(x.Ctx, email)
	if errors.Is(err, xui.ErrNotFound) {
		return nil, &engine.Result{Output: outNotFound, Data: map[string]any{"email": email}}, nil
	}
	return f, nil, err
}

func xuiGet(x *engine.Exec, c *xuiConn, p map[string]any) (engine.Result, error) {
	f, miss, err := xuiLookup(x, c, p)
	if err != nil || miss != nil {
		return deref(miss), err
	}
	return engine.Result{Output: engine.Main, Data: xuiInfo(x, c, f)}, nil
}

func deref(r *engine.Result) engine.Result {
	if r == nil {
		return engine.Result{}
	}
	return *r
}

func xuiUpdate(x *engine.Exec, c *xuiConn, p map[string]any) (engine.Result, error) {
	f, miss, err := xuiLookup(x, c, p)
	if err != nil || miss != nil {
		return deref(miss), err
	}
	now := time.Now()
	cl := make(map[string]any, len(f.Client))
	for k, v := range f.Client {
		cl[k] = v
	}
	expiry := int64Of(cl["expiryTime"])
	if f.Traffic.ExpiryTime != 0 {
		expiry = f.Traffic.ExpiryTime // the panel may have started a first-use timer
	}
	total := int64Of(cl["totalGB"])
	renewed := false
	if d, ok, err := xuiNum(p, "set_days"); err != nil {
		return engine.Result{}, err
	} else if ok {
		expiry, renewed = xui.ExpiryFor(d, false, now), true
	}
	if d, ok, err := xuiNum(p, "add_days"); err != nil {
		return engine.Result{}, err
	} else if ok && d != 0 {
		expiry, renewed = xui.Extend(expiry, d, now), true
	}
	if g, ok, err := xuiNum(p, "set_gb"); err != nil {
		return engine.Result{}, err
	} else if ok {
		total, renewed = gbBytes(g), true
	}
	if g, ok, err := xuiNum(p, "add_gb"); err != nil {
		return engine.Result{}, err
	} else if ok && g != 0 && total != 0 {
		total, renewed = max(total+gbBytes(g), 0), true
	}
	reset := tmpl.Truthy(p["reset_traffic"])
	cl["expiryTime"], cl["totalGB"] = expiry, total
	switch tmpl.ToString(p["enable"]) {
	case "true":
		cl["enable"] = true
	case "false":
		cl["enable"] = false
	default:
		if renewed || reset {
			cl["enable"] = true
		}
	}
	if l, ok, err := xuiNum(p, "limit_ip"); err != nil {
		return engine.Result{}, err
	} else if ok {
		cl["limitIp"] = int(l)
	}
	if s := tmpl.ToString(p["comment"]); s != "" {
		cl["comment"] = s
	}
	cl["updated_at"] = now.UnixMilli()
	if err := c.UpdateClient(x.Ctx, f, cl); err != nil {
		return engine.Result{}, err
	}
	if reset {
		if err := c.ResetTraffic(x.Ctx, f.Inbound.ID, tmpl.ToString(cl["email"])); err != nil {
			return engine.Result{}, err
		}
	}
	f2, err := c.FindClient(x.Ctx, tmpl.ToString(cl["email"]))
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: xuiInfo(x, c, f2)}, nil
}

func xuiDelete(x *engine.Exec, c *xuiConn, p map[string]any) (engine.Result, error) {
	f, miss, err := xuiLookup(x, c, p)
	if err != nil || miss != nil {
		return deref(miss), err
	}
	if err := c.DeleteClient(x.Ctx, f); err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: map[string]any{"email": f.Traffic.Email, "inbound_id": float64(f.Inbound.ID)}}, nil
}

func xuiFind(x *engine.Exec, c *xuiConn, p map[string]any) (engine.Result, error) {
	tgID := strings.TrimSpace(tmpl.ToString(p["tg_id"]))
	search := strings.ToLower(strings.TrimSpace(tmpl.ToString(p["search"])))
	only, hasOnly, err := xuiNum(p, "inbound_id")
	if err != nil {
		return engine.Result{}, err
	}
	ins, err := c.Inbounds(x.Ctx)
	if err != nil {
		return engine.Result{}, err
	}
	sub := c.SubBase(x.Ctx)
	now := time.Now()
	list := []any{}
	for i := range ins {
		in := &ins[i]
		if hasOnly && only != 0 && in.ID != int(only) {
			continue
		}
		clients, err := in.Clients()
		if err != nil {
			continue
		}
		for _, cl := range clients {
			if tgID != "" && tmpl.ToString(cl["tgId"]) != tgID {
				continue
			}
			if search != "" && !strings.Contains(strings.ToLower(tmpl.ToString(cl["email"])), search) &&
				!strings.Contains(strings.ToLower(tmpl.ToString(cl["comment"])), search) {
				continue
			}
			email := tmpl.ToString(cl["email"])
			st, _ := in.Stat(email)
			list = append(list, xui.Info(&xui.Found{Inbound: in, Client: cl, Traffic: st}, sub, c.address, now))
		}
	}
	out := engine.Main
	if len(list) == 0 {
		out = outNotFound
	}
	return engine.Result{Output: out, Data: map[string]any{"clients": list, "count": float64(len(list))}}, nil
}

func xuiInbounds(x *engine.Exec, c *xuiConn, _ map[string]any) (engine.Result, error) {
	ins, err := c.Inbounds(x.Ctx)
	if err != nil {
		return engine.Result{}, err
	}
	list := make([]any, len(ins))
	for i := range ins {
		list[i] = xui.InboundInfo(&ins[i])
	}
	return engine.Result{Output: engine.Main, Data: map[string]any{"inbounds": list, "count": float64(len(list))}}, nil
}

func xuiOnlines(x *engine.Exec, c *xuiConn, _ map[string]any) (engine.Result, error) {
	emails, err := c.Onlines(x.Ctx)
	if err != nil {
		return engine.Result{}, err
	}
	list := make([]any, len(emails))
	for i, e := range emails {
		list[i] = e
	}
	return engine.Result{Output: engine.Main, Data: map[string]any{"emails": list, "count": float64(len(list))}}, nil
}

func xuiRaw(x *engine.Exec, c *xuiConn, p map[string]any) (engine.Result, error) {
	method := strings.ToUpper(tmpl.ToString(p["method"]))
	if method == "" {
		method = http.MethodGet
	}
	path := "/" + strings.TrimLeft(tmpl.ToString(p["path"]), "/")
	var body any
	if b := p["body"]; b != nil && b != "" {
		body = b
	}
	raw, err := c.Call(x.Ctx, method, path, body)
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Output: engine.Main, Data: decodeJSON(raw)}, nil
}

func int64Of(v any) int64 {
	n, _ := tmpl.ToInt64(v)
	return n
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
