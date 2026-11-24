package command

import (
	"context"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
)

// withContainerCommandTransaction commits admission and all queued commands
// together, then starts result observation. Callers keep their operation-specific
// locking, authorization and eligibility checks inside prepare.
func withContainerCommandTransaction[T any](ctx context.Context, s *Service, prepare func(context.Context, sqlc.Querier) (T, error)) (T, error) {
	var commit func()
	result, err := db.WithTransaction(ctx, s.conn, func(q sqlc.Querier) (T, error) {
		txCtx, after := db.WithCommitHooks(db.WithTxQueries(ctx, q))
		commit = after
		return prepare(txCtx, q)
	})
	if err == nil && commit != nil {
		commit()
	}
	return result, err
}

// queueContainerCommand targets exactly one already-locked and authorized
// controller. Tank/module batches call it within their shared transaction.
func (s *Service) queueContainerCommand(ctx context.Context, identifier string, command *Command, eventType, description string) (*CommandResult, error) {
	command.deviceSelector = &pb.DeviceSelector{SelectionType: &pb.DeviceSelector_IncludeDevices{
		IncludeDevices: &commonpb.DeviceIdentifierList{DeviceIdentifiers: []string{identifier}},
	}}
	result, err := s.processCommand(ctx, command)
	if err == nil {
		s.finalizeDispatch(ctx, result, eventType, description)
	}
	return result, err
}
