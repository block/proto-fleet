package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/google/uuid"
)

type Service struct {
	grafana *Grafana
	policy  DestinationPolicy
	now     func() time.Time
}

type DestinationPolicy struct {
	AllowPrivateDestinations bool `help:"Allow notification destinations (webhook URLs, SMTP hosts) that resolve to loopback, link-local, or private network ranges. Enable for dev stacks or deployments whose relays live on internal addresses." default:"false" env:"ALLOW_PRIVATE_DESTINATIONS"`
}

func NewService(g *Grafana, policy DestinationPolicy) *Service {
	return &Service{grafana: g, policy: policy, now: time.Now}
}

var ErrZeroOrgID = errors.New("notifications: organization id is required")

// Surfaced as permission_denied so id scans aren't a list oracle.
var ErrNotFound = errors.New("notifications: not found")

func requireOrg(orgID int64) error {
	if orgID == 0 {
		return ErrZeroOrgID
	}
	return nil
}

func (s *Service) ListChannels(ctx context.Context, orgID int64) ([]Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	cps, err := s.grafana.ListContactPoints(ctx)
	if err != nil {
		return nil, err
	}
	prefix := channelNamePrefix(orgID)
	out := make([]Channel, 0, len(cps))
	for _, cp := range cps {
		if !strings.HasPrefix(cp.Name, prefix) {
			continue
		}
		c, err := contactPointToChannel(orgID, cp)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) CreateChannel(ctx context.Context, orgID int64, c Channel) (*Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := s.validateDestination(ctx, &c); err != nil {
		return nil, err
	}
	// Reject a duplicate name up front: Grafana would otherwise collapse the new
	// contact point onto the existing receiver as a second integration (they share
	// the org-prefixed name), which muddles per-channel test/delete semantics.
	grafanaName := channelGrafanaName(orgID, c.Name)
	existing, err := s.grafana.ListContactPoints(ctx)
	if err != nil {
		return nil, err
	}
	for _, cp := range existing {
		if cp.Name == grafanaName {
			return nil, fleeterror.NewAlreadyExistsErrorf("a channel named %q already exists", c.Name)
		}
	}
	c.OrganizationID = orgID
	c.CreatedAt = s.now()
	c.UpdatedAt = c.CreatedAt
	c.ValidationState = ValidationPending

	settings, err := encodeChannelSettings(&c)
	if err != nil {
		return nil, err
	}
	cp := GrafanaContactPoint{
		Name:     grafanaName,
		Type:     grafanaTypeFor(c.Kind),
		Settings: settings,
	}
	created, err := s.grafana.CreateContactPoint(ctx, cp)
	if err != nil {
		return nil, err
	}
	out, err := contactPointToChannel(orgID, *created)
	if err != nil {
		return nil, err
	}
	// Grafana's response strips the secret, so preserve the local HasSecret flag.
	out.HasSecret = c.HasSecret
	return &out, nil
}

func (s *Service) UpdateChannel(ctx context.Context, orgID int64, c Channel) (*Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if c.ID == "" {
		return nil, errors.New("channel id is required for update")
	}
	// Grafana doesn't enforce our prefix scheme, so verify ownership before the PUT.
	owned, ownedCP, err := s.findOwnedChannel(ctx, orgID, c.ID)
	if err != nil {
		return nil, err
	}
	// A rename to another channel's name would collapse both onto one Grafana
	// receiver, so reject it the same way CreateChannel does (excluding self).
	if c.Name != owned.Name {
		grafanaName := channelGrafanaName(orgID, c.Name)
		existing, err := s.grafana.ListContactPoints(ctx)
		if err != nil {
			return nil, err
		}
		for _, ecp := range existing {
			if ecp.Name == grafanaName && ecp.UID != c.ID {
				return nil, fleeterror.NewAlreadyExistsErrorf("a channel named %q already exists", c.Name)
			}
		}
	}
	// destinationChanged gates secret preservation: a stored secret must never be carried onto a new destination.
	destinationChanged := false
	keepStoredSlackURL := false
	switch c.Kind {
	case ChannelKindWebhook:
		if c.Webhook != nil {
			// Only reuse the stored URL when this was already a webhook; otherwise we'd graft the prior kind's secret (e.g. a Slack URL) onto the webhook.
			stored := ""
			if owned.Kind == ChannelKindWebhook {
				stored = webhookURLFromSettings(ownedCP.Settings)
			}
			if stored != "" && (c.Webhook.URL == "" || c.Webhook.URL == redactWebhookURL(stored)) {
				c.Webhook.URL = stored
			}
			destinationChanged = c.Webhook.URL != stored
		}
	case ChannelKindSlack:
		// Only keep the stored URL when this was already a Slack channel; otherwise carrySecretSettings would graft the prior kind's secret onto the new Slack contact point.
		keepStoredSlackURL = owned.Kind == ChannelKindSlack && (c.Slack == nil || c.Slack.WebhookURL == "")
		if c.Slack == nil {
			c.Slack = &SlackConfig{}
		}
		destinationChanged = !keepStoredSlackURL
	}
	if !keepStoredSlackURL {
		if err := s.validateDestination(ctx, &c); err != nil {
			return nil, err
		}
	}
	c.OrganizationID = orgID
	c.UpdatedAt = s.now()
	c.ValidationState = ValidationPending
	c.ValidatedAt = nil
	c.ValidationError = ""
	hasNewSecret := s.requestHasNewSecret(&c)

	settings, err := encodeChannelSettings(&c)
	if err != nil {
		return nil, err
	}
	// Carry the stored secret forward only when the destination is unchanged, so the old credential can't be delivered to a new destination.
	if !hasNewSecret {
		if destinationChanged {
			c.HasSecret = false
		} else {
			var carried bool
			settings, carried, err = carrySecretSettings(ownedCP.Settings, settings, c.Kind)
			if err != nil {
				return nil, err
			}
			c.HasSecret = owned.HasSecret || carried
		}
	}
	cp := GrafanaContactPoint{
		UID:      c.ID,
		Name:     channelGrafanaName(orgID, c.Name),
		Type:     grafanaTypeFor(c.Kind),
		Settings: settings,
	}
	if err := s.grafana.UpdateContactPoint(ctx, c.ID, cp); err != nil {
		return nil, err
	}
	// Grafana's provisioning PUT returns a 202 Ack, not the contact point, so build the response from what we sent.
	out, err := contactPointToChannel(orgID, cp)
	if err != nil {
		return nil, err
	}
	out.HasSecret = c.HasSecret
	return &out, nil
}

func (s *Service) DeleteChannel(ctx context.Context, orgID int64, id string) error {
	if err := requireOrg(orgID); err != nil {
		return err
	}
	if _, _, err := s.findOwnedChannel(ctx, orgID, id); err != nil {
		return err
	}
	if err := s.grafana.DeleteContactPoint(ctx, id); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

func (s *Service) TestChannel(ctx context.Context, orgID int64, c Channel) (bool, int, string, error) {
	if err := requireOrg(orgID); err != nil {
		return false, 0, "", err
	}

	if c.ID != "" {
		// Saved channel: verify org ownership, then replay the receiver's stored
		// integration so Grafana reuses its secrets. We can't rebuild the body from
		// a read — reads redact the secret (Slack url, webhook bearer), and sending
		// those placeholders back fails delivery.
		_, ownedCP, err := s.findOwnedChannel(ctx, orgID, c.ID)
		if err != nil {
			return false, 0, "", err
		}
		res, err := s.grafana.TestStoredReceiver(ctx, ownedCP.Name, ownedCP.UID)
		if err != nil {
			return false, 0, "", err
		}
		return res.OK, testStatusCode(res.OK), res.Error, nil
	}

	// Test-before-save: Grafana's receiver test API only addresses an existing
	// receiver, so stand up a transient org-scoped contact point, test it, and
	// tear it down. The temp name keeps the org prefix so isolation still holds.
	if err := s.validateDestination(ctx, &c); err != nil {
		return false, 0, "", err
	}
	c.OrganizationID = orgID
	settings, err := encodeChannelSettings(&c)
	if err != nil {
		return false, 0, "", err
	}
	gType := grafanaTypeFor(c.Kind)
	tmpName := channelGrafanaName(orgID, "test-"+uuid.NewString())
	created, err := s.grafana.CreateContactPoint(ctx, GrafanaContactPoint{Name: tmpName, Type: gType, Settings: settings})
	if err != nil {
		return false, 0, "", err
	}
	defer func() {
		// Fresh context: if the caller's ctx is already canceled (client gone or
		// deadline hit during the test), reusing it would skip the delete and leave
		// an org-<id>-test-* contact point that ListChannels would surface.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if delErr := s.grafana.DeleteContactPoint(cleanupCtx, created.UID); delErr != nil {
			slog.Warn("notifications.test_channel_cleanup_failed", "uid", created.UID, "err", delErr)
		}
	}()
	res, err := s.grafana.TestReceiverIntegration(ctx, tmpName, gType, settings)
	if err != nil {
		return false, 0, "", err
	}
	return res.OK, testStatusCode(res.OK), res.Error, nil
}

// testStatusCode keeps the wire response_code field meaningful for the legacy
// HTTP-status-shaped client: the receiver test API reports a boolean outcome, not
// a destination status code, so map a successful delivery to 200.
func testStatusCode(ok bool) int {
	if ok {
		return 200
	}
	return 0
}

// Returns the raw contact point too, needed to carry secret settings the decoded Channel drops.
func (s *Service) findOwnedChannel(ctx context.Context, orgID int64, id string) (*Channel, *GrafanaContactPoint, error) {
	cps, err := s.grafana.ListContactPoints(ctx)
	if err != nil {
		return nil, nil, err
	}
	prefix := channelNamePrefix(orgID)
	for i, cp := range cps {
		if cp.UID != id || !strings.HasPrefix(cp.Name, prefix) {
			continue
		}
		c, err := contactPointToChannel(orgID, cp)
		if err != nil {
			return nil, nil, err
		}
		return &c, &cps[i], nil
	}
	return nil, nil, ErrNotFound
}

func (s *Service) requestHasNewSecret(c *Channel) bool {
	switch c.Kind {
	case ChannelKindWebhook:
		return c.Webhook != nil && c.Webhook.BearerHeader != ""
	case ChannelKindSlack:
		return c.Slack != nil && c.Slack.WebhookURL != ""
	}
	return false
}

func secretSettingsKeyFor(kind ChannelKind) string {
	switch kind {
	case ChannelKindWebhook:
		return "authorization_credentials"
	case ChannelKindSlack:
		return "url"
	}
	return ""
}

func carrySecretSettings(existing, next json.RawMessage, kind ChannelKind) (json.RawMessage, bool, error) {
	key := secretSettingsKeyFor(kind)
	if key == "" {
		return next, false, nil
	}
	var prev map[string]json.RawMessage
	if err := json.Unmarshal(existing, &prev); err != nil {
		return nil, false, fmt.Errorf("unmarshal existing contact point settings: %w", err)
	}
	raw, ok := prev[key]
	if !ok || len(raw) == 0 || string(raw) == `""` || string(raw) == "null" {
		return next, false, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(next, &out); err != nil {
		return nil, false, fmt.Errorf("unmarshal update settings: %w", err)
	}
	out[key] = raw
	b, err := json.Marshal(out)
	if err != nil {
		return nil, false, fmt.Errorf("marshal settings with carried secret: %w", err)
	}
	return b, true, nil
}

// Grafana is what connects out, so an unvalidated destination is an SSRF vector.
func (s *Service) validateDestination(ctx context.Context, c *Channel) error {
	switch c.Kind {
	case ChannelKindWebhook:
		if c.Webhook == nil || c.Webhook.URL == "" {
			return fleeterror.NewInvalidArgumentError("webhook url is required")
		}
		return s.checkDestinationURL(ctx, c.Webhook.URL, "webhook")
	case ChannelKindSlack:
		if c.Slack == nil || c.Slack.WebhookURL == "" {
			return fleeterror.NewInvalidArgumentError("slack webhook url is required")
		}
		return s.checkDestinationURL(ctx, c.Slack.WebhookURL, "slack webhook")
	}
	return nil
}

func (s *Service) checkDestinationURL(ctx context.Context, raw, label string) error {
	u, err := url.Parse(raw)
	if err != nil {
		// url.Parse's error embeds the raw input (which can carry a capability token); keep the message generic so the secret can't leak.
		return fleeterror.NewInvalidArgumentErrorf("%s url is not parseable", label)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fleeterror.NewInvalidArgumentErrorf("%s url scheme must be http or https, got %q", label, u.Scheme)
	}
	if u.Hostname() == "" {
		return fleeterror.NewInvalidArgumentError(label + " url must include a host")
	}
	return s.checkDestinationHost(ctx, u.Hostname())
}

const destinationLookupTimeout = 3 * time.Second

// DNS failures fail closed. Not rebinding-proof; egress enforcement at Grafana's network boundary is the hard guarantee.
func (s *Service) checkDestinationHost(ctx context.Context, host string) error {
	if s.policy.AllowPrivateDestinations {
		return nil
	}
	reject := func() error {
		return fleeterror.NewInvalidArgumentErrorf(
			"destination host %q is a private or internal address; only external destinations are allowed", host)
	}
	var ips []net.IP
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		ips = []net.IP{ip}
	} else {
		lower := strings.ToLower(strings.TrimSuffix(host, "."))
		if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
			return reject()
		}
		lookupCtx, cancel := context.WithTimeout(ctx, destinationLookupTimeout)
		defer cancel()
		resolved, err := net.DefaultResolver.LookupIP(lookupCtx, "ip", host)
		if err != nil || len(resolved) == 0 {
			return fleeterror.NewInvalidArgumentErrorf(
				"destination host %q could not be resolved; refusing a destination we cannot classify", host)
		}
		ips = resolved
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() || isReservedIP(ip) {
			return reject()
		}
	}
	return nil
}

