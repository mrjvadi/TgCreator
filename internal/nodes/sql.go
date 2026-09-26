package nodes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mrjvadi/tgcreator/internal/engine"
	"github.com/mrjvadi/tgcreator/internal/tmpl"
	"github.com/mrjvadi/tgcreator/internal/workflow"
)

// sqlDrivers maps workflow driver names to database/sql driver names.
// Driver files (driver_*.go) add themselves; build tags can drop them.
var sqlDrivers = map[string]string{}

func dbName(params map[string]any) string {
	if s := tmpl.ToString(params["db"]); s != "" {
		return s
	}
	return "main"
}

func requireDB(params map[string]any) []string { return []string{"db:" + dbName(params)} }

func init() {
	engine.RegisterService("db", openDB)
	engine.Register(engine.NodeType{
		Name:        "db.query",
		Description: "Run a SELECT. Params: db (default \"main\"), query, args, single (first row only). Output: rows.",
		Requires:    requireDB,
		New:         func(b *engine.Build, n workflow.Node) (any, error) { return newSQLNode(b, n, true) },
	})
	engine.Register(engine.NodeType{
		Name:        "db.exec",
		Description: "Run INSERT/UPDATE/DELETE. Params: db, query, args. Output: {rows_affected, last_insert_id}.",
		Requires:    requireDB,
		New:         func(b *engine.Build, n workflow.Node) (any, error) { return newSQLNode(b, n, false) },
	})
}

type sqlNode struct {
	name   string
	query  string
	args   tmpl.Value
	read   bool
	single bool
	db     *sql.DB
	stmt   *sql.Stmt
}

func newSQLNode(b *engine.Build, n workflow.Node, read bool) (any, error) {
	q := tmpl.ToString(n.Params["query"])
	if q == "" {
		return nil, errors.New("query is required")
	}
	// Values go through args (placeholders), never into the SQL text, to
	// rule out SQL injection from user messages.
	args, err := b.Param(n, "args", []any{})
	if err != nil {
		return nil, err
	}
	single, _ := n.Params["single"].(bool)
	return &sqlNode{name: dbName(n.Params), query: q, args: args, read: read, single: single}, nil
}

func (n *sqlNode) Init(e *engine.Engine) error {
	db, ok := e.Service("db:" + n.name).(*sql.DB)
	if !ok {
		return fmt.Errorf("database %q not available", n.name)
	}
	n.db = db
	stmt, err := db.Prepare(n.query)
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	n.stmt = stmt
	return nil
}

func (n *sqlNode) Exec(x *engine.Exec) (engine.Result, error) {
	v, err := x.Eval(n.args)
	if err != nil {
		return engine.Result{}, err
	}
	in, _ := v.([]any)
	args := make([]any, len(in)) // never mutate compiled constants
	for i, a := range in {
		// JSON numbers are float64; ids must reach BIGINT columns as integers.
		if f, ok := a.(float64); ok && f == float64(int64(f)) {
			a = int64(f)
		}
		args[i] = a
	}
	if !n.read {
		res, err := n.stmt.ExecContext(x.Ctx, args...)
		if err != nil {
			return engine.Result{}, err
		}
		ra, _ := res.RowsAffected()
		li, _ := res.LastInsertId()
		return engine.Result{Output: engine.Main, Data: map[string]any{"rows_affected": ra, "last_insert_id": li}}, nil
	}
	rows, err := n.stmt.QueryContext(x.Ctx, args...)
	if err != nil {
		return engine.Result{}, err
	}
	defer rows.Close()
	out, err := scanRows(rows, n.single)
	if err != nil {
		return engine.Result{}, err
	}
	if n.single {
		if len(out) == 0 {
			return engine.Result{Output: engine.Main}, nil
		}
		return engine.Result{Output: engine.Main, Data: out[0]}, nil
	}
	return engine.Result{Output: engine.Main, Data: out}, nil
}

func scanRows(rows *sql.Rows, single bool) ([]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []any
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = normalizeSQL(vals[i])
		}
		out = append(out, row)
		if single {
			break
		}
	}
	return out, rows.Err()
}

// normalizeSQL converts driver values to the JSON-like types expressions use.
func normalizeSQL(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case int64:
		return float64(t)
	case int32:
		return float64(t)
	case float32:
		return float64(t)
	case time.Time:
		return t.Format(time.RFC3339)
	}
	return v
}

func openDB(ctx context.Context, e *engine.Engine, name string) (any, io.Closer, error) {
	cfg, ok := e.WF.Services.Databases[name]
	if !ok {
		return nil, nil, fmt.Errorf("services.databases.%s is not configured", name)
	}
	driver, ok := sqlDrivers[cfg.Driver]
	if !ok {
		return nil, nil, fmt.Errorf("driver %q not included in this build", cfg.Driver)
	}
	if cfg.DSN == "" {
		return nil, nil, fmt.Errorf("database %q: dsn is empty (set %s)", name, workflow.DSNEnv(name))
	}
	db, err := sql.Open(driver, cfg.DSN)
	if err != nil {
		return nil, nil, err
	}
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(20)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(10)
	}
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Databases started by docker compose may need a few seconds.
	var pingErr error
	for i := 0; i < 30; i++ {
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr = db.PingContext(pctx)
		cancel()
		if pingErr == nil {
			break
		}
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			_ = db.Close()
			return nil, nil, ctx.Err()
		}
	}
	if pingErr != nil {
		_ = db.Close()
		return nil, nil, pingErr
	}
	for i, m := range cfg.Migrations {
		if _, err := db.ExecContext(ctx, m); err != nil {
			_ = db.Close()
			return nil, nil, fmt.Errorf("migration #%d: %w", i+1, err)
		}
	}
	return db, db, nil
}
