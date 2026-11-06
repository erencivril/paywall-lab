// Package migrations embeds the SQL files so the binary can apply them.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