// Non-public ranges net.IP.IsPrivate misses (CGNAT, benchmarking, reserved); blocked so internal-only deployments stay off-limits.
var reservedDestinationCIDRs = parseCIDRs("100.64.0.0/10", "198.18.0.0/15", "240.0.0.0/4")

func parseCIDRs(specs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(specs))
	for _, s := range specs {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			panic(err)
		}
		nets = append(nets, n)
	}
	return nets
}

func isReservedIP(ip net.IP) bool {
	for _, n := range reservedDestinationCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Service) ListRules(ctx context.Context, orgID int64) ([]Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	rules, err := s.grafana.ListAlertRules(ctx)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatInt(orgID, 10)
	out := make([]Rule, 0, len(rules))
	for _, gr := range rules {
		if !ruleVisibleToOrg(gr, want) {
			continue
		}
		out = append(out, grafanaRuleToDomain(orgID, gr))
	}
	paused := s.pauseSilencedRules(ctx, orgID)
	if len(paused) > 0 {
		for i := range out {
			if paused[out[i].ID] {
				out[i].Enabled = false
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Mutes via a marker pause-silence rather than flipping isPaused: Grafana 11.6+ forbids the provisioning API from editing YAML-provisioned rules.
func (s *Service) PauseRule(ctx context.Context, orgID int64, id, actor string) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	rule, err := s.requireRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if !rule.Enabled {
		return rule, nil
	}
	silence := buildPauseSilence(orgID, id, actor, s.now())
	if _, err := s.grafana.PutSilence(ctx, silence); err != nil {
		return nil, err
	}
	out := *rule
	out.Enabled = false
	return &out, nil
}

// Clears any active pause silence; a YAML-provisioned isPaused still keeps the rule paused.
func (s *Service) ResumeRule(ctx context.Context, orgID int64, id string) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	_, err := s.requireRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatInt(orgID, 10)
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return nil, err
	}
	for _, sil := range sils {
		if !isPauseSilenceFor(sil, want, id) {
			continue
		}
		if sil.Status != nil && sil.Status.State == "expired" {
			continue
		}
		if err := s.grafana.DeleteSilence(ctx, sil.ID); err != nil && !IsNotFound(err) {
			return nil, err
		}
	}
	updated, err := s.requireRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Service) requireRule(ctx context.Context, orgID int64, id string) (*Rule, error) {
	if id == "" {
		return nil, errors.New("rule id is required")
	}
	rules, err := s.ListRules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i], nil
		}
	}
	return nil, ErrNotFound
}

