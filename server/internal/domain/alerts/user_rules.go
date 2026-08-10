package alerts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Must match infrastructure/metrics/contract.go, which cannot be imported
// here (it imports this package).
const (
	metricDeviceOnline                = "fleet_device_online"
	metricDeviceHashing               = "fleet_device_hashing"
	metricDeviceHashrateTerahash      = "fleet_device_hashrate_terahash"
	metricDeviceTemperatureMaxCelsius = "fleet_device_temperature_max_celsius"
)

const (
	ruleLabelOrigin = "proto_fleet_origin"
	ruleOriginUser  = "user"

	// Pre-config-store rules (before migration 000135) carried their config in this annotation; reads fall back to it when the store has no row.
	ruleAnnotationConfig = "proto_fleet_config"

	timescaleDatasourceUID   = "protofleet-timescaledb"
	userRuleGroupInterval    = int64(30)
	userRuleEvalWindowMinute = 10

	// Grafana caps alert-rule titles at 190 characters.
	maxRuleNameLength = 190

	// Each rule is a recurring SQL query against the metrics hypertable.
	maxUserRulesPerOrg = 50

	// Bounds the absolute hashrate threshold after PH→TH normalization.
	maxAbsoluteTerahash = 1e9

	// Floor keeps formatRatio's 10-digit rendering exact (0.01% → 0.0001).
	minHashratePercent = 0.01
)

// userRuleOrgSlug names the per-org folder and the rule_group label; each rule
// gets its own Grafana group (see compileUserRule) so group writes never race.
func userRuleOrgSlug(orgID int64) string {
	return "proto-fleet-user-" + strconv.FormatInt(orgID, 10)
}

func (s *Service) CreateRule(ctx context.Context, orgID int64, cfg RuleConfig, mode RouteMode, channelIDs []string) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateRuleConfig(cfg); err != nil {
		return nil, err
	}
	cfg.Scope = normalizeRuleScope(cfg.Scope)
	if err := s.validateRuleScope(ctx, orgID, cfg.Scope, nil); err != nil {
		return nil, err
	}
	// Validate routing before touching Grafana so a bad channel id can't leave an orphaned rule behind.
	policy, err := s.resolveRoutePolicy(ctx, orgID, mode, channelIDs)
	if err != nil {
		return nil, err
	}
	folderUID := userRuleOrgSlug(orgID)
	if err := s.grafana.EnsureFolder(ctx, folderUID, fmt.Sprintf("Proto Fleet User Rules (org %d)", orgID)); err != nil {
		return nil, err
	}
	uid, err := newUserRuleUID()
	if err != nil {
		return nil, err
	}
	body, err := compileUserRule(orgID, uid, cfg)
	if err != nil {
		return nil, err
	}
	// Policy and config before rule: rows for a never-created UID are inert, while the reverse
	// order would need a rule rollback whose failure mode is a live rule paging channels the
	// user explicitly routed away from (and that rollback couldn't hold userRuleMu).
	if policy != nil {
		policy.RuleUID = uid
		if err := s.routes.SetPolicy(ctx, orgID, *policy); err != nil {
			return nil, err
		}
		s.invalidateDeliveryPolicyCache(orgID)
	}
	if s.configs != nil {
		if err := s.configs.UpsertConfig(ctx, orgID, uid, cfg); err != nil {
			// The policy row above is already committed; without a rule it never
			// routes, but retries would accumulate orphans in every delivery
			// policy load. No rule exists for this fresh UID yet, so unlike the
			// post-create cleanup below the delete needs no Grafana probe.
			if policy != nil {
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				if derr := s.routes.DeletePolicy(cleanupCtx, orgID, uid); derr != nil {
					slog.Warn("alerts.user_rule_create_policy_cleanup", "org_id", orgID, "rule_id", uid, "error", derr)
				} else {
					s.invalidateDeliveryPolicyCache(orgID)
				}
			}
			return nil, err
		}
	}
	created, err := s.createRuleSerialized(ctx, orgID, body, folderUID)
	if err != nil {
		if policy != nil || s.configs != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			// Tidy only when the rule is provably absent: an ambiguous failure (timeout after Grafana committed) must keep the rows, or a live routed rule would fall back to paging every channel.
			if _, gerr := s.grafana.GetAlertRule(cleanupCtx, uid); IsNotFound(gerr) {
				if policy != nil {
					if derr := s.routes.DeletePolicy(cleanupCtx, orgID, uid); derr != nil {
						slog.Warn("alerts.user_rule_create_policy_cleanup", "org_id", orgID, "rule_id", uid, "error", derr)
					} else {
						s.invalidateDeliveryPolicyCache(orgID)
					}
				}
				if s.configs != nil {
					if derr := s.configs.DeleteConfig(cleanupCtx, orgID, uid); derr != nil {
						slog.Warn("alerts.user_rule_create_config_cleanup", "org_id", orgID, "rule_id", uid, "error", derr)
					}
				}
			}
		}
		return nil, err
	}
	out := grafanaRuleToDomain(orgID, *created)
	out.Config = &cfg
	out.Routing = policy
	return &out, nil
}

