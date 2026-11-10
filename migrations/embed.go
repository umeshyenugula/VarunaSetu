// Package migrations embeds the SQL schema so tools can apply it without
// needing the repository checkout at runtime.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
