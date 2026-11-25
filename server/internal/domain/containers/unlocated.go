package containers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "github.com/block/proto-fleet/server/generated/grpc/containers/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Placement-free inventory uses source timestamps for freshness, just as installed
// boards do. Never project a prior installation's readings onto these boards.
func projectUnlocated(c *pb.Container, data []byte, now time.Time) error {
	var boards []sdk.ContainerHashboard
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &boards); err != nil {
		return fmt.Errorf("decode unlocated hashboards: %w", err)
	}
	for _, board := range boards {
		b := &pb.UnlocatedHashboard{MacAddress: board.MACAddress, Serial: board.Serial, State: "unknown", Stale: c.Stale || observationStale(board.ObservedAt, now)}
		if !board.ObservedAt.IsZero() {
			b.ObservedAt = timestamppb.New(board.ObservedAt)
		}
		var metadata struct {
			FirmwareVersion string `json:"firmwareVersion"`
		}
		if len(board.MetadataJSON) > 0 {
			if err := json.Unmarshal(board.MetadataJSON, &metadata); err != nil {
				return fmt.Errorf("decode unlocated board metadata: %w", err)
			}
		}
		b.FirmwareVersion = metadata.FirmwareVersion
		if !b.Stale {
			b.HashrateHs, b.TemperatureC = board.HashrateHS, board.TemperatureC
			b.PowerW, b.EfficiencyJh = board.PowerW, board.EfficiencyJH
			if !observationStale(board.StateObservedAt, now) {
				b.State = board.State.String()
			}
			var health *struct {
				Timestamp time.Time `json:"timestamp"`
				Errors    []struct {
					Cause string `json:"cause"`
				} `json:"errors"`
				Warnings []struct {
					Cause string `json:"cause"`
				} `json:"warnings"`
			}
			if len(board.HealthJSON) > 0 {
				if err := json.Unmarshal(board.HealthJSON, &health); err != nil {
					return fmt.Errorf("decode unlocated board health: %w", err)
				}
			}
			if health != nil && !observationStale(health.Timestamp, now) {
				b.IssuesKnown = true
				for _, issue := range health.Errors {
					b.Issues = append(b.Issues, issueText(issue.Cause))
				}
				for _, issue := range health.Warnings {
					b.Issues = append(b.Issues, "Warning: "+issueText(issue.Cause))
				}
			}
		}
		c.UnlocatedBoards = append(c.UnlocatedBoards, b)
	}
	return nil
}

func issueText(cause string) string {
	if cause == "" {
		return "Unknown issue"
	}
	return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(cause, "HASHBOARD_ERROR_CAUSE_")), "_", " ")
}
