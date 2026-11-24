// Package migrations embeds the SQL migrations for the lewnotes index.
package migrations

import "embed"

// FS holds the numbered .sql files applied by internal/index.
//
//go:embed *.sql
var FS embed.FS
