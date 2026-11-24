package ct1

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc/status"
)

// ContainerPowerFeed reads the feed that supplies one module. The mapping comes
// only from the controller map's feed evidence, never from tank or module
// numbers, and is reported with its confidence so callers can refuse to act on
// an ambiguous association.
func (d *Device) ContainerPowerFeed(ctx context.Context, req sdk.ContainerPowerFeedRequest) (sdk.ContainerPowerFeedResult, error) {
	var result sdk.ContainerPowerFeedResult
	if err := req.Validate(); err != nil {
		return result, sdk.InvalidWriteRequest(err)
	}
	if !req.ReadOnly() {
		return d.writePowerFeed(ctx, req)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c, err := d.verifiedReadClient(ctx)
	if err != nil {
		return result, err
	}
	var (
		wg       sync.WaitGroup
		feeds    *mdk.ListPowerFeedsResponse
		feedsErr error
		cycle    *mdk.GetActivePowerRecoveryCycleResponse
		cycleErr error
		system   *mdk.GetSystemStatusResponse
		sysErr   error
	)
	wg.Add(3)
	go func() { defer wg.Done(); feeds, feedsErr = c.power.ListPowerFeeds(ctx, &mdk.ListPowerFeedsRequest{}) }()
	go func() {
		defer wg.Done()
		cycle, cycleErr = c.power.GetActivePowerRecoveryCycle(ctx, &mdk.GetActivePowerRecoveryCycleRequest{})
	}()
	go func() { defer wg.Done(); system, sysErr = c.system.GetSystemStatus(ctx, &mdk.GetSystemStatusRequest{}) }()
	mapWire, err := c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{})
	wg.Wait()
	if err != nil {
		return result, fmt.Errorf("read container map: %w", err)
	}
	inspection := &sdk.ModulePowerFeedInspection{Mapping: moduleFeedMapping(mapResponse(mapWire), req.Module)}
	if id := inspection.Mapping.FeedID; id != "" {
		if feedsErr != nil {
			inspection.FeedUnavailableReason = sdk.PowerFeedReadingReadFailed
		} else {
			inspection.Feed, inspection.FeedUnavailableReason = feedReading(feeds, id)
		}
	}
	if cycleErr == nil {
		inspection.RecoveryCycleKnown = true
		inspection.RecoveryCycle = recoveryCycle(cycle.GetCycle())
	}
	if sysErr == nil && system.GetStatus() != nil {
		inspection.ControllerWritePrimary = new(system.GetStatus().GetContainerWritePrimary())
	}
	inspection.ObservedAt = time.Now().UTC()
	result.Inspection = inspection
	return result, result.Validate(req)
}