// Best-effort: a fetch error returns an empty map so the list still renders.
func (s *Service) pauseSilencedRules(ctx context.Context, orgID int64) map[string]bool {
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return nil
	}
	want := strconv.FormatInt(orgID, 10)
	now := s.now()
	out := map[string]bool{}
	for _, sil := range sils {
		if !isPauseSilence(sil) {
			continue
		}
		if !silenceMatchesOrg(sil, want) {
			continue
		}
		if !maintenanceWindowActive(grafanaSilenceToDomain(orgID, sil, now), now) {
			continue
		}
		for _, m := range sil.Matchers {
			if m.Name == alertRuleUIDMatcher && m.IsEqual && !m.IsRegex {
				out[m.Value] = true
			}
		}
	}
	return out
}

func (s *Service) ListMaintenanceWindows(ctx context.Context, orgID int64) ([]MaintenanceWindow, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatInt(orgID, 10)
	now := s.now()
	out := make([]MaintenanceWindow, 0, len(sils))
	for _, gs := range sils {
		if !silenceMatchesOrg(gs, want) {
			continue
		}
		// Hide pause silences; they're an implementation detail of PauseRule.
		if isPauseSilence(gs) {
			continue
		}
		dom := grafanaSilenceToDomain(orgID, gs, now)
		out = append(out, dom)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.After(out[j].StartsAt) })
	return out, nil
}

