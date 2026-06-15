package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Service is the org-scoped domain layer; org isolation is enforced here via name prefixes (channels), label/matcher filtering (rules, silences), and there is deliberately no rule create/update/delete.
type Service struct {
	grafana *Grafana
	policy  DestinationPolicy
	now     func() time.Time
}

// DestinationPolicy is the egress policy for notification destinations: Grafana opens the connection, so user-supplied URLs/hosts are an SSRF vector and private ranges are rejected by default.
type DestinationPolicy struct {
	AllowPrivateDestinations bool `help:"Allow notification destinations (webhook URLs, SMTP hosts) that resolve to loopback, link-local, or private network ranges. Enable for dev stacks or deployments whose relays live on internal addresses." default:"false" env:"ALLOW_PRIVATE_DESTINATIONS"`
}

func NewService(g *Grafana, policy DestinationPolicy) *Service {
	return &Service{grafana: g, policy: policy, now: time.Now}
}

var ErrZeroOrgID = errors.New("notifications: organization id is required")

// ErrNotFound is returned when a Grafana row exists but isn't owned by the caller's org; surfaced as permission_denied so id scans aren't a list oracle.
var ErrNotFound = errors.New("notifications: not found")

func requireOrg(orgID int64) error {
	if orgID == 0 {
		return ErrZeroOrgID
	}
	return nil
}

// ListChannels returns every channel owned by the caller's organization.
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
			// Skip undecodable rows rather than failing the whole list.
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// CreateChannel inserts a new channel for orgID.
func (s *Service) CreateChannel(ctx context.Context, orgID int64, c Channel) (*Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := s.validateDestination(ctx, &c); err != nil {
		return nil, err
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
		Name:     channelGrafanaName(orgID, c.Name),
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

// UpdateChannel replaces a channel's name and destination, clearing the validation state.
func (s *Service) UpdateChannel(ctx context.Context, orgID int64, c Channel) (*Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if c.ID == "" {
		return nil, errors.New("channel id is required for update")
	}
	// Verify ownership before the PUT (Grafana doesn't enforce our prefix scheme) and resolve the carried-over URL before validation.
	owned, ownedCP, err := s.findOwnedChannel(ctx, orgID, c.ID)
	if err != nil {
		return nil, err
	}
	// Whether the destination changed gates secret preservation below: a stored secret must never be carried onto a new destination. Reads echo back a redacted host, so an empty/redacted URL means "unchanged" and restores the stored full URL.
	destinationChanged := false
	keepStoredSlackURL := false
	switch c.Kind {
	case ChannelKindWebhook:
		if c.Webhook != nil {
			stored := webhookURLFromSettings(ownedCP.Settings)
			if c.Webhook.URL == "" || c.Webhook.URL == redactWebhookURL(stored) {
				c.Webhook.URL = stored
			}
			destinationChanged = c.Webhook.URL != stored
		}
	case ChannelKindSMTP:
		// The SMTP server (host:port) is the credential's audience; recipient/name changes are not destination changes.
		if c.SMTP != nil && owned.SMTP != nil {
			destinationChanged = c.SMTP.Host != owned.SMTP.Host || c.SMTP.Port != owned.SMTP.Port
		}
	case ChannelKindSlack:
		// Slack URLs are write-only secrets: an edit without a fresh URL keeps the stored destination, a fresh URL is both a destination change and a new secret.
		keepStoredSlackURL = c.Slack == nil || c.Slack.WebhookURL == ""
		if c.Slack == nil {
			c.Slack = &SlackConfig{}
		}
		destinationChanged = !keepStoredSlackURL
	}
	// A kept stored Slack URL has nothing new to validate.
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
	// Edits without a secret carry the stored field forward (writing empty would wipe it), but only when the destination is unchanged so the old credential can't be delivered to a new destination.
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
	updated, err := s.grafana.UpdateContactPoint(ctx, c.ID, cp)
	if err != nil {
		return nil, err
	}
	out, err := contactPointToChannel(orgID, *updated)
	if err != nil {
		return nil, err
	}
	out.HasSecret = c.HasSecret
	return &out, nil
}

// DeleteChannel removes the channel from Grafana; missing rows make the delete idempotent.
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

// TestChannel sends a synthetic alert through the supplied channel definition; the id is optional so an unsaved definition can be tested directly.
func (s *Service) TestChannel(ctx context.Context, orgID int64, c Channel) (bool, int, string, error) {
	if err := requireOrg(orgID); err != nil {
		return false, 0, "", err
	}

	var body map[string]any
	if c.ID != "" {
		// Saved channel: test the stored settings (full URL + secure fields), since the echoed-back payload is redacted and would probe the wrong target.
		_, ownedCP, err := s.findOwnedChannel(ctx, orgID, c.ID)
		if err != nil {
			return false, 0, "", err
		}
		body = map[string]any{
			"name":     ownedCP.Name,
			"type":     ownedCP.Type,
			"settings": ownedCP.Settings,
		}
	} else {
		// Unsaved preview: validate and test the supplied definition directly.
		if err := s.validateDestination(ctx, &c); err != nil {
			return false, 0, "", err
		}
		c.OrganizationID = orgID
		settings, err := encodeChannelSettings(&c)
		if err != nil {
			return false, 0, "", err
		}
		body = map[string]any{
			"name":     channelGrafanaName(orgID, c.Name),
			"type":     grafanaTypeFor(c.Kind),
			"settings": settings,
		}
	}

	code, err := s.grafana.TestContactPoint(ctx, body)
	if err != nil {
		return false, code, err.Error(), err
	}
	ok := code >= 200 && code < 300
	return ok, code, "", nil
}

// findOwnedChannel returns the decoded channel plus the raw contact point it came from (needed to carry secret settings the decoded Channel drops).
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

// requestHasNewSecret reports whether the update payload includes a fresh secret value rather than the empty placeholder reads return.
func (s *Service) requestHasNewSecret(c *Channel) bool {
	switch c.Kind {
	case ChannelKindWebhook:
		return c.Webhook != nil && c.Webhook.BearerHeader != ""
	case ChannelKindSMTP:
		return c.SMTP != nil && c.SMTP.Password != ""
	case ChannelKindSlack:
		return c.Slack != nil && c.Slack.WebhookURL != ""
	}
	return false
}

// secretSettingsKeyFor names the Grafana settings field that carries the kind's secret, or "" for none.
func secretSettingsKeyFor(kind ChannelKind) string {
	switch kind {
	case ChannelKindWebhook:
		return "authorization_credentials"
	case ChannelKindSMTP:
		return "smtpPassword"
	case ChannelKindSlack:
		return "url"
	}
	return ""
}

// carrySecretSettings copies the secret field from the existing settings into the update payload, returning the settings and whether a secret was carried.
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

// validateDestination rejects malformed or policy-violating destinations before they reach Grafana, which is what connects out (an SSRF vector otherwise).
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
	case ChannelKindSMTP:
		if c.SMTP == nil || c.SMTP.Host == "" {
			return fleeterror.NewInvalidArgumentError("smtp host is required")
		}
		if len(c.SMTP.To) == 0 {
			return fleeterror.NewInvalidArgumentError("at least one smtp recipient is required")
		}
		return s.checkDestinationHost(ctx, c.SMTP.Host)
	}
	return nil
}

