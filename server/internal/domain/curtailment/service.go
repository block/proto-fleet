package curtailment

import (
	"context"
	"encoding/json"
	"math"
	"strings"

	"github.com/google/uuid"

	"github.com/block/proto-fleet/server/internal/domain/curtailment/models"
	"github.com/block/proto-fleet/server/internal/domain/curtailment/modes"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Scope identifies the target set: whole-org or explicit device-list;
// device-sets are deferred (resolver lives outside the curtailment domain).
type Scope struct {
	Type              models.ScopeType
	DeviceSetIDs      []string
	DeviceIdentifiers []string
}

// PreviewRequest is the service-level shape of a Preview call.
type PreviewRequest struct {
	OrgID                      int64
	Scope                      Scope
	Mode                       models.Mode     // must be ModeFixedKw
	Strategy                   models.Strategy // default StrategyLeastEfficientFirst
	Level                      models.Level    // must be LevelFull
	Priority                   models.Priority // PriorityNormal or PriorityEmergency (cooldown bypass)
	TargetKW                   float64
	ToleranceKW                float64
	IncludeMaintenance         bool
	ForceIncludeMaintenance    bool
	CandidateMinPowerWOverride *int32 // nil = use org default; admin-gated by handler
}

// StartRequest is the service-level shape of a Start call. The selector inputs
// are a superset of PreviewRequest; the additional fields configure the event
// row that Preview never persists.
type StartRequest struct {
	PreviewRequest

	// Reason is the operator-supplied audit string. Required (DB CHECK).
	Reason string

	// Operational controls. Zero values are passed through verbatim — the
	// handler is responsible for normalizing 0 to the per-org default before
	// the request reaches the service.
	RestoreBatchSize        int32
	RestoreBatchIntervalSec int32
	MinCurtailedDurationSec int32

	// MaxDurationSeconds is nil when AllowUnbounded=true; otherwise a finite
	// cap. The handler resolves the org default for non-admin callers before
	// reaching the service.
	MaxDurationSeconds *int32
	AllowUnbounded     bool

	// Idempotency / external attribution. Empty-string maps to NULL at the
	// store boundary so the partial-unique indexes only enforce uniqueness
	// for set keys.
	IdempotencyKey    *string
	ExternalSource    *string
	ExternalReference *string

	// SourceActorType is the audit-attribution dimension. Derived by the
	// handler from session.Info (auth method + Actor); the service avoids a
	// session dependency by accepting the typed value directly.
	SourceActorType models.SourceActorType
	SourceActorID   *string
}

// Service orchestrates Preview / Start through the shared config / scope /
// candidate / selector pipeline.
type Service struct {
	store interfaces.CurtailmentStore
}

func NewService(store interfaces.CurtailmentStore) *Service {
	return &Service{store: store}
}

// Preview computes a curtailment plan without persisting any rows. Returns
// fleeterror typed errors the handler maps to Connect codes.
func (s *Service) Preview(ctx context.Context, req PreviewRequest) (*Plan, error) {
	if err := validatePreviewRequest(req); err != nil {
		return nil, err
	}
	plan, _, _, err := s.runSelector(ctx, req)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// Start runs the same selector pipeline as Preview and persists the resulting
// event + targets when the plan is non-empty. The returned Plan carries the
// new event UUID; on OutcomeInsufficientLoad nothing is written and the Plan
// carries the rejection detail (mirrors Preview's contract).
func (s *Service) Start(ctx context.Context, req StartRequest) (*Plan, error) {
	if err := validateStartRequest(req); err != nil {
		return nil, err
	}

	plan, minPowerW, orgConfig, err := s.runSelector(ctx, req.PreviewRequest)
	if err != nil {
		return nil, err
	}

	// Insufficient-load: don't persist. Caller short-circuits on
	// plan.InsufficientLoadDetail and surfaces InvalidArgument, matching
	// Preview's contract.
	if plan.InsufficientLoadDetail != nil {
		return plan, nil
	}

	if len(plan.Selected) == 0 {
		// Defense-in-depth: a successful Outcome with zero Selected would
		// produce an empty curtailment_event row. The validator + selector
		// upstream already prevent this for FIXED_KW; reject explicitly so
		// a future mode regression surfaces the real cause.
		return nil, fleeterror.NewInvalidArgumentError("no targets selected")
	}

	// Normalize max_duration_seconds: nil + !AllowUnbounded means "use the
	// org's configured default" per the request contract.
	if !req.AllowUnbounded && req.MaxDurationSeconds == nil {
		v := orgConfig.MaxDurationDefaultSec
		req.MaxDurationSeconds = &v
	}

	// TODO: idempotency lookup. When req.IdempotencyKey is set we should
	// short-circuit to the existing event_uuid. Today the partial unique
	// index `uq_curtailment_event_idempotency` enforces uniqueness at the
	// DB level, so a retry surfaces as Internal until the lookup query is
	// added.

	eventParams, targetParams, err := buildInsertParams(req, plan, minPowerW)
	if err != nil {
		return nil, err
	}

	result, err := s.store.InsertEventWithTargets(ctx, eventParams, targetParams)
	if err != nil {
		return nil, err
	}

	plan.EventUUID = &result.EventUUID
	return plan, nil
}

// runSelector executes the org-config / scope / candidate / classify /
// build-plan pipeline shared by Preview and Start. The minPowerW return is
// the resolved candidate floor (after the admin-override) so callers that
// persist can echo it into the decision snapshot without re-resolving. The
// OrgConfig is returned so Service.Start can normalize "use org default"
// sentinels (e.g. max_duration_seconds=0) without a second DB read.
func (s *Service) runSelector(ctx context.Context, req PreviewRequest) (*Plan, int32, *models.OrgConfig, error) {
	deviceFilter, err := resolveScope(req.Scope)
	if err != nil {
		return nil, 0, nil, err
	}
	// Normalize empty-but-non-nil slice to nil; the candidate query's
	// `IS NULL` check would otherwise match-nothing on an empty array.
	if len(deviceFilter) == 0 {
		deviceFilter = nil
	}

	orgConfig, err := s.store.GetOrgConfig(ctx, req.OrgID)
	if err != nil {
		return nil, 0, nil, err
	}

	// Effective candidate floor: per-org default, optionally overridden by
	// the admin-gated request field. Handler enforces the admin role gate.
	minPowerW := orgConfig.CandidateMinPowerW
	if req.CandidateMinPowerWOverride != nil {
		minPowerW = *req.CandidateMinPowerWOverride
	}

	// Cooldown bypass: EMERGENCY priority skips post_event_cooldown_sec.
	bypassCooldown := req.Priority == models.PriorityEmergency

	activeDevices, err := s.store.ListActiveCurtailedDevices(ctx, req.OrgID)
	if err != nil {
		return nil, 0, nil, err
	}
	activeSet := toStringSet(activeDevices)

	cooldownSet := map[string]struct{}{}
	if !bypassCooldown {
		cd, err := s.store.ListRecentlyResolvedCurtailedDevices(ctx, req.OrgID, orgConfig.PostEventCooldownSec)
		if err != nil {
			return nil, 0, nil, err
		}
		cooldownSet = toStringSet(cd)
	}

	candidates, err := s.store.ListCandidates(ctx, req.OrgID, deviceFilter)
	if err != nil {
		return nil, 0, nil, err
	}

	// Org-ownership guard: cross-org ids are silently dropped by the SQL
	// org_id filter; surface them as NotFound so the caller sees the real
	// error instead of a misleading InsufficientLoad.
	if len(deviceFilter) > 0 {
		if missing := missingDeviceIdentifiers(deviceFilter, candidates); len(missing) > 0 {
			return nil, 0, nil, fleeterror.NewNotFoundErrorf(
				"device_identifiers not found in caller's org: %v", missing,
			)
		}
	}

	// TODO: registry-driven curtail_full capability check. classifyCandidates
	// already skips devices missing driver metadata as defense-in-depth.

	eligible, preFiltered, summary := classifyCandidates(candidates, classifyOpts{
		IncludeMaintenance: req.IncludeMaintenance && req.ForceIncludeMaintenance,
		ActiveEventDevices: activeSet,
		CooldownDevices:    cooldownSet,
		CandidateMinPowerW: minPowerW,
	})

	mode, err := modes.NewFixedKw(req.TargetKW, req.ToleranceKW, summary)
	if err != nil {
		return nil, 0, nil, fleeterror.NewInvalidArgumentErrorf("invalid FIXED_KW params: %v", err)
	}

	plan := BuildPlan(eligible, preFiltered, minPowerW, mode)
	return &plan, minPowerW, orgConfig, nil
}

// startTextFieldMaxLen mirrors proto/curtailment/v1/curtailment.proto's
// max_len bound on idempotency_key / reason / external_source /
// external_reference. Service-level enforcement protects non-Connect
// callers (internal CLIs, tests, future non-RPC entry points).
const startTextFieldMaxLen = 256

func validateStartRequest(req StartRequest) error {
	if err := validatePreviewRequest(req.PreviewRequest); err != nil {
		return err
	}
	if strings.TrimSpace(req.Reason) == "" {
		// reason is NOT NULL with a length(trim) > 0 CHECK at the DB level;
		// reject whitespace-only as well so the caller sees InvalidArgument
		// rather than a constraint violation surfaced as Internal.
		return fleeterror.NewInvalidArgumentError("reason must be non-empty")
	}
	if len(req.Reason) > startTextFieldMaxLen {
		return fleeterror.NewInvalidArgumentErrorf(
			"reason must be at most %d chars, got %d", startTextFieldMaxLen, len(req.Reason),
		)
	}
	if req.IdempotencyKey != nil && len(*req.IdempotencyKey) > startTextFieldMaxLen {
		return fleeterror.NewInvalidArgumentErrorf(
			"idempotency_key must be at most %d chars, got %d", startTextFieldMaxLen, len(*req.IdempotencyKey),
		)
	}
	if req.ExternalSource != nil && len(*req.ExternalSource) > startTextFieldMaxLen {
		return fleeterror.NewInvalidArgumentErrorf(
			"external_source must be at most %d chars, got %d", startTextFieldMaxLen, len(*req.ExternalSource),
		)
	}
	if req.ExternalReference != nil && len(*req.ExternalReference) > startTextFieldMaxLen {
		return fleeterror.NewInvalidArgumentErrorf(
			"external_reference must be at most %d chars, got %d", startTextFieldMaxLen, len(*req.ExternalReference),
		)
	}
	if req.RestoreBatchSize < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_size must be >= 0, got %d", req.RestoreBatchSize,
		)
	}
	if req.RestoreBatchIntervalSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_interval_sec must be >= 0, got %d", req.RestoreBatchIntervalSec,
		)
	}
	if req.MinCurtailedDurationSec < 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"min_curtailed_duration_sec must be >= 0, got %d", req.MinCurtailedDurationSec,
		)
	}
	// allow_unbounded and a finite max_duration_seconds are mutually
	// exclusive: the bounded duration is meaningless once the operator
	// has acknowledged opting out of the cap.
	if req.AllowUnbounded && req.MaxDurationSeconds != nil {
		return fleeterror.NewInvalidArgumentError(
			"max_duration_seconds must be unset when allow_unbounded is true",
		)
	}
	if !req.AllowUnbounded && req.MaxDurationSeconds != nil && *req.MaxDurationSeconds <= 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"max_duration_seconds must be > 0, got %d", *req.MaxDurationSeconds,
		)
	}
	// MaxDurationSeconds nil with !AllowUnbounded is the "use org default"
	// sentinel; Service.Start normalizes against curtailment_org_config
	// before persistence.
	if req.SourceActorType == "" {
		// source_actor_type is NOT NULL at the DB level; the handler must
		// derive a concrete value from session.Info before reaching here.
		return fleeterror.NewInvalidArgumentError("source_actor_type must be set")
	}
	return nil
}

