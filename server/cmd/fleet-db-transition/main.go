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
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	poolmigration "github.com/block/proto-fleet/server/internal/domain/poolconfigurations/migration"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
)

type cli struct {
	DB      db.Config `embed:"" prefix:"db-" envprefix:"DB_"`
	Migrate struct{}  `cmd:"" help:"Run ordinary migrations from a clean application checkpoint or an empty installation."`
	Check   struct {
		State         string `required:"" enum:"source,target,startup" help:"Check the transition source, release checkpoint, or startup eligibility."`
		Source        string `help:"Source application; required for source checks."`
		SourceVersion int    `help:"Recorded source version; required for source checks."`
	} `cmd:"" help:"Read-only schema/runtime check. Safe on a standby."`
	Apply struct {
		Source        string `required:"" enum:"public,internal"`
		SourceVersion int    `required:""`
	} `cmd:"" help:"Reconcile a qualified source while all applications are stopped. Requires a rehearsed coordinated backup."`
	ImportPoolConfigurations struct {
		OrganizationID int64          `required:"" help:"Internal organization ID to import."`
		Encryption     encrypt.Config `embed:""`
	} `cmd:"" help:"Consolidate operator and hosted pool libraries while application writers are stopped. Preserves sources and does not assign or change equipment."`
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
	if parsed.Command() == "check" && command.Check.State == "source" {
		for _, flag := range parsed.Flags() {
			if (flag.Name == "source" || flag.Name == "source-version") && !flag.Set {
				return fmt.Errorf("check --state=source requires --%s", flag.Name)
			}
		}
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
	var secrets *encrypt.Service
	if parsed.Command() == "import-pool-configurations" {
		if command.ImportPoolConfigurations.OrganizationID <= 0 {
			return fmt.Errorf("pool import requires a positive organization ID")
		}
		secrets, err = encrypt.NewService(&command.ImportPoolConfigurations.Encryption)
		if err != nil {
			return err
		}
	}
	// The stock migration runner uses background contexts; leave migrate on
	// default signal handling so an operator can actually terminate it.
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	conn, err := db.ConnectToDatabase(&command.DB)
	if err != nil {
		return err
	}
	defer conn.Close()
	switch parsed.Command() {
	case "import-pool-configurations":
		if _, err := db.CheckBaseline(ctx, conn, "startup", "", 0); err != nil {
			return err
		}
		report, err := db.WithTransaction(ctx, conn, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
			return poolmigration.Libraries(ctx, q, secrets, command.ImportPoolConfigurations.OrganizationID)
		})
		if err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			return fmt.Errorf("write pool import report: %w", err)
		}
		if len(report.Unresolved) > 0 {
			return fmt.Errorf("%d pool configurations require review before adoption", len(report.Unresolved))
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
		fmt.Println("Baseline reconciliation complete. Run migrate to apply any subsequent migrations.")
		return nil
	default:
		return fmt.Errorf("expected migrate, check, apply, or import-pool-configurations")
	}
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
