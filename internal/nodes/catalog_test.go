package nodes

import (
	"testing"

	"github.com/mrjvadi/tgcreator/internal/engine"
)

// Every node must be fully described for the web panel.
func TestEveryNodeHasBuilderMeta(t *testing.T) {
	editors := map[string]bool{"text": true, "textarea": true, "expr": true, "number": true, "bool": true, "select": true,
		"tags": true, "buttons": true, "keyboard": true, "json": true, "vars": true, "sql": true, "duration": true, "file": true, "method": true}
	cats := map[string]bool{"trigger": true, "telegram": true, "logic": true, "state": true, "redis": true, "db": true, "http": true}
	for _, nt := range engine.NodeTypes() {
		m := nt.Meta
		if m.Params == nil || m.Outputs == nil {
			t.Errorf("%s: params/outputs must be [] not null in JSON", nt.Name)
		}
		if m.Label == "" || m.Summary == "" || m.Icon == "" || !cats[m.Category] {
			t.Errorf("%s: missing label/summary/icon or unknown category %q", nt.Name, m.Category)
		}
		for _, p := range m.Params {
			if !editors[p.Type] || p.Label == "" {
				t.Errorf("%s.%s: bad editor %q or empty label", nt.Name, p.Name, p.Type)
			}
		}
	}
}
