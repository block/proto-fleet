// fleet-device-write-recovery is an offline, privileged host-operations command.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kong"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

type cli struct {
	DB      db.Config `embed:"" prefix:"db-" envprefix:"DB_"`
	Inspect struct {
		OrganizationID int64 `required:""`
		DeviceID       int64 `required:""`
	} `cmd:"" help:"Read unresolved attempts and unfinished pool removals with exact custody/identity bindings. Does not run migrations."`
	Resolve struct {
		Evidence                         string `required:"" type:"existingfile" help:"JSON operator attestation matching inspection. Use evidence references, never credentials."`
		Apply                            bool   `help:"Commit resolution; default is a read-only preview."`
		AcknowledgeAllDispatchersStopped bool   `help:"Required with --apply: every API replica, plugin, node dispatcher and scheduled writer is authoritatively stopped and will remain stopped until completion."`
	} `cmd:"" help:"Preview or apply an audited offline resolution. Not a timeout/lease recovery API."`
}

func readEvidence(reader io.Reader) (devicewrite.RecoveryEvidence, error) {
	var evidence devicewrite.RecoveryEvidence
	body, err := io.ReadAll(io.LimitReader(reader, (64<<10)+1))
	if err != nil {
		return evidence, fmt.Errorf("read recovery file: %w", err)
	}
	if len(body) > 64<<10 {
		return evidence, fmt.Errorf("recovery evidence exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return evidence, fmt.Errorf("read recovery evidence: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return evidence, fmt.Errorf("recovery evidence must contain exactly one JSON object")
	}
	return evidence, nil
}

func run(ctx context.Context, args []string) error {
	var command cli
	parser, err := kong.New(&command, kong.Name("fleet-device-write-recovery"))
	if err != nil {
		return fmt.Errorf("initialize recovery command: %w", err)
	}
	parsed, err := parser.Parse(args)
	if err != nil {
		return fmt.Errorf("parse recovery command: %w", err)
	}
	var evidence devicewrite.RecoveryEvidence
	if parsed.Command() == "resolve" {
		if command.Resolve.Apply && !command.Resolve.AcknowledgeAllDispatchersStopped {
			return fmt.Errorf("--apply requires --acknowledge-all-dispatchers-stopped and authoritative termination evidence")
		}
		file, err := os.Open(command.Resolve.Evidence)
		if err != nil {
			return fmt.Errorf("open recovery evidence: %w", err)
		}
		defer file.Close()
		evidence, err = readEvidence(file)
		if err != nil {
			return err
		}
	}
	conn, err := db.ConnectToDatabase(&command.DB)
	if err != nil {
		return err
	}
	defer conn.Close()
	service := devicewrite.NewService(conn)
	var result any
	switch parsed.Command() {
	case "inspect":
		result, err = service.InspectRecovery(ctx, command.Inspect.OrganizationID, command.Inspect.DeviceID)
	case "resolve":
		result, err = service.ResolveOffline(ctx, evidence, command.Resolve.Apply)
	default:
		return fmt.Errorf("expected inspect or resolve")
	}
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return fmt.Errorf("write recovery result: %w", err)
	}
	return nil
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