func validatePreviewRequest(req PreviewRequest) error {
	if req.Mode != "" && req.Mode != models.ModeFixedKw {
		return fleeterror.NewInvalidArgumentErrorf("mode %q is not supported; only FIXED_KW", req.Mode)
	}
	if req.Level != "" && req.Level != models.LevelFull {
		return fleeterror.NewInvalidArgumentErrorf("level %q is not supported; only FULL", req.Level)
	}
	if req.Strategy != "" && req.Strategy != models.StrategyLeastEfficientFirst {
		return fleeterror.NewInvalidArgumentErrorf(
			"strategy %q is not supported; only LEAST_EFFICIENT_FIRST", req.Strategy,
		)
	}
	// HIGH is proto-reserved but undesigned; reject explicitly.
	if req.Priority != "" && req.Priority != models.PriorityNormal && req.Priority != models.PriorityEmergency {
		return fleeterror.NewInvalidArgumentErrorf(
			"priority %q is not supported; use NORMAL or EMERGENCY", req.Priority,
		)
	}
	// NaN / +/-Inf must be rejected explicitly because every comparison with
	// NaN evaluates false, which would slip past the > 0 / >= 0 guards
	// below and propagate through the running sum in FixedKw.
	if math.IsNaN(req.TargetKW) || math.IsInf(req.TargetKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be a finite number, got %v", req.TargetKW)
	}
	if math.IsNaN(req.ToleranceKW) || math.IsInf(req.ToleranceKW, 0) {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be a finite number, got %v", req.ToleranceKW)
	}
	if req.TargetKW <= 0 {
		return fleeterror.NewInvalidArgumentErrorf("target_kw must be > 0, got %v", req.TargetKW)
	}
	if req.ToleranceKW < 0 {
		return fleeterror.NewInvalidArgumentErrorf("tolerance_kw must be >= 0, got %v", req.ToleranceKW)
	}
	// tolerance_kw >= target_kw makes the undershoot branch trivially pass
	// even when the candidate sum is zero, producing an empty plan that
	// looks like a successful preview. Reject so the caller sees the real
	// reason (insufficient load) rather than a no-op selection.
	if req.ToleranceKW >= req.TargetKW {
		return fleeterror.NewInvalidArgumentErrorf(
			"tolerance_kw must be < target_kw, got tolerance=%v target=%v",
			req.ToleranceKW, req.TargetKW,
		)
	}
	// candidate_min_power_w_override bounds [1, 10_000_000] are documented
	// at the proto layer; this is the service-level backstop for callers
	// that bypass proto validation (internal CLIs, tests, future non-Connect
	// surfaces). Below 1 disables the dual-signal floor; above 10M is so far
	// past any real miner's nameplate it indicates a typo or unit error.
	if req.CandidateMinPowerWOverride != nil &&
		(*req.CandidateMinPowerWOverride < 1 || *req.CandidateMinPowerWOverride > 10_000_000) {
		return fleeterror.NewInvalidArgumentErrorf(
			"candidate_min_power_w_override must be in [1, 10_000_000], got %d",
			*req.CandidateMinPowerWOverride,
		)
	}
	// Maintenance override pair is both-or-neither at the API boundary;
	// the DB CHECK constraint is the defense-in-depth backstop at Start time.
	if req.IncludeMaintenance != req.ForceIncludeMaintenance {
		return fleeterror.NewInvalidArgumentError(
			"include_maintenance and force_include_maintenance must be set together",
		)
	}
	return nil
}

