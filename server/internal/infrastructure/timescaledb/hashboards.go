package timescaledb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/generated/sqlc"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

const occupiedHashboardSlot = "occupied"

type hashboardObservation struct {
	unlocated bool
	claims    []string
	sdk.ContainerHashboard
	BoardID   int64      `json:"board_id"`
	ModuleID  int64      `json:"module_id"`
	SiteID    *int64     `json:"site_id"`
	Time      *time.Time `json:"time"`
	SourceAt  *time.Time `json:"source_at"`
	StateTime *time.Time `json:"state_observed_at"`
}

// Database JSON uses the same textual status values as ordinary miners.
func (v hashboardObservation) MarshalJSON() ([]byte, error) {
	type observation hashboardObservation
	data, err := json.Marshal(struct {
		observation
		State string `json:"state"`
	}{observation: observation(v), State: v.State.String()})
	if err != nil {
		return nil, fmt.Errorf("encode board observation: %w", err)
	}
	return data, nil
}

// Runs under the telemetry transaction and org lock. All database operations
// are batched, independent of the number of boards in the controller map.
func storeHashboards(ctx context.Context, q *sqlc.Queries, orgID, deviceID int64, snapshot *sdk.ContainerSnapshot) error {
	modules, err := q.ListHashboardModules(ctx, deviceID)
	if err != nil {
		return err
	}
	byPosition := map[[2]int32]sqlc.ListHashboardModulesRow{}
	retired := map[int64]bool{}
	moduleCoordinates := map[int64][2]int32{}
	moduleTanks := map[int64]int64{}
	for _, m := range modules {
		moduleTanks[m.ID] = m.TankID
		moduleCoordinates[m.ID] = [2]int32{m.TankPosition, m.Position}
		if m.RetiredAt.Valid {
			retired[m.ID] = true
		} else {
			byPosition[[2]int32{m.TankPosition, m.Position}] = m
		}
	}
	site, err := q.GetHashboardControllerSite(ctx, deviceID)
	if err != nil {
		return err
	}
	observations := collectHashboardObservations(ctx, deviceID, snapshot, byPosition)
	for i := range observations {
		if site.Valid {
			observations[i].SiteID = &site.Int64
		}
		if !observations[i].ObservedAt.IsZero() {
			observations[i].SourceAt = &observations[i].ObservedAt
		}
		if !observations[i].StateObservedAt.IsZero() {
			observations[i].StateTime = &observations[i].StateObservedAt
		}
	}
	identityClaims := []map[string]string{}
	for _, v := range observations {
		for _, key := range v.claims {
			kind, value, _ := strings.Cut(key, ":")
			if kind == "mac" {
				kind = "mac_address"
			}
			identityClaims = append(identityClaims, map[string]string{kind: value})
		}
	}
	data, err := json.Marshal(identityClaims)
	if err != nil {
		return fmt.Errorf("encode hashboard observations: %w", err)
	}
	identities, err := q.FindHashboardIdentities(ctx, sqlc.FindHashboardIdentitiesParams{OrgID: orgID, Boards: data})
	if err != nil {
		return err
	}
	aliases := map[string]int64{}
	lastSeen := map[int64]time.Time{}
	serials := map[int64]string{}
	for _, i := range identities {
		aliases[i.Kind+":"+i.Value] = i.HashboardID
		lastSeen[i.HashboardID] = i.LastSeenAt
		if i.Kind == "serial" {
			serials[i.HashboardID] = i.Value
		}
	}
	// Resolve and count every alias before writing anything. Distinct MAC/serial
	// observations may resolve to the same durable board even without raw duplicates.
	aliasCounts := map[string]int{}
	boardCounts := map[int64]int{}
	for _, v := range observations {
		ids := map[int64]bool{}
		for _, key := range v.claims {
			aliasCounts[key]++
			if id := aliases[key]; id != 0 {
				ids[id] = true
			}
		}
		for id := range ids {
			boardCounts[id]++
		}
	}
	boardIDs := []int64{}
	for i := range observations {
		v := &observations[i]
		macID, serialID := aliases["mac:"+v.MACAddress], aliases["serial:"+v.Serial]
		v.BoardID = max(macID, serialID)
		conflict := macID != 0 && serialID != 0 && macID != serialID
		for _, key := range v.claims {
			conflict = conflict || aliasCounts[key] > 1 || boardCounts[aliases[key]] > 1
		}
		knownSerial := serials[v.BoardID]
		conflict = conflict || (knownSerial != "" && v.Serial != "" && knownSerial != v.Serial)
		if conflict {
			rejectHashboard(ctx, deviceID, v, sdk.HashboardIdentityConflict)
			continue
		}
		if v.Occupancy != occupiedHashboardSlot {
			continue
		}
		if lastSeen[v.BoardID].After(snapshot.ObservedAt) {
			rejectHashboard(ctx, deviceID, v, sdk.HashboardOlderObservation)
			continue
		}
		if v.Serial == "" && knownSerial != "" {
			v.Serial = knownSerial
			if err := validateHashboard(&v.ContainerHashboard, snapshot.ObservedAt); err != nil {
				rejectHashboard(ctx, deviceID, v, sdk.HashboardInvalidObservation)
				continue
			}
		}
		if v.BoardID != 0 {
			boardIDs = append(boardIDs, v.BoardID)
		}
	}
	// Ownership and attribution are properties of the durable board, not its slot.
	prior, err := q.GetHashboardObservations(ctx, sqlc.GetHashboardObservationsParams{OrgID: orgID, BoardIds: boardIDs})
	if err != nil {
		return err
	}
	byObservation := map[int64]sqlc.HashboardObservation{}
	for _, o := range prior {
		byObservation[o.HashboardID] = o
	}
	for i := range observations {
		v := &observations[i]
		old, ok := byObservation[v.BoardID]
		if !ok || v.Occupancy != occupiedHashboardSlot {
			continue
		}
		if old.LastSeenAt.After(snapshot.ObservedAt) {
			rejectHashboard(ctx, deviceID, v, sdk.HashboardOlderObservation)
			continue
		}
		if old.DeviceID != deviceID && !old.ReleasedAt.Valid && snapshot.ObservedAt.Sub(old.LastSeenAt) < 2*time.Minute {
			rejectHashboard(ctx, deviceID, v, sdk.HashboardControllerConflict)
			continue
		}
		boundary := old.AttributionSince
		if old.DeviceID != deviceID || old.SiteID != site || old.ReleasedAt.Valid {
			boundary = sql.NullTime{Time: snapshot.ObservedAt, Valid: true}
		}
		if old.SourceAt.Valid && v.ObservedAt.Before(old.SourceAt.Time) {
			v.HashrateHS, v.PowerW, v.TemperatureC, v.EfficiencyJH = nil, nil, nil, nil
		}
		if boundary.Valid {
			if err := applyHashboardAttributionBoundary(v, boundary.Time); err != nil {
				return err
			}
		}
	}
	watermarks, err := q.GetHashboardMetricWatermarks(ctx, boardIDs)
	if err != nil {
		return err
	}
	sourceTimes := map[int64]time.Time{}
	for _, w := range watermarks {
		sourceTimes[w.HashboardID] = w.Time
	}
	for i := range observations {
		v := &observations[i]
		if v.ObservedAt.Before(sourceTimes[v.BoardID]) {
			v.HashrateHS = nil
			v.PowerW = nil
			v.TemperatureC = nil
			v.EfficiencyJH = nil
		}
	}
	installations, err := q.GetHashboardInstallations(ctx, sqlc.GetHashboardInstallationsParams{OrgID: orgID, DeviceID: deviceID, BoardIds: boardIDs})
	if err != nil {
		return err
	}
	byBoard := map[int64]sqlc.GetHashboardInstallationsRow{}
	for _, i := range installations {
		byBoard[i.HashboardID] = i
	}
	for i := range observations {
		v := &observations[i]
		old, ok := byBoard[v.BoardID]
		if !ok {
			continue
		}
		if old.LastSeenAt.After(snapshot.ObservedAt) {
			rejectHashboard(ctx, deviceID, v, sdk.HashboardOlderObservation)
			continue
		}
		coordinates := moduleCoordinates[v.ModuleID]
		changedPlacement := !v.unlocated && (old.ModuleID != v.ModuleID || old.Position != v.Position || old.DeviceID != deviceID || old.TankID != moduleTanks[v.ModuleID] || old.ModulePosition != coordinates[1])
		// Apply the installation boundary on every poll, not just the poll that
		// changes placement. Initial discovery may contain asynchronous readings.
		var boundary time.Time
		if old.HasPriorInstallation {
			boundary = old.InstalledAt
		}
		if changedPlacement {
			boundary = snapshot.ObservedAt
		}
		if !boundary.IsZero() {
			if err := applyHashboardAttributionBoundary(v, boundary); err != nil {
				return err
			}
		}
	}
	// Allocate durable IDs and enrich their aliases only after conflict checks.
	data, err = json.Marshal(observations)
	if err != nil {
		return fmt.Errorf("encode hashboard observations: %w", err)
	}
	boards, err := q.EnsureHashboards(ctx, sqlc.EnsureHashboardsParams{OrgID: orgID, ObservedAt: snapshot.ObservedAt, Boards: data})
	if err != nil {
		return err
	}
	for _, b := range boards {
		if b.MacAddress != "" {
			aliases["mac:"+b.MacAddress] = b.ID
		}
		if b.Serial != "" {
			aliases["serial:"+b.Serial] = b.ID
		}
	}
	for i := range observations {
		v := &observations[i]
		if v.Occupancy == occupiedHashboardSlot {
			v.BoardID = max(aliases["mac:"+v.MACAddress], aliases["serial:"+v.Serial])
		}
	}
	data, err = json.Marshal(observations)
	if err != nil {
		return fmt.Errorf("encode hashboard ownership: %w", err)
	}
	if err := q.UpsertHashboardObservations(ctx, sqlc.UpsertHashboardObservationsParams{DeviceID: deviceID, ObservedAt: snapshot.ObservedAt, Boards: data}); err != nil {
		return err
	}
	// Unlocated observations take part in validation and durable identity lookup,
	// but never enter slot, installation, or placement-scoped metric writes.
	mapped := make([]hashboardObservation, 0, len(observations))
	snapshot.UnlocatedHashboards = nil
	for _, v := range observations {
		if !v.unlocated {
			mapped = append(mapped, v)
			continue
		}
		snapshot.UnlocatedHashboards = append(snapshot.UnlocatedHashboards, v.ContainerHashboard)
	}
	observations = mapped
	bySlot := map[[2]int64]hashboardObservation{}
	newByBoard := map[int64]hashboardObservation{}
	occupied := []hashboardObservation{}
	for i := range observations {
		v := &observations[i]
		if v.Occupancy == occupiedHashboardSlot {
			v.BoardID = max(aliases["mac:"+v.MACAddress], aliases["serial:"+v.Serial])
			newByBoard[v.BoardID] = *v
			occupied = append(occupied, *v)
		}
		bySlot[[2]int64{v.ModuleID, int64(v.Position)}] = *v
	}
	closeIDs := []int64{}
	for _, old := range installations {
		if old.LastSeenAt.After(snapshot.ObservedAt) {
			continue
		}
		v, reported := bySlot[[2]int64{old.ModuleID, int64(old.Position)}]
		moved, present := newByBoard[old.HashboardID]
		changedSite := present && sql.NullInt64{Int64: siteValue(moved.SiteID), Valid: moved.SiteID != nil} != old.SiteID
		coordinates := moduleCoordinates[moved.ModuleID]
		changedPlacement := present && (old.DeviceID != deviceID || old.TankID != moduleTanks[moved.ModuleID] || old.ModulePosition != coordinates[1])
		// A confirmed module move ends its prior installations even when board
		// evidence is unavailable. Unknown slots at an unchanged placement do not.
		currentTank, moduleHere := moduleTanks[old.ModuleID]
		currentCoordinates := moduleCoordinates[old.ModuleID]
		moduleMoved := moduleHere && !retired[old.ModuleID] && (old.DeviceID != deviceID || old.TankID != currentTank || old.ModulePosition != currentCoordinates[1] || old.SiteID != site)
		retirePlacement := retired[old.ModuleID]
		// Rejected replacement evidence cannot retire the previous board placement.
		// The old module stays hidden; a later trustworthy observation resolves it.
		if replacement, ok := byPosition[moduleCoordinates[old.ModuleID]]; ok && replacement.ID != old.ModuleID {
			replacementSlot := bySlot[[2]int64{replacement.ID, int64(old.Position)}]
			if replacementSlot.UnavailableReason != "" {
				retirePlacement = false
			}
		}
		if moduleMoved || retirePlacement || (reported && (v.Occupancy == "empty" || (v.Occupancy == occupiedHashboardSlot && v.BoardID != old.HashboardID))) || (present && (moved.ModuleID != old.ModuleID || moved.Position != old.Position || changedSite || changedPlacement)) {
			closeIDs = append(closeIDs, old.ID)
		}
	}
	if err := q.CloseHashboardInstallations(ctx, sqlc.CloseHashboardInstallationsParams{ObservedAt: snapshot.ObservedAt, Ids: closeIDs}); err != nil {
		return err
	}
	if err := q.ReleaseHashboardObservations(ctx, sqlc.ReleaseHashboardObservationsParams{DeviceID: deviceID, ObservedAt: snapshot.ObservedAt, InstallationIds: closeIDs}); err != nil {
		return err
	}
	data, err = json.Marshal(observations)
	if err != nil {
		return fmt.Errorf("encode hashboard observations: %w", err)
	}
	if err := q.UpsertHashboardSlots(ctx, sqlc.UpsertHashboardSlotsParams{ObservedAt: snapshot.ObservedAt, Slots: data}); err != nil {
		return err
	}
	data, err = json.Marshal(occupied)
	if err != nil {
		return fmt.Errorf("encode occupied hashboards: %w", err)
	}
	if err := q.UpsertHashboardInstallations(ctx, sqlc.UpsertHashboardInstallationsParams{ObservedAt: snapshot.ObservedAt, Boards: data}); err != nil {
		return err
	}
	if err := q.ReconcileHostedHashboardReplacements(ctx, sqlc.ReconcileHostedHashboardReplacementsParams{OrganizationID: orgID, DeviceID: deviceID, ObservedAt: snapshot.ObservedAt}); err != nil {
		return err
	}
	if err := q.InsertHashboardMetrics(ctx, sqlc.InsertHashboardMetricsParams{CollectedAt: snapshot.ObservedAt, Boards: data}); err != nil {
		return err
	}
	if err := q.InsertHashboardDiagnosticObservations(ctx, sqlc.InsertHashboardDiagnosticObservationsParams{CollectedAt: snapshot.ObservedAt, Boards: data}); err != nil {
		return err
	}
	if err := q.WithdrawHashboardReadings(ctx, sqlc.WithdrawHashboardReadingsParams{CollectedAt: snapshot.ObservedAt, Boards: data}); err != nil {
		return err
	}
	// Rebuild from normalized slots so rejected positions cannot survive into rollups.
	for ti := range snapshot.Tanks {
		for mi := range snapshot.Tanks[ti].Modules {
			m := &snapshot.Tanks[ti].Modules[mi]
			resolved := byPosition[[2]int32{snapshot.Tanks[ti].Position, m.Position}]
			boards := make([]sdk.ContainerHashboard, 0, sdk.CT1HashboardsPerModule)
			for position := int32(1); position <= sdk.CT1HashboardsPerModule; position++ {
				board := sdk.UnavailableHashboard(position, sdk.HashboardInvalidObservation)
				if v, ok := bySlot[[2]int64{resolved.ID, int64(position)}]; ok {
					board = v.ContainerHashboard
				}
				for _, reported := range m.Hashboards {
					if reported.Position == position {
						board.Feed = reported.Feed
						break
					}
				}
				if board.UnavailableReason != "" {
					snapshot.TopologyComplete = false
					snapshot.Tanks[ti].TopologyComplete = false
				}
				boards = append(boards, board)
			}
			m.Hashboards = boards
		}
	}
	if hasContainerRollups(snapshot) {
		sdk.CalculateContainerRollups(snapshot)
	}
	return nil
}

