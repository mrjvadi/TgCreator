package xui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Links builds the share links of a client the way the panel's own
// subscription service does (vmess, vless, trojan, shadowsocks,
// hysteria2), one per external proxy when the inbound defines them.
// address is the server host used when the inbound listens on all
// interfaces.
func Links(in *Inbound, client map[string]any, address string) []string {
	email, _ := client["email"].(string)
	stream := map[string]any{}
	_ = json.Unmarshal([]byte(in.StreamSettings), &stream)
	settings := map[string]any{}
	_ = json.Unmarshal([]byte(in.Settings), &settings)
	host := address
	if l := in.Listen; l != "" && l != "0.0.0.0" && l != "::" && l != "::0" && !strings.HasPrefix(l, "@") {
		host = l
	}
	remark := strings.Trim(in.Remark+"-"+email, "-")
	network := str(stream["network"])
	if network == "" {
		network = "tcp"
	}
	security := str(stream["security"])
	proxies, _ := stream["externalProxy"].([]any)

	type endpoint struct {
		host, security, remark string
		port                   int
		omitTLS                bool
	}
	eps := []endpoint{{host: host, port: in.Port, security: security, remark: remark}}
	if len(proxies) > 0 {
		eps = eps[:0]
		for _, p := range proxies {
			ep, _ := p.(map[string]any)
			sec := security
			if f := str(ep["forceTls"]); f != "" && f != "same" {
				sec = f
			}
			r := remark
			if extra := str(ep["remark"]); extra != "" {
				r += "-" + extra
			}
			eps = append(eps, endpoint{host: str(ep["dest"]), port: toInt(ep["port"]), security: sec, remark: r, omitTLS: str(ep["forceTls"]) == "none"})
		}
	}

	var out []string
	switch in.Protocol {
	case "vmess":
		obj := map[string]any{"v": "2", "type": "none", "id": str(client["id"]), "scy": str(client["security"])}
		vmessNetwork(stream, network, obj)
		if security == "tls" {
			tlsObj(stream, obj)
		}
		for _, ep := range eps {
			o := map[string]any{}
			for k, v := range obj {
				if ep.omitTLS && (k == "alpn" || k == "sni" || k == "fp") {
					continue
				}
				o[k] = v
			}
			o["add"], o["port"], o["ps"], o["tls"] = ep.host, ep.port, ep.remark, ep.security
			raw, _ := json.MarshalIndent(o, "", "  ")
			out = append(out, "vmess://"+base64.StdEncoding.EncodeToString(raw))
		}
		return out
	case "vless", "trojan", "shadowsocks":
		params := map[string]string{"type": network}
		if in.Protocol == "vless" {
			if enc := str(settings["encryption"]); enc != "" {
				params["encryption"] = enc
			}
		}
		shareNetwork(stream, network, params)
		switch security {
		case "tls":
			tlsParams(stream, params)
		case "reality":
			if in.Protocol != "shadowsocks" {
				realityParams(stream, params)
			}
		default:
			if in.Protocol != "shadowsocks" {
				params["security"] = "none"
			}
		}
		if flow := str(client["flow"]); flow != "" && network == "tcp" && (security == "reality" || (security == "tls" && in.Protocol == "vless")) {
			params["flow"] = flow
		}
		var user string
		switch in.Protocol {
		case "vless":
			user = str(client["id"])
		case "trojan":
			user = str(client["password"])
		default:
			method := str(settings["method"])
			enc := method + ":" + str(client["password"])
			if strings.HasPrefix(method, "2") {
				enc = method + ":" + str(settings["password"]) + ":" + str(client["password"])
			}
			user = base64.StdEncoding.EncodeToString([]byte(enc))
		}
		scheme := map[string]string{"vless": "vless", "trojan": "trojan", "shadowsocks": "ss"}[in.Protocol]
		for _, ep := range eps {
			p := map[string]string{}
			for k, v := range params {
				if ep.omitTLS && (k == "alpn" || k == "sni" || k == "fp") {
					continue
				}
				p[k] = v
			}
			if len(proxies) > 0 {
				p["security"] = ep.security
			}
			out = append(out, buildLink(fmt.Sprintf("%s://%s@%s", scheme, user, joinHostPort(ep.host, ep.port)), p, ep.remark))
		}
		return out
	case "hysteria", "hysteria2":
		params := map[string]string{"security": "tls"}
		tlsParams(stream, params)
		scheme := "hysteria2"
		if toInt(settings["version"]) == 1 {
			scheme = "hysteria"
		}
		for _, ep := range eps {
			out = append(out, buildLink(fmt.Sprintf("%s://%s@%s", scheme, str(client["auth"]), joinHostPort(ep.host, ep.port)), params, ep.remark))
		}
		return out
	}
	return nil
}

