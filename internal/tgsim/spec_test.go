package tgsim

import (
	"slices"
	"strings"
	"testing"

	"github.com/mrjvadi/tgcreator/internal/tg"
)

func TestRuntimeKnowsEveryUpdateType(t *testing.T) {
	spec := LoadSpec()
	for _, u := range spec.UpdateTypes() {
		if !slices.Contains(tg.AllUpdateTypes, u) {
			t.Errorf("tg.AllUpdateTypes is missing %q (%s)", u, spec.Version)
		}
	}
	for _, u := range tg.AllUpdateTypes {
		if !slices.Contains(spec.UpdateTypes(), u) {
			t.Errorf("tg.AllUpdateTypes has %q which is not in %s", u, spec.Version)
		}
	}
}

func TestValidator(t *testing.T) {
	spec := LoadSpec()
	cases := []struct {
		method string
		params map[string]any
		want   string // substring of the first error, "" = valid
	}{
		{"sendMessage", map[string]any{"chat_id": -100.0, "text": "hi"}, ""},
		{"sendMessage", map[string]any{"chat_id": "@channel", "text": "hi"}, ""},
		{"sendMessage", map[string]any{"text": "hi"}, `missing required parameter "chat_id"`},
		{"sendMessage", map[string]any{"chat_id": 1.0, "text": "hi", "buttons": []any{}}, `unknown parameter "buttons"`},
		{"sendMessage", map[string]any{"chat_id": 1.5, "text": "hi"}, "expected Integer"},
		{"sendMessage", map[string]any{"chat_id": 1.0, "text": "x", "reply_markup": map[string]any{
			"inline_keyboard": []any{[]any{map[string]any{"text": "a", "callback_data": "b"}}},
		}}, ""},
		{"sendMessage", map[string]any{"chat_id": 1.0, "text": "x", "reply_markup": map[string]any{
			"inline_keyboard": []any{[]any{map[string]any{"txt": "a"}}},
		}}, `requires field "text"`},
		{"sendMediaGroup", map[string]any{"chat_id": 1.0, "media": []any{
			map[string]any{"type": "photo", "media": "attach://file1"},
			map[string]any{"type": "video", "media": "https://x/v.mp4"},
		}}, ""},
		{"sendMediaGroup", map[string]any{"chat_id": 1.0, "media": []any{
			map[string]any{"type": "photo", "media": "attach://missing"},
		}}, "not uploaded"},
		{"restrictChatMember", map[string]any{"chat_id": 1.0, "user_id": 2.0, "permissions": map[string]any{"can_send_messages": "no"}}, "expected Boolean"},
		{"nopeMethod", map[string]any{}, "unknown method"},
	}
	for _, c := range cases {
		_, errs := spec.ValidateCall(c.method, c.params, false, map[string]bool{"file1": true})
		switch {
		case c.want == "" && len(errs) > 0:
			t.Errorf("%s: unexpected errors %v", c.method, errs)
		case c.want != "" && (len(errs) == 0 || !strings.Contains(strings.Join(errs, "\n"), c.want)):
			t.Errorf("%s: want error containing %q, got %v", c.method, c.want, errs)
		}
	}
}
