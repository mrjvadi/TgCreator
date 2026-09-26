// Package workflow defines the file format exported by the web builder.
//
// A workflow is a graph (like n8n): nodes plus named connections between
// them. The runtime compiles it once and executes it for each update.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Workflow struct {
	Name      string         `json:"name"`
	Version   int            `json:"version"`
	Bot       Bot            `json:"bot"`
	Runtime   Runtime        `json:"runtime"`
	Services  Services       `json:"services"`
	Variables map[string]any `json:"variables"`
	Nodes     []Node         `json:"nodes"`
	// Connections: source node id -> output name -> target node ids.
	// Most nodes use the "main" output; logic.if uses "true"/"false";
	// any node may have an "error" output.
	Connections map[string]map[string][]string `json:"connections"`
}

type Bot struct {
	Token     string  `json:"token"`
	APIURL    string  `json:"api_url,omitempty"`
	Mode      string  `json:"mode,omitempty"` // "polling" (default) or "webhook"
	ParseMode string  `json:"parse_mode,omitempty"`
	Webhook   Webhook `json:"webhook,omitempty"`
}

type Webhook struct {
	URL            string `json:"url"`
	Listen         string `json:"listen,omitempty"` // default ":8080"
	Path           string `json:"path,omitempty"`   // default "/webhook"
	SecretToken    string `json:"secret_token,omitempty"`
	MaxConnections int    `json:"max_connections,omitempty"`
}

type Runtime struct {
	Workers       int    `json:"workers,omitempty"`    // default: 4 x CPU
	QueueSize     int    `json:"queue_size,omitempty"` // per worker
	MaxSteps      int    `json:"max_steps,omitempty"`  // loop guard per update
	DropPending   bool   `json:"drop_pending,omitempty"`
	LogLevel      string `json:"log_level,omitempty"`
	StateBackend  string `json:"state_backend,omitempty"` // "memory" (default) or "redis"
	StateTTL      string `json:"state_ttl,omitempty"`     // e.g. "24h"
	HealthListen  string `json:"health_listen,omitempty"` // e.g. ":9090"
	HandleTimeout string `json:"handle_timeout,omitempty"`
}

type Services struct {
	Redis     *Redis              `json:"redis,omitempty"`
	Databases map[string]Database `json:"databases,omitempty"`
}

type Redis struct {
	URL   string `json:"url"`
	Image string `json:"image,omitempty"` // compose image override
}

type Database struct {
	Driver       string   `json:"driver"` // postgres | mysql | sqlite
	DSN          string   `json:"dsn"`
	MaxOpenConns int      `json:"max_open_conns,omitempty"`
	MaxIdleConns int      `json:"max_idle_conns,omitempty"`
	Migrations   []string `json:"migrations,omitempty"` // run at startup, in order
	Image        string   `json:"image,omitempty"`
}

type Node struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Name            string         `json:"name,omitempty"`
	Params          map[string]any `json:"params,omitempty"`
	Disabled        bool           `json:"disabled,omitempty"`
	ContinueOnError bool           `json:"continue_on_error,omitempty"`
	// Position and other UI-only data are ignored by the runtime.
	Position any `json:"position,omitempty"`
}

// Load reads a workflow file and resolves ${ENV} references in the
// deployment settings (token, URLs, DSNs). Environment variables
// TGC_BOT_TOKEN, TGC_REDIS_URL and TGC_DB_<NAME>_DSN override the file,
// which is what the generated docker-compose uses.
func Load(path string) (*Workflow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Workflow, error) {
	var wf Workflow
	if err := json.Unmarshal(b, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	wf.resolveEnv()
	if err := wf.Validate(); err != nil {
		return nil, err
	}
	return &wf, nil
}

func (wf *Workflow) resolveEnv() {
	wf.Bot.Token = override("TGC_BOT_TOKEN", ExpandEnv(wf.Bot.Token))
	wf.Bot.APIURL = override("TGC_BOT_API_URL", ExpandEnv(wf.Bot.APIURL))
	wf.Bot.Mode = override("TGC_BOT_MODE", ExpandEnv(wf.Bot.Mode))
	wf.Bot.Webhook.URL = override("TGC_WEBHOOK_URL", ExpandEnv(wf.Bot.Webhook.URL))
	wf.Bot.Webhook.SecretToken = override("TGC_WEBHOOK_SECRET", ExpandEnv(wf.Bot.Webhook.SecretToken))
	wf.Bot.Webhook.Listen = ExpandEnv(wf.Bot.Webhook.Listen)
	if url := os.Getenv("TGC_REDIS_URL"); url != "" {
		if wf.Services.Redis == nil {
			wf.Services.Redis = &Redis{}
		}
		wf.Services.Redis.URL = url
	} else if wf.Services.Redis != nil {
		wf.Services.Redis.URL = ExpandEnv(wf.Services.Redis.URL)
	}
	for name, db := range wf.Services.Databases {
		db.DSN = override(DSNEnv(name), ExpandEnv(db.DSN))
		wf.Services.Databases[name] = db
	}
}

// DSNEnv is the environment variable that overrides a database's DSN.
func DSNEnv(name string) string {
	return "TGC_DB_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name)) + "_DSN"
}

func override(env, v string) string {
	if e := os.Getenv(env); e != "" {
		return e
	}
	return v
}

// ExpandEnv expands $VAR, ${VAR} and ${VAR:-default}.
func ExpandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		if name, def, ok := strings.Cut(key, ":-"); ok {
			if v := os.Getenv(name); v != "" {
				return v
			}
			return def
		}
		return os.Getenv(key)
	})
}

// Validate checks structural consistency; node types are checked by the engine.
func (wf *Workflow) Validate() error {
	ids := make(map[string]bool, len(wf.Nodes))
	for i, n := range wf.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node #%d: missing id", i)
		}
		if n.Type == "" {
			return fmt.Errorf("node %q: missing type", n.ID)
		}
		if ids[n.ID] {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = true
	}
	for from, outs := range wf.Connections {
		if !ids[from] {
			return fmt.Errorf("connection from unknown node %q", from)
		}
		for out, targets := range outs {
			for _, to := range targets {
				if !ids[to] {
					return fmt.Errorf("connection %s.%s -> unknown node %q", from, out, to)
				}
			}
		}
	}
	for name, db := range wf.Services.Databases {
		switch db.Driver {
		case "postgres", "mysql", "sqlite":
		default:
			return fmt.Errorf("database %q: unsupported driver %q (postgres, mysql, sqlite)", name, db.Driver)
		}
	}
	return nil
}
