// fleet-db-transition is the offline, one-time baseline reconciliation command.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/domain/poolconfigurations"
	poolmigration "github.com/block/proto-fleet/server/internal/domain/poolconfigurations/migration"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/infrastructure/encrypt"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
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
	PoolCutover struct {
		OrganizationID int64  `required:"" help:"Internal organization ID."`
		Action         string `enum:"inspect,freeze,complete" default:"inspect" help:"Inspect is read-only. Finish import and resolve all gaps before freeze. Freeze/complete require all application writers and dispatchers stopped; coordinate completion with PR4."`
	} `cmd:"" help:"Inventory and complete the pool backend cutover without changing hardware."`
	ImportPoolConfigurations struct {
		OrganizationID int64          `required:"" help:"Internal organization ID to import."`
		Encryption     encrypt.Config `embed:""`
		Plugins        plugins.Config `embed:"" prefix:"plugins-" envprefix:"PLUGINS_"`
	} `cmd:"" help:"Import pool libraries and adopt complete current device stacks while application writers are stopped. Identical stacks merge within their owner. Never changes equipment settings."`
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
	case "pool-cutover":
		orgID := command.PoolCutover.OrganizationID
		if orgID <= 0 {
			return fmt.Errorf("pool cutover requires a positive organization ID")
		}
		var report poolmigration.CutoverReport
		var actionErr error
		switch command.PoolCutover.Action {
		case "inspect":
			report, actionErr = poolmigration.InspectCutover(ctx, sqlc.New(conn), orgID)
		case "freeze":
			actionErr = db.WithTransactionTimeoutNoResult(ctx, conn, 30*time.Second, func(q sqlc.Querier) error {
				return poolmigration.FreezeCutover(ctx, q, orgID)
			})
			if actionErr == nil {
				report, actionErr = poolmigration.InspectCutover(ctx, sqlc.New(conn), orgID)
			}
		case "complete":
			report, actionErr = db.WithTransactionTimeout(ctx, conn, 30*time.Second, func(q sqlc.Querier) (poolmigration.CutoverReport, error) {
				return poolmigration.CompleteCutover(ctx, q, orgID)
			})
		}
		if actionErr != nil {
			inspection, inspectErr := poolmigration.InspectCutover(ctx, sqlc.New(conn), orgID)
			if inspectErr == nil {
				report = inspection
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			return fmt.Errorf("write pool cutover report: %w", err)
		}
		return actionErr
	case "import-pool-configurations":
		if _, err := db.CheckBaseline(ctx, conn, "startup", "", 0); err != nil {
			return err
		}
		pluginManager := plugins.NewManager(&command.ImportPoolConfigurations.Plugins)
		defer func() { _ = pluginManager.Shutdown(context.WithoutCancel(ctx)) }()
		if err := pluginManager.LoadPlugins(ctx); err != nil {
			return err
		}
		fileService, err := files.NewService(files.Config{})
		if err != nil {
			return err
		}
		minerService := miner.NewMinerService(conn, nil, secrets, fileService, pluginManager)
		snapshots, err := poolmigration.CollectDevices(ctx, sqlc.New(conn), secrets, command.ImportPoolConfigurations.OrganizationID, poolmigration.NewFirmwareReader(conn, minerService))
		if err != nil {
			return err
		}
		report, err := db.WithTransactionTimeout(ctx, conn, 2*time.Minute, func(q sqlc.Querier) (poolconfigurations.ImportReport, error) {
			return poolmigration.Import(ctx, q, secrets, command.ImportPoolConfigurations.OrganizationID, snapshots...)
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