func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(port)
}

func buildLink(base string, params map[string]string, remark string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	u.Fragment = remark
	return u.String()
}

func shareNetwork(stream map[string]any, network string, p map[string]string) {
	switch network {
	case "tcp":
		header := obj(obj(stream["tcpSettings"])["header"])
		if str(header["type"]) == "http" {
			req := obj(header["request"])
			if paths, _ := req["path"].([]any); len(paths) > 0 {
				p["path"] = str(paths[0])
			}
			p["host"] = searchHost(req["headers"])
			p["headerType"] = "http"
		}
	case "kcp":
		kcp := obj(stream["kcpSettings"])
		if t := str(obj(kcp["header"])["type"]); t != "" {
			p["headerType"] = t
		}
		if seed := str(kcp["seed"]); seed != "" {
			p["seed"] = seed
		}
	case "ws":
		pathHost(obj(stream["wsSettings"]), p)
	case "grpc":
		g := obj(stream["grpcSettings"])
		p["serviceName"] = str(g["serviceName"])
		if a := str(g["authority"]); a != "" {
			p["authority"] = a
		}
		if m, _ := g["multiMode"].(bool); m {
			p["mode"] = "multi"
		}
	case "httpupgrade":
		pathHost(obj(stream["httpupgradeSettings"]), p)
	case "xhttp":
		x := obj(stream["xhttpSettings"])
		pathHost(x, p)
		if m := str(x["mode"]); m != "" {
			p["mode"] = m
		}
	}
}

func vmessNetwork(stream map[string]any, network string, o map[string]any) {
	o["net"] = network
	p := map[string]string{}
	shareNetwork(stream, network, p)
	switch network {
	case "tcp":
		o["type"] = orStr(str(obj(obj(stream["tcpSettings"])["header"])["type"]), "none")
	case "grpc":
		o["path"] = p["serviceName"]
		if p["authority"] != "" {
			o["authority"] = p["authority"]
		}
		if p["mode"] == "multi" {
			o["type"] = "multi"
		}
		return
	case "kcp":
		if p["headerType"] != "" {
			o["type"] = p["headerType"]
		}
		if p["seed"] != "" {
			o["path"] = p["seed"]
		}
		return
	case "xhttp":
		if p["mode"] != "" {
			o["mode"] = p["mode"]
		}
	}
	if v, ok := p["path"]; ok {
		o["path"] = v
	}
	if v, ok := p["host"]; ok {
		o["host"] = v
	}
}

func pathHost(s map[string]any, p map[string]string) {
	p["path"] = str(s["path"])
	if h := str(s["host"]); h != "" {
		p["host"] = h
	} else {
		p["host"] = searchHost(s["headers"])
	}
}

func tlsParams(stream map[string]any, p map[string]string) {
	p["security"] = "tls"
	t := obj(stream["tlsSettings"])
	if alpn := strList(t["alpn"]); len(alpn) > 0 {
		p["alpn"] = strings.Join(alpn, ",")
	}
	if sni := str(t["serverName"]); sni != "" {
		p["sni"] = sni
	}
	if fp := str(obj(t["settings"])["fingerprint"]); fp != "" {
		p["fp"] = fp
	}
}

func tlsObj(stream map[string]any, o map[string]any) {
	p := map[string]string{}
	tlsParams(stream, p)
	for _, k := range []string{"alpn", "sni", "fp"} {
		if v, ok := p[k]; ok {
			o[k] = v
		}
	}
}

func realityParams(stream map[string]any, p map[string]string) {
	p["security"] = "reality"
	r := obj(stream["realitySettings"])
	rs := obj(r["settings"])
	if names := strList(r["serverNames"]); len(names) > 0 {
		p["sni"] = names[0]
	}
	if v := str(rs["publicKey"]); v != "" {
		p["pbk"] = v
	}
	if ids := strList(r["shortIds"]); len(ids) > 0 {
		p["sid"] = ids[0]
	}
	if v := str(rs["fingerprint"]); v != "" {
		p["fp"] = v
	}
	if v := str(rs["mldsa65Verify"]); v != "" {
		p["pqv"] = v
	}
	if v := str(rs["spiderX"]); v != "" {
		p["spx"] = v
	}
}

func searchHost(headers any) string {
	for k, v := range obj(headers) {
		if strings.EqualFold(k, "host") {
			switch t := v.(type) {
			case []any:
				if len(t) > 0 {
					return str(t[0])
				}
				return ""
			default:
				return str(t)
			}
		}
	}
	return ""
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func strList(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		if s := str(x); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func toInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}
