package rollout

import (
	"database/sql"
	"slices"
	"strings"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

const (
	// MethodAllAtOnce updates every mismatched miner in a single batch.
	MethodAllAtOnce = "all_at_once"
	// MethodBatched updates fixed-size batches, optionally reviewed after
	// each one.
	MethodBatched = "batched"
	// MethodPilotThenContinue updates a pilot batch, gates, then the rest.
	MethodPilotThenContinue = "pilot_then_continue"

	// OrderLeastEfficientFirst works through miners worst efficiency first.
	OrderLeastEfficientFirst = "least_efficient_first"
	// OrderRandom shuffles miners once when the rollout starts.
	OrderRandom = "random"

	// TargetTypeSite and friends are the selector kinds of a channel scope.
	TargetTypeSite     = "site"
	TargetTypeBuilding = "building"
	TargetTypeRack     = "rack"
	TargetTypeGroup    = "group"
	TargetTypeMiner    = "miner"
)

// Scope describes which miners a channel applies to. Every dimension is a
// union: a miner is in scope when any selector matches its placement.
type Scope struct {
	SiteIDs     []int64
	BuildingIDs []int64
	RackIDs     []int64
	GroupIDs    []int64
	// Individual miners by device identifier (the client's currency);
	// stored by device id.
	DeviceIdentifiers []string
}

// IsEmpty reports whether the scope selects nothing.
func (s *Scope) IsEmpty() bool {
	return len(s.SiteIDs)+len(s.BuildingIDs)+len(s.RackIDs)+len(s.GroupIDs)+len(s.DeviceIdentifiers) == 0
}

func (s *Scope) normalize() {
	s.SiteIDs = uniquePositive(s.SiteIDs)
	s.BuildingIDs = uniquePositive(s.BuildingIDs)
	s.RackIDs = uniquePositive(s.RackIDs)
	s.GroupIDs = uniquePositive(s.GroupIDs)
	s.DeviceIdentifiers = uniqueNonEmpty(s.DeviceIdentifiers)
}

// targets flattens the scope into parallel (type, id) arrays for storage,
// given the device ids the identifiers resolved to.
func (s *Scope) targets(deviceIDs []int64) (types []string, ids []int64) {
	add := func(kind string, list []int64) {
		for _, id := range list {
			types = append(types, kind)
			ids = append(ids, id)
		}
	}
	add(TargetTypeSite, s.SiteIDs)
	add(TargetTypeBuilding, s.BuildingIDs)
	add(TargetTypeRack, s.RackIDs)
	add(TargetTypeGroup, s.GroupIDs)
	add(TargetTypeMiner, deviceIDs)
	return types, ids
}

func scopeFromTargets(rows []sqlc.ListReleaseChannelTargetsRow) Scope {
	var s Scope
	for _, t := range rows {
		switch t.TargetType {
		case TargetTypeSite:
			s.SiteIDs = append(s.SiteIDs, t.TargetID)
		case TargetTypeBuilding:
			s.BuildingIDs = append(s.BuildingIDs, t.TargetID)
		case TargetTypeRack:
			s.RackIDs = append(s.RackIDs, t.TargetID)
		case TargetTypeGroup:
			s.GroupIDs = append(s.GroupIDs, t.TargetID)
		case TargetTypeMiner:
			if t.DeviceIdentifier != "" {
				s.DeviceIdentifiers = append(s.DeviceIdentifiers, t.DeviceIdentifier)
			}
		}
	}
	return s
}

func uniqueNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

func uniquePositive(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Thresholds a reviewed batch must satisfy for auto-continue. A nil
// threshold is not checked.
type Thresholds struct {
	MaxHashrateDropPercent       *float64
	MaxEfficiencyIncreasePercent *float64
	MaxTempIncreaseC             *float64
	MaxNewErrors                 *int32
}

// Behavior is how updates started in a channel are paced. Stored on the
// channel and copied onto each rollout when it starts.
type Behavior struct {
	Method                    string
	Order                     string
	BatchSize                 int32
	PilotSize                 int32
	WaitBetweenBatchesSeconds int32
	ReviewAfterEachBatch      bool
	AutoContinue              bool
	StabilizationSeconds      int32
	Thresholds                Thresholds
	MaxConcurrentOffline      int32
}

// allAtOnce is the behavior of drift-correction and rollback rollouts: no
// operator is present to review a gate, so they never stage.
var allAtOnce = Behavior{Method: MethodAllAtOnce, Order: OrderLeastEfficientFirst}

// gatesAfterBatch reports whether a finished batch holds for review.
func (b *Behavior) gatesAfterBatch() bool {
	return b.Method == MethodPilotThenContinue || b.ReviewAfterEachBatch
}

func (b *Behavior) validate() error {
	if b.Method == "" {
		b.Method = MethodAllAtOnce
	}
	if b.Order == "" {
		b.Order = OrderLeastEfficientFirst
	}
	switch b.Order {
	case OrderLeastEfficientFirst, OrderRandom:
	default:
		return fleeterror.NewInvalidArgumentErrorf("unknown rollout order %q", b.Order)
	}
	switch b.Method {
	case MethodAllAtOnce:
		b.BatchSize, b.PilotSize, b.WaitBetweenBatchesSeconds = 0, 0, 0
		b.ReviewAfterEachBatch, b.AutoContinue, b.StabilizationSeconds = false, false, 0
		b.Thresholds = Thresholds{}
	case MethodBatched:
		if b.BatchSize < 1 {
			return fleeterror.NewInvalidArgumentError("batched updates need a batch size of at least 1")
		}
		b.PilotSize = 0
		if b.ReviewAfterEachBatch {
			b.WaitBetweenBatchesSeconds = 0
		}
	case MethodPilotThenContinue:
		if b.PilotSize < 1 {
			return fleeterror.NewInvalidArgumentError("pilot updates need a pilot batch size of at least 1")
		}
		b.BatchSize, b.WaitBetweenBatchesSeconds = 0, 0
		b.ReviewAfterEachBatch = true
	default:
		return fleeterror.NewInvalidArgumentErrorf("unknown rollout method %q", b.Method)
	}
	if !b.gatesAfterBatch() {
		b.AutoContinue, b.StabilizationSeconds = false, 0
		b.Thresholds = Thresholds{}
	}
	if b.WaitBetweenBatchesSeconds < 0 || b.StabilizationSeconds < 0 || b.MaxConcurrentOffline < 0 {
		return fleeterror.NewInvalidArgumentError("durations and limits must not be negative")
	}
	t := b.Thresholds
	if t.MaxHashrateDropPercent != nil && (*t.MaxHashrateDropPercent < 0 || *t.MaxHashrateDropPercent > 100) {
		return fleeterror.NewInvalidArgumentError("max hashrate drop must be between 0 and 100 percent")
	}
	if t.MaxEfficiencyIncreasePercent != nil && *t.MaxEfficiencyIncreasePercent < 0 {
		return fleeterror.NewInvalidArgumentError("max efficiency increase must not be negative")
	}
	if t.MaxTempIncreaseC != nil && *t.MaxTempIncreaseC < 0 {
		return fleeterror.NewInvalidArgumentError("max temperature increase must not be negative")
	}
	if t.MaxNewErrors != nil && *t.MaxNewErrors < 0 {
		return fleeterror.NewInvalidArgumentError("max new errors must not be negative")
	}
	return nil
}

func behaviorFromChannel(c sqlc.ReleaseChannel) Behavior {
	return Behavior{
		Method:                    c.Method,
		Order:                     c.OrderBy,
		BatchSize:                 c.BatchSize,
		PilotSize:                 c.PilotSize,
		WaitBetweenBatchesSeconds: c.WaitBetweenBatchesSeconds,
		ReviewAfterEachBatch:      c.ReviewAfterEachBatch,
		AutoContinue:              c.AutoContinue,
		StabilizationSeconds:      c.StabilizationSeconds,
		MaxConcurrentOffline:      c.MaxConcurrentOffline,
		Thresholds: Thresholds{
			MaxHashrateDropPercent:       nullFloat(c.MaxHashrateDropPercent),
			MaxEfficiencyIncreasePercent: nullFloat(c.MaxEfficiencyIncreasePercent),
			MaxTempIncreaseC:             nullFloat(c.MaxTempIncreaseC),
			MaxNewErrors:                 nullInt(c.MaxNewErrors),
		},
	}
}

func behaviorFromRollout(r sqlc.FirmwareRollout) Behavior {
	return Behavior{
		Method:                    r.Method,
		Order:                     r.OrderBy,
		BatchSize:                 r.BatchSize,
		PilotSize:                 r.PilotSize,
		WaitBetweenBatchesSeconds: r.WaitBetweenBatchesSeconds,
		ReviewAfterEachBatch:      r.ReviewAfterEachBatch,
		AutoContinue:              r.AutoContinue,
		StabilizationSeconds:      r.StabilizationSeconds,
		MaxConcurrentOffline:      r.MaxConcurrentOffline,
		Thresholds: Thresholds{
			MaxHashrateDropPercent:       nullFloat(r.MaxHashrateDropPercent),
			MaxEfficiencyIncreasePercent: nullFloat(r.MaxEfficiencyIncreasePercent),
			MaxTempIncreaseC:             nullFloat(r.MaxTempIncreaseC),
			MaxNewErrors:                 nullInt(r.MaxNewErrors),
		},
	}
}

func nullFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func nullInt(v sql.NullInt32) *int32 {
	if !v.Valid {
		return nil
	}
	i := v.Int32
	return &i
}

func toNullFloat(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

func toNullInt(v *int32) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *v, Valid: true}
}