// verifiedReadClient selects a controller for a read and, for a standalone
// controller without HA endpoints, confirms its live identity first.
func (d *Device) verifiedReadClient(ctx context.Context) (*Client, error) {
	c, err := d.selectClient(ctx, false)
	if err != nil {
		return nil, sdk.WriteRefused(err, sdk.WriteStageBackendSelection, sdk.WriteReasonTransportFailed)
	}
	if len(d.info.Endpoints) > 0 {
		return c, nil
	}
	expected := strings.TrimSpace(d.info.SerialNumber)
	if expected == "" {
		return nil, sdk.WriteRefused(fmt.Errorf("container identity unavailable"), sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
	}
	info, err := c.Discover(ctx, d.info)
	if err != nil {
		reason := sdk.WriteReasonStaleBinding
		if _, rpcError := status.FromError(err); rpcError {
			reason = sdk.WriteReasonReadFailed
		}
		return nil, sdk.WriteRefused(fmt.Errorf("container identity unavailable: %w", err), sdk.WriteStageConfigurationRead, reason)
	}
	if strings.TrimSpace(info.SerialNumber) != expected {
		return nil, sdk.WriteRefused(fmt.Errorf("container identity changed"), sdk.WriteStageConfigurationRead, sdk.WriteReasonStaleBinding)
	}
	return c, nil
}

// Wiring statuses that do not contradict the configured feed. Frigate reports
// an empty status when its switch-port wiring check is not configured.
func benignWiringStatus(status string) bool {
	return status == "" || status == "verified" || status == "unverified"
}

func slotFeedIDs(f *sdk.ContainerFeedEvidence) []string {
	if f == nil {
		return nil
	}
	out := []string{}
	for _, id := range []string{f.PowerFeedID, f.ObservedPowerFeedID, f.ExpectedPowerFeedID} {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// moduleFeedMapping derives the module's feed from every slot's evidence:
// the slot's configured feed, the board's observed and expected feeds, and its
// wiring status. A unique mapping needs exactly one feed across all of it,
// claimed by no other module, at a module identity Fleet confirmed, in a map
// complete enough to show every module that could share the feed.
func moduleFeedMapping(wire MapResponse, target sdk.ContainerPowerFeedModule) sdk.PowerFeedMapping {
	mapping := sdk.PowerFeedMapping{Confidence: sdk.PowerFeedMappingUnavailable}
	reasons := map[string]bool{}
	if wire.ContainerMap == nil {
		mapping.Reasons = []string{"topology_unavailable"}
		return mapping
	}
	var module *Module
	matches := 0
	for ti := range wire.ContainerMap.Tanks {
		tank := &wire.ContainerMap.Tanks[ti]
		if tank.ID != target.Tank {
			continue
		}
		for mi := range tank.Modules {
			if tank.Modules[mi].ID == target.Module {
				module = &tank.Modules[mi]
				matches++
			}
		}
	}
	if module == nil {
		mapping.Reasons = []string{"module_not_in_topology"}
		return mapping
	}
	if matches > 1 {
		reasons["duplicate_module"] = true
	}
	if target.ReportedID == "" {
		reasons["module_identity_unconfirmed"] = true
	} else if !collectTopologyIdentities(wire).uniqueModule(target.Tank, target.Module, target.ReportedID) {
		reasons["module_identity_changed"] = true
	}
	ids := []string{}
	occupied, verified := 0, 0
	unnamed := false // an occupied slot that names no feed
	for i, slot := range module.Slots {
		evidence := sdk.PowerFeedBoardEvidence{Occupied: slot.Occupied}
		if i < boardsPerModule && !slot.invalid {
			evidence.Position = int32(i + 1) //nolint:gosec // bounded by boardsPerModule
		}
		if slot.invalid {
			reasons["slot_placement_invalid"] = true
		}
		if b := slot.Hashboard; b != nil {
			evidence.Serial, evidence.MACAddress = strings.TrimSpace(b.Serial), normalizedBoardMAC(b.MACAddress)
		}
		if f := slot.Feed; f != nil {
			evidence.SlotFeedID = strings.TrimSpace(f.PowerFeedID)
			evidence.ObservedFeedID = strings.TrimSpace(f.ObservedPowerFeedID)
			evidence.ExpectedFeedID = strings.TrimSpace(f.ExpectedPowerFeedID)
			evidence.WiringStatus = strings.ToLower(strings.TrimSpace(f.WiringStatus))
			evidence.WiringReasons = slices.Clone(f.WiringReasons)
			for _, id := range slotFeedIDs(f) {
				if !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
			if !benignWiringStatus(evidence.WiringStatus) {
				reasons["wiring_"+wiringReasonSuffix(evidence.WiringStatus)] = true
			}
			if (f.ConfiguredTank != 0 && f.ConfiguredTank != target.Tank) || (f.ConfiguredModule != 0 && f.ConfiguredModule != target.Module) {
				reasons["configured_location_mismatch"] = true
			}
		}
		if evidence.Occupied {
			if len(slotFeedIDs(slot.Feed)) == 0 {
				unnamed = true
			}
			occupied++
			if evidence.WiringStatus == "verified" {
				verified++
			}
		}
		if !slot.unreported || slot.Feed != nil {
			mapping.Boards = append(mapping.Boards, evidence)
		}
	}
	switch len(ids) {
	case 0:
		reasons["no_feed_assigned"] = true
	case 1:
		mapping.FeedID = ids[0]
		mapping.SharedWith = modulesClaimingFeed(wire, ids[0], target)
		if len(mapping.SharedWith) > 0 {
			reasons["feed_shared"] = true
		}
		if !feedSharingKnown(wire, target) {
			reasons["topology_incomplete"] = true
		}
	default:
		reasons["multiple_feeds"] = true
	}
	if unnamed && len(ids) > 0 {
		reasons["feed_evidence_incomplete"] = true
	}
	for reason := range reasons {
		mapping.Reasons = append(mapping.Reasons, reason)
	}
	slices.Sort(mapping.Reasons)
	switch {
	case len(ids) == 0 && len(reasons) == 1:
		mapping.Confidence = sdk.PowerFeedMappingUnavailable
	case len(reasons) > 0:
		mapping.Confidence = sdk.PowerFeedMappingAmbiguous
	case occupied > 0 && verified == occupied:
		mapping.Confidence = sdk.PowerFeedMappingWiringVerified
	default:
		mapping.Confidence = sdk.PowerFeedMappingConfigured
	}
	return mapping
}

func wiringReasonSuffix(status string) string {
	var b strings.Builder
	for _, r := range status {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
		if b.Len() >= 32 {
			break
		}
	}
	return b.String()
}

// modulesClaimingFeed lists other placed modules whose evidence names feed.
func modulesClaimingFeed(wire MapResponse, feed string, target sdk.ContainerPowerFeedModule) []sdk.PowerFeedModuleRef {
	out := []sdk.PowerFeedModuleRef{}
	for _, tank := range wire.ContainerMap.Tanks {
		if tank.ID == 0 {
			continue
		}
		for _, module := range tank.Modules {
			if tank.ID == target.Tank && module.ID == target.Module {
				continue
			}
			for _, slot := range module.Slots {
				if slices.Contains(slotFeedIDs(slot.Feed), feed) {
					out = append(out, sdk.PowerFeedModuleRef{Tank: tank.ID, Module: module.ID})
					break
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// feedSharingKnown reports whether the map accounts for everything that could
// draw from a feed. Feed records name no modules, so sharing is known only
// from the map: a tank or module position it omits, a slot it does not
// report, an unlocated board, or an occupied slot elsewhere that names no
// feed could share the target's feed without appearing in SharedWith.
func feedSharingKnown(wire MapResponse, target sdk.ContainerPowerFeedModule) bool {
	tanks := map[int32]bool{}
	for _, tank := range wire.ContainerMap.Tanks {
		if tank.ID == 0 {
			for _, module := range tank.Modules {
				for _, slot := range module.Slots {
					if slot.Occupied || slot.Hashboard != nil {
						return false
					}
				}
			}
			continue
		}
		expected := tank.ExpectedModules
		if expected == 0 {
			expected = sdk.CT1ModulesPerTank
		}
		if tank.ID < 0 || tank.ID > sdk.CT1TankCount || tanks[tank.ID] || expected < 0 {
			return false
		}
		tanks[tank.ID] = true
		modules := map[int32]bool{}
		for _, module := range tank.Modules {
			if module.ID < 1 || module.ID > expected || modules[module.ID] || len(module.Slots) < boardsPerModule {
				return false
			}
			modules[module.ID] = true
			// The target's own unnamed boards are feed_evidence_incomplete.
			isTarget := tank.ID == target.Tank && module.ID == target.Module
			for _, slot := range module.Slots {
				if slot.unreported || (!isTarget && slot.Occupied && len(slotFeedIDs(slot.Feed)) == 0) {
					return false
				}
			}
		}
		if len(modules) != int(expected) {
			return false
		}
	}
	return len(tanks) == sdk.CT1TankCount
}

func finiteReading(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return new(*v)
}

// feedReading returns the snapshot of exactly one feed or why it is missing.
func feedReading(resp *mdk.ListPowerFeedsResponse, id string) (*sdk.PowerFeedReading, string) {
	if resp.SnapshotReady != nil && !resp.GetSnapshotReady() {
		return nil, sdk.PowerFeedReadingSnapshotNotReady
	}
	var match *mdk.PowerFeed
	for _, feed := range resp.GetPowerFeeds() {
		if strings.TrimSpace(feed.GetPowerFeedId()) != id {
			continue
		}
		if match != nil {
			return nil, sdk.PowerFeedReadingDuplicate
		}
		match = feed
	}
	if match == nil {
		return nil, sdk.PowerFeedReadingNotInSnapshot
	}
	return powerFeedReading(match, id), ""
}

// powerFeedReading converts one controller feed record, keeping absent and
// non-finite measurements unavailable.
func powerFeedReading(match *mdk.PowerFeed, id string) *sdk.PowerFeedReading {
	state := sdk.PowerFeedStateUnknown
	if match.State != nil {
		switch match.GetState() {
		case mdk.PowerFeedState_POWER_FEED_STATE_ENABLED:
			state = sdk.PowerFeedStateEnabled
		case mdk.PowerFeedState_POWER_FEED_STATE_DISABLED:
			state = sdk.PowerFeedStateDisabled
		case mdk.PowerFeedState_POWER_FEED_STATE_UNSPECIFIED:
		}
	}
	phase := ""
	switch match.GetPhase() {
	case mdk.Phase_PHASE_A:
		phase = "A"
	case mdk.Phase_PHASE_B:
		phase = "B"
	case mdk.Phase_PHASE_C:
		phase = "C"
	case mdk.Phase_PHASE_UNSPECIFIED:
	}
	return &sdk.PowerFeedReading{
		ID: id, State: state, Active: match.Active, Faulted: match.Faulted, Overridden: match.GetOverridden(), Phase: phase,
		VoltageV: finiteReading(match.VoltageV), CurrentA: finiteReading(match.CurrentA), PowerW: finiteReading(match.PowerW),
		VoltageABV: finiteReading(match.VoltageAbV), VoltageBCV: finiteReading(match.VoltageBcV), VoltageCAV: finiteReading(match.VoltageCaV),
		CurrentAA: finiteReading(match.CurrentAA), CurrentBA: finiteReading(match.CurrentBA), CurrentCA: finiteReading(match.CurrentCA),
	}
}

func recoveryCycle(c *mdk.PowerRecoveryCycle) *sdk.PowerRecoveryCycle {
	if c == nil || strings.TrimSpace(c.GetId()) == "" {
		return nil
	}
	out := &sdk.PowerRecoveryCycle{ID: c.GetId(), Scope: c.GetScope(), Phase: c.GetPhase(), OutletIDs: slices.Clone(c.GetOutletIds()), LastError: c.GetLastError()}
	if t := sourceTime(c.GetRestoreAfter()); !t.IsZero() {
		out.RestoreAfter = &t
	}
	if t := sourceTime(c.GetCreatedAt()); !t.IsZero() {
		out.CreatedAt = &t
	}
	return out
}
