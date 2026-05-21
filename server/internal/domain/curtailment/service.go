package curtailment

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

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

// StartRequest is the service-level shape of a Start call. Adds event-row
// fields (audit + operational controls) on top of PreviewRequest's
// selector inputs.
type StartRequest struct {
	PreviewRequest

	// Reason: operator-supplied audit string. Required (DB CHECK).
	Reason string

	// Zero values pass through verbatim; handler normalizes to org defaults.
	RestoreBatchSize        int32
	RestoreBatchIntervalSec int32
	MinCurtailedDurationSec int32

	// MaxDurationSeconds: nil when AllowUnbounded=true, else a finite cap.
	MaxDurationSeconds  *int32
	AllowUnbounded      bool
	CanUseAdminControls bool

	// External attribution. Empty-string normalizes to NULL at the store
	// boundary so partial-unique indexes only enforce uniqueness for set keys.
	IdempotencyKey    *string
	ExternalSource    *string
	ExternalReference *string

	// SourceActorType / SourceActorID: audit attribution. Handler derives
	// from session.Info; service stays session-free.
	SourceActorType models.SourceActorType
	SourceActorID   *string

	// CreatedByUserID: operator's user.id captured at handler entry.
	// Persisted on the event so reconciler dispatches under a real user
	// (command_batch_log.created_by has a NOT NULL FK to user.id).
	CreatedByUserID int64
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

// Start runs Preview's selector pipeline and persists the event + targets.
// On OutcomeInsufficientLoad nothing is written; the Plan carries the
// rejection detail (mirrors Preview).
func (s *Service) Start(ctx context.Context, req StartRequest) (*Plan, error) {
	if err := validateStartRequest(req); err != nil {
		return nil, err
	}

	plan, minPowerW, orgConfig, err := s.runSelector(ctx, req.PreviewRequest)
	if err != nil {
		return nil, err
	}

	// Insufficient-load: don't persist; caller surfaces InvalidArgument
	// from plan.InsufficientLoadDetail (matches Preview).
	if plan.InsufficientLoadDetail != nil {
		return plan, nil
	}

	if len(plan.Selected) == 0 {
		// Defense-in-depth against a future mode regression. FIXED_KW's
		// validator + selector already prevent this.
		return nil, fleeterror.NewInvalidArgumentError("no targets selected")
	}

	// max_duration_seconds=nil + !AllowUnbounded means "use org default".
	// Reject an out-of-range org default (data-quality issue) — the same
	// upper bound validateStartRequest enforces against caller-supplied
	// values, applied here to the normalized-from-org-default path so a
	// misconfigured org default doesn't tunnel past validation into the DB
	// CHECK constraint ck_curtailment_event_max_duration_bounds.
	if !req.AllowUnbounded && req.MaxDurationSeconds == nil {
		if orgConfig.MaxDurationDefaultSec <= 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"org's max_duration_default_sec must be > 0, got %d", orgConfig.MaxDurationDefaultSec,
			)
		}
		if orgConfig.MaxDurationDefaultSec > maxFiniteDurationSeconds {
			return nil, fleeterror.NewInvalidArgumentErrorf(
				"org's max_duration_default_sec must be <= %d, got %d",
				maxFiniteDurationSeconds, orgConfig.MaxDurationDefaultSec,
			)
		}
		v := orgConfig.MaxDurationDefaultSec
		req.MaxDurationSeconds = &v
	}
	// Admin-gate is intrinsically post-normalization: it compares the
	// resolved value to the org default.
	if req.MaxDurationSeconds != nil &&
		orgConfig.MaxDurationDefaultSec > 0 &&
		*req.MaxDurationSeconds > orgConfig.MaxDurationDefaultSec &&
		!req.CanUseAdminControls {
		return nil, fleeterror.NewForbiddenErrorf(
			"only admins can set max_duration_seconds above org default %d",
			orgConfig.MaxDurationDefaultSec,
		)
	}
	if req.RestoreBatchIntervalSec == 0 {
		req.RestoreBatchIntervalSec = defaultRestoreBatchIntervalSec
	}
	if req.RestoreBatchIntervalSec > restoreBatchIntervalUpperBoundSec {
		return nil, fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_interval_sec must be <= %d, got %d",
			restoreBatchIntervalUpperBoundSec, req.RestoreBatchIntervalSec,
		)
	}
	if req.RestoreBatchIntervalSec > nonAdminRestoreBatchIntervalMax && !req.CanUseAdminControls {
		return nil, fleeterror.NewForbiddenErrorf(
			"only admins can set restore_batch_interval_sec above %d",
			nonAdminRestoreBatchIntervalMax,
		)
	}

	// Stamp the adaptive batch size on the plan so buildInsertParams and the
	// Start response both read the same value (avoid recomputation drift).
	// Selected-target count is bounded by per-org fleet size — well under
	// MaxInt32 at any realistic scale.
	plan.EffectiveBatchSize = ComputeEffectiveBatchSize(req.RestoreBatchSize, int32(len(plan.Selected))) //nolint:gosec // bounded by per-org fleet size

	eventParams, targetParams, err := buildInsertParams(req, plan, minPowerW)
	if err != nil {
		return nil, err
	}

	result, err := s.store.InsertEventWithTargets(ctx, eventParams, targetParams)
	if err != nil {
		if errors.Is(err, interfaces.ErrCurtailmentNonTerminalEventExists) {
			// Race: another Start beat us between the selector check and
			// the insert. Surface the existing event's identity so the
			// caller can act on it.
			existing, getErr := s.store.GetActiveEvent(ctx, req.OrgID)
			if getErr != nil || existing == nil {
				return nil, fleeterror.NewAlreadyExistsError(
					"a non-terminal curtailment event already exists for this organization",
				)
			}
			return nil, fleeterror.NewAlreadyExistsErrorf(
				"a non-terminal curtailment event already exists for this organization (event_uuid=%s, state=%q)",
				existing.EventUUID, existing.State,
			)
		}
		return nil, err
	}

	plan.EventUUID = &result.EventUUID
	plan.EffectiveMaxDurationSeconds = req.MaxDurationSeconds
	plan.EffectiveRestoreBatchIntervalSec = req.RestoreBatchIntervalSec
	return plan, nil
}

