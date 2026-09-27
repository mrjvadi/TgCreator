package xui

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

const (
	GB    = int64(1) << 30
	DayMs = int64(24 * time.Hour / time.Millisecond)
)

// Spec describes a client to create.
type Spec struct {
	Email          string
	TotalBytes     int64 // 0 = unlimited
	Days           float64
	AfterFirstUse  bool // expiry counts from the first connection
	LimitIP        int
	Flow           string
	TgID           string
	Comment        string
	SubID          string
	Disabled       bool
	CredentialSeed string // fixed id/password; random when empty
}

// NewClient builds the client object for an inbound's protocol the way the
// panel's own "add client" form does. panelType decides how tgId is typed
// (a number in 3x-ui, a string in x-ui).
func NewClient(in *Inbound, s Spec, panelType string, now time.Time) map[string]any {
	c := map[string]any{
		"email":      s.Email,
		"limitIp":    s.LimitIP,
		"totalGB":    s.TotalBytes,
		"expiryTime": ExpiryFor(s.Days, s.AfterFirstUse, now),
		"enable":     !s.Disabled,
		"subId":      orStr(s.SubID, RandomString(16)),
		"reset":      0,
	}
	if panelType == "x-ui" {
		c["tgId"] = s.TgID
	} else {
		id, _ := strconv.ParseInt(s.TgID, 10, 64)
		c["tgId"] = id
		c["comment"] = s.Comment
	}
	switch in.Protocol {
	case "vmess":
		c["id"] = orStr(s.CredentialSeed, UUID())
		c["security"] = "auto"
	case "vless":
		c["id"] = orStr(s.CredentialSeed, UUID())
		c["flow"] = ""
		if CanFlow(in) {
			c["flow"] = s.Flow
		}
	case "trojan":
		c["password"] = orStr(s.CredentialSeed, RandomString(10))
	case "shadowsocks":
		var st struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal([]byte(in.Settings), &st)
		c["method"] = ""
		c["password"] = orStr(s.CredentialSeed, ShadowsocksPassword(st.Method))
	case "hysteria", "hysteria2":
		c["auth"] = orStr(s.CredentialSeed, RandomString(16))
	default:
		c["id"] = orStr(s.CredentialSeed, UUID())
	}
	return c
}

// CanFlow reports whether clients of the inbound may use an XTLS flow:
// VLESS over TCP with TLS or Reality, as the panel's own form allows.
// Xray rejects a flow on any other inbound.
func CanFlow(in *Inbound) bool {
	var st struct {
		Network  string `json:"network"`
		Security string `json:"security"`
	}
	_ = json.Unmarshal([]byte(in.StreamSettings), &st)
	return in.Protocol == "vless" && (st.Network == "tcp" || st.Network == "raw" || st.Network == "") && (st.Security == "tls" || st.Security == "reality")
}

// ExpiryFor converts days to the panel's expiryTime: 0 = never, a positive
// epoch in ms, or a negative duration that starts at the first connection.
func ExpiryFor(days float64, afterFirstUse bool, now time.Time) int64 {
	if days <= 0 {
		return 0
	}
	ms := int64(math.Round(days * float64(DayMs)))
	if afterFirstUse {
		return -ms
	}
	return now.UnixMilli() + ms
}

// Extend adds days to an expiryTime. Unlimited stays unlimited, a pending
// (negative) expiry gets longer, and an expired one restarts from now.
func Extend(expiry int64, days float64, now time.Time) int64 {
	ms := int64(math.Round(days * float64(DayMs)))
	switch {
	case expiry == 0 || days == 0:
		return expiry
	case expiry < 0:
		return expiry - ms
	case expiry < now.UnixMilli():
		return now.UnixMilli() + ms
	}
	return expiry + ms
}

