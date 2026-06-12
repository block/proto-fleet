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

// Service is the org-scoped domain layer. All public methods require
// a non-zero organization id and refuse if zero.
//
// The Grafana client is the only outbound transport. Org isolation is
// enforced here by:
//
//   - Channels: every contact-point name is prefixed with `org-<id>-`
//     so a list scoped to a prefix returns only the caller's rows.
//   - Rules: every rule gets `labels.organization_id="<id>"` injected
//     on read filtering. The provisioned defaults ship as org=0 (or
//     no label) and are visible to every org; ops-authored rules
//     carry the label and are visible only to their owner.
//   - Silences: every silence carries an `organization_id="<id>"`
//     matcher; list filters by the same matcher and pause/resume of
//     a silence implicitly recheck ownership.
//
// IMPORTANT: there is no CreateRule / UpdateRule / DeleteRule — by
// product decision. Operators can pause / resume / silence the
// closed set of provisioned rules; new rules require a deploy.
//
// Secrets: webhook bearer headers and SMTP passwords are passed
// straight through to Grafana, which stores them encrypted at rest
// in its own datastore. We do not keep a parallel copy in fleet-api
// — there's nothing for fleet-api to do with them (Grafana is the
// one calling out to the destination), and storing them twice would
// double the rotation surface area.
type Service struct {
	grafana *Grafana
	policy  DestinationPolicy
	now     func() time.Time
}

// DestinationPolicy is the operator-facing egress policy for
// notification destinations. Grafana (not fleet-api) opens the
// outbound connection, so user-supplied webhook URLs and SMTP hosts
// are an SSRF vector against whatever network the Grafana sidecar can
// reach. By default destinations that resolve to loopback, link-local
// (incl. cloud metadata), or RFC-1918 private ranges are rejected.
type DestinationPolicy struct {
	AllowPrivateDestinations bool `help:"Allow notification destinations (webhook URLs, SMTP hosts) that resolve to loopback, link-local, or private network ranges. Enable for dev stacks or deployments whose relays live on internal addresses." default:"false" env:"ALLOW_PRIVATE_DESTINATIONS"`
}

// NewService returns a notifications service bound to the supplied
// Grafana client. The clock is `time.Now` outside tests; tests
// inject a deterministic clock.
func NewService(g *Grafana, policy DestinationPolicy) *Service {
	return &Service{grafana: g, policy: policy, now: time.Now}
}

// ErrZeroOrgID rejects callers that forgot to populate the org id on
// the request. The handler layer is the first line of defence
// (it pulls the org id from the auth interceptor's context); this is
// the second.
var ErrZeroOrgID = errors.New("notifications: organization id is required")

// ErrNotFound is returned when a Grafana row exists but doesn't
// belong to the caller's org — surfaced as permission_denied so a
// scan for ids isn't a list oracle.
var ErrNotFound = errors.New("notifications: not found")

func requireOrg(orgID int64) error {
	if orgID == 0 {
		return ErrZeroOrgID
	}
	return nil
}

// === Channels ==========================================================

// ListChannels returns every channel owned by the caller's
// organization. Secrets are zeroed; HasSecret indicates whether a
// secret is stored.
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
			// Skip rows we can't decode rather than failing the whole
			// list. The UI surfaces a callout if this ever happens
			// repeatedly.
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// CreateChannel inserts a new channel for orgID. Secrets are stored
// in the encrypt service and the secret_ref is embedded in the
// Grafana contact-point settings JSON.
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
	// Preserve the HasSecret flag that encodeChannelSettings set on the
	// local copy — Grafana's response strips the secret value, so the
	// decoder sees an empty string and reports HasSecret=false.
	out.HasSecret = c.HasSecret
	return &out, nil
}