// Health and state have independent source times. Redact evidence before it
// reaches either slot state or metric history; a first installation otherwise
// permits asynchronous discovery samples. Keep ObservedAt/SourceAt intact for
// durable ordering, while Time=nil prevents a rejected health row being written.
func applyHashboardAttributionBoundary(v *hashboardObservation, boundary time.Time) error {
	if v.ObservedAt.Before(boundary) {
		v.Time = nil
		v.HashrateHS, v.PowerW, v.TemperatureC, v.EfficiencyJH = nil, nil, nil, nil
		v.HashrailOK, v.RefclksOK = nil, nil
		v.HealthJSON = nil
	}
	if v.StateObservedAt.Before(boundary) {
		v.State = sdk.MinerStatusUnknown
		v.StateObservedAt = time.Time{}
		v.StateTime = nil
		v.MiningState = ""
		v.Isolated = nil
		// The raw health object also contains fault/state classification evidence.
		v.HealthJSON = nil
		if len(v.MetadataJSON) > 0 {
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(v.MetadataJSON, &metadata); err != nil {
				return fmt.Errorf("decode hashboard metadata for attribution: %w", err)
			}
			delete(metadata, "miningState")
			delete(metadata, "isolated")
			var err error
			v.MetadataJSON, err = json.Marshal(metadata)
			if err != nil {
				return fmt.Errorf("encode attributed hashboard metadata: %w", err)
			}
		}
	}
	return nil
}

