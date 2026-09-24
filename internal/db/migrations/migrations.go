package migrations

import "embed"

// FS contains schema and data migrations grouped by GORM dialect name.
//
//go:embed sqlite/*.sql postgres/*.sql data/sqlite/*.sql data/postgres/*.sql
var FS embed.FS