// Info flattens a client into the values bot messages need.
func Info(f *Found, subBase, address string, now time.Time) map[string]any {
	c, in, t := f.Client, f.Inbound, f.Traffic
	total := int64Of(c["totalGB"])
	if t.Total != 0 {
		total = t.Total
	}
	expiry := int64Of(c["expiryTime"])
	if t.ExpiryTime != 0 {
		expiry = t.ExpiryTime
	}
	used := t.Up + t.Down
	enable, _ := c["enable"].(bool)
	subID := str(c["subId"])
	links := Links(in, c, address)
	link := ""
	if len(links) > 0 {
		link = links[0]
	}
	out := map[string]any{
		"email":        str(c["email"]),
		"inbound_id":   float64(in.ID),
		"inbound":      in.Remark,
		"protocol":     in.Protocol,
		"id":           str(c[ClientKey(in.Protocol)]),
		"enable":       enable,
		"limit_ip":     float64(int64Of(c["limitIp"])),
		"tg_id":        str(c["tgId"]),
		"comment":      str(c["comment"]),
		"flow":         str(c["flow"]),
		"sub_id":       subID,
		"sub_link":     SubLink(subBase, subID),
		"link":         link,
		"links":        toAny(links),
		"up":           float64(t.Up),
		"down":         float64(t.Down),
		"used_bytes":   float64(used),
		"used_gb":      round2(float64(used) / float64(GB)),
		"total_bytes":  float64(total),
		"total_gb":     round2(float64(total) / float64(GB)),
		"unlimited":    total == 0,
		"expiry_time":  float64(expiry),
		"never_expire": expiry == 0,
		"last_online":  float64(t.LastOnline),
	}
	if total > 0 {
		rem := max(total-used, 0)
		out["remaining_bytes"] = float64(rem)
		out["remaining_gb"] = round2(float64(rem) / float64(GB))
		out["depleted"] = used >= total
	} else {
		out["remaining_bytes"], out["remaining_gb"], out["depleted"] = float64(-1), float64(-1), false
	}
	switch {
	case expiry > 0:
		at := time.UnixMilli(expiry).In(tehran)
		left := float64(expiry-now.UnixMilli()) / float64(DayMs)
		out["days_left"] = math.Max(0, math.Floor(left*10)/10)
		out["expired"] = left <= 0
		out["started"] = true
		out["expiry_date"] = at.Format("2006-01-02 15:04")
		out["expiry_jalali"] = Jalali(at)
	case expiry < 0:
		out["days_left"] = math.Floor(float64(-expiry)/float64(DayMs)*10) / 10
		out["expired"] = false
		out["started"] = false
		out["expiry_date"], out["expiry_jalali"] = "", ""
	default:
		out["days_left"] = float64(-1)
		out["expired"] = false
		out["started"] = true
		out["expiry_date"], out["expiry_jalali"] = "", ""
	}
	out["active"] = enable && !out["expired"].(bool) && !out["depleted"].(bool)
	return out
}

// InboundInfo summarizes an inbound for list outputs.
func InboundInfo(in *Inbound) map[string]any {
	clients, _ := in.Clients()
	return map[string]any{
		"id":       float64(in.ID),
		"remark":   in.Remark,
		"protocol": in.Protocol,
		"port":     float64(in.Port),
		"enable":   in.Enable,
		"clients":  float64(len(clients)),
		"up":       float64(in.Up),
		"down":     float64(in.Down),
		"used_gb":  round2(float64(in.Up+in.Down) / float64(GB)),
	}
}

var tehran = time.FixedZone("IRST", 3*3600+1800)

// Jalali formats t as a Solar Hijri date, YYYY/MM/DD.
func Jalali(t time.Time) string {
	gy, gm, gd := t.Date()
	jy, jm, jd := toJalali(gy, int(gm), gd)
	return fmt.Sprintf("%04d/%02d/%02d", jy, jm, jd)
}

// toJalali is the standard arithmetic Gregorian → Solar Hijri conversion.
func toJalali(gy, gm, gd int) (int, int, int) {
	gdm := [...]int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[gm-1]
	jy := -1595 + 33*(days/12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	var jm, jd int
	if days < 186 {
		jm, jd = 1+days/31, 1+days%31
	} else {
		jm, jd = 7+(days-186)/30, 1+(days-186)%30
	}
	return jy, jm, jd
}

// UUID returns a random version 4 UUID.
func UUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

const alnum = "abcdefghijklmnopqrstuvwxyz0123456789"

// RandomString returns n random lowercase letters and digits.
func RandomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alnum[int(b[i])%len(alnum)]
	}
	return string(b)
}

// ShadowsocksPassword returns a key of the length a 2022 method needs, or
// a random password for the classic ones.
func ShadowsocksPassword(method string) string {
	n := 32
	if method == "2022-blake3-aes-128-gcm" {
		n = 16
	}
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func int64Of(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	}
	return 0
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}