func (s *Service) GetActive(ctx context.Context, orgID int64) (*models.Event, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	return s.store.GetActiveEvent(ctx, orgID)
}

func (s *Service) GetActiveWithTargets(ctx context.Context, orgID int64) (*models.Event, []*models.Target, error) {
	event, err := s.GetActive(ctx, orgID)
	if err != nil || event == nil {
		return event, nil, err
	}
	targets, err := s.store.ListTargetsByEvent(ctx, orgID, event.EventUUID)
	if err != nil {
		return nil, nil, err
	}
	return event, targets, nil
}

func (s *Service) ListTargetsByEvent(ctx context.Context, orgID int64, eventUUID uuid.UUID) ([]*models.Target, error) {
	if orgID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if eventUUID == uuid.Nil {
		return nil, fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	return s.store.ListTargetsByEvent(ctx, orgID, eventUUID)
}

// runSelector executes the org-config + scope + candidate + classify +
// build-plan pipeline shared by Preview and Start. Returns the resolved
// candidate floor (so persisters can echo it into the decision snapshot)
// and the OrgConfig (so Start can resolve max_duration_seconds=0 without a
// second DB read).
func (s *Service) runSelector(ctx context.Context, req PreviewRequest) (*Plan, int32, *models.OrgConfig, error) {
	deviceFilter, err := resolveScope(req.Scope)
	if err != nil {
		return nil, 0, nil, err
	}
	// Empty-but-non-nil would match nothing under the query's `IS NULL` check.
	if len(deviceFilter) == 0 {
		deviceFilter = nil
	}

	orgConfig, err := s.store.GetOrgConfig(ctx, req.OrgID)
	if err != nil {
		return nil, 0, nil, err
	}

	// Effective candidate floor: per-org default, admin-overridable.
	// Handler enforces the admin role gate.
	minPowerW := orgConfig.CandidateMinPowerW
	if req.CandidateMinPowerWOverride != nil {
		minPowerW = *req.CandidateMinPowerWOverride
	}

	// EMERGENCY skips post_event_cooldown_sec.
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

	// Cross-org ids are silently dropped by the SQL org_id filter; surface
	// them as NotFound rather than masquerading as InsufficientLoad.
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

const (
	// startTextFieldMaxLen mirrors the proto max_len for idempotency_key /
	// reason / external_source / external_reference. Service-level backstop
	// for non-Connect callers (CLIs, tests, future non-RPC entry points).
	startTextFieldMaxLen = 256

	maxFiniteDurationSeconds          int32 = 7 * 24 * 60 * 60
	defaultRestoreBatchIntervalSec    int32 = 30
	nonAdminRestoreBatchIntervalMax   int32 = 5 * 60
	restoreBatchIntervalUpperBoundSec int32 = 60 * 60
)

func validateStartRequest(req StartRequest) error {
	if err := validatePreviewRequest(req.PreviewRequest); err != nil {
		return err
	}
	if strings.TrimSpace(req.Reason) == "" {
		// DB CHECK enforces length(trim) > 0; reject here so callers see
		// InvalidArgument instead of Internal from the constraint.
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
	// allow_unbounded + finite max_duration are mutually exclusive.
	if req.AllowUnbounded && req.MaxDurationSeconds != nil {
		return fleeterror.NewInvalidArgumentError(
			"max_duration_seconds must be unset when allow_unbounded is true",
		)
	}
	if req.AllowUnbounded && !req.CanUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set allow_unbounded")
	}
	if req.CandidateMinPowerWOverride != nil && !req.CanUseAdminControls {
		return fleeterror.NewForbiddenError("only admins can set candidate_min_power_w_override")
	}
	if !req.AllowUnbounded && req.MaxDurationSeconds != nil && *req.MaxDurationSeconds <= 0 {
		return fleeterror.NewInvalidArgumentErrorf(
			"max_duration_seconds must be > 0, got %d", *req.MaxDurationSeconds,
		)
	}
	// MaxDurationSeconds=nil + !AllowUnbounded is the "use org default"
	// sentinel; Service.Start resolves it before persistence.
	if req.MaxDurationSeconds != nil && *req.MaxDurationSeconds > maxFiniteDurationSeconds {
		return fleeterror.NewInvalidArgumentErrorf(
			"max_duration_seconds must be <= %d, got %d",
			maxFiniteDurationSeconds, *req.MaxDurationSeconds,
		)
	}
	if req.RestoreBatchIntervalSec > restoreBatchIntervalUpperBoundSec {
		return fleeterror.NewInvalidArgumentErrorf(
			"restore_batch_interval_sec must be <= %d, got %d",
			restoreBatchIntervalUpperBoundSec, req.RestoreBatchIntervalSec,
		)
	}
	if req.SourceActorType == "" {
		// NOT NULL at the DB; handler derives from session.Info.
		return fleeterror.NewInvalidArgumentError("source_actor_type must be set")
	}
	if req.CreatedByUserID <= 0 {
		// NOT NULL FK to user.id; handler derives from session.Info.UserID.
		return fleeterror.NewInvalidArgumentError("created_by_user_id must be set")
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
	// HIGH is proto-reserved but unimplemented; reject explicitly.
	if req.Priority != "" && req.Priority != models.PriorityNormal && req.Priority != models.PriorityEmergency {
		return fleeterror.NewInvalidArgumentErrorf(
			"priority %q is not supported; use NORMAL or EMERGENCY", req.Priority,
		)
	}
	// NaN/+/-Inf comparisons evaluate false, slipping past the > 0/>= 0
	// guards below and poisoning FixedKw's running sum.
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
	// at zero candidate sum, producing a misleading empty "successful" plan.
	if req.ToleranceKW >= req.TargetKW {
		return fleeterror.NewInvalidArgumentErrorf(
			"tolerance_kw must be < target_kw, got tolerance=%v target=%v",
			req.ToleranceKW, req.TargetKW,
		)
	}
	// candidate_min_power_w_override [1, 10_000_000] bounds documented at
	// proto. Below 1 disables the dual-signal floor; above 10M is a unit
	// error. Service-level backstop for non-Connect callers.
	if req.CandidateMinPowerWOverride != nil &&
		(*req.CandidateMinPowerWOverride < 1 || *req.CandidateMinPowerWOverride > 10_000_000) {
		return fleeterror.NewInvalidArgumentErrorf(
			"candidate_min_power_w_override must be in [1, 10_000_000], got %d",
			*req.CandidateMinPowerWOverride,
		)
	}
	// Maintenance override pair is both-or-neither (DB CHECK is the backstop).
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
		// Empty Type defaults to whole-org, but device IDs alongside it
		// signal mismatched intent — reject rather than silently widening.
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
		// Mutual exclusion: enforce the oneof-style scope contract for
		// non-Connect callers; otherwise DeviceSetIDs would be silently dropped.
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

// classifyCandidates partitions candidates into selector inputs vs. a
// pre-filter skipped list with reasons; summary counts increment in lockstep
// so insufficient-load can echo per-reason totals without re-walking.
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
		// Partial capability gate: a device with no driver can't be
		// dispatched. Registry-driven curtail_full check is follow-up work.
		if c.DriverName == nil || *c.DriverName == "" {
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipCurtailFullUnsupported})
			summary.ExcludedCapabilityMiss++
			continue
		}
		switch c.DeviceStatus {
		case "":
			// Missing device_status (COALESCE sentinel): not provably curtail-safe.
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
			// Fleet load the system can't address; counted as residual.
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipUnreachableResidualLoad})
			summary.ExcludedOffline++
			continue
		case "INACTIVE", "NEEDS_MINING_POOL":
			// Non-actionable per nonActionableStatuses (sqlstores/device_query_fragments.go).
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipNonActionableStatus})
			summary.ExcludedNonActionable++
			continue
		case "MAINTENANCE":
			if !opts.IncludeMaintenance {
				skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipMaintenance})
				summary.ExcludedMaintenance++
				continue
			}
			// Admitted via override pair; fall through to freshness check.
		}
		if c.LatestMetricsAt == nil {
			// No usable telemetry sample (same bucket as empty device_status).
			skipped = append(skipped, SkippedDevice{c.DeviceIdentifier, SkipStaleTelemetry})
			summary.ExcludedStale++
			continue
		}
		// Non-finite telemetry would slip past the dual-signal filter:
		// NaN comparisons always false, +Inf satisfies any target_kw on
		// the first iteration. Treat as stale.
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
		// Non-finite avg_efficiency violates sort.SliceStable's transitivity
		// contract; treat as unknown so the nil-handling ranks it last.
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

