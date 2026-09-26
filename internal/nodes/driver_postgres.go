//go:build !no_postgres

package nodes

import _ "github.com/jackc/pgx/v5/stdlib"

func init() { sqlDrivers["postgres"] = "pgx" }
