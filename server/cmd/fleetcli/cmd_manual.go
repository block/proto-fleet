package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	cohortv1 "github.com/block/proto-fleet/server/generated/grpc/cohort/v1"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const cohortServicePrefix = "/cohort.v1.CohortService/"

func manualGroupCommands(group string) []*cli.Command {
	switch group {
	case "cohorts":
		return []*cli.Command{
			cohortReserveCommand(),
		}
	default:
		return nil
	}
}

func cohortReserveCommand() *cli.Command {
	return &cli.Command{
		Name:  "reserve",
		Usage: "Reserve rigs into an owned cohort",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "label", Usage: "Human-readable cohort label", Required: true},
			&cli.StringFlag{Name: "purpose", Usage: "Why this cohort is being reserved"},
			&cli.StringSliceFlag{Name: "device", Usage: "Device identifier to reserve; repeat for multiple devices"},
			&cli.IntFlag{Name: "count", Usage: "Reserve the first N available default-cohort rigs"},
			&cli.StringFlag{Name: "product", Usage: "Limit --count allocation to a manufacturer/product"},
			&cli.StringFlag{Name: "model", Usage: "Limit --count allocation to a model"},
			&cli.Int64Flag{Name: "site-id", Usage: "Limit --count allocation or rig listing to a site id"},
			&cli.Int64Flag{Name: "source-device-set-id", Usage: "Reserve current members of a group/device set"},
			&cli.StringFlag{Name: "expires-at", Usage: "Lease expiration timestamp in RFC3339 format"},
			&cli.StringFlag{Name: "firmware-file-id", Usage: "Desired firmware file id for the cohort"},
			&cli.StringFlag{Name: "idempotency-key", Usage: "Idempotency key for retry-safe reservation"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			req, err := buildCreateCohortRequest(ctx, cmd, client)
			if err != nil {
				return err
			}
			resp := &cohortv1.CreateCohortResponse{}
			err = client.CallBearer(ctx, cohortServicePrefix+"CreateCohort", req, resp)
			if isConflict(err) {
				return cliExitError{code: 2, err: err}
			}
			if err != nil {
				return err
			}
			return printProto(resp)
		},
	}
}

func buildCreateCohortRequest(ctx context.Context, cmd *cli.Command, client *Client) (*cohortv1.CreateCohortRequest, error) {
	if cmd.IsSet("device") && cmd.IsSet("source-device-set-id") {
		return nil, fmt.Errorf("--device and --source-device-set-id are mutually exclusive")
	}
	if cmd.IsSet("count") && (cmd.IsSet("device") || cmd.IsSet("source-device-set-id")) {
		return nil, fmt.Errorf("--count cannot be combined with --device or --source-device-set-id")
	}
	if !cmd.IsSet("count") && (cmd.IsSet("product") || cmd.IsSet("model") || cmd.IsSet("site-id")) {
		return nil, fmt.Errorf("--product, --model, and --site-id require --count")
	}

	req := &cohortv1.CreateCohortRequest{
		Label:                 cmd.String("label"),
		Purpose:               cmd.String("purpose"),
		ClaimOwnership:        true,
		DesiredFirmwareFileId: cmd.String("firmware-file-id"),
		IdempotencyKey:        cmd.String("idempotency-key"),
	}
	if cmd.IsSet("expires-at") {
		expiresAt, err := parseRFC3339Timestamp(cmd.String("expires-at"), "expires-at")
		if err != nil {
			return nil, err
		}
		req.ExpiresAt = expiresAt
	}

	switch {
	case cmd.IsSet("source-device-set-id"):
		req.InitialMembers = &cohortv1.CreateCohortRequest_SourceDeviceSetId{
			SourceDeviceSetId: cmd.Int64("source-device-set-id"),
		}
	case cmd.IsSet("count"):
		count := cmd.Int("count")
		if count <= 0 {
			return nil, fmt.Errorf("--count must be greater than zero")
		}
		if count > 10000 {
			return nil, fmt.Errorf("--count must be at most 10000")
		}
		selector := &cohortv1.CohortDeviceSelector{Count: int32(count)}
		if cmd.IsSet("product") {
			value := strings.TrimSpace(cmd.String("product"))
			if value != "" {
				selector.Product = &value
			}
		}
		if cmd.IsSet("model") {
			value := strings.TrimSpace(cmd.String("model"))
			if value != "" {
				selector.Model = &value
			}
		}
		if cmd.IsSet("site-id") {
			value := cmd.Int64("site-id")
			selector.SiteId = &value
		}
		req.InitialMembers = &cohortv1.CreateCohortRequest_Select{
			Select: selector,
		}
	case cmd.IsSet("device"):
		req.InitialMembers = &cohortv1.CreateCohortRequest_DeviceIdentifiers{
			DeviceIdentifiers: &cohortv1.CohortDeviceIdentifierList{DeviceIdentifiers: cmd.StringSlice("device")},
		}
	default:
		return nil, fmt.Errorf("one of --device, --count, or --source-device-set-id is required")
	}

	return req, nil
}

func parseRFC3339Timestamp(value string, flagName string) (*timestamppb.Timestamp, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value: %w", flagName, err)
	}
	return timestamppb.New(parsed), nil
}

func isConflict(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return strings.HasPrefix(apiErr.Status, fmt.Sprintf("%d ", http.StatusConflict))
	}
	return false
}
