package nodes

import (
	"strings"
	"testing"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func TestCheckReportsAllIssues(t *testing.T) {
	wf := &workflow.Workflow{
		Nodes: []workflow.Node{
			{ID: "t", Type: "trigger.command", Params: map[string]any{"commands": []any{"start"}}},
			{ID: "send", Type: "telegram.send_message", Params: map[string]any{}},
			{ID: "bad", Type: "nope.x"},
			{ID: "cond", Type: "logic.if", Params: map[string]any{"condition": "text ==="}},
			{ID: "lonely", Type: "logic.log", Params: map[string]any{"message": "x"}},
			{ID: "q", Type: "db.query", Params: map[string]any{"query": "SELECT 1"}},
		},
		Connections: map[string]map[string][]string{
			"t":    {"main": {"send", "cond", "ghost", "q"}},
			"send": {"main": {"t"}},
		},
	}
	got := map[string]string{}
	for _, is := range engine.Check(wf) {
		got[is.Node+"|"+is.Level] += is.Message + ";"
	}
	want := map[string]string{
		"send|error":     "«متن» خالی است",
		"bad|error":      "نوع ناشناخته",
		"cond|error":     "compile",
		"t|error":        "ghost",
		"lonely|warning": "وصل نیست",
		"|error":         "دیتابیس \"main\"",
	}
	for k, sub := range want {
		if !strings.Contains(got[k], sub) {
			t.Errorf("%s: want %q in %q", k, sub, got[k])
		}
	}
	if !strings.Contains(got["t|error"], "trigger") {
		t.Errorf("connecting into a trigger must be reported: %q", got["t|error"])
	}

	ok, err := workflow.Load("../../examples/community-bot/workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, is := range engine.Check(ok) {
		if is.Level == "error" {
			t.Errorf("community-bot: %v", is)
		}
	}
}