// checkDestinationURL applies the URL-shaped half of the policy: parseable, http(s) scheme, host present and passing checkDestinationHost.
func (s *Service) checkDestinationURL(ctx context.Context, raw, label string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fleeterror.NewInvalidArgumentError("invalid " + label + " url: " + err.Error())
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fleeterror.NewInvalidArgumentErrorf("%s url scheme must be http or https, got %q", label, u.Scheme)
	}
	if u.Hostname() == "" {
		return fleeterror.NewInvalidArgumentError(label + " url must include a host")
	}
	return s.checkDestinationHost(ctx, u.Hostname())
}

// destinationLookupTimeout bounds the DNS check so a slow resolver can't hang a save.
const destinationLookupTimeout = 3 * time.Second

// checkDestinationHost rejects hosts that are or resolve to loopback/link-local/private/unspecified addresses (unless opted in); DNS failures fail closed. Not rebinding-proof — egress enforcement at Grafana's network boundary is the hard guarantee.
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
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return reject()
		}
	}
	return nil
}

// ListRules returns the provisioned alert rules visible to the caller (unlabelled rules are global defaults); Enabled ORs the rule's own isPaused with any active pause-silence overlay.
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
	// Pause-silence overlay: an active pause silence forces Enabled=false.
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

// PauseRule mutes a rule via a marker pause-silence rather than flipping isPaused, since Grafana 11.6+ forbids the provisioning API from editing YAML-provisioned rules. Idempotent.
func (s *Service) PauseRule(ctx context.Context, orgID int64, id string) (*Rule, error) {
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
	silence := buildPauseSilence(orgID, id, s.now())
	if _, err := s.grafana.PutSilence(ctx, silence); err != nil {
		return nil, err
	}
	out := *rule
	out.Enabled = false
	return &out, nil
}

