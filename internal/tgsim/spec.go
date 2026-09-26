// Package tgsim is a Telegram Bot API simulator for end-to-end tests.
//
// It validates every request against the official Bot API specification
// (botapi.json, generated from https://core.telegram.org/bots/api by
// github.com/PaulSonOfLars/telegram-bot-api-spec) and keeps a model of
// chats, members and messages so scenarios behave like real Telegram,
// including its error responses.
package tgsim

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed botapi.json
var specJSON []byte

type Field struct {
	Name        string   `json:"name"`
	Types       []string `json:"types"`
	Required    bool     `json:"required"`
	Description string   `json:"description"`
}

type Method struct {
	Name    string   `json:"name"`
	Fields  []Field  `json:"fields"`
	Returns []string `json:"returns"`
}

type Type struct {
	Name     string   `json:"name"`
	Fields   []Field  `json:"fields"`
	Subtypes []string `json:"subtypes"`
}

type Spec struct {
	Version string             `json:"version"`
	Methods map[string]*Method `json:"methods"`
	Types   map[string]*Type   `json:"types"`
}

// LoadSpec parses the embedded Bot API specification.
func LoadSpec() *Spec {
	var s Spec
	if err := json.Unmarshal(specJSON, &s); err != nil {
		panic("tgsim: bad embedded spec: " + err.Error())
	}
	return &s
}

// UpdateTypes lists the update kinds defined by the spec.
func (s *Spec) UpdateTypes() []string {
	var out []string
	for _, f := range s.Types["Update"].Fields {
		if f.Name != "update_id" {
			out = append(out, f.Name)
		}
	}
	return out
}