func (s *Service) CreateMaintenanceWindow(ctx context.Context, orgID int64, sil MaintenanceWindow) (*MaintenanceWindow, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateMaintenanceWindowScope(sil.Scope); err != nil {
		return nil, err
	}
	if err := validateMaintenanceWindowComment(sil.Comment); err != nil {
		return nil, err
	}
	if err := validateMaintenanceWindowTimes(sil.StartsAt, sil.EndsAt); err != nil {
		return nil, err
	}
	if err := s.requireScopeTargetVisible(ctx, orgID, sil.Scope); err != nil {
		return nil, err
	}
	sil.OrganizationID = orgID
	sil.CreatedAt = s.now()
	gs := maintenanceWindowToGrafanaSilence(orgID, sil)
	id, err := s.grafana.PutSilence(ctx, gs)
	if err != nil {
		return nil, err
	}
	sil.ID = id
	sil.Active = maintenanceWindowActive(sil, s.now())
	return &sil, nil
}

// Grafana has no dedicated update endpoint; POST with the existing id replaces.
func (s *Service) UpdateMaintenanceWindow(ctx context.Context, orgID int64, sil MaintenanceWindow) (*MaintenanceWindow, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if sil.ID == "" {
		return nil, errors.New("maintenance window id is required for update")
	}
	if err := validateMaintenanceWindowScope(sil.Scope); err != nil {
		return nil, err
	}
	if err := validateMaintenanceWindowComment(sil.Comment); err != nil {
		return nil, err
	}
	if err := validateMaintenanceWindowTimes(sil.StartsAt, sil.EndsAt); err != nil {
		return nil, err
	}
	if err := s.requireScopeTargetVisible(ctx, orgID, sil.Scope); err != nil {
		return nil, err
	}
	existing, err := s.ListMaintenanceWindows(ctx, orgID)
	if err != nil {
		return nil, err
	}
	owned := false
	for _, e := range existing {
		if e.ID == sil.ID {
			owned = true
			// Carry the original creator; the update request has no created_by, so a blank would wipe the audit owner.
			sil.CreatedBy = e.CreatedBy
			break
		}
	}
	if !owned {
		return nil, ErrNotFound
	}
	sil.OrganizationID = orgID
	gs := maintenanceWindowToGrafanaSilence(orgID, sil)
	gs.ID = sil.ID
	id, err := s.grafana.PutSilence(ctx, gs)
	if err != nil {
		return nil, err
	}
	sil.ID = id
	sil.Active = maintenanceWindowActive(sil, s.now())
	return &sil, nil
}