func siteValue(site *int64) int64 {
	if site == nil {
		return 0
	}
	return *site
}

func validateHashboard(b *sdk.ContainerHashboard, collected time.Time) error {
	if !sdk.SafeHashboardIdentity(b.MACAddress) || !sdk.SafeHashboardIdentity(b.Serial) || !sdk.SafeHashboardIdentity(b.MiningState) {
		return fmt.Errorf("invalid hashboard text")
	}
	for _, data := range []json.RawMessage{b.MetadataJSON, b.HealthJSON} {
		if len(data) == 0 || string(data) == "null" {
			continue
		}
		var object map[string]json.RawMessage
		if len(data) > 256*1024 || json.Unmarshal(data, &object) != nil || object == nil || sdk.ValidateHashboardJSON(data) != nil {
			return fmt.Errorf("invalid hashboard diagnostic object")
		}
	}
	if b.Occupancy == occupiedHashboardSlot {
		metadata := map[string]json.RawMessage{}
		if len(b.MetadataJSON) > 0 && string(b.MetadataJSON) != "null" {
			if err := json.Unmarshal(b.MetadataJSON, &metadata); err != nil {
				return fmt.Errorf("decode hashboard metadata: %w", err)
			}
		}
		var err error
		metadata["macAddress"], err = json.Marshal(b.MACAddress)
		if err != nil {
			return fmt.Errorf("encode hashboard MAC: %w", err)
		}
		metadata["serial"], err = json.Marshal(b.Serial)
		if err != nil {
			return fmt.Errorf("encode hashboard serial: %w", err)
		}
		b.MetadataJSON, err = json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("encode hashboard metadata: %w", err)
		}
	}
	switch b.State {
	case sdk.MinerStatusUnknown, sdk.MinerStatusActive, sdk.MinerStatusInactive, sdk.MinerStatusError, sdk.MinerStatusOffline, sdk.MinerStatusMaintenance, sdk.MinerStatusUpdating, sdk.MinerStatusNeedsMiningPool, sdk.MinerStatusRebootRequired:
	default:
		return fmt.Errorf("invalid hashboard state")
	}
	if b.ObservedAt.IsZero() || collected.Sub(b.ObservedAt) >= 2*time.Minute || b.ObservedAt.After(collected.Add(5*time.Second)) {
		b.HashrateHS, b.PowerW, b.EfficiencyJH, b.TemperatureC, b.HashrailOK, b.RefclksOK = nil, nil, nil, nil, nil, nil
	}
	for _, value := range []**float64{&b.HashrateHS, &b.TemperatureC, &b.PowerW, &b.EfficiencyJH} {
		if *value != nil && (math.IsNaN(**value) || math.IsInf(**value, 0)) {
			*value = nil
		}
	}
	if b.PowerW != nil && *b.PowerW < 0 {
		b.PowerW = nil
		b.EfficiencyJH = nil
	}
	if b.HashrateHS != nil && *b.HashrateHS < 0 {
		b.HashrateHS = nil
	}
	b.EfficiencyJH = nil
	if b.HashrateHS != nil && *b.HashrateHS > 0 && b.PowerW != nil {
		efficiency := *b.PowerW / *b.HashrateHS
		if !math.IsNaN(efficiency) && !math.IsInf(efficiency, 0) {
			b.EfficiencyJH = &efficiency
		}
	}
	if b.StateObservedAt.IsZero() || collected.Sub(b.StateObservedAt) >= 2*time.Minute || b.StateObservedAt.After(collected.Add(5*time.Second)) {
		b.State = sdk.MinerStatusUnknown
	}
	// Future source times must not pin the latest-value watermark.
	if b.ObservedAt.After(collected.Add(5 * time.Second)) {
		b.ObservedAt = time.Time{}
		b.HealthJSON = nil
	}
	return nil
}

