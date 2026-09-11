// Package migrations embeds the SQL migrations into the binary.
//
// R-253: Pando ships as a single binary, which means the schema travels with
// it. There is no separate migration step for an operator to forget, and no
// version skew between a binary and a migration directory on disk.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
