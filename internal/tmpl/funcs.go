package tmpl

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"

	"github.com/expr-lang/expr"
)

var linkRe = regexp.MustCompile(`(?i)(https?://|www\.|t\.me/|telegram\.me/|tg://)\S+|\b[a-z0-9-]+\.(com|net|org|ir|io|me|xyz|info|app|dev|link|site)\b`)

// Extra functions available to every expression on top of expr's builtins
// (lower, upper, trim, split, len, now, ...).
var functions = []expr.Option{
	expr.Function("hasLink", func(p ...any) (any, error) { return hasLink(p[0]), nil },
		new(func(any) bool)),
	expr.Function("hasMention", func(p ...any) (any, error) { return hasEntity(p[0], "mention", "text_mention"), nil },
		new(func(any) bool)),
	expr.Function("hasEntity", func(p ...any) (any, error) {
		types := make([]string, 0, len(p)-1)
		for _, t := range p[1:] {
			types = append(types, ToString(t))
		}
		return hasEntity(p[0], types...), nil
	}),
	expr.Function("str", func(p ...any) (any, error) { return ToString(p[0]), nil },
		new(func(any) string)),
	expr.Function("escapeHTML", func(p ...any) (any, error) { return html.EscapeString(ToString(p[0])), nil },
		new(func(any) string)),
	expr.Function("mention", func(p ...any) (any, error) { return mention(p[0]), nil },
		new(func(any) string)),
	expr.Function("toJSON", func(p ...any) (any, error) {
		b, err := json.Marshal(p[0])
		return string(b), err
	}, new(func(any) string)),
	expr.Function("fromJSON", func(p ...any) (any, error) {
		var v any
		err := json.Unmarshal([]byte(ToString(p[0])), &v)
		return v, err
	}, new(func(any) any)),
	// at(list, i) / at(map, key): nil instead of an error when missing.
	expr.Function("at", func(p ...any) (any, error) { return at(p[0], p[1]), nil }),
	expr.Function("coalesce", func(p ...any) (any, error) {
		for _, v := range p {
			if v != nil && v != "" {
				return v, nil
			}
		}
		return nil, nil
	}),
}

// hasLink accepts a message object (checks entities too) or plain text.
func hasLink(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		if hasEntity(t, "url", "text_link") {
			return true
		}
		return linkRe.MatchString(ToString(t["text"])) || linkRe.MatchString(ToString(t["caption"]))
	case nil:
		return false
	default:
		return linkRe.MatchString(ToString(t))
	}
}

func hasEntity(v any, types ...string) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range [...]string{"entities", "caption_entities"} {
		ents, _ := m[key].([]any)
		for _, e := range ents {
			em, _ := e.(map[string]any)
			typ, _ := em["type"].(string)
			for _, want := range types {
				if typ == want {
					return true
				}
			}
		}
	}
	return false
}

// mention renders an HTML link to a user object.
func mention(v any) string {
	u, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	name := strings.TrimSpace(ToString(u["first_name"]) + " " + ToString(u["last_name"]))
	if name == "" {
		name = ToString(u["username"])
	}
	return `<a href="tg://user?id=` + ToString(u["id"]) + `">` + html.EscapeString(name) + `</a>`
}

func at(container, key any) any {
	switch c := container.(type) {
	case []any:
		i, ok := ToInt64(key)
		if !ok {
			return nil
		}
		if i < 0 {
			i += int64(len(c))
		}
		if i < 0 || i >= int64(len(c)) {
			return nil
		}
		return c[i]
	case []string:
		i, ok := ToInt64(key)
		if !ok || i < 0 || i >= int64(len(c)) {
			return nil
		}
		return c[i]
	case map[string]any:
		return c[ToString(key)]
	case string:
		i, ok := ToInt64(key)
		r := []rune(c)
		if !ok || i < 0 || i >= int64(len(r)) {
			return nil
		}
		return string(r[i])
	}
	return nil
}