// missingDeviceIdentifiers returns requested IDs the org-scoped listing
// did not surface (cross-org or soft-deleted; both are out of scope).
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

// isFiniteFloat: nil → true; otherwise checks the pointee. Non-finite
// samples are treated as missing so they route to the stale-telemetry
// skip path instead of poisoning downstream arithmetic.
func isFiniteFloat(p *float64) bool {
	if p == nil {
		return true
	}
	return !math.IsNaN(*p) && !math.IsInf(*p, 0)
}

// curtailment_target column values written at Start.
const targetTypeMiner = "miner"

// buildInsertParams assembles event + per-target params from a successful
// plan. baseline_power_w comes from the telemetry snapshot the selector
// ranked against; non-positive PowerW maps to NULL (a zero baseline would
// produce a misleading "100% reduction" report at restore).
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

	// Stamp effective_batch_size at Start so the column is non-null from
	// event creation and Stop/restorer/Start-response just read it. Service.Start
	// pre-computes plan.EffectiveBatchSize from the selected target count.
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
		CreatedByUserID:         req.CreatedByUserID,
		EffectiveBatchSize:      plan.EffectiveBatchSize,
	}
	if event.Priority == "" {
		// Validation admits PriorityUnspecified as Normal; persist the
		// resolved value so audit reflects intent.
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
			DesiredState:     models.DesiredStateCurtailed,
			BaselinePowerW:   baseline,
		}
	}
	return event, targets, nil
}