// ResumeRule clears any active pause silence; a YAML-provisioned isPaused still keeps the rule paused. Idempotent.
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
	// Re-fetch so Enabled reflects the now-cleared pause silences.
	updated, err := s.requireRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// requireRule looks up a rule visible to orgID via ListRules, returning ErrNotFound if missing.
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

// pauseSilencedRules returns the rule UIDs with an active pause silence; best-effort, a fetch error returns an empty map so the list still renders.
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
		if !silenceActive(grafanaSilenceToDomain(orgID, sil, now), now) {
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

// ListSilences returns every silence carrying the caller's org matcher.
func (s *Service) ListSilences(ctx context.Context, orgID int64) ([]Silence, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatInt(orgID, 10)
	now := s.now()
	out := make([]Silence, 0, len(sils))
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

// CreateSilence inserts a new silence, always including the caller's org id matcher.
func (s *Service) CreateSilence(ctx context.Context, orgID int64, sil Silence) (*Silence, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateSilenceScope(sil.Scope); err != nil {
		return nil, err
	}
	sil.OrganizationID = orgID
	sil.CreatedAt = s.now()
	gs := domainSilenceToGrafana(orgID, sil)
	id, err := s.grafana.PutSilence(ctx, gs)
	if err != nil {
		return nil, err
	}
	sil.ID = id
	sil.Active = silenceActive(sil, s.now())
	return &sil, nil
}

// UpdateSilence replaces an existing silence (Grafana has no dedicated update endpoint; POST with the existing id replaces).
func (s *Service) UpdateSilence(ctx context.Context, orgID int64, sil Silence) (*Silence, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if sil.ID == "" {
		return nil, errors.New("silence id is required for update")
	}
	if err := validateSilenceScope(sil.Scope); err != nil {
		return nil, err
	}
	existing, err := s.ListSilences(ctx, orgID)
	if err != nil {
		return nil, err
	}
	owned := false
	for _, e := range existing {
		if e.ID == sil.ID {
			owned = true
			break
		}
	}
	if !owned {
		return nil, ErrNotFound
	}
	sil.OrganizationID = orgID
	gs := domainSilenceToGrafana(orgID, sil)
	gs.ID = sil.ID
	id, err := s.grafana.PutSilence(ctx, gs)
	if err != nil {
		return nil, err
	}
	sil.ID = id
	sil.Active = silenceActive(sil, s.now())
	return &sil, nil
}

// DeleteSilence (a "lift") removes the silence from Grafana.
func (s *Service) DeleteSilence(ctx context.Context, orgID int64, id string) error {
	if err := requireOrg(orgID); err != nil {
		return err
	}
	existing, err := s.ListSilences(ctx, orgID)
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

// validateSilenceScope rejects targetless scopes, which would compile to just the org matcher and silence every alert in the organization.
func validateSilenceScope(scope SilenceScope) error {
	switch scope.Kind {
	case SilenceScopeRule:
		if scope.RuleID == "" {
			return fleeterror.NewInvalidArgumentError("rule_id is required for a rule-scoped silence")
		}
	case SilenceScopeGroup:
		if scope.GroupID == "" {
			return fleeterror.NewInvalidArgumentError("group_id is required for a group-scoped silence")
		}
	case SilenceScopeSite:
		if scope.SiteID == "" {
			return fleeterror.NewInvalidArgumentError("site_id is required for a site-scoped silence")
		}
	case SilenceScopeDevice:
		if len(scope.DeviceIDs) == 0 {
			return fleeterror.NewInvalidArgumentError("device_ids is required for a device-scoped silence")
		}
		if len(scope.DeviceIDs) > maxSilenceDeviceIDs {
			return fleeterror.NewInvalidArgumentErrorf("too many device_ids: %d (max %d)", len(scope.DeviceIDs), maxSilenceDeviceIDs)
		}
		// Restrict ids to the identifier alphabet so a crafted id like ".*" can't broaden the silence to the whole org.
		for _, id := range scope.DeviceIDs {
			if !deviceIDPattern.MatchString(id) {
				return fleeterror.NewInvalidArgumentErrorf("invalid device id: %q", id)
			}
		}
	default:
		return fleeterror.NewInvalidArgumentErrorf("unknown silence scope kind: %q", scope.Kind)
	}
	return nil
}

// maxSilenceDeviceIDs caps the device list so a request can't compile to an unbounded matcher or oversized write.
const maxSilenceDeviceIDs = 500

// deviceIDPattern is the identifier alphabet for device ids, excluding every regex metacharacter except "." (which domainSilenceToGrafana escapes).
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// pauseSilenceMatcher is the marker matcher distinguishing a system pause from an operator-authored silence.
const pauseSilenceMatcher = "proto_fleet_pause"

// alertRuleUIDMatcher is Grafana's reserved matcher label scoping a silence to a single alert rule.
const alertRuleUIDMatcher = "__alert_rule_uid__"

// pauseSilenceEndsAt is a far-future end time making a pause behave as indefinite; Resume removes the silence before it expires.
var pauseSilenceEndsAt = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

// buildPauseSilence assembles the pause silence for a rule, carrying org-id, alert-rule-uid, and pause-marker matchers.
func buildPauseSilence(orgID int64, ruleID string, now time.Time) GrafanaSilence {
	return GrafanaSilence{
		StartsAt:  now,
		EndsAt:    pauseSilenceEndsAt,
		CreatedBy: "Proto Fleet",
		Comment:   "Paused via Proto Fleet UI",
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
			{
				Name:    pauseSilenceMatcher,
				Value:   "true",
				IsEqual: true,
			},
		},
	}
}

// isPauseSilence is true when the silence carries the pause marker matcher.
func isPauseSilence(sil GrafanaSilence) bool {
	for _, m := range sil.Matchers {
		if m.Name == pauseSilenceMatcher && m.Value == "true" && m.IsEqual && !m.IsRegex {
			return true
		}
	}
	return false
}

// isPauseSilenceFor is isPauseSilence narrowed to a specific org and rule.
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

// ruleLabelOrganizationID is the label deciding which org owns a rule; absent on YAML defaults, which every org can see.
const ruleLabelOrganizationID = "organization_id"

const silenceLabelOrganizationID = "organization_id"

// channelNamePrefix is the per-org prefix on every contact point name; Grafana doesn't sandbox by org, so we sandbox by name.
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
	case ChannelKindSMTP:
		return "email"
	case ChannelKindSlack:
		return "slack"
	}
	return ""
}

