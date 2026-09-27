package engine

import (
	"regexp"
	"sort"
	"strings"

	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// ButtonPrefix marks a connection output that starts from an inline button
// of the message a node sends: "btn:<callback_data>". The runtime turns
// every such output into an implicit trigger, so pressing the button runs
// the connected nodes (with message = the message holding the button).
const ButtonPrefix = "btn:"

var templateRe = regexp.MustCompile(`\{\{.*?\}\}`)

// buttonTrigger matches callback queries whose data equals the button's
// callback_data; {{ expressions }} inside it match any text.
type buttonTrigger struct {
	re     *regexp.Regexp
	data   string
	source string
}

func newButtonTrigger(source, data string) (*buttonTrigger, error) {
	parts := templateRe.Split(data, -1)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	re, err := regexp.Compile("^" + strings.Join(parts, ".*") + "$")
	if err != nil {
		return nil, err
	}
	return &buttonTrigger{re: re, data: data, source: source}, nil
}

func (b *buttonTrigger) UpdateTypes() []string { return []string{"callback_query"} }

func (b *buttonTrigger) Match(x *Exec) (any, bool) {
	data := tmpl.ToString(x.Callback()["data"])
	if !b.re.MatchString(data) {
		return nil, false
	}
	return map[string]any{"data": data, "button": b.data, "source": b.source}, true
}

// addButtonTriggers registers one implicit trigger per button output.
func (e *Engine) addButtonTriggers() error {
	ids := make([]string, 0, len(e.nodes))
	for id := range e.nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic trigger order
	for _, id := range ids {
		src := e.nodes[id]
		if src.trigger != nil {
			continue
		}
		for out, targets := range src.next {
			data, ok := strings.CutPrefix(out, ButtonPrefix)
			if !ok || len(targets) == 0 {
				continue
			}
			bt, err := newButtonTrigger(id, data)
			if err != nil {
				return err
			}
			n := &node{
				spec:    workflow.Node{ID: id + "#" + data, Type: "button"},
				trigger: bt,
				next:    map[string][]*node{Main: targets},
			}
			e.triggers["callback_query"] = append(e.triggers["callback_query"], n)
		}
	}
	return nil
}

// ButtonData lists the static callback_data values of a node's inline
// buttons (params.buttons), for validation.
func ButtonData(params map[string]any) (data []string, ok bool) {
	rows, isList := params["buttons"].([]any)
	if !isList {
		return nil, false
	}
	for _, r := range rows {
		row, isRow := r.([]any)
		if !isRow {
			row = []any{r}
		}
		for _, b := range row {
			if m, isMap := b.(map[string]any); isMap {
				if d, isStr := m["callback_data"].(string); isStr && d != "" {
					data = append(data, d)
				}
			}
		}
	}
	return data, true
}