// UpdateChannel replaces a channel's name + destination. Editing the
// destination clears the validation state — the caller is expected
// to re-test the channel before relying on it.
func (s *Service) UpdateChannel(ctx context.Context, orgID int64, c Channel) (*Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if c.ID == "" {
		return nil, errors.New("channel id is required for update")
	}
	// Verify ownership before issuing the PUT — Grafana doesn't
	// enforce our prefix scheme. Fetched before validation so we can
	// resolve the carried-over webhook URL (see below) and validate
	// the URL we will actually write.
	owned, ownedCP, err := s.findOwnedChannel(ctx, orgID, c.ID)
	if err != nil {
		return nil, err
	}
	// Webhook URLs are returned host-only on reads (they embed
	// capability tokens), so an ordinary edit echoes back the redacted
	// host rather than the full URL. Treat an empty or still-redacted
	// URL as "unchanged" and carry the stored full URL through, the
	// same way secrets are preserved. A manage user replacing the
	// destination submits a fresh full URL, which overrides this.
	if c.Kind == ChannelKindWebhook && c.Webhook != nil {
		stored := webhookURLFromSettings(ownedCP.Settings)
		if c.Webhook.URL == "" || c.Webhook.URL == redactWebhookURL(stored) {
			c.Webhook.URL = stored
		}
	}
	if err := s.validateDestination(ctx, &c); err != nil {
		return nil, err
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
	// Ordinary edits (rename, destination change) arrive without the
	// secret — reads never return it, so the UI can't echo it back.
	// Writing the empty value would silently wipe the stored credential
	// in Grafana, so carry the existing settings' secret field into the
	// update payload instead. For webhook channels the carried value is
	// Grafana's "[REDACTED]" placeholder, which Grafana's provisioning
	// API resolves back to the stored secure value on PUT.
	if !hasNewSecret {
		var carried bool
		settings, carried, err = carrySecretSettings(ownedCP.Settings, settings, c.Kind)
		if err != nil {
			return nil, err
		}
		c.HasSecret = owned.HasSecret || carried
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

// DeleteChannel removes the channel from Grafana. Missing rows are
// treated as deletes that already happened — repeated DELETEs are
// idempotent.
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

// TestChannel sends a synthetic alert through the supplied channel
// definition. The id field is optional; an unsaved definition can be
// tested directly so the UI's "Test before save" flow doesn't need a
// prior write.
func (s *Service) TestChannel(ctx context.Context, orgID int64, c Channel) (bool, int, string, error) {
	if err := requireOrg(orgID); err != nil {
		return false, 0, "", err
	}
	if err := s.validateDestination(ctx, &c); err != nil {
		return false, 0, "", err
	}
	c.OrganizationID = orgID
	settings, err := encodeChannelSettings(&c)
	if err != nil {
		return false, 0, "", err
	}
	body := map[string]any{
		"name":     channelGrafanaName(orgID, c.Name),
		"type":     grafanaTypeFor(c.Kind),
		"settings": settings,
	}
	code, err := s.grafana.TestContactPoint(ctx, body)
	if err != nil {
		return false, code, err.Error(), err
	}
	ok := code >= 200 && code < 300
	return ok, code, "", nil
}

// findOwnedChannel returns the decoded channel together with the raw
// Grafana contact point it came from. The raw row is needed by
// UpdateChannel to carry secret settings (which the decoded Channel
// deliberately drops) into the update payload.
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

// requestHasNewSecret tells whether the caller's update payload
// includes a fresh secret value (as opposed to the empty placeholder
// returned by reads).
func (s *Service) requestHasNewSecret(c *Channel) bool {
	switch c.Kind {
	case ChannelKindWebhook:
		return c.Webhook != nil && c.Webhook.BearerHeader != ""
	case ChannelKindSMTP:
		return c.SMTP != nil && c.SMTP.Password != ""
	}
	return false
}

// secretSettingsKeyFor names the Grafana settings field that carries
// the channel kind's secret, or "" for kinds without one.
func secretSettingsKeyFor(kind ChannelKind) string {
	switch kind {
	case ChannelKindWebhook:
		return "authorization_credentials"
	case ChannelKindSMTP:
		return "smtpPassword"
	}
	return ""
}

// carrySecretSettings copies the secret field from the existing
// contact point's settings into the update payload. Returns the
// (possibly rewritten) settings and whether a secret was carried.
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

// validateDestination rejects malformed or policy-violating outbound
// destinations before they reach Grafana. Grafana is the process that
// connects to the destination, so without this check any caller with
// notification:manage could point the sidecar at internal services or
// the cloud metadata endpoint (and TestChannel triggers the connection
// immediately).
func (s *Service) validateDestination(ctx context.Context, c *Channel) error {
	switch c.Kind {
	case ChannelKindWebhook:
		if c.Webhook == nil || c.Webhook.URL == "" {
			return fleeterror.NewInvalidArgumentError("webhook url is required")
		}
		u, err := url.Parse(c.Webhook.URL)
		if err != nil {
			return fleeterror.NewInvalidArgumentError("invalid webhook url: " + err.Error())
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fleeterror.NewInvalidArgumentErrorf("webhook url scheme must be http or https, got %q", u.Scheme)
		}
		if u.Hostname() == "" {
			return fleeterror.NewInvalidArgumentError("webhook url must include a host")
		}
		return s.checkDestinationHost(ctx, u.Hostname())
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

// destinationLookupTimeout bounds the DNS check in
// checkDestinationHost so a slow resolver can't hang a save.
const destinationLookupTimeout = 3 * time.Second

// checkDestinationHost rejects hosts that are (or resolve to)
// loopback, link-local, private, or unspecified addresses unless the
// operator opted in via AllowPrivateDestinations. DNS failures fail
// closed: a host we can't classify is rejected rather than waved
// through.
//
// Residual risk: Grafana resolves the hostname again at delivery
// time, so a DNS-rebinding attacker (public IP at validation, private
// IP at delivery) can still beat this pre-check. Deployments that
// need a hard guarantee must enforce egress at the Grafana container's
// network boundary; this check is the application-level backstop, not
// the whole story.
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

// === Rules =============================================================

// ListRules returns the full set of provisioned alert rules visible
// to the caller. Rules without an `organization_id` label are
// treated as global defaults (visible to every org); rules that
// carry the label are visible only to that org.
//
// A rule's `Enabled` flag reflects two signals OR'd together:
//
//   - The rule's own isPaused state (managed by ops via YAML).
//   - The presence of an active pause-silence on the rule. We can't
//     flip isPaused via the provisioning API on YAML-provisioned
//     rules (Grafana 11.6+ "cannot change provenance from 'file' to
//     ”" guard), so PauseRule below records pauses as a system
//     silence with a marker matcher. ListRules resolves those and
//     reports `Enabled = false` when one is active.
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
	// Apply pause-silence overlay: rules with an active pause silence
	// surface as `Enabled = false` even if the underlying rule's
	// isPaused is false.
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

// PauseRule mutes a rule by writing a "pause silence" into Grafana's
// Alertmanager — a silence with a far-future end time and a marker
// matcher that identifies it as a system pause (not an operator-
// authored silence). We use this rather than flipping isPaused on
// the rule because Grafana 11.6+ refuses to let the provisioning
// API edit a YAML-provisioned rule ("cannot change provenance from
// 'file' to ”"); a silence is the only side-channel that achieves
// the same observable behaviour (no alerts fire) without touching
// the rule's provenance. Idempotent.
func (s *Service) PauseRule(ctx context.Context, orgID int64, id string) (*Rule, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	rule, err := s.requireRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if !rule.Enabled {
		// Already paused — either via the YAML or a prior pause-silence.
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

// ResumeRule clears any active pause silence on the rule. If the
// underlying rule's YAML-provisioned isPaused is true, the rule
// remains paused even after the silence is lifted — that's a
// product decision, since YAML-paused means "ops wants this off",
// which the UI shouldn't override. Idempotent.
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
		// Skip already-expired pause silences — Grafana garbage-collects
		// them eventually, no need to issue a redundant DELETE.
		if sil.Status != nil && sil.Status.State == "expired" {
			continue
		}
		if err := s.grafana.DeleteSilence(ctx, sil.ID); err != nil && !IsNotFound(err) {
			return nil, err
		}
	}
	// Re-fetch through ListRules so the Enabled flag reflects both
	// the rule's own isPaused and the (now-cleared) pause silences.
	updated, err := s.requireRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// requireRule looks up a rule visible to orgID via ListRules and
// returns ErrNotFound if it's missing. Used by Pause / Resume to
// share the visibility check.
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

// pauseSilencedRules returns the set of rule UIDs that have an
// active pause silence on them. Best-effort: a Grafana fetch error
// returns an empty map so the rules list still renders (the user
// just sees stale Enabled flags rather than no rules at all).
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

// === Silences ==========================================================

// ListSilences returns every silence carrying the caller's org
// matcher, with the Active flag derived from Now().
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
		// Pause silences are an implementation detail of PauseRule —
		// the operator sees the rule's "Paused" badge, not a stray
		// silence entry. Hide them here.
		if isPauseSilence(gs) {
			continue
		}
		dom := grafanaSilenceToDomain(orgID, gs, now)
		out = append(out, dom)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartsAt.After(out[j].StartsAt) })
	return out, nil
}

// CreateSilence inserts a new silence. The scope is compiled to the
// Alertmanager matcher set Grafana stores; the caller's org id is
// always one of the matchers.
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

// UpdateSilence replaces an existing silence. Grafana doesn't have a
// dedicated update endpoint — POST with the existing id replaces.
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
	// Verify ownership.
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

// validateSilenceScope rejects scopes without a concrete target. The
// Grafana compiler in domainSilenceToGrafana only emits a target
// matcher when the scope's field is populated — a targetless scope
// would compile to just the org matcher and silence every alert in
// the organization.
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
		// Device ids are compiled into an Alertmanager regex matcher.
		// Restrict them to the identifier alphabet (UUIDs, MACs,
		// serials) so a crafted id like ".*" can't broaden a
		// device-scoped silence to the whole org.
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

// maxSilenceDeviceIDs caps the device list on a device-scoped silence
// so a single request can't compile to an unbounded regex matcher or
// an oversized Grafana write.
const maxSilenceDeviceIDs = 500

// deviceIDPattern is the identifier alphabet accepted in device-scoped
// silences: covers UUIDs, MAC addresses, and serial-style ids while
// excluding every regex metacharacter except ".", which
// domainSilenceToGrafana escapes.
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// === helpers ===========================================================

// pauseSilenceMatcher is the marker matcher PauseRule stamps onto
// the silences it creates. Reading this matcher back tells the
// service "this silence is a system pause, not a user-authored
// silence" — so it should drive the rule's Enabled flag (instead of
// appearing in the silences list).
const pauseSilenceMatcher = "proto_fleet_pause"

// alertRuleUIDMatcher is Grafana's reserved matcher label used to
// scope a silence to a single alert rule.
const alertRuleUIDMatcher = "__alert_rule_uid__"

// pauseSilenceEndsAt is the silence end time PauseRule writes.
// Grafana requires a finite EndsAt on every silence; we pick a date
// well outside any realistic operational horizon so the pause
// behaves like "indefinite" in practice. Resume removes the silence
// before it expires.
var pauseSilenceEndsAt = time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

// buildPauseSilence assembles the Alertmanager silence that
// PauseRule writes for the given rule. The matcher set carries:
//
//   - organization_id == orgID: same per-org isolation every other
//     silence carries.
//   - __alert_rule_uid__ == ruleID: scopes the silence to a single
//     rule (Grafana's reserved label for rule-level scoping).
//   - proto_fleet_pause == true: marker matcher that lets the
//     service distinguish system pauses from operator-authored
//     silences.
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

// isPauseSilence is true when the silence carries the proto-fleet
// pause marker matcher.
func isPauseSilence(sil GrafanaSilence) bool {
	for _, m := range sil.Matchers {
		if m.Name == pauseSilenceMatcher && m.Value == "true" && m.IsEqual && !m.IsRegex {
			return true
		}
	}
	return false
}

// isPauseSilenceFor is isPauseSilence narrowed to a specific org +
// rule. Used by ResumeRule to find the silence(s) to clear.
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

// ruleLabelOrganizationID is the label name we read on alert rules
// to decide which org owns them. Set on rules ops author per-org;
// absent on the defaults shipped in the YAML, which are visible
// (and pauseable) by every org's admin.
const ruleLabelOrganizationID = "organization_id"

// silenceLabelOrganizationID is the matcher name we inject onto
// every silence so the list filter can scope per-org.
const silenceLabelOrganizationID = "organization_id"

// channelNamePrefix is the per-org prefix every contact point name
// carries. Grafana doesn't sandbox by org at the provisioning API
// level, so we sandbox by name.
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
	}
	return ""
}

// encodeChannelSettings serialises the destination fields into the
// JSON shape Grafana expects. Secrets ride along in the settings
// payload; Grafana stores them encrypted at rest in its own datastore.
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
	}
	return nil, fmt.Errorf("unsupported channel kind %q", c.Kind)
}