// createRuleSerialized holds userRuleMu across quota check, create, and the
// group pin: the pin PUT replays the rule body, so it must not interleave
// with another mutation of the same rule.
func (s *Service) createRuleSerialized(ctx context.Context, orgID int64, body GrafanaAlertRule, folderUID string) (*GrafanaAlertRule, error) {
	s.userRuleMu.Lock()
	defer s.userRuleMu.Unlock()
	existing, err := s.grafana.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatInt(orgID, 10)
	userCount := 0
	for _, gr := range existing {
		if ruleVisibleToOrg(gr, want) && gr.Labels[ruleLabelOrigin] == ruleOriginUser {
			userCount++
		}
	}
	if userCount >= maxUserRulesPerOrg {
		return nil, fleeterror.NewFailedPreconditionErrorf("rule limit reached (%d); delete a rule first", maxUserRulesPerOrg)
	}
	created, err := s.grafana.CreateAlertRule(ctx, body)
	if err != nil {
		return nil, err
	}
	// Pin the fresh per-rule group's evaluation interval. Best-effort: a
	// default-interval group still evaluates; `for` carries the sustain semantics.
	group := GrafanaRuleGroup{
		Title:     created.RuleGroup,
		FolderUID: folderUID,
		Interval:  userRuleGroupInterval,
		Rules:     []GrafanaAlertRule{*created},
	}
	if err := s.grafana.SetRuleGroup(ctx, group); err != nil {
		slog.Warn("alerts.user_rule_group_interval", "org_id", orgID, "error", err)
	}
	return created, nil
}

func (s *Service) UpdateRule(ctx context.Context, orgID int64, id string, cfg RuleConfig) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateRuleConfig(cfg); err != nil {
		return nil, err
	}
	cfg.Scope = normalizeRuleScope(cfg.Scope)
	updated, err := s.updateRuleSerialized(ctx, orgID, id, cfg)
	if err != nil {
		return nil, err
	}
	out := grafanaRuleToDomain(orgID, *updated)
	out.Config = &cfg
	// Fail closed rather than degrade: a response missing the stored routing reads as DEFAULT and invites the client to overwrite the real policy.
	outSlice := []Rule{out}
	if err := s.attachRouting(ctx, orgID, outSlice); err != nil {
		return nil, err
	}
	out = outSlice[0]
	// The write is committed; misreporting it as failed over a silence-read
	// hiccup invites confused retries, so degrade to the rule's own state.
	if paused, err := s.pauseSilencedRules(ctx, orgID); err != nil {
		slog.Warn("alerts.user_rule_update_pause_state", "org_id", orgID, "rule_id", id, "error", err)
	} else if paused[out.ID] {
		out.Enabled = false
	}
	return &out, nil
}

