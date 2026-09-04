package migrations

import "embed"

// FS contains the immutable database migration history.
//
//go:embed *.sql
var FS embed.FS