func hashboardIdentityKeys(mac, serial string) []string {
	keys := []string{}
	if mac != "" && sdk.SafeHashboardIdentity(mac) {
		keys = append(keys, "mac:"+mac)
	}
	if serial != "" && sdk.SafeHashboardIdentity(serial) {
		keys = append(keys, "serial:"+serial)
	}
	return keys
}

func hashboardClaims(board sdk.ContainerHashboard) []string {
	keys := hashboardIdentityKeys(normalizeHashboardMAC(board.MACAddress), strings.TrimSpace(board.Serial))
	for _, claim := range board.IdentityClaims {
		keys = append(keys, hashboardIdentityKeys(normalizeHashboardMAC(claim.MACAddress), strings.TrimSpace(claim.Serial))...)
	}
	// Multiple aliases on one observation still claim only one placement.
	unique := map[string]bool{}
	out := []string{}
	for _, key := range keys {
		if !unique[key] {
			unique[key] = true
			out = append(out, key)
		}
	}
	return out
}

func rejectHashboard(ctx context.Context, deviceID int64, v *hashboardObservation, reason string) {
	slog.WarnContext(ctx, "hashboard observation unavailable", "device_id", deviceID, "module_id", v.ModuleID, "position", v.Position, "board_id", v.BoardID, "mac", v.MACAddress, "serial", v.Serial, "reason", reason)
	v.ContainerHashboard = sdk.UnavailableHashboard(v.Position, reason)
	v.BoardID, v.Time, v.StateTime = 0, nil, nil
}

