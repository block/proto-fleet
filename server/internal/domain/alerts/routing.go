package alerts

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sort"
	"strings"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Generous bound; orgs hold far fewer channels, this just caps a hostile request.
const maxRouteChannels = 100

// PolicyCacheInvalidator drops a delivery-side policy snapshot after a routing write (implemented by Deliverer).
type PolicyCacheInvalidator interface {
	InvalidatePolicyCache(orgID int64)
}

// invalidateDeliveryPolicyCache keeps the deliverer's last-known-good snapshot coherent with routing writes.
func (s *Service) invalidateDeliveryPolicyCache(orgID int64) {
	if inv, ok := s.tester.(PolicyCacheInvalidator); ok {
		inv.InvalidatePolicyCache(orgID)
	}
}

// SetRuleRouting replaces the rule's delivery policy: default deletes the policy row, custom requires org-owned channels, none clears delivery.
func (s *Service) SetRuleRouting(ctx context.Context, orgID int64, ruleID string, mode RouteMode, channelIDs []string) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if s.routes == nil {
		return nil, errors.New("alerts: route store not configured")
	}
	// Same visibility gate as pause/maintenance windows: an id the caller can't list is NotFound.
	rule, err := s.requireRule(ctx, orgID, ruleID)
	if err != nil {
		return nil, err
	}
	policy, err := s.resolveRoutePolicy(ctx, orgID, mode, channelIDs)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		if err := s.routes.DeletePolicy(ctx, orgID, ruleID); err != nil {
			return nil, err
		}
		s.invalidateDeliveryPolicyCache(orgID)
		rule.Routing = nil
		return rule, nil
	}
	policy.RuleUID = ruleID
	if err := s.routes.SetPolicy(ctx, orgID, *policy); err != nil {
		return nil, err
	}
	s.invalidateDeliveryPolicyCache(orgID)
	// Like confirmRuleSilenceTarget: undo a policy written concurrently with the rule's deletion, whose sweep couldn't have seen it.
	if _, err := s.grafana.GetAlertRule(ctx, ruleID); err != nil {
		if !IsNotFound(err) {
			return nil, err
		}
		if derr := s.routes.DeletePolicy(ctx, orgID, ruleID); derr != nil {
			return nil, derr
		}
		s.invalidateDeliveryPolicyCache(orgID)
		return nil, ErrNotFound
	}
	rule.Routing = policy
	return rule, nil
}

// resolveRoutePolicy validates a requested mode + channel set; a nil policy means default routing (no row).
func (s *Service) resolveRoutePolicy(ctx context.Context, orgID int64, mode RouteMode, channelIDs []string) (*RoutePolicy, error) {
	if mode != RouteModeDefault && s.routes == nil {
		return nil, errors.New("alerts: route store not configured")
	}
	switch mode {
	case RouteModeDefault, RouteModeNone:
		if len(channelIDs) > 0 {
			return nil, fleeterror.NewInvalidArgumentErrorf("channel_ids must be empty for %s routing", mode)
		}
	case RouteModeCustom:
		if len(channelIDs) == 0 {
			return nil, fleeterror.NewInvalidArgumentError("custom routing requires at least one channel")
		}
		if len(channelIDs) > maxRouteChannels {
			return nil, fleeterror.NewInvalidArgumentErrorf("too many channels: %d (max %d)", len(channelIDs), maxRouteChannels)
		}
	default:
		return nil, fleeterror.NewInvalidArgumentErrorf("unknown routing mode: %q", mode)
	}
	if mode == RouteModeDefault {
		return nil, nil
	}
	policy := &RoutePolicy{Mode: mode}
	if mode == RouteModeCustom {
		ids, err := s.resolveOrgChannelIDs(ctx, orgID, channelIDs)
		if err != nil {
			return nil, err
		}
		policy.ChannelIDs = ids
	}
	return policy, nil
}

// resolveOrgChannelIDs parses and dedupes the ids and rejects any channel the org doesn't own.
func (s *Service) resolveOrgChannelIDs(ctx context.Context, orgID int64, channelIDs []string) ([]int64, error) {
	recs, err := s.channels.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	owned := make(map[int64]bool, len(recs))
	for _, rec := range recs {
		owned[rec.ID] = true
	}
	seen := map[int64]bool{}
	out := make([]int64, 0, len(channelIDs))
	for _, raw := range channelIDs {
		id, err := parseChannelID(raw)
		if err != nil || !owned[id] {
			return nil, fleeterror.NewInvalidArgumentErrorf("unknown channel id: %q", raw)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// policiesByRule indexes an org's policies by rule UID; delivery and listing must share one keying.
func policiesByRule(policies []RoutePolicy) map[string]RoutePolicy {
	byRule := make(map[string]RoutePolicy, len(policies))
	for _, p := range policies {
		byRule[p.RuleUID] = p
	}
	return byRule
}

// attachRoutingBestEffort decorates one committed-mutation response; a read failure only warns, since
// failing a pause/resume that already took effect would read as "action failed" to the operator.
func (s *Service) attachRoutingBestEffort(ctx context.Context, orgID int64, rule *Rule) {
	rules := []Rule{*rule}
	if err := s.attachRouting(ctx, orgID, rules); err != nil {
		slog.Warn("alerts.rule_routing_decorate", "org_id", orgID, "rule_id", rule.ID, "error", err)
		return
	}
	*rule = rules[0]
}

// attachRouting decorates the rules with their org's route policies in one read.
func (s *Service) attachRouting(ctx context.Context, orgID int64, rules []Rule) error {
	if s.routes == nil {
		return nil
	}
	policies, err := s.routes.ListPolicies(ctx, orgID)
	if err != nil {
		return err
	}
	byRule := policiesByRule(policies)
	for i := range rules {
		if p, ok := byRule[rules[i].ID]; ok {
			rules[i].Routing = &p
		}
	}
	return nil
}

// RuleUIDFromGeneratorURL extracts the rule UID from a Grafana generatorURL (".../alerting/grafana/<uid>/view"), the fallback identity for rules compiled before the proto_fleet_rule_uid label; empty on an unrecognized shape.
func RuleUIDFromGeneratorURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i+2 < len(segs); i++ {
		if segs[i] == "alerting" && segs[i+1] == "grafana" {
			return segs[i+2]
		}
	}
	return ""
}
