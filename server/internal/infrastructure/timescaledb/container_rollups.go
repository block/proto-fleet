package timescaledb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type topologySlot struct {
	Position  int32                      `json:"position"`
	Occupancy string                     `json:"occupancy"`
	BoardID   int64                      `json:"board_id,omitempty"`
	MAC       string                     `json:"mac,omitempty"`
	Serial    string                     `json:"serial,omitempty"`
	Reason    string                     `json:"reason,omitempty"`
	Feed      *sdk.ContainerFeedEvidence `json:"feed,omitempty"`
}
type topologyModule struct {
	Position   int32          `json:"position"`
	ReportedID string         `json:"reported_id"`
	Slots      []topologySlot `json:"slots"`
}
type topologyTank struct {
	Position    int32            `json:"position"`
	Complete    bool             `json:"complete"`
	PowerSource string           `json:"power_source"`
	Attribution string           `json:"attribution"`
	Modules     []topologyModule `json:"modules"`
}

// Validate plugin/FleetNode evidence before any source-ordering comparison or
// write. Invalid values may retain trustworthy times, but invalid times must
// never become durable watermarks that suppress subsequent valid readings.
func normalizeContainerPowerTimes(s *sdk.ContainerSnapshot, received time.Time) {
	normalize := func(r *sdk.ContainerRollup) {
		if r == nil {
			return
		}
		for key, at := range r.SourceTimes {
			if key == "" || at.IsZero() || at.After(s.ObservedAt.Add(sdk.ContainerClockSkewAllowance)) || at.After(received.Add(sdk.ContainerClockSkewAllowance)) {
				delete(r.SourceTimes, key)
			}
		}
		at := r.SourceTimes[r.PowerSource]
		// A dedicated meter has one source time. Coverage cannot establish a
		// fresh value while its ordering evidence refers to a different sample.
		if at.IsZero() || (r.PowerW != nil && (!r.Power.OldestAt.Equal(at) || !r.Power.NewestAt.Equal(at))) {
			delete(r.SourceTimes, r.PowerSource)
			r.PowerW, r.EfficiencyJH = nil, nil
			r.Power.Reporting = 0
			r.Power.OldestAt, r.Power.NewestAt = time.Time{}, time.Time{}
		}
	}
	normalize(s.Metrics)
	for ti := range s.Tanks {
		t := &s.Tanks[ti]
		normalize(t.Metrics)
		for mi := range t.Modules {
			if m := t.Modules[mi].Metrics; m != nil {
				normalize(m.Rollup)
			}
		}
	}
}

// rejectRegressedRollup withdraws a delayed measurement without advancing its
// source watermark. The latest tables merge watermarks independently of values.
func rejectRegressedRollup(r *sdk.ContainerRollup, previous []byte) error {
	var times map[string]time.Time
	if len(previous) > 0 {
		if err := json.Unmarshal(previous, &times); err != nil {
			return fmt.Errorf("decode power source watermarks: %w", err)
		}
	}
	// Board readings have already passed durable-identity/source validation.
	// Only this level's own power meter can withdraw its power and efficiency.
	if at := r.SourceTimes[r.PowerSource]; !at.IsZero() && at.Before(times[r.PowerSource]) {
		r.PowerW = nil
		r.Power.Reporting = 0
		r.EfficiencyJH = nil
	}

	return nil
}