func collectHashboardObservations(ctx context.Context, deviceID int64, snapshot *sdk.ContainerSnapshot, modules map[[2]int32]sqlc.ListHashboardModulesRow) []hashboardObservation {
	observations := []hashboardObservation{}
	for _, b := range snapshot.UnlocatedHashboards {
		v := hashboardObservation{ContainerHashboard: b, unlocated: true, claims: hashboardClaims(b)}
		if b.Position != 0 || b.Occupancy == "empty" {
			rejectHashboard(ctx, deviceID, &v, sdk.HashboardInvalidObservation)
		} else if err := normalizeHashboard(&v.ContainerHashboard, snapshot.ObservedAt); err != nil {
			rejectHashboard(ctx, deviceID, &v, sdk.HashboardInvalidObservation)
		}
		v.Position, v.Feed = 0, nil
		observations = append(observations, v)
	}
	for _, tank := range snapshot.Tanks {
		for _, module := range tank.Modules {
			m, ok := modules[[2]int32{tank.Position, module.Position}]
			if !ok {
				continue
			}
			slots := map[int32][]sdk.ContainerHashboard{}
			ambiguous := len(module.Hashboards) > sdk.CT1HashboardsPerModule
			for _, b := range module.Hashboards {
				if b.Position < 1 || b.Position > sdk.CT1HashboardsPerModule {
					ambiguous = true
					continue
				}
				slots[b.Position] = append(slots[b.Position], b)
			}
			for position := int32(1); position <= sdk.CT1HashboardsPerModule; position++ {
				v := hashboardObservation{ContainerHashboard: sdk.UnavailableHashboard(position, ""), ModuleID: m.ID}
				if m.SiteID.Valid {
					v.SiteID = &m.SiteID.Int64
				}
				reported := slots[position]
				for _, claim := range reported {
					v.claims = append(v.claims, hashboardClaims(claim)...)
				}
				if position == 1 {
					for _, claim := range module.Hashboards {
						if claim.Position < 1 || claim.Position > sdk.CT1HashboardsPerModule {
							v.claims = append(v.claims, hashboardClaims(claim)...)
						}
					}
				}
				if len(reported) == 1 {
					v.ContainerHashboard = reported[0]
				}
				if ambiguous || len(reported) > 1 {
					rejectHashboard(ctx, deviceID, &v, sdk.HashboardAmbiguousSlot)
				} else if err := normalizeHashboard(&v.ContainerHashboard, snapshot.ObservedAt); err != nil {
					rejectHashboard(ctx, deviceID, &v, sdk.HashboardInvalidObservation)
				}
				if !v.ObservedAt.IsZero() {
					v.Time = &v.ObservedAt
				}
				observations = append(observations, v)
			}
		}
	}
	return observations
}

