// fleet-db-transition is the offline, one-time baseline reconciliation command.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type cli struct {
	DB      db.Config `embed:"" prefix:"db-" envprefix:"DB_"`
	Catalog struct{}  `cmd:"" help:"Read a catalog digest from a disposable development fixture for reviewed release assertions."`
	Migrate struct{}  `cmd:"" help:"Run ordinary migrations on an admitted schema or an empty installation."`
	Check   struct {
		State         string `required:"" enum:"source,target,startup" help:"Check an exact source or this release's destination schema."`
		Source        string `help:"Source application; required for source checks."`
		SourceVersion int    `help:"Recorded source version; required for source checks."`
	} `cmd:"" help:"Read-only schema/runtime check. Safe on a standby."`
	Apply struct {
		Source        string `required:"" enum:"public,internal"`
		SourceVersion int    `required:""`
	} `cmd:"" help:"Reconcile a qualified source while all applications are stopped. Requires a rehearsed coordinated backup."`
}

func run(ctx context.Context, args []string) error {
	var command cli
	parser, err := kong.New(&command, kong.Name("fleet-db-transition"))
	if err != nil {
		return fmt.Errorf("initialize transition command: %w", err)
	}
	parsed, err := parser.Parse(args)
	if err != nil {
		return fmt.Errorf("parse transition command: %w", err)
	}
	if parsed.Command() == "migrate" {
		migrated, err := db.ConnectAndMigrate(&command.DB)
		if err != nil {
			return err
		}
		if err := migrated.Close(); err != nil {
			return fmt.Errorf("close migrated database: %w", err)
		}
		return nil
	}
	conn, err := db.ConnectToDatabase(&command.DB)
	if err != nil {
		return err
	}
	defer conn.Close()
	switch parsed.Command() {
	case "catalog":
		digest, err := db.BaselineCatalog(ctx, conn)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"catalog": digest}); err != nil {
			return fmt.Errorf("write catalog assertion: %w", err)
		}
		return nil
	case "check":
		status, err := db.CheckBaseline(ctx, conn, command.Check.State, command.Check.Source, command.Check.SourceVersion)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
			return fmt.Errorf("write baseline status: %w", err)
		}
		return nil
	case "apply":
		if err = db.ApplyBaseline(ctx, conn, command.Apply.Source, command.Apply.SourceVersion); err != nil {
			return err
		}
		status, err := db.CheckBaseline(ctx, conn, "target", "", 0)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(status); err != nil {
			return fmt.Errorf("write baseline status: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("expected catalog, migrate, check, or apply")
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