func storeContainerRollups(ctx context.Context, q *sqlc.Queries, id int64, s *sdk.ContainerSnapshot) error {
	parent, err := q.GetContainerRollupContext(ctx, id)
	if err != nil {
		return err
	}
	slots, err := q.GetContainerTopologyIdentities(ctx, id)
	if err != nil {
		return err
	}
	boardIDs := map[[3]int32]int64{}
	for _, v := range slots {
		boardIDs[[3]int32{v.TankPosition, v.ModulePosition, v.Position}] = v.HashboardID.Int64
	}
	tanks := make([]topologyTank, 0, len(s.Tanks))
	type boardSample struct {
		HashboardID int64     `json:"hashboard_id"`
		Time        time.Time `json:"time"`
	}
	boardSamples := make([]boardSample, 0, len(slots))
	for _, t := range s.Tanks {
		tt := topologyTank{Position: t.Position, Complete: t.TopologyComplete, Modules: []topologyModule{}}
		if t.Metrics != nil {
			tt.PowerSource = t.Metrics.PowerSource
			tt.Attribution = t.Metrics.Attribution
		}
		for _, m := range t.Modules {
			tm := topologyModule{Position: m.Position, ReportedID: m.ReportedID, Slots: []topologySlot{}}
			for _, b := range m.Hashboards {
				boardID := int64(0)
				if b.Occupancy == "occupied" {
					boardID = boardIDs[[3]int32{t.Position, m.Position, b.Position}]
					if boardID != 0 && !b.ObservedAt.IsZero() {
						boardSamples = append(boardSamples, boardSample{boardID, b.ObservedAt})
					}
				}
				tm.Slots = append(tm.Slots, topologySlot{b.Position, b.Occupancy, boardID, b.MACAddress, b.Serial, b.UnavailableReason, b.Feed})
			}
			sort.Slice(tm.Slots, func(i, j int) bool { return tm.Slots[i].Position < tm.Slots[j].Position })
			tt.Modules = append(tt.Modules, tm)
		}
		sort.Slice(tt.Modules, func(i, j int) bool { return tt.Modules[i].Position < tt.Modules[j].Position })
		tanks = append(tanks, tt)
	}
	sort.Slice(tanks, func(i, j int) bool { return tanks[i].Position < tanks[j].Position })
	topology, err := json.Marshal(struct {
		Schema     int            `json:"schema"`
		Controller string         `json:"controller"`
		Site       sql.NullInt64  `json:"site"`
		Complete   bool           `json:"complete"`
		Tanks      []topologyTank `json:"tanks"`
	}{1, parent.SerialNumber.String, parent.SiteID, s.TopologyComplete, tanks})
	if err != nil {
		return fmt.Errorf("encode topology: %w", err)
	}
	digest := sha256.Sum256(topology)
	version, err := q.UpsertContainerTopologyVersion(ctx, sqlc.UpsertContainerTopologyVersionParams{DeviceID: id, Fingerprint: hex.EncodeToString(digest[:]), Topology: topology})
	if err != nil {
		return err
	}
	observation, err := q.InsertContainerTopologyObservation(ctx, sqlc.InsertContainerTopologyObservationParams{DeviceID: id, VersionID: version, CollectedAt: s.ObservedAt, Complete: s.TopologyComplete})
	if err != nil {
		return err
	}
	encodedSamples, err := json.Marshal(boardSamples)
	if err != nil {
		return fmt.Errorf("encode board provenance: %w", err)
	}
	if err := q.StampHashboardTopology(ctx, sqlc.StampHashboardTopologyParams{VersionID: version, CollectedAt: s.ObservedAt, Samples: encodedSamples}); err != nil {
		return err
	}
	// Old plugins cannot supply the new measurement contract. Explicitly withdraw
	// latest values on their next poll; never restore legacy power fallbacks.
	if s.Metrics == nil {
		s.Metrics = &sdk.ContainerRollup{}
		for i := range s.Tanks {
			s.Tanks[i].Metrics = &sdk.ContainerRollup{}
		}
	}
	previous, err := q.GetContainerRollupWatermarks(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := rejectRegressedRollup(s.Metrics, previous); err != nil {
		return err
	}
	tankPrevious, err := q.GetTankRollupWatermarks(ctx, id)
	if err != nil {
		return err
	}
	byTank := map[int32][]byte{}
	for _, p := range tankPrevious {
		byTank[p.Position] = p.SourceTimes
	}
	type sample struct {
		Position int32 `json:"position"`
		*sdk.ContainerRollup
		Coverage *sdk.ContainerRollup `json:"coverage"`
	}
	samples := []sample{}
	for i := range s.Tanks {
		t := &s.Tanks[i]
		if t.Metrics == nil {
			t.Metrics = &sdk.ContainerRollup{}
		}
		if err := rejectRegressedRollup(t.Metrics, byTank[t.Position]); err != nil {
			return err
		}
		samples = append(samples, sample{t.Position, t.Metrics, t.Metrics})
	}
	data, err := json.Marshal(samples)
	if err != nil {
		return fmt.Errorf("encode tank samples: %w", err)
	}
	if err := q.InsertContainerTankRollups(ctx, sqlc.InsertContainerTankRollupsParams{DeviceID: id, Time: s.ObservedAt, ObservationID: observation, Samples: data}); err != nil {
		return fmt.Errorf("insert tank rollups: %w", err)
	}
	data, err = json.Marshal(s.Metrics)
	if err != nil {
		return fmt.Errorf("encode container rollup: %w", err)
	}
	return q.UpsertContainerRollup(ctx, sqlc.UpsertContainerRollupParams{DeviceID: id, Time: s.ObservedAt, ObservationID: observation, Coverage: data})
}
