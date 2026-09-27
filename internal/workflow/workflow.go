// Package workflow defines the file format exported by the web builder.
//
// A workflow is a graph (like n8n): nodes plus named connections between
// them. The runtime compiles it once and executes it for each update.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
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
	Workers       int    `json:"workers,omitempty"`    // updates handled at once (default 512)
	QueueSize     int    `json:"queue_size,omitempty"` // buffered updates before polling pauses (default 100000)
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
	// VPN are external VPN panels (X-UI, Marzban, ...) the bot manages.
	VPN map[string]VPNPanel `json:"vpn,omitempty"`
}

// VPNPanel is a connection to a VPN panel. Fields accept ${ENV}.
type VPNPanel struct {
	// Type is one of VPNTypes; "3x-ui" when empty.
	Type string `json:"type,omitempty"`
	// URL is the panel address as opened in a browser, including its
	// secret path (X-UI base path, Hiddify admin proxy path).
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// Token is an API token (Remnawave) or API key (Hiddify admin UUID).
	Token string `json:"token,omitempty"`
	// TOTPSecret is the panel's two-factor secret, when 2FA is on (X-UI).
	TOTPSecret string `json:"totp_secret,omitempty"`
	// SubURL is the subscription base; read from the panel when empty.
	SubURL string `json:"sub_url,omitempty"`
	// Address is the server host put into X-UI config links; defaults to the panel host.
	Address string `json:"address,omitempty"`
	// APIPath overrides the X-UI inbounds API path for panel forks.
	APIPath  string `json:"api_path,omitempty"`
	Insecure bool   `json:"insecure_tls,omitempty"` // accept self-signed certificates
	Timeout  string `json:"timeout,omitempty"`      // per request, default 15s
}

// VPNTypes are the supported panels.
var VPNTypes = []string{"3x-ui", "x-ui", "marzban", "pasarguard", "marzneshin", "remnawave", "hiddify"}

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

// LoadRaw reads a workflow without resolving ${ENV} references.
func LoadRaw(path string) (*Workflow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wf Workflow
	if err := json.Unmarshal(b, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	return &wf, nil
}

// EnvRefs lists the variables a value references ($VAR, ${VAR}, ${VAR:-x}).
func EnvRefs(s string) []string {
	var out []string
	os.Expand(s, func(key string) string {
		name, _, _ := strings.Cut(key, ":-")
		out = append(out, name)
		return ""
	})
	return out
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
	for name, p := range wf.Services.VPN {
		wf.Services.VPN[name] = p.Resolve(name)
	}
}

// VPNEnv is the prefix of the variables that override a VPN panel's
// settings: TGC_VPN_<NAME>_URL, _USERNAME, _PASSWORD, _TOKEN and _TOTP.
func VPNEnv(name string) string {
	return "TGC_VPN_" + envName(name)
}

// Resolve expands ${ENV} references and applies TGC_VPN_<NAME>_* overrides.
func (p VPNPanel) Resolve(name string) VPNPanel {
	env := VPNEnv(name)
	p.URL = override(env+"_URL", ExpandEnv(p.URL))
	p.Username = override(env+"_USERNAME", ExpandEnv(p.Username))
	p.Password = override(env+"_PASSWORD", ExpandEnv(p.Password))
	p.Token = override(env+"_TOKEN", ExpandEnv(p.Token))
	p.TOTPSecret = override(env+"_TOTP", ExpandEnv(p.TOTPSecret))
	p.SubURL = ExpandEnv(p.SubURL)
	p.Address = ExpandEnv(p.Address)
	return p
}

// Secrets are the fields that may reference environment variables.
func (p VPNPanel) Secrets() []string {
	return []string{p.URL, p.Username, p.Password, p.Token, p.TOTPSecret, p.SubURL, p.Address}
}

// DSNEnv is the environment variable that overrides a database's DSN.
func DSNEnv(name string) string {
	return "TGC_DB_" + envName(name) + "_DSN"
}

func envName(name string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(name))
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
	for name, p := range wf.Services.VPN {
		if p.Type != "" && !slices.Contains(VPNTypes, p.Type) {
			return fmt.Errorf("vpn panel %q: unsupported type %q (%s)", name, p.Type, strings.Join(VPNTypes, ", "))
		}
	}
	return nil
}
