package migrations

import "embed"

//go:embed *.sql
var Migrations embed.FS

// Bridges contains narrowly scoped compatibility SQL that prepares a legacy
// schema for an already-shipped immutable migration. Bridge execution is owned
// by the migration runner; these files are not part of golang-migrate's ordered
// source and must reach the same final schema as the migration they bridge.
//
//go:embed bridges/*.sql
var Bridges embed.FS