func normalizeHashboard(b *sdk.ContainerHashboard, collected time.Time) error {
	// Claims have already been extracted; never serialize untrusted claims to SQL.
	b.IdentityClaims = nil
	b.MACAddress = normalizeHashboardMAC(b.MACAddress)
	b.Serial = strings.TrimSpace(b.Serial)
	switch b.Occupancy {
	case occupiedHashboardSlot:
		if b.MACAddress == "" && b.Serial == "" {
			*b = sdk.UnavailableHashboard(b.Position, sdk.HashboardMissingIdentity)
			return nil
		}
		b.UnavailableReason = ""
	case "empty", "unknown":
		if b.MACAddress != "" || b.Serial != "" {
			return fmt.Errorf("unoccupied slot has hardware identity")
		}
		reason := b.UnavailableReason
		switch reason {
		case "", sdk.HashboardInvalidObservation, sdk.HashboardAmbiguousSlot, sdk.HashboardMissingIdentity, sdk.HashboardIdentityConflict, sdk.HashboardControllerConflict, sdk.HashboardOlderObservation:
		default:
			reason = sdk.HashboardInvalidObservation
		}
		occupancy := b.Occupancy
		*b = sdk.UnavailableHashboard(b.Position, reason)
		if reason == "" {
			b.Occupancy = occupancy
		}
	default:
		return fmt.Errorf("invalid hashboard occupancy")
	}
	return validateHashboard(b, collected)
}

func normalizeHashboardMAC(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if mac, err := net.ParseMAC(value); err == nil {
		return mac.String()
	}
	return value
}
