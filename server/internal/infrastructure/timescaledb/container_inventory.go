package timescaledb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type inventoryModule struct {
	TankPosition      int32                      `json:"tank_position"`
	Position          int32                      `json:"position"`
	ReportedID        string                     `json:"reported_id"`
	IdentityConfirmed bool                       `json:"identity_confirmed"`
	State             string                     `json:"state"`
	ObservedAt        *time.Time                 `json:"observed_at"`
	Issues            *sdk.ContainerModuleIssues `json:"issues"`
	IssuesObservedAt  *time.Time                 `json:"issues_observed_at"`
}

type moduleSample struct {
	EfficiencyJH *float64             `json:"efficiency_jh"`
	Coverage     *sdk.ContainerRollup `json:"coverage"`
	TankPosition int32                `json:"tank_position"`
	Position     int32                `json:"position"`
	ReportedID   string               `json:"reported_id"`
	Time         time.Time            `json:"time"`
	HashrateHS   *float64             `json:"hashrate_hs"`
	PowerW       *float64             `json:"power_w"`
	TemperatureC *float64             `json:"temperature_c"`
}

// Runs inside the telemetry transaction. The parent row serializes competing
// observations; fixed-size batches avoid issuing an insert per module.
func storeContainerInventory(ctx context.Context, q *sqlc.Queries, identifier string, snapshot *sdk.ContainerSnapshot) error {
	received := time.Now()
	if snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(received.Add(5*time.Second)) {
		return fmt.Errorf("invalid container observation time")
	}
	if len(snapshot.UnlocatedHashboards) > 0 {
		// Unlocated inventory cannot establish that absent placements are empty.
		snapshot.TopologyComplete = false
	}
	normalizeContainerPowerTimes(snapshot, received)
	for ti := range snapshot.Tanks {
		for mi := range snapshot.Tanks[ti].Modules {
			module := &snapshot.Tanks[ti].Modules[mi]
			if strings.TrimSpace(module.ReportedID) == "" {
				module.ReportedID, module.IdentityConfirmed = "", false
			}
		}
	}
	orgID, err := q.LockHashboardOrganization(ctx, identifier)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	id, err := q.AdvanceContainerInventory(ctx, sqlc.AdvanceContainerInventoryParams{DeviceIdentifier: identifier, ObservedAt: snapshot.ObservedAt})
	if errors.Is(err, sql.ErrNoRows) {
		// Delayed topology cannot establish hardware or membership at a new site.
		// Independently timestamped equipment observations have their own ordering.
		snapshot.InventoryAccepted = false
		return storeContainerEquipment(ctx, q, identifier, snapshot)
	}
	if err != nil {
		return err
	}
	snapshot.InventoryAccepted = true
	if err := resolveContainerModuleIdentities(ctx, q, orgID, identifier, snapshot); err != nil {
		return err
	}
	modules := make([]inventoryModule, 0)
	samples := make([]moduleSample, 0)
	tanks := make([]struct {
		Position int32 `json:"position"`
	}, 0, len(snapshot.Tanks))
	seenTanks := map[int32]bool{}
	for ti := range snapshot.Tanks {
		tank := &snapshot.Tanks[ti]
		if tank.Position <= 0 || seenTanks[tank.Position] {
			return fmt.Errorf("invalid or duplicate tank selector")
		}
		seenTanks[tank.Position] = true
		tanks = append(tanks, struct {
			Position int32 `json:"position"`
		}{tank.Position})
		seenModules := map[int32]bool{}
		for mi := range tank.Modules {
			module := &tank.Modules[mi]
			if module.Position <= 0 || seenModules[module.Position] {
				return fmt.Errorf("invalid or duplicate module selector")
			}
			seenModules[module.Position] = true
			state := module.State
			switch state {
			case sdk.MinerStatusUnknown, sdk.MinerStatusActive, sdk.MinerStatusError, sdk.MinerStatusOffline, sdk.MinerStatusInactive, sdk.MinerStatusMaintenance, sdk.MinerStatusUpdating, sdk.MinerStatusNeedsMiningPool, sdk.MinerStatusRebootRequired:
			default:
				return fmt.Errorf("invalid module state %q", state)
			}
			var observed *time.Time
			if !module.ObservedAt.IsZero() {
				observed = &module.ObservedAt
			}
			var issueTime *time.Time
			if v := module.Issues; v != nil {
				latest := snapshot.ObservedAt.Add(5 * time.Second)
				boards := 0
				for source, at := range v.SourceTimes {
					if at.IsZero() || at.After(latest) {
						return fmt.Errorf("invalid module issue source time for %q", source)
					}
					if v.ObservedAt.After(at) {
						return fmt.Errorf("invalid module issue observation time: newer than source %q", source)
					}
					if strings.HasPrefix(source, "hashboard:") && strings.TrimSpace(strings.TrimPrefix(source, "hashboard:")) != "" {
						boards++
					}
				}
				// CT1 reports are complete only with all three boards and the shared PSU.
				const ct1IssueBoardCount = 3
				if v.Complete && (v.ObservedAt.IsZero() || boards != ct1IssueBoardCount || v.SourceTimes["psu"].IsZero()) {
					return fmt.Errorf("invalid module issue complete report: missing required source evidence")
				}
				if !v.ObservedAt.IsZero() {
					if v.ObservedAt.After(latest) {
						return fmt.Errorf("invalid module issue observation time")
					}
					issueTime = &v.ObservedAt
				}
			}
			// Normalize blank evidence once for inventory, retirement, and metrics.
			// Preserve nonblank firmware IDs verbatim for command identity matching.
			reportedID := module.ReportedID
			if strings.TrimSpace(reportedID) == "" {
				reportedID = ""
				module.ReportedID = ""
				module.IdentityConfirmed = false
			}
			modules = append(modules, inventoryModule{tank.Position, module.Position, reportedID, module.IdentityConfirmed && reportedID != "", state.String(), observed, module.Issues, issueTime})
			if m := module.Metrics; m != nil && !m.ObservedAt.IsZero() && !m.ObservedAt.After(snapshot.ObservedAt.Add(5*time.Second)) {
				samples = append(samples, moduleSample{TankPosition: tank.Position, Position: module.Position, ReportedID: reportedID, Time: m.ObservedAt, HashrateHS: m.HashrateHS, PowerW: m.PowerW, TemperatureC: m.TemperatureC, EfficiencyJH: m.EfficiencyJH, Coverage: m.Rollup})
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
	if err := q.InsertModuleDiagnosticObservations(ctx, sqlc.InsertModuleDiagnosticObservationsParams{DeviceID: id, CollectedAt: snapshot.ObservedAt}); err != nil {
		return err
	}
	if hasContainerRollups(snapshot) {
		previous, err := q.GetModuleRollupWatermarks(ctx, id)
		if err != nil {
			return err
		}
		watermarks := map[[2]int32][]byte{}
		for _, v := range previous {
			watermarks[[2]int32{v.TankPosition, v.Position}] = v.SourceTimes
		}
		for ti := range snapshot.Tanks {
			t := &snapshot.Tanks[ti]
			for mi := range t.Modules {
				m := &t.Modules[mi]
				if m.Metrics != nil && m.Metrics.Rollup != nil {
					if err := rejectRegressedRollup(m.Metrics.Rollup, watermarks[[2]int32{t.Position, m.Position}]); err != nil {
						return err
					}
				}
			}
		}
	}
	if err := storeHashboards(ctx, q, orgID, id, snapshot); err != nil {
		return err
	}
	if snapshot.Metrics != nil {
		samples = []moduleSample{}
		for _, t := range snapshot.Tanks {
			for _, m := range t.Modules {
				if v := m.Metrics; v != nil {
					samples = append(samples, moduleSample{TankPosition: t.Position, Position: m.Position, ReportedID: m.ReportedID, Time: snapshot.ObservedAt, HashrateHS: v.HashrateHS, PowerW: v.PowerW, TemperatureC: v.TemperatureC, EfficiencyJH: v.EfficiencyJH, Coverage: v.Rollup})
				}
			}
		}
		sampleJSON, err = json.Marshal(samples)
		if err != nil {
			return fmt.Errorf("encode accepted module samples: %w", err)
		}
	}
	if err := storeContainerRollups(ctx, q, id, snapshot); err != nil {
		return err
	}

	if err := q.InsertContainerModuleMetrics(ctx, sqlc.InsertContainerModuleMetricsParams{DeviceIdentifier: identifier, CollectedAt: snapshot.ObservedAt, Samples: sampleJSON}); err != nil {
		return err
	}
	return storeContainerEquipment(ctx, q, identifier, snapshot)
}