// marshalScopeJSON renders the request scope as the JSONB column value.
// Whole-org stores `{}` (NOT NULL).
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

// StopRequest is the service-level shape of a Stop call. The handler maps
// it from `StopCurtailmentRequest` after deriving OrgID from session.Info
// and gating `Force` on Admin role.
type StopRequest struct {
	OrgID     int64
	EventUUID uuid.UUID
	Force     bool // admin-gated upstream; bypasses min_curtailed_duration_sec
}

// Adaptive batch-sizing constants. [10, 100] is the inrush envelope, computed
// at Start time from the selected target count.
const (
	minBatchSizeFloor   int32 = 10
	maxBatchSizeCeiling int32 = 100
)

// Stop transitions a non-terminal event to `restoring` and flips every
// non-terminal target to (desired_state='active', state='pending'). The
// effective_batch_size was stamped at Start; this call does not touch it.
// Idempotent re-Stop returns the current event without writing. Terminal
// events return FailedPrecondition.
func (s *Service) Stop(ctx context.Context, req StopRequest) (*models.Event, error) {
	if err := validateStopRequest(req); err != nil {
		return nil, err
	}

	event, err := s.store.GetEventByUUID(ctx, req.OrgID, req.EventUUID)
	if err != nil {
		return nil, err
	}

	// Fast-path TOCTOU read for caller-facing latency. BeginRestoreTransition
	// is the authoritative atomic enforcement under the WHERE state-guard.
	if event.State.IsTerminal() {
		return nil, fleeterror.NewFailedPreconditionErrorf(
			"cannot stop curtailment event %s in terminal state %q",
			event.EventUUID, event.State,
		)
	}
	if event.State == models.EventStateRestoring {
		// Idempotent re-Stop: a second call after the first transitioned the
		// event returns the persisted row.
		return event, nil
	}

	if err := checkMinCurtailedDurationGate(event, req.Force, time.Now()); err != nil {
		return nil, err
	}

	return s.store.BeginRestoreTransition(ctx, req.OrgID, req.EventUUID)
}

