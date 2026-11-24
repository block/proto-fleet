package migrations

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

const (
	ConsolidationName    = "001115_shared_repository_consolidation"
	ConsolidationVersion = 1115
	ConsolidationUpPath  = "current/" + consolidationUp

	consolidationUp         = ConsolidationName + ".up.sql"
	consolidationDown       = ConsolidationName + ".down.sql"
	lastSharedLegacyVersion = "000149"
)

// Active contains the migration files used for schema generation and release
// metadata. Current also exposes the steps packed into the consolidation.
//
//go:embed current/*.sql
var Active embed.FS

//go:embed *.sql
var legacy embed.FS

//go:embed legacy_internal.fixture
var legacyArchive string

// Bridges contains narrowly scoped compatibility SQL that prepares a legacy
// schema for an already-shipped immutable migration. Bridge execution is owned
// by the migration runner; these files are not part of golang-migrate's ordered
// source and must reach the same final schema as the migration they bridge.
//
//go:embed bridges/*.sql
var Bridges embed.FS

// Current keeps the original SQL and transaction boundaries available to the
// runner. Existing internal databases resume at their recorded checkpoint.
var Current = currentFiles()

// Migrations exposes the original internal legacy sequence for reconciliation
// and upgrade fixtures. The files in the repository root retain the public
// sequence at the versions where the two histories diverged.
var Migrations = legacyFiles()

func currentFiles() migrationFiles {
	scripts := loadSQL(Active, "current")
	down := scripts[consolidationDown]
	addArchivedSQL(scripts, scripts[consolidationUp])
	addArchivedSQL(scripts, strings.TrimSuffix(down, "\n*/\n"))

	scripts[consolidationUp] = "SELECT 1;\n"
	scripts[consolidationDown], _, _ = strings.Cut(down, "\n/*\n")
	return newMigrationFiles("current", scripts)
}

func legacyFiles() migrationFiles {
	scripts := loadSQL(legacy, ".")
	for name := range scripts {
		if name[:6] > lastSharedLegacyVersion {
			delete(scripts, name)
		}
	}
	addArchivedSQL(scripts, legacyArchive)
	return newMigrationFiles(".", scripts)
}

func loadSQL(files embed.FS, directory string) map[string]string {
	paths, err := fs.Glob(files, path.Join(directory, "*.sql"))
	if err != nil {
		panic(err)
	}
	scripts := make(map[string]string, len(paths))
	for _, filename := range paths {
		body, err := files.ReadFile(filename)
		if err != nil {
			panic(err)
		}
		scripts[path.Base(filename)] = string(body)
	}
	return scripts
}

func addArchivedSQL(scripts map[string]string, body string) {
	for _, section := range strings.Split(body, "\n-- fleet-migration: ")[1:] {
		name, sql, ok := strings.Cut(section, "\n")
		if !ok || !fs.ValidPath(name) || path.Base(name) != name || !strings.HasSuffix(name, ".sql") {
			panic("invalid archived migration name: " + name)
		}
		if _, exists := scripts[name]; exists {
			panic("duplicate archived migration: " + name)
		}
		scripts[name] = sql
	}
}
