// Command tgcreator runs Telegram bots described by workflow files exported
// from the TgCreator web builder.
//
//	tgcreator run      -w workflow.json   start the bot
//	tgcreator validate -w workflow.json   check the file, print what it needs
//	tgcreator compose  -w workflow.json   write a docker-compose.yml with only needed services
//	tgcreator nodes                       list available node types
//	tgcreator panel                       run the visual builder (web panel)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mrjvadi/tgcreator/internal/compose"
	"github.com/mrjvadi/tgcreator/internal/engine"
	_ "github.com/mrjvadi/tgcreator/internal/nodes"
	"github.com/mrjvadi/tgcreator/internal/panel"
	"github.com/mrjvadi/tgcreator/internal/realtime"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "run":
		err = cmdRun(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "compose":
		err = cmdCompose(os.Args[2:])
	case "nodes":
		cmdNodes(os.Args[2:])
	case "panel":
		err = cmdPanel(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `tgcreator - Telegram bot runtime for TgCreator workflows

Usage:
  tgcreator run      -w workflow.json
  tgcreator validate -w workflow.json
  tgcreator compose  -w workflow.json [-o docker-compose.yml] [--build .] [--image tgcreator:latest]
  tgcreator nodes [-json]
  tgcreator panel    [-addr :8090]
`)
}

func load(fs *flag.FlagSet, args []string) (*workflow.Workflow, *engine.Engine, *slog.Logger, error) {
	path := fs.String("w", envOr("TGC_WORKFLOW", "workflow.json"), "workflow file")
	if err := fs.Parse(args); err != nil {
		return nil, nil, nil, err
	}
	wf, err := workflow.Load(*path)
	if err != nil {
		return nil, nil, nil, err
	}
	log := newLogger(wf.Runtime.LogLevel)
	e, err := engine.New(wf, engine.Options{Logger: log})
	if err != nil {
		return nil, nil, nil, err
	}
	return wf, e, log, nil
}

func cmdRun(args []string) error {
	_, e, log, err := load(flag.NewFlagSet("run", flag.ExitOnError), args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := e.Open(ctx); err != nil {
		return err
	}
	defer e.Close()
	log.Info("workflow loaded", "name", e.WF.Name, "nodes", len(e.WF.Nodes))
	return e.Run(ctx)
}

func cmdValidate(args []string) error {
	wf, e, _, err := load(flag.NewFlagSet("validate", flag.ExitOnError), args)
	if err != nil {
		return err
	}
	fmt.Printf("workflow:        %s\n", wf.Name)
	fmt.Printf("nodes:           %d\n", len(wf.Nodes))
	fmt.Printf("update types:    %s\n", strings.Join(e.AllowedUpdates(), ", "))
	reqs := e.Requirements()
	if len(reqs) == 0 {
		fmt.Println("services:        none (runtime only)")
	} else {
		fmt.Printf("services:        %s\n", strings.Join(reqs, ", "))
	}
	fmt.Println("OK")
	return nil
}

func cmdCompose(args []string) error {
	fs := flag.NewFlagSet("compose", flag.ExitOnError)
	out := fs.String("o", "", "output file (default: docker-compose.yml next to the workflow, \"-\" for stdout)")
	build := fs.String("build", "", "build the runtime image from this source directory")
	image := fs.String("image", "tgcreator:latest", "runtime image")
	project := fs.String("project", "", "compose project name")
	path := fs.String("w", envOr("TGC_WORKFLOW", "workflow.json"), "workflow file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	wf, err := workflow.Load(*path)
	if err != nil {
		return err
	}
	e, err := engine.New(wf, engine.Options{})
	if err != nil {
		return err
	}
	target := *out
	if target == "" {
		target = filepath.Join(filepath.Dir(*path), "docker-compose.yml")
	}
	var mounts []string
	// Files referenced as file:///app/assets/... live in ./assets.
	if st, err := os.Stat(filepath.Join(filepath.Dir(*path), "assets")); err == nil && st.IsDir() {
		mounts = append(mounts, "./assets:/app/assets:ro")
	}
	// compose needs the ${VAR} references as written, not their values here.
	raw, err := workflow.LoadRaw(*path)
	if err != nil {
		return err
	}
	yml, err := compose.Generate(raw, e.Requirements(), compose.Options{
		WorkflowFile: "./" + filepath.Base(*path),
		Image:        *image,
		BuildContext: *build,
		Project:      *project,
		ExtraMounts:  mounts,
	})
	if err != nil {
		return err
	}
	if target == "-" {
		fmt.Print(yml)
		return nil
	}
	if err := os.WriteFile(target, []byte(yml), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (services: %s)\n", target, strings.Join(append([]string{"bot"}, e.Requirements()...), ", "))
	return nil
}

func cmdNodes(args []string) {
	fs := flag.NewFlagSet("nodes", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the full catalog (labels, params) as JSON")
	_ = fs.Parse(args)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(engine.NodeTypes())
		return
	}
	for _, t := range engine.NodeTypes() {
		name := t.Name
		if strings.HasSuffix(name, ".") {
			name += "<method>"
		}
		fmt.Printf("%-26s %s\n", name, t.Description)
	}
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch strings.ToLower(envOr("TGC_LOG_LEVEL", level)) {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
	slog.SetDefault(log)
	return log
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func cmdPanel(args []string) error {
	fs := flag.NewFlagSet("panel", flag.ExitOnError)
	addr := fs.String("addr", envOr("TGC_PANEL_ADDR", ":8090"), "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	log := newLogger("")
	srv := panel.New(panel.Config{
		Password: os.Getenv("TGC_PANEL_PASSWORD"),
		Realtime: realtime.Config{
			APIURL: os.Getenv("TGC_CENTRIFUGO_API_URL"),
			APIKey: os.Getenv("TGC_CENTRIFUGO_API_KEY"),
			Secret: os.Getenv("TGC_CENTRIFUGO_SECRET"),
			WSURL:  os.Getenv("TGC_CENTRIFUGO_WS_URL"),
		},
	}, log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.ListenAndServe(ctx, *addr)
}
