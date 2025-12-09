package telemetry

//go:generate mockgen -source=interfaces.go -destination=mocks/mock_interfaces.go -package=mock ErrorPoller

import (
	"context"

	"github.com/btc-mining/proto-fleet/server/internal/domain/diagnostics"
	minerInterfaces "github.com/btc-mining/proto-fleet/server/internal/domain/miner/interfaces"
)

// ErrorPoller polls device errors alongside telemetry collection.
type ErrorPoller interface {
	PollErrors(ctx context.Context, miners ...minerInterfaces.Miner) diagnostics.PollResult
}
