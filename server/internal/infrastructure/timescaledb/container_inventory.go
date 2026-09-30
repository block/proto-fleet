package timescaledb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type inventoryModule struct {
	TankPosition int32           `json:"tank_position"`
	Position     int32           `json:"position"`
	ReportedID   string          `json:"reported_id"`
	State        sdk.ModuleState `json:"state"`
	ObservedAt   *time.Time      `json:"observed_at"`
}

type moduleSample struct {
	TankPosition int32     `json:"tank_position"`
	Position     int32     `json:"position"`
	ReportedID   string    `json:"reported_id"`
	Time         time.Time `json:"time"`
	HashrateHS   *float64  `json:"hashrate_hs"`
	PowerW       *float64  `json:"power_w"`
	TemperatureC *float64  `json:"temperature_c"`
}

// Runs inside the telemetry transaction. The parent row serializes competing
// observations; fixed-size batches avoid issuing an insert per module.
func storeContainerInventory(ctx context.Context, q *sqlc.Queries, identifier string, snapshot *sdk.ContainerSnapshot) error {
	if snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(time.Now().Add(5*time.Second)) {
		return fmt.Errorf("invalid container observation time")
	}
	modules := make([]inventoryModule, 0)
	samples := make([]moduleSample, 0)
	tanks := make([]struct {
		Position int32 `json:"position"`
	}, 0, len(snapshot.Tanks))
	seenTanks := map[int32]bool{}
	for _, tank := range snapshot.Tanks {
		if tank.Position <= 0 || seenTanks[tank.Position] {
			return fmt.Errorf("invalid or duplicate tank selector")
		}
		seenTanks[tank.Position] = true
		tanks = append(tanks, struct {
			Position int32 `json:"position"`
		}{tank.Position})
		seenModules := map[int32]bool{}
		for _, module := range tank.Modules {
			if module.Position <= 0 || seenModules[module.Position] {
				return fmt.Errorf("invalid or duplicate module selector")
			}
			seenModules[module.Position] = true
			state := module.State
			if state == "" {
				state = sdk.ModuleUnknown
			}
			switch state {
			case sdk.ModuleUnknown, sdk.ModuleHashing, sdk.ModuleAttention, sdk.ModuleOffline, sdk.ModuleSleeping, sdk.ModuleIsolated:
			default:
				return fmt.Errorf("invalid module state %q", state)
			}
			var observed *time.Time
			if !module.ObservedAt.IsZero() {
				observed = &module.ObservedAt
			}
			modules = append(modules, inventoryModule{tank.Position, module.Position, module.ReportedID, state, observed})
			if m := module.Metrics; m != nil && !m.ObservedAt.IsZero() && !m.ObservedAt.After(snapshot.ObservedAt.Add(5*time.Second)) {
				samples = append(samples, moduleSample{tank.Position, module.Position, module.ReportedID, m.ObservedAt, m.HashrateHS, m.PowerW, m.TemperatureC})
			}
		}
	}
	tankJSON, err := json.Marshal(tanks)
	if err != nil {
		return fmt.Errorf("encode container tanks: %w", err)
	}
	moduleJSON, err := json.Marshal(modules)
	if err != nil {
		return fmt.Errorf("encode container modules: %w", err)
	}
	sampleJSON, err := json.Marshal(samples)
	if err != nil {
		return fmt.Errorf("encode container samples: %w", err)
	}
	id, err := q.AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: identifier, ObservedAt: snapshot.ObservedAt})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err := q.ReconcileContainerTanks(ctx, sqlc.ReconcileContainerTanksParams{DeviceID: id, ObservedAt: snapshot.ObservedAt, Tanks: tankJSON}); err != nil {
			return err
		}
		if err := q.RetireContainerModules(ctx, sqlc.RetireContainerModulesParams{DeviceID: id, ObservedAt: sql.NullTime{Time: snapshot.ObservedAt, Valid: true}, Complete: snapshot.TopologyComplete, Modules: moduleJSON}); err != nil {
			return err
		}
		if snapshot.TopologyComplete {
			if err := q.MarkAbsentContainerTanks(ctx, sqlc.MarkAbsentContainerTanksParams{DeviceID: id, Tanks: tankJSON}); err != nil {
				return err
			}
		}
		if err := q.ReconcileContainerModules(ctx, sqlc.ReconcileContainerModulesParams{DeviceID: id, ObservedAt: snapshot.ObservedAt, Modules: moduleJSON}); err != nil {
			return err
		}
	}
	return q.InsertContainerModuleMetrics(ctx, sqlc.InsertContainerModuleMetricsParams{DeviceIdentifier: identifier, CollectedAt: snapshot.ObservedAt, Samples: sampleJSON, InventoryAccepted: err == nil})
}
