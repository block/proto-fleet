package migrationspg

import "embed"

// Migrations contains all database migration files.
//
//go:embed *.sql
var Migrations embed.FS