// encodeChannelSettings serialises the destination fields into the JSON shape Grafana expects.
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
	case ChannelKindSMTP:
		if c.SMTP == nil {
			return nil, errors.New("smtp config is required")
		}
		settings := map[string]any{
			"addresses":    strings.Join(c.SMTP.To, ";"),
			"singleEmail":  false,
			"smtpHost":     c.SMTP.Host,
			"smtpPort":     c.SMTP.Port,
			"smtpUsername": c.SMTP.Username,
			"fromAddress":  c.SMTP.From,
			"fromName":     "Proto Fleet Alerts",
		}
		if c.SMTP.Password != "" {
			settings["smtpPassword"] = c.SMTP.Password
		}
		c.HasSecret = c.SMTP.Password != ""
		b, err := json.Marshal(settings)
		if err != nil {
			return nil, fmt.Errorf("marshal smtp settings: %w", err)
		}
		return b, nil
	case ChannelKindSlack:
		if c.Slack == nil {
			return nil, errors.New("slack config is required")
		}
		// The URL is the only setting and it's the secret; omit it when empty so carrySecretSettings can fill it on a stored-destination edit.
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

// redactWebhookURL reduces a webhook URL to scheme://host[:port], dropping the userinfo/path/query/fragment where capability tokens live. This is the value reads expose.
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

// webhookURLFromSettings pulls the full (unredacted) webhook URL out of a stored contact-point settings blob.
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

// contactPointToChannel reverses encodeChannelSettings on reads, returning HasSecret but never the secret value.
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
	case "email":
		out.Kind = ChannelKindSMTP
		smtp := &SMTPConfig{}
		if raw, ok := settings["addresses"]; ok {
			var addrs string
			_ = json.Unmarshal(raw, &addrs)
			if addrs != "" {
				smtp.To = strings.Split(addrs, ";")
			}
		}
		if raw, ok := settings["smtpHost"]; ok {
			_ = json.Unmarshal(raw, &smtp.Host)
		}
		if raw, ok := settings["smtpPort"]; ok {
			_ = json.Unmarshal(raw, &smtp.Port)
		}
		if raw, ok := settings["smtpUsername"]; ok {
			_ = json.Unmarshal(raw, &smtp.Username)
		}
		if raw, ok := settings["fromAddress"]; ok {
			_ = json.Unmarshal(raw, &smtp.From)
		}
		if raw, ok := settings["smtpPassword"]; ok && len(raw) > 0 && string(raw) != `""` {
			out.HasSecret = true
		}
		out.SMTP = smtp
	case "slack":
		out.Kind = ChannelKindSlack
		// The URL is the secret; expose presence only, not even the placeholder.
		out.Slack = &SlackConfig{}
		if raw, ok := settings["url"]; ok && len(raw) > 0 && string(raw) != `""` {
			out.HasSecret = true
		}
	}
	// Default to pending; loading the real last-validated state on every list is too expensive, so the operator presses Test to refresh.
	out.ValidationState = ValidationPending
	return out, nil
}