// redactWebhookURL reduces a webhook URL to scheme://host[:port],
// dropping the userinfo, path, query, and fragment where capability
// tokens (Slack/PagerDuty/Teams) live. Returns "" for an empty or
// unparseable URL. This is the value reads expose; the full URL is
// kept only in Grafana's stored settings.
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

// webhookURLFromSettings pulls the full (unredacted) webhook URL out of
// a stored Grafana contact-point settings blob. Used by UpdateChannel
// to carry the stored URL through an edit that didn't replace it.
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

// contactPointToChannel reverses encodeChannelSettings on reads. The
// secret value is never returned — only the boolean indicating one
// exists.
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
		// URLs go out host-only — webhook URLs embed capability tokens
		// in the path/query, and these reads are reachable by
		// notification:read holders who can't otherwise see secrets.
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
	}
	// Default unknown channels to pending; the encrypt-service metadata
	// bag carries the real last-validated state but loading it on every
	// list is too expensive, so the UI sees pending and the operator
	// presses Test to refresh.
	out.ValidationState = ValidationPending
	return out, nil
}

// ruleVisibleToOrg decides whether the caller can see / pause / resume
// a rule. The rule is visible if it carries no organization_id label
// (provisioned default) or carries one matching the caller's id.
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

// grafanaRuleToDomain pulls the user-facing metadata off a Grafana
// alert rule. Opaque Data / Settings / Condition fields are ignored —
// the UI doesn't render them and we don't expose an authoring surface
// that would write them.
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