// updateRuleSerialized holds userRuleMu across fetch, scope validation, PUT,
// and group re-pin so a concurrent same-rule mutation can't be overwritten by
// this body replay (the validation lookups are quick indexed reads).
func (s *Service) updateRuleSerialized(ctx context.Context, orgID int64, id string, cfg RuleConfig) (*GrafanaAlertRule, error) {
	s.userRuleMu.Lock()
	defer s.userRuleMu.Unlock()
	current, err := s.requireUserRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	// The stored config backs both the update-time scope tolerance below and the
	// PUT-failure restore.
	storedCfg, err := s.storedRuleConfig(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	// Tolerate placement ids the rule already stores: a scope site/building/
	// rack/group deleted after creation must not brick unrelated edits (rename,
	// threshold). The rule stays visible-but-inert for that id, per the TDD;
	// added ids are still checked, and a stored id can never be cross-org
	// (create validated it).
	var keep *RuleScope
	if storedCfg != nil {
		keep = storedCfg.Scope
	}
	// A stale pre-scope client cannot round-trip the scope field, so its writes
	// arrive with no scope message at all; treating that as org-wide would let
	// an unrelated edit (rename, threshold) silently widen a scoped rule to
	// every miner. Unscoping requires the explicit org_wide marker.
	if keep != nil && cfg.Scope == nil && !cfg.ScopeOrgWideExplicit {
		return nil, fleeterror.NewInvalidArgumentError("this rule is scoped: send the current scope, or an explicit org-wide scope to remove it")
	}
	if err := s.validateRuleScope(ctx, orgID, cfg.Scope, keep); err != nil {
		return nil, err
	}
	body, err := compileUserRule(orgID, id, cfg)
	if err != nil {
		return nil, err
	}
	// Keep group/folder identity stable so pause silences (matched by UID) survive edits.
	body.FolderUID = current.FolderUID
	body.RuleGroup = current.RuleGroup
	body.IsPaused = current.IsPaused
	updated, err := s.grafana.UpdateAlertRule(ctx, body)
	if err != nil {
		// The row was not touched, so reads still match the live rule — unless
		// Grafana committed before erroring (timeout after commit): publish the
		// new config only when the live rule provably carries it.
		s.publishConfigIfCommitted(ctx, orgID, id, body, cfg)
		return nil, err
	}
	// Publish only after the confirmed commit: reads must never report a
	// configuration Grafana might not be evaluating. If this write fails,
	// compensate by restoring the previous rule body so the live SQL keeps
	// matching the stored config (a stale row doesn't merely understate a
	// change — a failed narrowing edit would show org-wide coverage the rule no
	// longer provides). Either way the caller sees an error and a retried save
	// converges both sides.
	if s.configs != nil {
		if uerr := s.configs.UpsertConfig(ctx, orgID, id, cfg); uerr != nil {
			if err := s.reconcileFailedConfigWrite(ctx, orgID, id, cfg, *current, uerr); err != nil {
				return nil, err
			}
			// nil: the upsert had actually committed — both sides carry the update.
		}
	}
	// Re-pin the group interval so an edit converges a pin that failed at create.
	group := GrafanaRuleGroup{
		Title:     body.RuleGroup,
		FolderUID: body.FolderUID,
		Interval:  userRuleGroupInterval,
		Rules:     []GrafanaAlertRule{*updated},
	}
	if err := s.grafana.SetRuleGroup(ctx, group); err != nil {
		slog.Warn("alerts.user_rule_group_interval", "org_id", orgID, "error", err)
	}
	return updated, nil
}

// reconcileFailedConfigWrite handles an UpsertConfig error after a committed
// Grafana update. The error does not prove the row was not written — a timeout
// or connection reset can land after Postgres committed — so probe the row
// before compensating: restoring Grafana against a committed row would create
// the very divergence the restore exists to prevent. Returns nil when both
// sides provably carry the update (converged despite the error).
func (s *Service) reconcileFailedConfigWrite(ctx context.Context, orgID int64, id string, cfg RuleConfig, previous GrafanaAlertRule, uerr error) error {
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	// The upsert is idempotent, so retry it first: a success converges both
	// sides regardless of whether the failed attempt had committed.
	if rerr := s.configs.UpsertConfig(probeCtx, orgID, id, cfg); rerr == nil {
		slog.Warn("alerts.user_rule_update_config_retry_converged", "org_id", orgID, "rule_id", id, "error", uerr)
		return nil
	}
	stored, gerr := s.configs.GetConfig(probeCtx, orgID, id)
	if gerr != nil {
		// Row state unknown: leave both sides alone — reads flag any mismatch
		// (markConfigOutOfSync) until a re-save converges them.
		slog.Error("alerts.user_rule_update_config_probe", "org_id", orgID, "rule_id", id, "error", gerr)
		return fmt.Errorf("storing the rule config failed and its state could not be confirmed — re-save to converge: %w", uerr)
	}
	if stored != nil && ruleConfigsEqual(*stored, cfg) {
		slog.Warn("alerts.user_rule_update_config_committed_despite_error", "org_id", orgID, "rule_id", id, "error", uerr)
		return nil
	}
	// The row provably still holds the previous config: restore the rule so the
	// live SQL keeps matching it, and surface a retriable error.
	if _, rerr := s.grafana.UpdateAlertRule(probeCtx, previous); rerr != nil {
		// Failed compensation must not read as a clean rollback: Grafana may
		// keep evaluating the new SQL while the row holds the old config.
		slog.Error("alerts.user_rule_update_rule_restore", "org_id", orgID, "rule_id", id, "error", rerr)
		return fmt.Errorf("rule updated in Grafana but storing its config failed, and rolling the rule back also failed — re-save to converge (the rule may be evaluating the new definition): %w", uerr)
	}
	return fmt.Errorf("rule update rolled back: storing its config failed (retry the save): %w", uerr)
}

// ruleConfigsEqual compares the persisted fields (ScopeOrgWideExplicit is
// request metadata and never stored, so it stays out of the comparison).
func ruleConfigsEqual(a, b RuleConfig) bool {
	ja, aerr := json.Marshal(a)
	jb, berr := json.Marshal(b)
	return aerr == nil && berr == nil && bytes.Equal(ja, jb)
}

// publishConfigIfCommitted handles the ambiguous PUT failure: an error response
// does not prove Grafana rejected the update — a timeout can land after the
// commit. Probe the live rule and publish the attempted config only when the
// rule provably carries it; otherwise the untouched row still matches the
// previous rule. The row can therefore only ever be BEHIND the live rule: a
// residual mismatch understates the change (extra alerts stay visible) rather
// than reporting coverage Grafana isn't evaluating, and a retried save
// converges both sides.
func (s *Service) publishConfigIfCommitted(ctx context.Context, orgID int64, id string, attempted GrafanaAlertRule, cfg RuleConfig) {
	if s.configs == nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	live, err := s.grafana.GetAlertRule(probeCtx, id)
	if err != nil {
		slog.Warn("alerts.user_rule_update_config_probe", "org_id", orgID, "rule_id", id, "error", err)
		return
	}
	if !ruleCarriesUpdate(*live, attempted) {
		return
	}
	if uerr := s.configs.UpsertConfig(probeCtx, orgID, id, cfg); uerr != nil {
		slog.Warn("alerts.user_rule_update_config_publish", "org_id", orgID, "rule_id", id, "error", uerr)
	}
}

// ruleCarriesUpdate reports whether the live rule already reflects the
// attempted update body (Grafana committed despite the error response).
func ruleCarriesUpdate(live, attempted GrafanaAlertRule) bool {
	return live.Title == attempted.Title &&
		live.For == attempted.For &&
		ruleRawSQL(live.Data) == ruleRawSQL(attempted.Data)
}

// ruleRawSQL extracts refId A's rawSql; "" on any parse mismatch so an
// unparseable body compares only on the cheap fields above.
func ruleRawSQL(data json.RawMessage) string {
	var entries []struct {
		RefID string `json:"refId"`
		Model struct {
			RawSQL string `json:"rawSql"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.RefID == "A" {
			return entry.Model.RawSQL
		}
	}
	return ""
}

// legacyRuleConfig parses a pre-config-store annotation config (rules created
// before migration 000135, which the table deliberately did not backfill).
// Invalid content degrades to nil — the rule reads as non-editable — rather
// than failing the list. Legacy configs predate scopes, so they are always
// org-wide; the first successful update migrates them into a store row.
func legacyRuleConfig(ctx context.Context, orgID int64, ruleUID, raw string) *RuleConfig {
	if raw == "" {
		return nil
	}
	var cfg RuleConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		slog.WarnContext(ctx, "alerts.rule_legacy_config_parse", "org_id", orgID, "rule_id", ruleUID, "error", err)
		return nil
	}
	cfg.Scope = normalizeRuleScope(cfg.Scope)
	if err := validateRuleConfig(cfg); err != nil {
		slog.WarnContext(ctx, "alerts.rule_legacy_config_parse", "org_id", orgID, "rule_id", ruleUID, "error", err)
		return nil
	}
	return &cfg
}

// storedRuleConfig reads the rule's persisted config row. Store errors fail the
// caller (a lost row must not silently drop the update-time scope tolerance or
// the PUT-failure restore).
func (s *Service) storedRuleConfig(ctx context.Context, orgID int64, ruleUID string) (*RuleConfig, error) {
	if s.configs == nil {
		return nil, nil
	}
	cfg, err := s.configs.GetConfig(ctx, orgID, ruleUID)
	if err != nil {
		return nil, fmt.Errorf("read rule config: %w", err)
	}
	return cfg, nil
}

func (s *Service) DeleteRule(ctx context.Context, orgID int64, id string) error {
	if err := requireOrg(orgID); err != nil {
		return err
	}
	if id == "" {
		return fleeterror.NewInvalidArgumentError("rule id is required")
	}
	cleanup := func() error {
		sweepErr := s.removeSilencesTargetingRule(ctx, orgID, id, func(sil GrafanaSilence) bool {
			return isPauseSilence(sil) || isMaintenanceWindowSilence(sil)
		})
		// The policy and config rows are inert once the rule is gone; drop them without letting their errors gate the safety-relevant silence sweep above.
		var policyErr error
		if s.routes != nil {
			if policyErr = s.routes.DeletePolicy(ctx, orgID, id); policyErr == nil {
				s.invalidateDeliveryPolicyCache(orgID)
			}
		}
		var configErr error
		if s.configs != nil {
			configErr = s.configs.DeleteConfig(ctx, orgID, id)
		}
		return errors.Join(sweepErr, policyErr, configErr)
	}
	err := s.deleteRuleSerialized(ctx, orgID, id)
	switch {
	case err == nil:
		// Silences are inert once the rule is gone; don't fail the committed
		// delete over cleanup (a delete retry re-sweeps via the not-found path).
		if err := cleanup(); err != nil {
			slog.Warn("alerts.user_rule_delete_silence_cleanup", "org_id", orgID, "rule_id", id, "error", err)
		}
		return nil
	case errors.Is(err, errRuleNotDeletable):
		// The rule exists (provisioned, another org's, or hidden): uniform
		// NotFound with NO cleanup — a delete probe with only alert:manage must
		// not clear the org's route policy or pause silences on a live rule.
		return ErrNotFound
	case IsNotFound(err):
		// The rule is already gone: re-sweep its silences so a half-failed
		// earlier delete converges, then keep the uniform NotFound (no id oracle).
		if err := cleanup(); err != nil {
			return err
		}
		return ErrNotFound
	default:
		return err
	}
}

// errRuleNotDeletable: the rule exists in Grafana but is not this org's mutable
// user rule; distinct from a genuine 404 so DeleteRule never cleans up a live rule.
var errRuleNotDeletable = errors.New("alerts: rule exists but is not deletable")

// deleteRuleSerialized holds userRuleMu across fetch, guard, and delete so a
// concurrent update's group re-pin can't replay the rule back after deletion.
func (s *Service) deleteRuleSerialized(ctx context.Context, orgID int64, id string) error {
	s.userRuleMu.Lock()
	defer s.userRuleMu.Unlock()
	rule, err := s.grafana.GetAlertRule(ctx, id)
	if err != nil {
		return err
	}
	if !isMutableUserRule(*rule, orgID) {
		return errRuleNotDeletable
	}
	if err := s.grafana.DeleteAlertRule(ctx, id); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

// requireUserRule resolves NotFound for missing rules, provisioned rules, other
// orgs' rules, and operator-hidden rules alike, so probing ids can't distinguish
// them and mutability never exceeds visibility (ruleVisibleToOrg).
func (s *Service) requireUserRule(ctx context.Context, orgID int64, id string) (*GrafanaAlertRule, error) {
	if id == "" {
		return nil, fleeterror.NewInvalidArgumentError("rule id is required")
	}
	rule, err := s.grafana.GetAlertRule(ctx, id)
	if err != nil {
		if IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !isMutableUserRule(*rule, orgID) {
		return nil, ErrNotFound
	}
	return rule, nil
}

func isMutableUserRule(rule GrafanaAlertRule, orgID int64) bool {
	org := strconv.FormatInt(orgID, 10)
	return rule.Labels[ruleLabelOrigin] == ruleOriginUser &&
		rule.Labels[ruleLabelOrganizationID] == org &&
		ruleVisibleToOrg(rule, org)
}

func newUserRuleUID() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate rule uid: %w", err)
	}
	return "pfu-" + hex.EncodeToString(b), nil
}

func validateRuleConfig(cfg RuleConfig) error {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return fleeterror.NewInvalidArgumentError("rule name is required")
	}
	if utf8.RuneCountInString(name) > maxRuleNameLength {
		return fleeterror.NewInvalidArgumentErrorf("rule name must be at most %d characters", maxRuleNameLength)
	}
	// Grafana uses these alertnames for its synthetic evaluation-failure alerts.
	if strings.EqualFold(name, "DatasourceError") || strings.EqualFold(name, "DatasourceNoData") {
		return fleeterror.NewInvalidArgumentErrorf("%q is a reserved rule name", name)
	}
	if cfg.DurationSeconds < 60 || cfg.DurationSeconds > 86400 {
		return fleeterror.NewInvalidArgumentError("duration must be between 60 seconds and 24 hours")
	}
	set := 0
	for _, present := range []bool{cfg.Offline != nil, cfg.Hashrate != nil, cfg.Temperature != nil} {
		if present {
			set++
		}
	}
	if set != 1 {
		return fleeterror.NewInvalidArgumentError("exactly one of offline, hashrate, or temperature must be set")
	}
	if h := cfg.Hashrate; h != nil {
		if math.IsNaN(h.Value) || math.IsInf(h.Value, 0) {
			return fleeterror.NewInvalidArgumentError("hashrate value must be a finite number")
		}
		switch h.Mode {
		case HashrateModePctExpected:
			if h.Value < minHashratePercent || h.Value > 100 {
				return fleeterror.NewInvalidArgumentErrorf("hashrate percent must be between %v and 100", minHashratePercent)
			}
		case HashrateModeAbsolute:
			if h.Value <= 0 {
				return fleeterror.NewInvalidArgumentError("hashrate value must be greater than 0")
			}
			if h.Unit != HashrateUnitTerahash && h.Unit != HashrateUnitPetahash {
				return fleeterror.NewInvalidArgumentError("hashrate unit must be TH or PH")
			}
			if absoluteTerahash(*h) > maxAbsoluteTerahash {
				return fleeterror.NewInvalidArgumentError("hashrate threshold is too large")
			}
		default:
			return fleeterror.NewInvalidArgumentError("hashrate mode must be pct_expected or absolute")
		}
	}
	if t := cfg.Temperature; t != nil {
		if math.IsNaN(t.MaxCelsius) || t.MaxCelsius <= 0 || t.MaxCelsius > 150 {
			return fleeterror.NewInvalidArgumentError("temperature must be greater than 0 and at most 150 °C")
		}
	}
	return nil
}

// Bounds mirror the proto field constraints; every id becomes a literal in the compiled SQL.
const (
	maxRuleScopePlacementIDs = 100
	maxRuleScopeDeviceIDs    = 500
)

func normalizeIDList(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// normalizeRuleScope dedupes and sorts the id lists so equal scopes compile to
// identical SQL, and collapses an empty scope to nil (org-wide). AllSites
// supersedes an explicit site list.
func normalizeRuleScope(scope *RuleScope) *RuleScope {
	if scope.IsZero() {
		return nil
	}
	out := &RuleScope{
		AllSites:    scope.AllSites,
		SiteIDs:     normalizeIDList(scope.SiteIDs),
		BuildingIDs: normalizeIDList(scope.BuildingIDs),
		RackIDs:     normalizeIDList(scope.RackIDs),
		GroupIDs:    normalizeIDList(scope.GroupIDs),
	}
	if out.AllSites {
		out.SiteIDs = nil
	}
	if len(scope.DeviceIDs) > 0 {
		out.DeviceIDs = slices.Clone(scope.DeviceIDs)
		slices.Sort(out.DeviceIDs)
		out.DeviceIDs = slices.Compact(out.DeviceIDs)
	}
	return out
}

// scopeDimension is one placement dimension of a scope; the accessor serves
// both the requested scope and the update-time keep scope, and lookup is nil
// when the Service has no ScopeLookup (format checks still run).
type scopeDimension struct {
	name   string
	ids    func(*RuleScope) []int64
	lookup func(ctx context.Context, orgID int64, ids []int64) ([]int64, error)
}

func (s *Service) scopeDimensions() []scopeDimension {
	dims := []scopeDimension{
		{name: "site", ids: func(sc *RuleScope) []int64 { return sc.SiteIDs }},
		{name: "building", ids: func(sc *RuleScope) []int64 { return sc.BuildingIDs }},
		{name: "rack", ids: func(sc *RuleScope) []int64 { return sc.RackIDs }},
		{name: "group", ids: func(sc *RuleScope) []int64 { return sc.GroupIDs }},
	}
	if s.scopeLookup == nil {
		return dims
	}
	deviceSets := func(setType string) func(ctx context.Context, orgID int64, ids []int64) ([]int64, error) {
		return func(ctx context.Context, orgID int64, ids []int64) ([]int64, error) {
			return s.scopeLookup.DeviceSetsByIDs(ctx, orgID, setType, ids)
		}
	}
	dims[0].lookup = s.scopeLookup.SitesByIDs
	dims[1].lookup = s.scopeLookup.BuildingsByIDs
	dims[2].lookup = deviceSets("rack")
	dims[3].lookup = deviceSets("group")
	return dims
}

// validateRuleScope format-checks scope ids and confirms placement ids are
// live and org-owned; keep holds the rule's already-stored scope, whose ids
// updates may retain even if since deleted. Device ids get the same pattern
// check as device-scoped maintenance windows and, like them, no existence
// check: the compiled SQL resolves device ids through the org-filtered
// live-device view (see scopeFilterSQL), so an unknown, deleted, or cross-org
// id is inert.
func (s *Service) validateRuleScope(ctx context.Context, orgID int64, scope, keep *RuleScope) error {
	if scope.IsZero() {
		return nil
	}
	dims := s.scopeDimensions()
	for _, dim := range dims {
		ids := dim.ids(scope)
		if len(ids) > maxRuleScopePlacementIDs {
			return fleeterror.NewInvalidArgumentErrorf("too many scope %s_ids: %d (max %d)", dim.name, len(ids), maxRuleScopePlacementIDs)
		}
		for _, id := range ids {
			if id <= 0 {
				return fleeterror.NewInvalidArgumentErrorf("invalid scope %s id: %d", dim.name, id)
			}
		}
	}
	if len(scope.DeviceIDs) > maxRuleScopeDeviceIDs {
		return fleeterror.NewInvalidArgumentErrorf("too many scope device_ids: %d (max %d)", len(scope.DeviceIDs), maxRuleScopeDeviceIDs)
	}
	// Restrict ids to the identifier alphabet so a crafted id can't escape its SQL string literal.
	for _, id := range scope.DeviceIDs {
		if len(id) > maxDeviceIDLength {
			return fleeterror.NewInvalidArgumentErrorf("scope device id too long: %d (max %d)", len(id), maxDeviceIDLength)
		}
		if !deviceIDPattern.MatchString(id) {
			return fleeterror.NewInvalidArgumentErrorf("invalid scope device id: %q", id)
		}
	}
	for _, dim := range dims {
		ids := dim.ids(scope)
		if len(ids) == 0 || dim.lookup == nil {
			continue
		}
		found, err := dim.lookup(ctx, orgID, ids)
		if err != nil {
			return fmt.Errorf("validate scope %ss: %w", dim.name, err)
		}
		live := make(map[int64]bool, len(found))
		for _, id := range found {
			live[id] = true
		}
		if keep != nil {
			for _, id := range dim.ids(keep) {
				live[id] = true
			}
		}
		for _, id := range ids {
			if !live[id] {
				// Same audit signal and generic message as the canonical
				// cross-org validators (interfaces.ValidateFilterSites): the
				// rejected ids may be another org's, so never echo them.
				slog.WarnContext(ctx, "cross_org_filter_probe",
					"org_id", orgID,
					"surface", "alert_rule_scope",
					"dimension", dim.name,
					"rejected_count", 1,
				)
				return fleeterror.NewInvalidArgumentErrorf(
					"one or more scope %s_ids reference %ss outside the caller's org or that no longer exist", dim.name, dim.name)
			}
		}
	}
	return nil
}

func absoluteTerahash(h HashrateRuleConfig) float64 {
	if h.Unit == HashrateUnitPetahash {
		return h.Value * 1000
	}
	return h.Value
}

func compileUserRule(orgID int64, uid string, cfg RuleConfig) (GrafanaAlertRule, error) {
	sql, summary, description := compileTemplate(orgID, cfg)
	data, err := json.Marshal([]map[string]any{
		{
			"refId":             "A",
			"relativeTimeRange": map[string]any{"from": userRuleEvalWindowMinute * 60, "to": 0},
			"datasourceUid":     timescaleDatasourceUID,
			"model":             map[string]any{"refId": "A", "format": "table", "rawSql": sql},
		},
		{
			"refId":         "B",
			"datasourceUid": "__expr__",
			"model":         map[string]any{"refId": "B", "type": "math", "expression": "$A"},
		},
	})
	if err != nil {
		return GrafanaAlertRule{}, fmt.Errorf("marshal rule data: %w", err)
	}
	org := strconv.FormatInt(orgID, 10)
	return GrafanaAlertRule{
		UID:       uid,
		FolderUID: userRuleOrgSlug(orgID),
		// One Grafana group per rule: group PUTs (interval pinning) replace the
		// whole group, so sharing one would let concurrent creates erase siblings.
		RuleGroup: uid,
		Title:     strings.TrimSpace(cfg.Name),
		Condition: "B",
		Data:      data,
		For:       fmt.Sprintf("%ds", cfg.DurationSeconds),
		// Missing data is healthy. Error alerts inherit this rule's static org
		// label, so the deliverer drops synthetic DatasourceError/NoData alerts
		// to keep evaluation failures operator-only (history still records them).
		NoDataState:  "OK",
		ExecErrState: "Error",
		Labels: map[string]string{
			ruleLabelOrganizationID: org,
			ruleLabelOrigin:         ruleOriginUser,
			ruleLabelSeverity:       "warning",
			ruleLabelTemplate:       string(cfg.Template()),
			ruleLabelRuleGroup:      userRuleOrgSlug(orgID),
			ruleLabelRuleUID:        uid,
		},
		Annotations: map[string]string{
			"summary":     summary,
			"description": description,
		},
	}, nil
}

// scopeFilterSQL renders the sample filter for a scoped rule: one
// fleet_device_placement semijoin over the union of the scope's placements and
// explicit devices. Both dimensions resolve through the view so rows only match
// CURRENT membership of LIVE devices — placements never match on stale emit-time
// attributes, and a soft-deleted miner's retained samples (device deletion does
// not purge them) stop matching immediately instead of riding out the eval
// window. Membership is deliberately not timestamp-gated: a miner moved INTO
// scope is evaluated on its latest samples immediately — last(value, time)
// reflects its current state, and a rule covering a currently-violating miner
// should fire after its configured duration, not wait out a placement grace
// period. Placement ids are server-formatted integers and device ids are
// allowlist-validated by deviceIDPattern (no quotes or backslashes), so the
// embedded literals cannot escape their quoting. Returns "" for an org-wide
// scope so unscoped rules compile byte-identical to before.
func scopeFilterSQL(org string, scope *RuleScope, indent string) string {
	if scope.IsZero() {
		return ""
	}
	var conds []string
	if len(scope.DeviceIDs) > 0 {
		quoted := make([]string, len(scope.DeviceIDs))
		for i, id := range scope.DeviceIDs {
			quoted[i] = "'" + id + "'"
		}
		conds = append(conds, "device_id IN ("+strings.Join(quoted, ", ")+")")
	}
	if cond := placementCondSQL(scope); cond != "" {
		conds = append(conds, cond)
	}
	cond := conds[0]
	if len(conds) == 2 {
		cond = "(" + conds[0] + " OR " + conds[1] + ")"
	}
	return "\n" + indent + "AND device_id IN (\n" +
		indent + "  SELECT device_id FROM fleet_device_placement\n" +
		indent + "  WHERE org_id = " + org + "\n" +
		indent + "    AND " + cond + "\n" +
		indent + ")"
}

func int64ListSQL(ids []int64) string {
	rendered := make([]string, len(ids))
	for i, id := range ids {
		rendered[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(rendered, ", ")
}

// placementCondSQL renders the fleet_device_placement predicate for the scope's
// placement dimensions; "" when the scope is device-list-only.
func placementCondSQL(scope *RuleScope) string {
	var conds []string
	switch {
	case scope.AllSites:
		conds = append(conds, "site_id IS NOT NULL")
	case len(scope.SiteIDs) > 0:
		conds = append(conds, "site_id IN ("+int64ListSQL(scope.SiteIDs)+")")
	}
	if len(scope.BuildingIDs) > 0 {
		conds = append(conds, "building_id IN ("+int64ListSQL(scope.BuildingIDs)+")")
	}
	if len(scope.RackIDs) > 0 {
		conds = append(conds, "rack_id IN ("+int64ListSQL(scope.RackIDs)+")")
	}
	if len(scope.GroupIDs) > 0 {
		conds = append(conds, "group_id IN ("+int64ListSQL(scope.GroupIDs)+")")
	}
	if len(conds) == 0 {
		return ""
	}
	if len(conds) == 1 {
		return conds[0]
	}
	return "(" + strings.Join(conds, " OR ") + ")"
}

// scopeSiteColumnSQL emits the site label column for scoped rules so their alert
// instances carry site_id (silences and history can key on it); "" when unscoped.
// The label reads CURRENT placement from fleet_device_placement rather than the
// row's emit-time site_id: retained samples keep their old stamp after a move,
// and an offline miner emits nothing new, so a telemetry-derived label would
// stay stale indefinitely and change alert identity whenever fresh telemetry
// finally lands. LIMIT 1 collapses the view's per-group row fan-out (site_id is
// identical across a device's rows). deviceCol is the caller's qualified device
// column; the correlated subquery runs once per emitted device row, after
// aggregation. The value is cast to text and COALESCEd to empty because the
// view's site_id is BIGINT while Grafana's table contract allows exactly one
// numeric column (the value) and only labels non-numeric ones; empty matches
// the sample column's not-null default, so an unplaced device keeps the same
// empty label it had under the old telemetry-derived column.
func scopeSiteColumnSQL(org, deviceCol string, scope *RuleScope, indent string) string {
	if scope.IsZero() {
		return ""
	}
	return "\n" + indent + "COALESCE((SELECT site_id::text FROM fleet_device_placement\n" +
		indent + " WHERE org_id = " + org + " AND device_id = " + deviceCol + " LIMIT 1), '') AS site_id,"
}

// latestValueSQL is the shared per-device skeleton: newest sample of one metric
// per device in the eval window, org-scoped, matching on the HAVING clause.
func latestValueSQL(org, metric, having string, scope *RuleScope) string {
	return fmt.Sprintf(`SELECT
    organization_id,
    device_id,%s
    1 AS value
FROM notification_metric_sample
WHERE metric = '%s'
  AND organization_id = '%s'%s
  AND time > NOW() - INTERVAL '%d minutes'
GROUP BY organization_id, device_id
HAVING %s`, scopeSiteColumnSQL(org, "notification_metric_sample.device_id", scope, "    "), metric, org, scopeFilterSQL(org, scope, "  "), userRuleEvalWindowMinute, having)
}

// compileTemplate renders the org-scoped SQL plus human summary/description.
// Every interpolated value is a server-validated number, a pattern-validated
// device id (see validateRuleScope), or the session org id, so no free-form
// request string ever reaches the SQL.
func compileTemplate(orgID int64, cfg RuleConfig) (sql, summary, description string) {
	org := strconv.FormatInt(orgID, 10)
	dur := humanizeDuration(cfg.DurationSeconds)
	switch {
	case cfg.Offline != nil:
		sql = latestValueSQL(org, metricDeviceOnline, "last(value, time) = 0", cfg.Scope)
		summary = fmt.Sprintf("Device is offline for at least %s.", dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been reporting %s=0 for at least %s.", metricDeviceOnline, dur)
	case cfg.Hashrate != nil && cfg.Hashrate.Mode == HashrateModePctExpected:
		ratio := formatRatio(cfg.Hashrate.Value)
		sql = latestValueSQL(org, metricDeviceHashing, "last(value, time) < "+ratio, cfg.Scope)
		summary = fmt.Sprintf("Device hashrate is below %s%% of expected for at least %s.", formatFloat(cfg.Hashrate.Value), dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been hashing below %s%% of its expected rate for at least %s.", formatFloat(cfg.Hashrate.Value), dur)
	case cfg.Hashrate != nil:
		threshold := formatFloat(absoluteTerahash(*cfg.Hashrate))
		// The observed-TH metric keeps reporting ~0 for curtailed/paused miners; the
		// ratio metric's 1.0 doubles as the "not expected to hash" sentinel, so only
		// suppress when the sentinel coincides with no observed hashing — a positive
		// reading (at-nameplate or no-nameplate devices also sit at ratio 1) still alerts.
		sql = fmt.Sprintf(`WITH latest AS (
    SELECT
        organization_id,
        device_id,
        metric,
        last(value, time) AS latest_value
    FROM notification_metric_sample
    WHERE metric IN ('%s', '%s')
      AND organization_id = '%s'%s
      AND time > NOW() - INTERVAL '%d minutes'
    GROUP BY organization_id, device_id, metric
)
SELECT
    obs.organization_id,
    obs.device_id,%s
    1 AS value
FROM latest AS obs
JOIN latest AS gate
  ON gate.organization_id = obs.organization_id
 AND gate.device_id = obs.device_id
 AND gate.metric = '%s'
WHERE obs.metric = '%s'
  AND (gate.latest_value < 1 OR obs.latest_value > 0)
  AND obs.latest_value < %s`,
			metricDeviceHashrateTerahash, metricDeviceHashing, org, scopeFilterSQL(org, cfg.Scope, "      "), userRuleEvalWindowMinute,
			scopeSiteColumnSQL(org, "obs.device_id", cfg.Scope, "    "),
			metricDeviceHashing, metricDeviceHashrateTerahash, threshold)
		summary = fmt.Sprintf("Device hashrate is below %s %s/s for at least %s.", formatFloat(cfg.Hashrate.Value), cfg.Hashrate.Unit, dur)
		description = fmt.Sprintf("Device {{ $labels.device_id }} (org {{ $labels.organization_id }})\nhas been hashing below %s %s/s for at least %s.", formatFloat(cfg.Hashrate.Value), cfg.Hashrate.Unit, dur)
	case cfg.Temperature != nil:
		limit := formatFloat(cfg.Temperature.MaxCelsius)
		// Freshness gate mirrors the provisioned temperature rule: a device that
		// stopped reporting while hot must not keep firing on an unconfirmable reading.
		sql = fmt.Sprintf(`WITH latest_per_kind AS (
    SELECT
        organization_id,
        device_id,
        sensor_kind,
        last(value, time) AS latest_temp,
        max(time) AS last_sample_time
    FROM notification_metric_sample
    WHERE metric = '%s'
      AND organization_id = '%s'%s
      AND time > NOW() - INTERVAL '%d minutes'
    GROUP BY organization_id, device_id, sensor_kind
)
SELECT
    organization_id,
    device_id,%s
    max(latest_temp) AS latest_temp
FROM latest_per_kind
WHERE last_sample_time > NOW() - INTERVAL '3 minutes'
GROUP BY organization_id, device_id
HAVING max(latest_temp) > %s`,
			metricDeviceTemperatureMaxCelsius, org, scopeFilterSQL(org, cfg.Scope, "      "), userRuleEvalWindowMinute,
			scopeSiteColumnSQL(org, "latest_per_kind.device_id", cfg.Scope, "    "), limit)
		summary = fmt.Sprintf("Max sensor temperature for device is above %sC for at least %s.", limit, dur)
		description = fmt.Sprintf("Maximum sensor temperature for device {{ $labels.device_id }}\nhas been above %sC for at least %s.", limit, dur)
	}
	return sql, summary, description
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatRatio renders percent/100 without binary-division drift (33.3 → "0.333",
// not "0.33299999999999996") so the SQL matches what the summary claims.
func formatRatio(percent float64) string {
	s := strconv.FormatFloat(percent/100, 'f', 10, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

func humanizeDuration(seconds int32) string {
	switch {
	case seconds%3600 == 0:
		if seconds == 3600 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", seconds/3600)
	case seconds%60 == 0:
		if seconds == 60 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", seconds/60)
	default:
		return fmt.Sprintf("%d seconds", seconds)
	}
}