// ruleVisibleToOrg is true if the rule carries no organization_id label (default) or one matching the caller.
func ruleVisibleToOrg(r GrafanaAlertRule, wantOrgID string) bool {
	if r.Labels == nil {
		return true
	}
	got, ok := r.Labels[ruleLabelOrganizationID]
	if !ok {
		return true
	}
	return got == wantOrgID
}

// grafanaRuleToDomain pulls the user-facing metadata off a Grafana alert rule.
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

// templateFromLabel maps the YAML `template` label to the UI enum; unknown labels map to "" ("fall back to rule name").
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

// parseDurationSeconds parses Grafana's go-duration string into seconds; unparseable input returns zero ("fires immediately").
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

// silenceMatchesOrg is true if the silence has an `organization_id=<wantOrgID>` equality matcher.
func silenceMatchesOrg(s GrafanaSilence, wantOrgID string) bool {
	for _, m := range s.Matchers {
		if m.Name == silenceLabelOrganizationID && m.IsEqual && !m.IsRegex && m.Value == wantOrgID {
			return true
		}
	}
	return false
}

// grafanaSilenceToDomain reverses domainSilenceToGrafana on reads and stamps Active from the supplied clock.
func grafanaSilenceToDomain(orgID int64, gs GrafanaSilence, now time.Time) Silence {
	out := Silence{
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
	out.Active = silenceActive(out, now)
	return out
}

// matchersToScope reconstructs the structured scope from the matcher list (the inverse of domainSilenceToGrafana).
func matchersToScope(ms []GrafanaSilenceMatcher) SilenceScope {
	scope := SilenceScope{Kind: SilenceScopeRule}
	for _, m := range ms {
		switch m.Name {
		case "alertname_uid", alertRuleUIDMatcher:
			scope.Kind = SilenceScopeRule
			scope.RuleID = m.Value
		case "group_id":
			scope.Kind = SilenceScopeGroup
			scope.GroupID = m.Value
		case "site_id":
			scope.Kind = SilenceScopeSite
			scope.SiteID = m.Value
		case "device_id":
			scope.Kind = SilenceScopeDevice
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

// domainSilenceToGrafana compiles the structured scope to Alertmanager matchers, always including the org-id matcher.
func domainSilenceToGrafana(orgID int64, sil Silence) GrafanaSilence {
	matchers := []GrafanaSilenceMatcher{
		{
			Name:    silenceLabelOrganizationID,
			Value:   strconv.FormatInt(orgID, 10),
			IsRegex: false,
			IsEqual: true,
		},
	}
	switch sil.Scope.Kind {
	case SilenceScopeRule:
		if sil.Scope.RuleID != "" {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    alertRuleUIDMatcher,
				Value:   sil.Scope.RuleID,
				IsEqual: true,
			})
		}
	case SilenceScopeGroup:
		if sil.Scope.GroupID != "" {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    "group_id",
				Value:   sil.Scope.GroupID,
				IsEqual: true,
			})
		}
	case SilenceScopeSite:
		if sil.Scope.SiteID != "" {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    "site_id",
				Value:   sil.Scope.SiteID,
				IsEqual: true,
			})
		}
	case SilenceScopeDevice:
		if len(sil.Scope.DeviceIDs) == 1 {
			matchers = append(matchers, GrafanaSilenceMatcher{
				Name:    "device_id",
				Value:   sil.Scope.DeviceIDs[0],
				IsEqual: true,
			})
		} else if len(sil.Scope.DeviceIDs) > 1 {
			// Escape ids and anchor the alternation so a partial match can't widen the silence to substring-containing ids.
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
	return GrafanaSilence{
		StartsAt:  sil.StartsAt,
		EndsAt:    sil.EndsAt,
		CreatedBy: sil.CreatedBy,
		Comment:   sil.Comment,
		Matchers:  matchers,
	}
}

// silenceActive reports whether now is inside [StartsAt, EndsAt); a zero EndsAt means indefinite.
func silenceActive(s Silence, now time.Time) bool {
	if now.Before(s.StartsAt) {
		return false
	}
	if s.EndsAt.IsZero() {
		return true
	}
	return now.Before(s.EndsAt)
}