func resolveScope(s Scope) ([]string, error) {
	switch s.Type {
	case models.ScopeTypeWholeOrg, "":
		// Empty Type is admitted as whole-org for backward compatibility
		// with callers that omit the field. But device-id slices implicitly
		// signal a different intent — admitting both silently widens the
		// plan. Reject so the caller surfaces the type/payload mismatch.
		if len(s.DeviceIdentifiers) > 0 || len(s.DeviceSetIDs) > 0 {
			return nil, fleeterror.NewInvalidArgumentError(
				"scope type must be set when device_identifiers or device_set_ids are provided",
			)
		}
		return nil, nil
	case models.ScopeTypeDeviceList:
		if len(s.DeviceIdentifiers) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("device_identifiers must be non-empty for device-list scope")
		}
		// Mutual exclusion: a populated DeviceSetIDs alongside DeviceList
		// is silently ignored without this guard, breaking the oneof-style
		// scope contract for non-Connect callers.
		if len(s.DeviceSetIDs) > 0 {
			return nil, fleeterror.NewInvalidArgumentError(
				"device_set_ids must be empty when scope type is device_list",
			)
		}
		return s.DeviceIdentifiers, nil
	case models.ScopeTypeDeviceSets:
		// Deferred: device-set resolution requires DeviceSetStore wiring
		// outside the curtailment domain. Whole-org and device-list cover
		// the critical paths. Symmetric mutual-exclusion guard for callers
		// who set this Type with DeviceIdentifiers populated.
		if len(s.DeviceIdentifiers) > 0 {
			return nil, fleeterror.NewInvalidArgumentError(
				"device_identifiers must be empty when scope type is device_sets",
			)
		}
		return nil, fleeterror.NewUnimplementedErrorf("device-set scope is not implemented; use whole_org or device_list")
	default:
		return nil, fleeterror.NewInvalidArgumentErrorf("unrecognized scope type: %q", s.Type)
	}
}