func (s *Service) DeleteMaintenanceWindow(ctx context.Context, orgID int64, id string) error {
	if err := requireOrg(orgID); err != nil {
		return err
	}
	existing, err := s.ListMaintenanceWindows(ctx, orgID)
	if err != nil {
		return err
	}
	owned := false
	for _, e := range existing {
		if e.ID == id {
			owned = true
			break
		}
	}
	if !owned {
		return ErrNotFound
	}
	if err := s.grafana.DeleteSilence(ctx, id); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

// Rejects targetless scopes, which would compile to just the org matcher and silence every alert in the organization.
func validateMaintenanceWindowScope(scope MaintenanceWindowScope) error {
	switch scope.Kind {
	case MaintenanceWindowScopeRule:
		if scope.RuleID == "" {
			return fleeterror.NewInvalidArgumentError("rule_id is required for a rule-scoped maintenance window")
		}
	case MaintenanceWindowScopeGroup, MaintenanceWindowScopeSite:
		// Not yet supported: a group/site silence would emit a group_id/site_id matcher,
		// but the provisioned alert rules only label instances with organization_id and
		// device_id, so the silence would be saved and shown active while muting nothing.
		// Reject until the alert queries emit the matching label (see proto-fleet-rules.yaml).
		return fleeterror.NewInvalidArgumentErrorf("maintenance window scope %q is not yet supported", scope.Kind)
	case MaintenanceWindowScopeDevice:
		if len(scope.DeviceIDs) == 0 {
			return fleeterror.NewInvalidArgumentError("device_ids is required for a device-scoped maintenance window")
		}
		if len(scope.DeviceIDs) > maxMaintenanceWindowDeviceIDs {
			return fleeterror.NewInvalidArgumentErrorf("too many device_ids: %d (max %d)", len(scope.DeviceIDs), maxMaintenanceWindowDeviceIDs)
		}
		// Restrict ids to the identifier alphabet so a crafted id like ".*" can't broaden the silence to the whole org.
		for _, id := range scope.DeviceIDs {
			if !deviceIDPattern.MatchString(id) {
				return fleeterror.NewInvalidArgumentErrorf("invalid device id: %q", id)
			}
		}
	default:
		return fleeterror.NewInvalidArgumentErrorf("unknown maintenance window scope kind: %q", scope.Kind)
	}
	return nil
}

// For a rule-scoped window, confirm the target rule is one the caller can actually see
// (same check PauseRule uses), so a manage user can't silence a rule they can't list or a
// guessed/future rule UID. Group/site/device scopes carry no such existence check yet.
func (s *Service) requireScopeTargetVisible(ctx context.Context, orgID int64, scope MaintenanceWindowScope) error {
	if scope.Kind != MaintenanceWindowScopeRule {
		return nil
	}
	_, err := s.requireRule(ctx, orgID, scope.RuleID)
	return err
}

// A maintenance window and a pause silence are distinguished only by the pause comment
// marker, so reject a window comment that carries it: otherwise a same-org caller could
// hide a window from the list and have it overlaid as a paused rule.
func validateMaintenanceWindowComment(comment string) error {
	if strings.Contains(comment, pauseSilenceCommentMarker) {
		return fleeterror.NewInvalidArgumentError("comment may not contain a reserved marker")
	}
	return nil
}

// Maintenance windows are finite: the UI enforces this, but a direct RPC could omit ends_at
// (which would compile to the far-future sentinel and silence alerts for decades) or pass an
// end at/before the start. Indefinite suppression is only available via PauseRule.
func validateMaintenanceWindowTimes(startsAt, endsAt time.Time) error {
	if startsAt.IsZero() {
		return fleeterror.NewInvalidArgumentError("starts_at is required for a maintenance window")
	}
	if endsAt.IsZero() {
		return fleeterror.NewInvalidArgumentError("ends_at is required for a maintenance window")
	}
	if !endsAt.After(startsAt) {
		return fleeterror.NewInvalidArgumentError("ends_at must be after starts_at")
	}
	return nil
}

const maxMaintenanceWindowDeviceIDs = 500

// Excludes every regex metacharacter except "." (which maintenanceWindowToGrafanaSilence escapes).
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// A pause silence is structurally identical to a rule-scoped maintenance window
// (org + alert-rule-UID matchers), so it carries a marker to tell the two apart.
// The marker lives in the comment, NOT in a matcher: Alertmanager ANDs every matcher
// against an alert's labels, and no provisioned rule emits a marker label, so a marker
// matcher would mute nothing while pauseSilencedRules still reported the rule as paused.
const pauseSilenceCommentMarker = "[proto-fleet:rule-paused]"

// Grafana's reserved matcher label scoping a silence to a single alert rule.
const alertRuleUIDMatcher = "__alert_rule_uid__"

// Far-future end time making a pause behave as indefinite; Resume removes the silence before it expires.
var pauseSilenceEndsAt = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

func buildPauseSilence(orgID int64, ruleID, actor string, now time.Time) GrafanaSilence {
	// Attribute the indefinite mute to the operator who paused, so suppression of a
	// critical rule is auditable; fall back to the app name when the actor is unknown.
	createdBy := actor
	comment := pauseSilenceCommentMarker + " Paused via Proto Fleet UI"
	if createdBy == "" {
		createdBy = "Proto Fleet"
	} else {
		comment += " by " + actor
	}
	return GrafanaSilence{
		StartsAt:  now,
		EndsAt:    pauseSilenceEndsAt,
		CreatedBy: createdBy,
		Comment:   comment,
		Matchers: []GrafanaSilenceMatcher{
			{
				Name:    silenceLabelOrganizationID,
				Value:   strconv.FormatInt(orgID, 10),
				IsEqual: true,
			},
			{
				Name:    alertRuleUIDMatcher,
				Value:   ruleID,
				IsEqual: true,
			},
		},
	}
}

func isPauseSilence(sil GrafanaSilence) bool {
	return strings.HasPrefix(sil.Comment, pauseSilenceCommentMarker)
}

func isPauseSilenceFor(sil GrafanaSilence, wantOrgID, ruleID string) bool {
	if !isPauseSilence(sil) {
		return false
	}
	if !silenceMatchesOrg(sil, wantOrgID) {
		return false
	}
	for _, m := range sil.Matchers {
		if m.Name == alertRuleUIDMatcher && m.Value == ruleID && m.IsEqual && !m.IsRegex {
			return true
		}
	}
	return false
}

const ruleLabelOrganizationID = "organization_id"

// Operator-only rules (e.g. self-monitoring) set proto_fleet_scope=internal to stay hidden
// from every org. A rule with no org label and no internal marker is a shared default that
// monitors all orgs' devices and is visible to each of them.
const (
	ruleLabelScope    = "proto_fleet_scope"
	ruleScopeInternal = "internal"
)

const silenceLabelOrganizationID = "organization_id"

// Grafana doesn't sandbox by org, so we sandbox by name prefix.
func channelNamePrefix(orgID int64) string {
	return fmt.Sprintf("org-%d-", orgID)
}

func channelGrafanaName(orgID int64, name string) string {
	return channelNamePrefix(orgID) + name
}

func channelDisplayName(orgID int64, grafanaName string) string {
	return strings.TrimPrefix(grafanaName, channelNamePrefix(orgID))
}

func grafanaTypeFor(kind ChannelKind) string {
	switch kind {
	case ChannelKindWebhook:
		return "webhook"
	case ChannelKindSlack:
		return "slack"
	}
	return ""
}

func encodeChannelSettings(c *Channel) (json.RawMessage, error) {
	switch c.Kind {
	case ChannelKindWebhook:
		if c.Webhook == nil {
			return nil, errors.New("webhook config is required")
		}
		settings := map[string]any{
			"url":                       c.Webhook.URL,
			"authorization_scheme":      "Bearer",
			"authorization_credentials": c.Webhook.BearerHeader,
		}
		c.HasSecret = c.Webhook.BearerHeader != ""
		b, err := json.Marshal(settings)
		if err != nil {
			return nil, fmt.Errorf("marshal webhook settings: %w", err)
		}
		return b, nil
	case ChannelKindSlack:
		if c.Slack == nil {
			return nil, errors.New("slack config is required")
		}
		// Omit the URL when empty so carrySecretSettings can fill it on a stored-destination edit.
		settings := map[string]any{}
		if c.Slack.WebhookURL != "" {
			settings["url"] = c.Slack.WebhookURL
		}
		c.HasSecret = c.Slack.WebhookURL != ""
		b, err := json.Marshal(settings)
		if err != nil {
			return nil, fmt.Errorf("marshal slack settings: %w", err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("unsupported channel kind %q", c.Kind)
}

// Reduces a webhook URL to scheme://host[:port], dropping userinfo/path/query/fragment where capability tokens live.
func redactWebhookURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func webhookURLFromSettings(raw json.RawMessage) string {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(raw, &settings); err != nil {
		return ""
	}
	v, ok := settings["url"]
	if !ok {
		return ""
	}
	var url string
	_ = json.Unmarshal(v, &url)
	return url
}

// Returns HasSecret but never the secret value.
func contactPointToChannel(orgID int64, cp GrafanaContactPoint) (Channel, error) {
	out := Channel{
		ID:             cp.UID,
		OrganizationID: orgID,
		Name:           channelDisplayName(orgID, cp.Name),
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(cp.Settings, &settings); err != nil {
		return Channel{}, fmt.Errorf("unmarshal contact point settings: %w", err)
	}
	switch cp.Type {
	case "webhook":
		out.Kind = ChannelKindWebhook
		var url string
		if raw, ok := settings["url"]; ok {
			_ = json.Unmarshal(raw, &url)
		}
		// Host-only: webhook URLs embed capability tokens reachable by notification:read holders.
		out.Webhook = &WebhookConfig{URL: redactWebhookURL(url)}
		if raw, ok := settings["authorization_credentials"]; ok && len(raw) > 0 && string(raw) != `""` {
			out.HasSecret = true
		}
	case "slack":
		out.Kind = ChannelKindSlack
		// The URL is the secret; expose presence only, not even the placeholder.
		out.Slack = &SlackConfig{}
		if raw, ok := settings["url"]; ok && len(raw) > 0 && string(raw) != `""` {
			out.HasSecret = true
		}
	}
	// Default to pending; loading the real last-validated state on every list is too expensive.
	out.ValidationState = ValidationPending
	return out, nil
}

func ruleVisibleToOrg(r GrafanaAlertRule, wantOrgID string) bool {
	// Operator-only rules (self-monitoring) are hidden from every org.
	if r.Labels[ruleLabelScope] == ruleScopeInternal {
		return false
	}
	// A tenant-specific rule carries an org label and is visible only to that org.
	if got, ok := r.Labels[ruleLabelOrganizationID]; ok {
		return got == wantOrgID
	}
	// Otherwise it's a shared default rule that monitors every org's devices.
	return true
}

func grafanaRuleToDomain(orgID int64, r GrafanaAlertRule) Rule {
	out := Rule{
		ID:              r.UID,
		OrganizationID:  orgID,
		Name:            r.Title,
		Group:           r.RuleGroup,
		Enabled:         !r.IsPaused,
		DurationSeconds: parseDurationSeconds(r.For),
	}
	if r.Labels != nil {
		out.Template = templateFromLabel(r.Labels["template"])
		out.Severity = r.Labels["severity"]
	}
	if r.Annotations != nil {
		out.Summary = r.Annotations["summary"]
		out.Description = r.Annotations["description"]
	}
	return out
}

func templateFromLabel(label string) RuleTemplate {
	switch label {
	case "offline":
		return RuleTemplateOffline
	case "hashrate":
		return RuleTemplateHashrate
	case "temperature":
		return RuleTemplateTemperature
	case "pool":
		return RuleTemplatePool
	case "command_failure":
		return RuleTemplateCommandFailure
	case "telemetry-poll":
		return RuleTemplateTelemetryPoll
	}
	return ""
}

func parseDurationSeconds(s string) int32 {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	secs := int64(d / time.Second)
	if secs > math.MaxInt32 {
		return math.MaxInt32
	}
	if secs < math.MinInt32 {
		return math.MinInt32
	}
	return int32(secs)
}

func silenceMatchesOrg(s GrafanaSilence, wantOrgID string) bool {
	for _, m := range s.Matchers {
		if m.Name == silenceLabelOrganizationID && m.IsEqual && !m.IsRegex && m.Value == wantOrgID {
			return true
		}
	}
	return false
}

func grafanaSilenceToDomain(orgID int64, gs GrafanaSilence, now time.Time) MaintenanceWindow {
	out := MaintenanceWindow{
		ID:             gs.ID,
		OrganizationID: orgID,
		StartsAt:       gs.StartsAt,
		EndsAt:         gs.EndsAt,
		Comment:        gs.Comment,
		CreatedBy:      gs.CreatedBy,
	}
	// The Alertmanager API exposes no created_at, so approximate it with StartsAt.
	out.CreatedAt = gs.StartsAt

	out.Scope = matchersToScope(gs.Matchers)
	out.Active = maintenanceWindowActive(out, now)
	return out
}

func matchersToScope(ms []GrafanaSilenceMatcher) MaintenanceWindowScope {
	scope := MaintenanceWindowScope{Kind: MaintenanceWindowScopeRule}
	for _, m := range ms {
		switch m.Name {
		case "alertname_uid", alertRuleUIDMatcher:
			scope.Kind = MaintenanceWindowScopeRule
			scope.RuleID = m.Value
		case "group_id":
			scope.Kind = MaintenanceWindowScopeGroup
			scope.GroupID = m.Value
		case "site_id":
			scope.Kind = MaintenanceWindowScopeSite
			scope.SiteID = m.Value
		case "device_id":
			scope.Kind = MaintenanceWindowScopeDevice
			// A regex matcher holds many ids as `^(?:id1|id2)$`; strip anchors and escapes to recover the plain list.
			if m.IsRegex {
				v := strings.TrimSuffix(strings.TrimPrefix(m.Value, "^(?:"), ")$")
				for id := range strings.SplitSeq(v, "|") {
					scope.DeviceIDs = append(scope.DeviceIDs, strings.ReplaceAll(id, `\`, ""))
				}
			} else {
				scope.DeviceIDs = append(scope.DeviceIDs, m.Value)
			}
		}
	}
	return scope
}

func maintenanceWindowToGrafanaSilence(orgID int64, sil MaintenanceWindow) GrafanaSilence {
	matchers := []GrafanaSilenceMatcher{
		{
			Name:    silenceLabelOrganizationID,
			Value:   strconv.FormatInt(orgID, 10),
			IsRegex: false,
			IsEqual: true,
		},
	}
	switch sil.Scope.Kind {
	case MaintenanceWindowScopeRule:
		if sil.Scope.RuleID != "" {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    alertRuleUIDMatcher,
				Value:   sil.Scope.RuleID,
				IsEqual: true,
			})
		}
	case MaintenanceWindowScopeGroup:
		if sil.Scope.GroupID != "" {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    "group_id",
				Value:   sil.Scope.GroupID,
				IsEqual: true,
			})
		}
	case MaintenanceWindowScopeSite:
		if sil.Scope.SiteID != "" {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    "site_id",
				Value:   sil.Scope.SiteID,
				IsEqual: true,
			})
		}
	case MaintenanceWindowScopeDevice:
		if len(sil.Scope.DeviceIDs) == 1 {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    "device_id",
				Value:   sil.Scope.DeviceIDs[0],
				IsEqual: true,
			})
		} else if len(sil.Scope.DeviceIDs) > 1 {
			// Anchor the alternation so a partial match can't widen the silence to substring-containing ids.
			quoted := make([]string, len(sil.Scope.DeviceIDs))
			for i, id := range sil.Scope.DeviceIDs {
				quoted[i] = regexp.QuoteMeta(id)
			}
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    "device_id",
				Value:   "^(?:" + strings.Join(quoted, "|") + ")$",
				IsRegex: true,
				IsEqual: true,
			})
		}
	}
	// Alertmanager requires a concrete endsAt; represent an open-ended mute with the far-future sentinel.
	endsAt := sil.EndsAt
	if endsAt.IsZero() {
		endsAt = pauseSilenceEndsAt
	}
	return GrafanaSilence{
		StartsAt:  sil.StartsAt,
		EndsAt:    endsAt,
		CreatedBy: sil.CreatedBy,
		Comment:   sil.Comment,
		Matchers:  matchers,
	}
}

// A zero EndsAt means indefinite.
func maintenanceWindowActive(s MaintenanceWindow, now time.Time) bool {
	if now.Before(s.StartsAt) {
		return false
	}
	if s.EndsAt.IsZero() {
		return true
	}
	return now.Before(s.EndsAt)
}