func validateStopRequest(req StopRequest) error {
	if req.OrgID <= 0 {
		return fleeterror.NewInvalidArgumentError("org_id must be set")
	}
	if req.EventUUID == uuid.Nil {
		return fleeterror.NewInvalidArgumentError("event_uuid must be set")
	}
	return nil
}

// checkMinCurtailedDurationGate enforces `min_curtailed_duration_sec` on
// active events. Pending events haven't curtailed anything yet, so the
// hysteresis gate doesn't apply. Admin callers can pass force=true on Stop
// to bypass the gate explicitly.
func checkMinCurtailedDurationGate(event *models.Event, force bool, now time.Time) error {
	if force {
		return nil
	}
	if event.State != models.EventStateActive {
		return nil
	}
	if event.MinCurtailedDurationSec <= 0 || event.StartedAt == nil {
		return nil
	}
	elapsed := now.Sub(*event.StartedAt)
	required := time.Duration(event.MinCurtailedDurationSec) * time.Second
	if elapsed >= required {
		return nil
	}
	return fleeterror.NewFailedPreconditionErrorf(
		"min_curtailed_duration_sec not elapsed: %ds of %ds; an admin can supply force=true on Stop to bypass this gate",
		int64(elapsed.Seconds()), event.MinCurtailedDurationSec,
	)
}

// ComputeEffectiveBatchSize returns max(restore_batch_size, ceil(0.01 × non_terminal_count))
// clamped to [minBatchSizeFloor, maxBatchSizeCeiling]. Called at Start with
// the selected target count; the value is stamped on the event row and read
// back by the restorer.
func ComputeEffectiveBatchSize(restoreBatchSize, nonTerminalCount int32) int32 {
	base := restoreBatchSize
	if base < 0 {
		base = 0
	}
	if nonTerminalCount > 0 {
		onePercent := int32(math.Ceil(float64(nonTerminalCount) * 0.01))
		if onePercent > base {
			base = onePercent
		}
	}
	if base < minBatchSizeFloor {
		base = minBatchSizeFloor
	}
	if base > maxBatchSizeCeiling {
		base = maxBatchSizeCeiling
	}
	return base
}

// marshalDecisionSnapshot captures the selector outputs (rejection counters,
// realized vs. requested kW, resolved candidate floor) audit/UI need to
// reconstruct the decision.
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
