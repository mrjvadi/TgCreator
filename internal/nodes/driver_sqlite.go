//go:build sqlite

package nodes

// SQLite is opt-in (go build -tags sqlite) because the pure-Go driver is
// large; most deployments use postgres or mysql.
import _ "modernc.org/sqlite"

func init() { sqlDrivers["sqlite"] = "sqlite" }
