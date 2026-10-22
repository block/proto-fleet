package fleetinventory

import (
	"time"

	errorpb "github.com/block/proto-fleet/server/generated/grpc/errors/v1"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetinventory/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func projectModuleIssues(evidence *sdk.ContainerModuleIssues, lastSeen *time.Time, now time.Time) *pb.ModuleIssues {
	if evidence == nil {
		return nil
	}
	fresh := func(at time.Time) bool {
		return !at.IsZero() && now.Sub(at) < 2*time.Minute && !at.After(now.Add(5*time.Second))
	}
	out := &pb.ModuleIssues{Complete: evidence.Complete && fresh(evidence.ObservedAt)}
	if !evidence.ObservedAt.IsZero() {
		out.ObservedAt = timestamppb.New(evidence.ObservedAt)
	}
	out.Stale = !evidence.ObservedAt.IsZero() && (!fresh(evidence.ObservedAt) || lastSeen == nil || !fresh(*lastSeen))
	if lastSeen == nil || !fresh(*lastSeen) {
		out.Complete = false
		return out
	}
	for _, issue := range evidence.Items {
		if !fresh(issue.ObservedAt) {
			out.Complete = false
			continue
		}
		out.Items = append(out.Items, &pb.ModuleIssue{Component: issue.Component, ComponentId: issue.ComponentID, Cause: issue.Cause, Severity: issue.Severity, CanonicalError: errorpb.MinerError(issue.CanonicalError), ObservedAt: timestamppb.New(issue.ObservedAt)})
	}
	return out
}
