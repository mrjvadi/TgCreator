package compose

import (
	"strings"
	"testing"

	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func TestOnlyRequiredServices(t *testing.T) {
	wf := &workflow.Workflow{Name: "x"}
	yml, err := Generate(wf, nil, Options{BuildContext: "."})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"redis:", "db-", "depends_on"} {
		if strings.Contains(yml, s) {
			t.Errorf("unexpected %q in:\n%s", s, yml)
		}
	}
	if !strings.Contains(yml, `TAGS: "no_redis no_vpn no_postgres no_mysql"`) {
		t.Errorf("expected minimal build tags:\n%s", yml)
	}
}

func TestRedisAndDatabases(t *testing.T) {
	wf := &workflow.Workflow{
		Name: "x",
		Services: workflow.Services{Databases: map[string]workflow.Database{
			"main":  {Driver: "postgres"},
			"stats": {Driver: "mysql"},
			"local": {Driver: "sqlite"},
		}},
	}
	yml, err := Generate(wf, []string{"db:local", "db:main", "db:stats", "redis"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"  redis:\n", "  db-main:\n", "  db-stats:\n",
		`TGC_REDIS_URL: "redis://redis:6379/0"`,
		"TGC_DB_MAIN_DSN", "TGC_DB_STATS_DSN", `TGC_DB_LOCAL_DSN: "file:/data/local.db`,
		"tgc-data:/data",
	} {
		if !strings.Contains(yml, s) {
			t.Errorf("missing %q in:\n%s", s, yml)
		}
	}
	if strings.Contains(yml, "db-local") {
		t.Error("sqlite must not get a container")
	}
}

func TestMissingDatabaseConfig(t *testing.T) {
	_, err := Generate(&workflow.Workflow{}, []string{"db:main"}, Options{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestVPNPanelIsExternal(t *testing.T) {
	wf := &workflow.Workflow{Name: "x", Services: workflow.Services{VPN: map[string]workflow.VPNPanel{
		"main": {URL: "${XUI_URL:-https://p.example.com/x/}", Username: "admin", Password: "${XUI_PASSWORD}"},
	}}}
	yml, err := Generate(wf, []string{"vpn:main"}, Options{BuildContext: "."})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`XUI_PASSWORD: "${XUI_PASSWORD:-}"`, `XUI_URL: "${XUI_URL:-}"`, `TAGS: "no_redis no_postgres no_mysql"`} {
		if !strings.Contains(yml, s) {
			t.Errorf("missing %q in:\n%s", s, yml)
		}
	}
	if strings.Contains(yml, "depends_on") {
		t.Errorf("a panel needs no container:\n%s", yml)
	}
}