// templateFromLabel is the closed mapping between the `template`
// label the YAML stamps on each rule and the closed enum the UI
// uses. Unknown labels (including the self-monitoring rules that
// don't carry a template label) map to the empty string, which the
// UI treats as "fall back to rule name".
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

// parseDurationSeconds parses Grafana's go-duration string ("5m",
// "10m", "30s") into seconds. Best-effort: anything we can't parse
// returns zero, which the UI renders as "fires immediately".
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

// silenceMatchesOrg returns true if the silence has a matcher
// `organization_id=<wantOrgID>` (isEqual + non-regex). Anything else
// belongs to a different org or is a malformed-by-our-rules silence
// and is filtered out.
func silenceMatchesOrg(s GrafanaSilence, wantOrgID string) bool {
	for _, m := range s.Matchers {
		if m.Name == silenceLabelOrganizationID && m.IsEqual && !m.IsRegex && m.Value == wantOrgID {
			return true
		}
	}
	return false
}

// grafanaSilenceToDomain reverses domainSilenceToGrafana on reads
// and stamps the Active flag from the supplied clock.
func grafanaSilenceToDomain(orgID int64, gs GrafanaSilence, now time.Time) Silence {
	out := Silence{
		ID:             gs.ID,
		OrganizationID: orgID,
		StartsAt:       gs.StartsAt,
		EndsAt:         gs.EndsAt,
		Comment:        gs.Comment,
		CreatedBy:      gs.CreatedBy,
	}
	// CreatedBy is the only timestamp Grafana stamps; map to CreatedAt
	// when StartsAt looks like a creation marker so the UI's "Created"
	// column isn't always empty. Best-effort: the Alertmanager API
	// doesn't expose a created_at field.
	out.CreatedAt = gs.StartsAt

	out.Scope = matchersToScope(gs.Matchers)
	out.Active = silenceActive(out, now)
	return out
}

// matchersToScope reconstructs the structured scope payload from the
// Alertmanager-style matcher list. It mirrors domainSilenceToGrafana
// exactly; the order doesn't matter because Grafana stores them as a
// set.
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
			// device_id silences may carry many ids via a regex matcher,
			// or a single id via an equality matcher. The regex form is
			// `^(?:id1|id2)$` with QuoteMeta-escaped ids (see
			// domainSilenceToGrafana); strip the anchors and escapes to
			// recover the plain id list.
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

// domainSilenceToGrafana compiles the structured scope payload to
// Alertmanager matchers, always including the org-id matcher.
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
			// Anchor and escape: validateSilenceScope restricts ids to
			// the identifier alphabet, but QuoteMeta still escapes "."
			// and the anchors stop a partial match from widening the
			// silence to ids that merely contain a target as substring.
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

// silenceActive reports whether now is inside [StartsAt, EndsAt).
// EndsAt zero means "no end" (Alertmanager's "indefinite" silence);
// in that case any time after StartsAt is active.
func silenceActive(s Silence, now time.Time) bool {
	if now.Before(s.StartsAt) {
		return false
	}
	if s.EndsAt.IsZero() {
		return true
	}
	return now.Before(s.EndsAt)
}