type classifyOpts struct {
	IncludeMaintenance bool
	ActiveEventDevices map[string]struct{}
	CooldownDevices    map[string]struct{}
	CandidateMinPowerW int32
}

// classifyCandidates partitions candidates into selector inputs and a
// pre-selector skipped list with reasons; summary counts are incremented in
// lockstep so the rejection branch can echo per-reason totals without a re-walk.
func classifyCandidates(cands []*models.Candidate, opts classifyOpts) ([]CandidateInput, []SkippedDevice, modes.InsufficientLoadDetail) {
	eligible := make([]CandidateInput, 0, len(cands))
	skipped := make([]SkippedDevice, 0, len(cands))
	summary := modes.InsufficientLoadDetail{
		CandidateMinPowerW: opts.CandidateMinPowerW,
	}

	for _, c := range cands {
		if _, locked := opts.ActiveEventDevices[c.DeviceIdentifier]; locked {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipActiveEvent})
			summary.ExcludedActiveEvent++
			continue
		}
		if c.PairingStatus != "PAIRED" {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipPairing})
			summary.ExcludedPairing++
			continue
		}
		// Partial capability gate: skip devices with no driver metadata so
		// the selector can't pick a Curtail target with no plugin to dispatch.
		// Full registry-driven curtail_full check is follow-up work.
		if c.DriverName == nil || *c.DriverName == "" {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCurtailFullUnsupported})
			summary.ExcludedCapabilityMiss++
			continue
		}
		switch c.DeviceStatus {
		case "":
			// COALESCE sentinel for a missing device_status row: treat as
			// stale, since we can't prove the device is curtail-safe.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		case "UPDATING":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUpdating})
			summary.ExcludedUpdating++
			continue
		case "REBOOT_REQUIRED":
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipRebootRequired})
			summary.ExcludedRebootRequired++
			continue
		case "OFFLINE":
			// Unreachable residual load: counted in the rejection summary
			// since it's fleet load the system can't address.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUnreachableResidualLoad})
			summary.ExcludedOffline++
			continue
		case "INACTIVE", "NEEDS_MINING_POOL":
			// Non-actionable per the project's nonActionableStatuses set
			// (sqlstores/device_query_fragments.go): the device isn't a
			// curtailment candidate even when telemetry is fresh.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipNonActionableStatus})
			summary.ExcludedNonActionable++
			continue
		case "MAINTENANCE":
			if !opts.IncludeMaintenance {
				skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipMaintenance})
				summary.ExcludedMaintenance++
				continue
			}
			// Admitted by override pair; fall through to freshness.
		}
		if c.LatestMetricsAt == nil {
			// Same SkipStaleTelemetry reason as the empty-device_status
			// sentinel above: both signal "no usable telemetry sample,"
			// just from different sources. Both funnel into ExcludedStale.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		}
		// Non-finite telemetry samples (NaN / +Inf / -Inf) would slip
		// past the downstream dual-signal filter — NaN comparisons
		// always return false, so a miner with NaN power and a positive
		// hash signal would be admitted. The mode then accumulates
		// totalW += PowerW; one NaN poisons the running sum (Insufficient
		// with NaN kW) and +Inf satisfies any target_kw on the first
		// iteration ("successful" plan with +Inf realized). Treat
		// non-finite samples as stale: bad sensor data, no usable signal.
		if !isFiniteFloat(c.LatestPowerW) || !isFiniteFloat(c.LatestHashRateHS) {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		}
		if _, cooled := opts.CooldownDevices[c.DeviceIdentifier]; cooled {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCooldown})
			summary.ExcludedCooldown++
			continue
		}
		// Non-finite avg_efficiency would violate sort.SliceStable's
		// transitivity contract in BuildPlan (NaN comparisons return
		// false). Treat as unknown — existing nil-handling ranks last.
		avgEff := c.AvgEfficiencyJH
		if !isFiniteFloat(avgEff) {
			avgEff = nil
		}
		eligible = append(eligible, CandidateInput{
			DeviceIdentifier: c.DeviceIdentifier,
			PowerW:           derefFloat(c.LatestPowerW),
			HashRateHS:       derefFloat(c.LatestHashRateHS),
			AvgEfficiencyJH:  avgEff,
		})
	}
	return eligible, skipped, summary
}

