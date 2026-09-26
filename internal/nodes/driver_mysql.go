//go:build !no_mysql

package nodes

import _ "github.com/go-sql-driver/mysql"

func init() { sqlDrivers["mysql"] = "mysql" }