// MethodNames returns all method names, sorted.
func (s *Spec) MethodNames() []string {
	out := make([]string, 0, len(s.Methods))
	for n := range s.Methods {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (m *Method) field(name string) *Field {
	for i := range m.Fields {
		if m.Fields[i].Name == name {
			return &m.Fields[i]
		}
	}
	return nil
}

var constRe = regexp.MustCompile(`(?:must be|always)\s+[“"]?([a-z_]+)[”"]?`)

// ConstValue extracts the fixed discriminator value of a field, e.g. the
// "type" of InputMediaPhoto is always "photo".
func ConstValue(f Field) string {
	switch f.Name {
	case "type", "status", "source":
	default:
		return ""
	}
	if m := constRe.FindStringSubmatch(f.Description); m != nil {
		return m[1]
	}
	return ""
}

// ValidateCall checks a request the way Telegram does: the method exists,
// required parameters are present, there are no unknown parameters and
// every value has the documented type (recursively for objects).
// multipart=true means top-level values arrived as form strings; files is
// the set of uploaded part names.
func (s *Spec) ValidateCall(method string, params map[string]any, multipart bool, files map[string]bool) (typed map[string]any, errs []string) {
	m, ok := s.Methods[method]
	if !ok {
		return nil, []string{fmt.Sprintf("unknown method %q", method)}
	}
	v := &validator{spec: s, files: files}
	typed = make(map[string]any, len(params))
	for k, val := range params {
		f := m.field(k)
		if f == nil {
			v.errorf("%s: unknown parameter %q", method, k)
			continue
		}
		if multipart {
			val = fromForm(val, f.Types)
		}
		typed[k] = val
		v.check(method+"."+k, val, f.Types)
	}
	for _, f := range m.Fields {
		if !f.Required {
			continue
		}
		if _, ok := params[f.Name]; ok {
			continue
		}
		if files[f.Name] && accepts(f.Types, "InputFile") {
			continue
		}
		v.errorf("%s: missing required parameter %q", method, f.Name)
	}
	return typed, v.errs
}

func accepts(types []string, t string) bool {
	for _, x := range types {
		if x == t {
			return true
		}
	}
	return false
}

// fromForm converts a multipart string to the JSON value it stands for.
func fromForm(val any, types []string) any {
	s, ok := val.(string)
	if !ok {
		return val
	}
	for _, t := range types {
		switch {
		case t == "Integer" || t == "Float":
			if n, err := strconv.ParseFloat(s, 64); err == nil {
				return n
			}
		case t == "Boolean" || t == "True":
			if b, err := strconv.ParseBool(s); err == nil {
				return b
			}
		case strings.HasPrefix(t, "Array of") || (t != "String" && t != "InputFile"):
			var j any
			if json.Unmarshal([]byte(s), &j) == nil {
				if _, isStr := j.(string); !isStr {
					return j
				}
			}
		}
	}
	return s
}

type validator struct {
	spec  *Spec
	files map[string]bool
	errs  []string
}

func (v *validator) errorf(format string, a ...any) {
	v.errs = append(v.errs, fmt.Sprintf(format, a...))
}

// check validates val against any of the allowed types.
func (v *validator) check(path string, val any, types []string) {
	// "Array of A, Array of B" in the docs means a list whose items may be
	// A or B (albums mix photos and videos).
	if len(types) > 1 {
		elems := make([]string, 0, len(types))
		for _, t := range types {
			if e, ok := strings.CutPrefix(t, "Array of "); ok {
				elems = append(elems, e)
			}
		}
		if len(elems) == len(types) {
			list, ok := val.([]any)
			if !ok {
				v.errorf("%s: expected array, got %s", path, describe(val))
				return
			}
			for i, x := range list {
				v.check(fmt.Sprintf("%s[%d]", path, i), x, elems)
			}
			return
		}
	}
	var best []string
	for i, t := range types {
		errs := v.match(path, val, t)
		if len(errs) == 0 {
			return
		}
		if i == 0 || len(errs) < len(best) {
			best = errs
		}
	}
	v.errs = append(v.errs, best...)
}

func (v *validator) match(path string, val any, t string) []string {
	bad := func() []string {
		return []string{fmt.Sprintf("%s: expected %s, got %s", path, t, describe(val))}
	}
	switch t {
	case "Integer":
		if f, ok := val.(float64); ok && f == float64(int64(f)) {
			return nil
		}
		return bad()
	case "Float":
		if _, ok := val.(float64); ok {
			return nil
		}
		return bad()
	case "String":
		s, ok := val.(string)
		if !ok {
			return bad()
		}
		if name, isRef := strings.CutPrefix(s, "attach://"); isRef && !v.files[name] {
			return []string{fmt.Sprintf("%s: attach://%s refers to a file that was not uploaded", path, name)}
		}
		return nil
	case "Boolean":
		if _, ok := val.(bool); ok {
			return nil
		}
		return bad()
	case "True":
		if b, ok := val.(bool); ok && b {
			return nil
		}
		return bad()
	case "InputFile":
		if s, ok := val.(string); ok {
			if name, isRef := strings.CutPrefix(s, "attach://"); isRef && v.files[name] {
				return nil
			}
		}
		return bad()
	}
	if elem, ok := strings.CutPrefix(t, "Array of "); ok {
		list, ok := val.([]any)
		if !ok {
			return bad()
		}
		sub := &validator{spec: v.spec, files: v.files}
		for i, x := range list {
			sub.check(fmt.Sprintf("%s[%d]", path, i), x, []string{elem})
		}
		return sub.errs
	}
	typ, ok := v.spec.Types[t]
	if !ok {
		return []string{fmt.Sprintf("%s: unknown type %s in spec", path, t)}
	}
	if len(typ.Subtypes) > 0 {
		var best []string
		for i, st := range typ.Subtypes {
			errs := v.match(path, val, st)
			if len(errs) == 0 {
				return nil
			}
			if i == 0 || len(errs) < len(best) {
				best = errs
			}
		}
		return best
	}
	obj, ok := val.(map[string]any)
	if !ok {
		return bad()
	}
	sub := &validator{spec: v.spec, files: v.files}
	known := make(map[string]bool, len(typ.Fields))
	for _, f := range typ.Fields {
		known[f.Name] = true
		x, present := obj[f.Name]
		if !present {
			if f.Required {
				sub.errorf("%s: %s requires field %q", path, t, f.Name)
			}
			continue
		}
		if c := ConstValue(f); c != "" && x != c {
			sub.errorf("%s.%s: %s requires %q, got %v", path, f.Name, t, c, x)
			continue
		}
		sub.check(path+"."+f.Name, x, f.Types)
	}
	for k := range obj {
		if !known[k] {
			sub.errorf("%s: %s has no field %q", path, t, k)
		}
	}
	return sub.errs
}

func describe(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("string %q", t)
	case float64:
		return "number " + strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}