// missingDeviceIdentifiers returns identifiers from `requested` that the org-
// scoped candidate listing did not surface. An empty result means every
// requested device belongs to the caller's org (or has been soft-deleted —
// soft-deleted devices are out of scope by design).
func missingDeviceIdentifiers(requested []string, candidates []*models.Candidate) []string {
	if len(requested) == 0 {
		return nil
	}
	have := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		have[c.DeviceIdentifier] = struct{}{}
	}
	var missing []string
	for _, id := range requested {
		if _, ok := have[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

func toStringSet(s []string) map[string]struct{} {
	set := make(map[string]struct{}, len(s))
	for _, v := range s {
		set[v] = struct{}{}
	}
	return set
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// isFiniteFloat reports whether p is nil or points to a finite IEEE-754
// value. Non-finite samples (NaN / +Inf / -Inf) are treated as missing,
// not zero, so callers can route them through the stale-telemetry skip
// path rather than letting them poison downstream arithmetic.
func isFiniteFloat(p *float64) bool {
	if p == nil {
		return true
	}
	return !math.IsNaN(*p) && !math.IsInf(*p, 0)
}

// targetTypeMiner is the v1 curtailment_target.target_type value.
const targetTypeMiner = "miner"

// desiredStateCurtailed is the v1 curtailment_target.desired_state at Start.
const desiredStateCurtailed = "curtailed"

// buildInsertParams assembles the event + per-target params from a successful
// selector plan. baseline_power_w is captured per device from the same
// telemetry sample the selector ranked against; non-positive PowerW maps to
// NULL (the dual-signal floor admits sub-watt loads, but a zero/negative
// baseline would produce a misleading "100% reduction" report at restore).
func buildInsertParams(req StartRequest, plan *Plan, minPowerW int32) (models.InsertEventParams, []models.InsertTargetParams, error) {
	scopeJSON, err := marshalScopeJSON(req.Scope)
	if err != nil {
		return models.InsertEventParams{}, nil, err
	}
	modeParamsJSON, err := json.Marshal(map[string]float64{
		"target_kw":    req.TargetKW,
		"tolerance_kw": req.ToleranceKW,
	})
	if err != nil {
		return models.InsertEventParams{}, nil, fleeterror.NewInternalErrorf(
			"failed to encode mode_params: %v", err,
		)
	}
	decisionJSON, err := marshalDecisionSnapshot(plan, minPowerW)
	if err != nil {
		return models.InsertEventParams{}, nil, err
	}

	event := models.InsertEventParams{
		EventUUID:               uuid.New(),
		OrgID:                   req.OrgID,
		State:                   models.EventStatePending,
		Mode:                    models.ModeFixedKw,
		Strategy:                models.StrategyLeastEfficientFirst,
		Level:                   models.LevelFull,
		Priority:                req.Priority,
		LoopType:                models.LoopTypeOpen,
		ScopeType:               req.Scope.Type,
		ScopeJSON:               scopeJSON,
		ModeParamsJSON:          modeParamsJSON,
		RestoreBatchSize:        req.RestoreBatchSize,
		RestoreBatchIntervalSec: req.RestoreBatchIntervalSec,
		MinCurtailedDurationSec: req.MinCurtailedDurationSec,
		MaxDurationSeconds:      req.MaxDurationSeconds,
		AllowUnbounded:          req.AllowUnbounded,
		IncludeMaintenance:      req.IncludeMaintenance,
		ForceIncludeMaintenance: req.ForceIncludeMaintenance,
		DecisionSnapshotJSON:    decisionJSON,
		SourceActorType:         req.SourceActorType,
		SourceActorID:           req.SourceActorID,
		ExternalSource:          req.ExternalSource,
		ExternalReference:       req.ExternalReference,
		IdempotencyKey:          req.IdempotencyKey,
		Reason:                  req.Reason,
	}
	if event.Priority == "" {
		// PriorityUnspecified at the proto layer is admitted as Normal in
		// validation; persist the resolved value rather than the empty
		// passthrough so audit reflects the intent.
		event.Priority = models.PriorityNormal
	}
	if event.ScopeType == "" {
		event.ScopeType = models.ScopeTypeWholeOrg
	}

	targets := make([]models.InsertTargetParams, len(plan.Selected))
	for i, sel := range plan.Selected {
		var baseline *float64
		if sel.PowerW > 0 {
			v := sel.PowerW
			baseline = &v
		}
		targets[i] = models.InsertTargetParams{
			DeviceIdentifier: sel.DeviceIdentifier,
			TargetType:       targetTypeMiner,
			State:            models.TargetStatePending,
			DesiredState:     desiredStateCurtailed,
			BaselinePowerW:   baseline,
		}
	}
	return event, targets, nil
}

// marshalScopeJSON renders the request scope into the JSONB column shape.
// Whole-org events store an empty object so the column stays NOT NULL.
func marshalScopeJSON(s Scope) ([]byte, error) {
	switch s.Type {
	case models.ScopeTypeWholeOrg, "":
		return []byte("{}"), nil
	case models.ScopeTypeDeviceList:
		b, err := json.Marshal(map[string][]string{
			"device_identifiers": s.DeviceIdentifiers,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to encode scope: %v", err)
		}
		return b, nil
	case models.ScopeTypeDeviceSets:
		b, err := json.Marshal(map[string][]string{
			"device_set_ids": s.DeviceSetIDs,
		})
		if err != nil {
			return nil, fleeterror.NewInternalErrorf("failed to encode scope: %v", err)
		}
		return b, nil
	default:
		return nil, fleeterror.NewInternalErrorf("unrecognized scope type: %q", s.Type)
	}
}

// marshalDecisionSnapshot captures the selector outputs the audit / UI need
// to reconstruct the Start decision after the fact (rejected counters,
// realized vs. requested kW, the resolved candidate floor).
func marshalDecisionSnapshot(plan *Plan, minPowerW int32) ([]byte, error) {
	skipped := make([]map[string]string, len(plan.Skipped))
	for i, s := range plan.Skipped {
		skipped[i] = map[string]string{
			"device_identifier": s.DeviceIdentifier,
			"reason":            string(s.Reason),
		}
	}
	snapshot := map[string]any{
		"candidate_min_power_w":        minPowerW,
		"estimated_reduction_kw":       plan.EstimatedReductionKW,
		"estimated_remaining_power_kw": plan.EstimatedRemainingPowerKW,
		"selected_count":               len(plan.Selected),
		"skipped":                      skipped,
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fleeterror.NewInternalErrorf(
			"failed to encode decision_snapshot: %v", err,
		)
	}
	return b, nil
}
