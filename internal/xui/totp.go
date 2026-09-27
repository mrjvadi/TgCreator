package xui

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238 TOTP uses HMAC-SHA1
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// TOTP returns the 6-digit RFC 6238 code of a base32 secret, as the
// panel's two-factor login expects.
func TOTP(secret string, t time.Time) (string, error) {
	s := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(t.Unix()/30))
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1_000_000), nil
}
