package telemetry

//go:generate go run go.uber.org/mock/mockgen -source=interfaces.go -destination=mocks/mock_interfaces.go -package=mock ErrorPoller,OrgIDLister

import (
	"context"

	"github.com/block/proto-fleet/server/internal/domain/diagnostics"
	minerInterfaces "github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
)

// ErrorPoller polls device errors alongside telemetry collection.
type ErrorPoller interface {
	PollErrors(ctx context.Context, miners ...minerInterfaces.Miner) diagnostics.PollResult
}

type OrgIDLister interface {
	ListOrgIDsForSnapshots(ctx context.Context) ([]int64, error)
}
