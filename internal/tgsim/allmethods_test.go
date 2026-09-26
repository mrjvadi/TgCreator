package tgsim_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mrjvadi/tgcreator/internal/engine"
	_ "github.com/mrjvadi/tgcreator/internal/nodes"
	"github.com/mrjvadi/tgcreator/internal/tg"
	"github.com/mrjvadi/tgcreator/internal/tgsim"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// gen builds a minimal valid value for a spec type.
func gen(spec *tgsim.Spec, f tgsim.Field, types []string, file string, depth int) any {
	for _, t := range types {
		if t == "InputFile" {
			return tg.FilePrefix + file
		}
	}
	t := types[0]
	switch t {
	case "Integer":
		return 1
	case "Float":
		return 1.5
	case "String":
		if strings.Contains(f.Description, "attach://") {
			return tg.FilePrefix + file
		}
		if c := tgsim.ConstValue(f); c != "" {
			return c
		}
		return "x"
	case "Boolean", "True":
		return true
	}
	if elem, ok := strings.CutPrefix(t, "Array of "); ok {
		return []any{gen(spec, tgsim.Field{Name: f.Name, Description: f.Description}, []string{elem}, file, depth+1)}
	}
	typ := spec.Types[t]
	if len(typ.Subtypes) > 0 {
		return gen(spec, f, []string{typ.Subtypes[0]}, file, depth+1)
	}
	obj := map[string]any{}
	if depth > 8 {
		return obj
	}
	for _, sf := range typ.Fields {
		if sf.Required {
			obj[sf.Name] = gen(spec, sf, sf.Types, file, depth+1)
		}
	}
	return obj
}

// TestEveryBotAPIMethod calls every method of the Bot API through the
// runtime (tg.<method> nodes) and checks each request against the spec:
// parameter names, types, nested objects and multipart file uploads.
func TestEveryBotAPIMethod(t *testing.T) {
	sim := tgsim.New("gopher_helper_bot")
	defer sim.Close()
	sim.ValidateOnly = true
	spec := sim.Spec

	file := filepath.Join(t.TempDir(), "sample.jpg")
	if err := os.WriteFile(file, []byte("\xff\xd8\xff fake jpeg"), 0o644); err != nil {
		t.Fatal(err)
	}

	wf := &workflow.Workflow{
		Name:        "all methods",
		Runtime:     workflow.Runtime{MaxSteps: 5000},
		Nodes:       []workflow.Node{{ID: "start", Type: "trigger.update", Params: map[string]any{"on": "*"}}},
		Connections: map[string]map[string][]string{},
	}
	prev := "start"
	var methods []string
	for _, name := range spec.MethodNames() {
		if name == "getUpdates" {
			continue // used by the runtime itself
		}
		m := spec.Methods[name]
		params := map[string]any{}
		for _, f := range m.Fields {
			if f.Required {
				params[f.Name] = gen(spec, f, f.Types, file, 0)
			}
		}
		id := "m_" + name
		wf.Nodes = append(wf.Nodes, workflow.Node{ID: id, Type: "tg." + name, Params: params, ContinueOnError: true})
		wf.Connections[prev] = map[string][]string{"main": {id}}
		prev = id
		methods = append(methods, name)
	}

	e, err := engine.New(wf, engine.Options{Client: tg.New(sim.Token, sim.URL)})
	if err != nil {
		t.Fatal(err)
	}
	e.HandleUpdate(context.Background(), map[string]any{"update_id": 1.0, "message": map[string]any{"message_id": 1.0, "text": "go"}})

	if len(sim.Violations) > 0 {
		for _, v := range sim.Violations {
			t.Error(v)
		}
	}
	called := map[string]bool{}
	uploads := 0
	for _, c := range sim.Calls {
		called[c.Method] = true
		for _, v := range c.Params {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "upload:") {
				uploads++
			}
		}
	}
	for _, m := range methods {
		if !called[m] {
			t.Errorf("method %s was never received", m)
		}
	}
	t.Logf("%s: %d methods called, %d spec violations, %d top-level file uploads", spec.Version, len(called), len(sim.Violations), uploads)
}
