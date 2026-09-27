package xui

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
