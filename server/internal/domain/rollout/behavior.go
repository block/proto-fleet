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
	// MethodDelegated leaves dispatch to an external controller. The schema
	// and behavior carry it so the delegated-control slice can enable it
	// without another migration; until then channels cannot use it.
	MethodDelegated = "delegated"

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
	// Individual miners are stored by device identifier so their selectors
	// survive deletion and re-pairing with a new device row.
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

// targets flattens placement selectors into parallel (type, id) arrays.
// Individual miner identifiers are stored separately.
func (s *Scope) targets() (types []string, ids []int64) {
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
	// MinSampleCoveragePercent is the share of verified miners a sampled
	// metric must cover before its limit can pass; nil means 100.
	MinSampleCoveragePercent *float64
}

// coverage returns the sample coverage a metric limit requires, as a
// fraction.
func (t *Thresholds) coverage() float64 {
	if t.MinSampleCoveragePercent == nil {
		return 1
	}
	return *t.MinSampleCoveragePercent / 100
}

// hasSampledLimit reports whether any limit judged against a sampled
// aggregate (hashrate, efficiency, temperature) is set.
func (t *Thresholds) hasSampledLimit() bool {
	return t.MaxHashrateDropPercent != nil || t.MaxEfficiencyIncreasePercent != nil || t.MaxTempIncreaseC != nil
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
	// ControllerTimeoutSeconds pauses a delegated rollout left waiting for
	// its controller; 0 never times out.
	ControllerTimeoutSeconds int32
}

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
	case MethodDelegated:
		return fleeterror.NewInvalidArgumentError("delegated rollouts are not available yet")
	default:
		return fleeterror.NewInvalidArgumentErrorf("unknown rollout method %q", b.Method)
	}
	if b.Method != MethodDelegated {
		b.ControllerTimeoutSeconds = 0
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
	if t.MinSampleCoveragePercent != nil {
		if *t.MinSampleCoveragePercent <= 0 || *t.MinSampleCoveragePercent > 100 {
			return fleeterror.NewInvalidArgumentError("min sample coverage must be above 0 and at most 100 percent")
		}
		if !t.hasSampledLimit() {
			return fleeterror.NewInvalidArgumentError("min sample coverage applies only with a hashrate, efficiency or temperature limit")
		}
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
		ControllerTimeoutSeconds:  c.ControllerTimeoutSeconds,
		Thresholds: Thresholds{
			MaxHashrateDropPercent:       nullFloat(c.MaxHashrateDropPercent),
			MaxEfficiencyIncreasePercent: nullFloat(c.MaxEfficiencyIncreasePercent),
			MaxTempIncreaseC:             nullFloat(c.MaxTempIncreaseC),
			MaxNewErrors:                 nullInt(c.MaxNewErrors),
			MinSampleCoveragePercent:     nullFloat(c.MinSampleCoveragePercent),
		},
	}
}

// PairKey is a canonical (manufacturer, model) assignment key: printable
// ASCII with no surrounding spaces, compared ASCII-case-insensitively.
type PairKey struct {
	Manufacturer string
	Model        string
}

// normalizePairKey trims the key and checks it is printable ASCII.
func normalizePairKey(manufacturer, model string) (PairKey, error) {
	k := PairKey{Manufacturer: strings.TrimSpace(manufacturer), Model: strings.TrimSpace(model)}
	for name, v := range map[string]string{"manufacturer": k.Manufacturer, "model": k.Model} {
		if v == "" {
			return PairKey{}, fleeterror.NewInvalidArgumentErrorf("%s is required", name)
		}
		for i := range len(v) {
			if v[i] < '!' || v[i] > '~' {
				if v[i] == ' ' && i > 0 && i < len(v)-1 {
					continue
				}
				return PairKey{}, fleeterror.NewInvalidArgumentErrorf("%s must be printable ASCII without surrounding spaces", name)
			}
		}
	}
	return k, nil
}

// fold lowercases ASCII letters only, mirroring the SQL lower(x COLLATE "C")
// the queries use and the files service's equalFoldASCII.
func fold(v string) string {
	b := []byte(v)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// matchesObserved reports whether an observed device identity belongs to the
// pair: trimmed, then compared ASCII-case-insensitively.
func (k PairKey) matchesObserved(manufacturer, model string) bool {
	return fold(strings.TrimSpace(manufacturer)) == fold(k.Manufacturer) && fold(strings.TrimSpace(model)) == fold(k.Model)
}

// folded returns the comparison form of the key.
func (k PairKey) folded() PairKey {
	return PairKey{Manufacturer: fold(k.Manufacturer), Model: fold(k.Model)}
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
