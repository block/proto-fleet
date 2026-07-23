package alerts

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
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

// Cipher encrypts/decrypts a channel's destination secret at rest.
type Cipher interface {
	Encrypt(plaintext []byte) (string, error)
	Decrypt(ciphertext string) ([]byte, error)
}

// ChannelTester delivers a one-off test message to a destination (implemented by Deliverer),
// so channel CRUD can verify a destination without a Grafana receiver-test round trip.
type ChannelTester interface {
	SendTest(ctx context.Context, kind ChannelKind, url, bearer string) (ok bool, errMsg string, err error)
}

type Service struct {
	grafana  *Grafana
	channels ChannelStore
	routes   RouteStore
	crypto   Cipher
	tester   ChannelTester
	policy   DestinationPolicy
	now      func() time.Time
	// Serializes user-rule creation so the quota read-then-create can't race.
	userRuleMu sync.Mutex
}

type DestinationPolicy struct {
	AllowPrivateDestinations bool `help:"Allow alert destinations (webhook URLs, SMTP hosts) that resolve to loopback, link-local, or private network ranges. Enable for dev stacks or deployments whose relays live on internal addresses." default:"false" env:"ALLOW_PRIVATE_DESTINATIONS"`
}

func NewService(g *Grafana, channels ChannelStore, routes RouteStore, crypto Cipher, tester ChannelTester, policy DestinationPolicy) *Service {
	return &Service{grafana: g, channels: channels, routes: routes, crypto: crypto, tester: tester, policy: policy, now: time.Now}
}

var ErrZeroOrgID = errors.New("alerts: organization id is required")

// Surfaced as permission_denied so id scans aren't a list oracle.
var ErrNotFound = errors.New("alerts: not found")

func requireOrg(orgID int64) error {
	if orgID == 0 {
		return ErrZeroOrgID
	}
	return nil
}

// channelConfig is the plaintext destination secret, persisted encrypted in ChannelRecord.
type channelConfig struct {
	URL    string `json:"url"`
	Bearer string `json:"bearer,omitempty"`
}

func encodeChannelConfig(crypto Cipher, cfg channelConfig) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal channel config: %w", err)
	}
	return crypto.Encrypt(b)
}

func decodeChannelConfig(crypto Cipher, enc string) (channelConfig, error) {
	if enc == "" {
		return channelConfig{}, nil
	}
	b, err := crypto.Decrypt(enc)
	if err != nil {
		return channelConfig{}, err
	}
	var cfg channelConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return channelConfig{}, fmt.Errorf("unmarshal channel config: %w", err)
	}
	return cfg, nil
}

func (s *Service) encodeConfig(cfg channelConfig) (string, error) {
	return encodeChannelConfig(s.crypto, cfg)
}

func (s *Service) decodeConfig(enc string) (channelConfig, error) {
	return decodeChannelConfig(s.crypto, enc)
}

func configFromChannel(c Channel) channelConfig {
	switch c.Kind {
	case ChannelKindWebhook:
		if c.Webhook != nil {
			return channelConfig{URL: c.Webhook.URL, Bearer: c.Webhook.BearerHeader}
		}
	case ChannelKindSlack:
		if c.Slack != nil {
			return channelConfig{URL: c.Slack.WebhookURL}
		}
	}
	return channelConfig{}
}

// recordToChannel derives HasSecret and a redacted webhook URL from the stored config, never returning the secret itself.
func (s *Service) recordToChannel(rec ChannelRecord) (Channel, error) {
	cfg, err := s.decodeConfig(rec.EncryptedConfig)
	if err != nil {
		return Channel{}, err
	}
	c := Channel{
		ID:              strconv.FormatInt(rec.ID, 10),
		OrganizationID:  rec.OrganizationID,
		Name:            rec.Name,
		Kind:            rec.Kind,
		CreatedAt:       rec.CreatedAt,
		UpdatedAt:       rec.UpdatedAt,
		ValidatedAt:     rec.ValidatedAt,
		ValidationState: rec.ValidationState,
		ValidationError: rec.ValidationError,
	}
	switch rec.Kind {
	case ChannelKindWebhook:
		// Host-only: webhook URLs embed capability tokens reachable by alert:read holders.
		c.Webhook = &WebhookConfig{URL: redactWebhookURL(cfg.URL)}
		c.HasSecret = cfg.Bearer != ""
	case ChannelKindSlack:
		// The URL is the secret; expose presence only.
		c.Slack = &SlackConfig{}
		c.HasSecret = cfg.URL != ""
	}
	return c, nil
}

// A non-numeric id can't name a real row, so treat it as not found rather than a parse error.
func parseChannelID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, ErrNotFound
	}
	return n, nil
}

func (s *Service) ListChannels(ctx context.Context, orgID int64) ([]Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	recs, err := s.channels.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]Channel, 0, len(recs))
	for _, rec := range recs {
		c, err := s.recordToChannel(rec)
		if err != nil {
			// A row we can't decrypt (e.g. rotated master key) shouldn't sink the whole list.
			slog.Error("alerts.channel_decode_failed", "id", rec.ID, "err", err)
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) CreateChannel(ctx context.Context, orgID int64, c Channel) (*Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateChannelName(c.Name); err != nil {
		return nil, err
	}
	if err := s.validateDestination(ctx, &c); err != nil {
		return nil, err
	}
	// Reject a duplicate name up front (the live-rows unique index would reject it anyway).
	if _, err := s.channels.GetByName(ctx, orgID, c.Name); err == nil {
		return nil, fleeterror.NewAlreadyExistsErrorf("a channel named %q already exists", c.Name)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	enc, err := s.encodeConfig(configFromChannel(c))
	if err != nil {
		return nil, err
	}
	rec, err := s.channels.Insert(ctx, ChannelRecord{
		OrganizationID:  orgID,
		Name:            c.Name,
		Kind:            c.Kind,
		EncryptedConfig: enc,
		ValidationState: ValidationPending,
	})
	if err != nil {
		return nil, err
	}
	out, err := s.recordToChannel(rec)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Service) UpdateChannel(ctx context.Context, orgID int64, c Channel) (*Channel, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if c.ID == "" {
		return nil, errors.New("channel id is required for update")
	}
	if err := validateChannelName(c.Name); err != nil {
		return nil, err
	}
	id, err := parseChannelID(c.ID)
	if err != nil {
		return nil, err
	}
	rec, err := s.channels.Get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	stored, err := s.decodeConfig(rec.EncryptedConfig)
	if err != nil {
		return nil, err
	}
	// Reject a rename onto another live channel's name (the unique index would reject it too).
	if c.Name != rec.Name {
		if other, err := s.channels.GetByName(ctx, orgID, c.Name); err == nil && other.ID != id {
			return nil, fleeterror.NewAlreadyExistsErrorf("a channel named %q already exists", c.Name)
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	newCfg, needValidate := mergeChannelConfig(c, rec.Kind, stored)
	if needValidate {
		if err := s.validateConfig(ctx, c.Kind, newCfg); err != nil {
			return nil, err
		}
	}
	enc, err := s.encodeConfig(newCfg)
	if err != nil {
		return nil, err
	}
	updated, err := s.channels.Update(ctx, ChannelRecord{
		ID:              id,
		OrganizationID:  orgID,
		Name:            c.Name,
		Kind:            c.Kind,
		EncryptedConfig: enc,
		ValidationState: ValidationPending,
	})
	if err != nil {
		return nil, err
	}
	out, err := s.recordToChannel(updated)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// mergeChannelConfig folds an update onto the stored config, carrying the secret only when the destination is unchanged and the caller didn't ask to clear it; returns whether the result still needs SSRF validation.
func mergeChannelConfig(c Channel, storedKind ChannelKind, stored channelConfig) (channelConfig, bool) {
	switch c.Kind {
	case ChannelKindWebhook:
		req := configFromChannel(c) // {URL, Bearer} from the request
		// Only reuse the stored URL when this was already a webhook; otherwise we'd graft the
		// prior kind's secret onto the webhook. Reads redact the URL to host, so treat an
		// unchanged (empty or host-only) submission as "keep the stored destination".
		storedURL := ""
		if storedKind == ChannelKindWebhook {
			storedURL = stored.URL
		}
		if storedURL != "" && (req.URL == "" || req.URL == redactWebhookURL(storedURL)) {
			req.URL = storedURL
		}
		destinationChanged := req.URL != storedURL
		clearBearer := c.Webhook != nil && c.Webhook.ClearBearer
		if req.Bearer == "" && !clearBearer && !destinationChanged && storedKind == ChannelKindWebhook {
			req.Bearer = stored.Bearer // carry the stored bearer unless the caller asked to revoke it
		}
		return req, true
	case ChannelKindSlack:
		keepStored := storedKind == ChannelKindSlack && (c.Slack == nil || c.Slack.WebhookURL == "")
		if keepStored {
			return channelConfig{URL: stored.URL}, false
		}
		return configFromChannel(c), true
	}
	return channelConfig{}, false
}

// validateConfig runs the SSRF/destination checks against an effective (post-merge) config.
func (s *Service) validateConfig(ctx context.Context, kind ChannelKind, cfg channelConfig) error {
	tmp := Channel{Kind: kind}
	switch kind {
	case ChannelKindWebhook:
		tmp.Webhook = &WebhookConfig{URL: cfg.URL, BearerHeader: cfg.Bearer}
	case ChannelKindSlack:
		tmp.Slack = &SlackConfig{WebhookURL: cfg.URL}
	}
	return s.validateDestination(ctx, &tmp)
}

func (s *Service) DeleteChannel(ctx context.Context, orgID int64, id string) error {
	if err := requireOrg(orgID); err != nil {
		return err
	}
	n, err := parseChannelID(id)
	if err != nil {
		return err
	}
	return s.channels.SoftDelete(ctx, orgID, n)
}

// Reserves the "test-<uuid>" shape (kept from the earlier Grafana-routed design) so a saved
// channel can never be named to collide with transient test receivers.
var transientReceiverName = regexp.MustCompile(`^test-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *Service) TestChannel(ctx context.Context, orgID int64, c Channel) (bool, int, string, error) {
	if err := requireOrg(orgID); err != nil {
		return false, 0, "", err
	}
	var (
		kind        ChannelKind
		url, bearer string
	)
	if c.ID != "" {
		// Saved channel: decrypt the stored destination so we test the real secret, not the
		// redacted placeholder a read returns.
		id, err := parseChannelID(c.ID)
		if err != nil {
			return false, 0, "", err
		}
		rec, err := s.channels.Get(ctx, orgID, id)
		if err != nil {
			return false, 0, "", err
		}
		cfg, err := s.decodeConfig(rec.EncryptedConfig)
		if err != nil {
			return false, 0, "", err
		}
		kind, url, bearer = rec.Kind, cfg.URL, cfg.Bearer
	} else {
		// Test-before-save: validate the submitted destination, then send to it directly.
		if err := s.validateDestination(ctx, &c); err != nil {
			return false, 0, "", err
		}
		cfg := configFromChannel(c)
		kind, url, bearer = c.Kind, cfg.URL, cfg.Bearer
	}
	ok, errMsg, err := s.tester.SendTest(ctx, kind, url, bearer)
	if err != nil {
		return false, 0, "", err
	}
	return ok, testStatusCode(ok), errMsg, nil
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

// Rejects names matching the transient test-receiver pattern so a saved channel can never be misclassified as transient and dropped from routing.
func validateChannelName(name string) error {
	if transientReceiverName.MatchString(name) {
		return fleeterror.NewInvalidArgumentError("channel name may not match the reserved transient test-receiver pattern")
	}
	return nil
}

// fleet-api is what connects out, so an unvalidated destination is an SSRF vector.
func (s *Service) validateDestination(ctx context.Context, c *Channel) error {
	switch c.Kind {
	case ChannelKindWebhook:
		if c.Webhook == nil || c.Webhook.URL == "" {
			return fleeterror.NewInvalidArgumentError("webhook url is required")
		}
		return checkDestinationURL(ctx, s.policy, c.Webhook.URL, "webhook")
	case ChannelKindSlack:
		if c.Slack == nil || c.Slack.WebhookURL == "" {
			return fleeterror.NewInvalidArgumentError("slack webhook url is required")
		}
		return checkDestinationURL(ctx, s.policy, c.Slack.WebhookURL, "slack webhook")
	}
	return nil
}

func checkDestinationURL(ctx context.Context, policy DestinationPolicy, raw, label string) error {
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
	return checkDestinationHost(ctx, policy, u.Hostname())
}

const destinationLookupTimeout = 3 * time.Second

// DNS failures fail closed. This preflight is TOCTOU-prone on its own; the deliverer pins the validated IP at dial time (destinationIPAllowed) to close the rebind gap.
func checkDestinationHost(ctx context.Context, policy DestinationPolicy, host string) error {
	if policy.AllowPrivateDestinations {
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
		if !destinationIPAllowed(policy, ip) {
			return reject()
		}
	}
	return nil
}

// destinationIPAllowed reports whether an IP may be reached; the deliverer re-checks the
// dialed IP at connect time with this so a DNS rebind between preflight and connect is refused.
func destinationIPAllowed(policy DestinationPolicy, ip net.IP) bool {
	if policy.AllowPrivateDestinations {
		return true
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || isReservedIP(ip))
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
	out, err := s.listVisibleRules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	// Fail closed here (unlike the requireRule gate): rendering routed rules as default would mislead operators.
	if err := s.attachRouting(ctx, orgID, out); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// listVisibleRules is the routing-free listing shared by requireRule: rule-targeted actions
// (pause, resume, maintenance windows) must keep working while the route-policy table is unreadable.
func (s *Service) listVisibleRules(ctx context.Context, orgID int64) ([]Rule, error) {
	out, err := s.visibleRulesNoPauseState(ctx, orgID)
	if err != nil {
		return nil, err
	}
	// Fail closed: without pause-silence state we can't trust the Enabled flag, so error
	// rather than render a muted rule as enabled.
	paused, err := s.pauseSilencedRules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if paused[out[i].ID] {
			out[i].Enabled = false
		}
	}
	return out, nil
}

// visibleRulesNoPauseState lists the org's visible rules without the pause-silence overlay,
// so silence-independent mutations (routing) stay available during a silence-API outage.
// Enabled may read true for a paused rule; callers owning a response must re-apply pause state.
func (s *Service) visibleRulesNoPauseState(ctx context.Context, orgID int64) ([]Rule, error) {
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
	silenceID, err := s.grafana.PutSilence(ctx, silence)
	if err != nil {
		return nil, err
	}
	if err := s.confirmRuleSilenceTarget(ctx, id, silenceID, true); err != nil {
		return nil, err
	}
	out := *rule
	out.Enabled = false
	s.attachRoutingBestEffort(ctx, orgID, &out)
	return &out, nil
}

// confirmRuleSilenceTarget undoes a silence written concurrently with its
// target rule's deletion: the delete's sweep cannot see a silence written
// after it ran, so whichever side runs last performs the cleanup. rollbackNew
// deletes a NEWLY created silence when the check is inconclusive, so a retry
// can't duplicate it; updates pass false — their edit replaced the previous
// silence already, and deleting it would lift planned suppression entirely
// (an update retry converges without duplicating).
func (s *Service) confirmRuleSilenceTarget(ctx context.Context, ruleID, silenceID string, rollbackNew bool) error {
	_, err := s.grafana.GetAlertRule(ctx, ruleID)
	if err == nil {
		return nil
	}
	if !IsNotFound(err) {
		if rollbackNew {
			if derr := s.grafana.DeleteSilence(ctx, silenceID); derr != nil && !IsNotFound(derr) {
				slog.Warn("alerts.silence_rollback_failed", "rule_id", ruleID, "silence_id", silenceID, "error", derr)
			}
		}
		return err
	}
	// Rule gone: the silence must die regardless of create-vs-update.
	if derr := s.grafana.DeleteSilence(ctx, silenceID); derr != nil && !IsNotFound(derr) {
		return derr
	}
	return ErrNotFound
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
	if err := s.removeSilencesTargetingRule(ctx, orgID, id, isPauseSilence); err != nil {
		return nil, err
	}
	updated, err := s.requireRule(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	s.attachRoutingBestEffort(ctx, orgID, updated)
	return updated, nil
}

// removeSilencesTargetingRule deletes the org's non-expired silences pinned to
// the rule that also satisfy match (e.g. pause-only for resume, pause-or-
// maintenance-window for rule deletion).
func (s *Service) removeSilencesTargetingRule(ctx context.Context, orgID int64, id string, match func(GrafanaSilence) bool) error {
	want := strconv.FormatInt(orgID, 10)
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return err
	}
	for _, sil := range sils {
		if !match(sil) || !silenceMatchesOrg(sil, want) || !silenceTargetsRule(sil, id) {
			continue
		}
		if sil.Status != nil && sil.Status.State == "expired" {
			continue
		}
		if err := s.grafana.DeleteSilence(ctx, sil.ID); err != nil && !IsNotFound(err) {
			return err
		}
	}
	return nil
}

// requireRule is the routing-free visibility gate: it must not couple rule actions to route-policy reads.
func (s *Service) requireRule(ctx context.Context, orgID int64, id string) (*Rule, error) {
	if id == "" {
		return nil, errors.New("rule id is required")
	}
	rules, err := s.listVisibleRules(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return findRuleByID(rules, id)
}

// requireVisibleRule is requireRule minus the pause-silence read: same uniform NotFound, but
// usable during a silence-API outage. The caller owns re-applying pause state to any response.
func (s *Service) requireVisibleRule(ctx context.Context, orgID int64, id string) (*Rule, error) {
	if id == "" {
		return nil, errors.New("rule id is required")
	}
	rules, err := s.visibleRulesNoPauseState(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return findRuleByID(rules, id)
}

func findRuleByID(rules []Rule, id string) (*Rule, error) {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i], nil
		}
	}
	return nil, ErrNotFound
}

// Propagates the silences-read error so ListRules can fail closed: without pause state we
// can't tell a muted rule from an enabled one, and silently showing it enabled would mislead
// operators (and let PauseRule write a duplicate pause silence during an outage).
func (s *Service) pauseSilencedRules(ctx context.Context, orgID int64) (map[string]bool, error) {
	sils, err := s.grafana.ListSilences(ctx)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatInt(orgID, 10)
	now := s.now()
	out := map[string]bool{}
	for _, sil := range sils {
		if !isPauseSilence(sil) {
			continue
		}
		// Skip expired/deleted silences (they linger with the 2099 sentinel end time, as ResumeRule/ListMaintenanceWindows do) so a lifted pause doesn't keep reporting the rule disabled.
		if sil.Status != nil && sil.Status.State == "expired" {
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
	return out, nil
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
		// Only surface silences Proto Fleet created (carry the marker): this both hides
		// pause silences and keeps externally-created Grafana silences read-only/invisible,
		// so they can't be listed, updated, or deleted through these RPCs.
		if !isMaintenanceWindowSilence(gs) {
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
	if sil.Scope.Kind == MaintenanceWindowScopeRule && sil.Scope.RuleID != "" {
		if err := s.confirmRuleSilenceTarget(ctx, sil.Scope.RuleID, id, true); err != nil {
			return nil, err
		}
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
	if sil.Scope.Kind == MaintenanceWindowScopeRule && sil.Scope.RuleID != "" {
		// rollbackNew=false: this PUT replaced the previous silence, so deleting
		// it on an inconclusive check would lift planned suppression entirely.
		if err := s.confirmRuleSilenceTarget(ctx, sil.Scope.RuleID, id, false); err != nil {
			return nil, err
		}
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
			// Bound length before the regex so an oversized id can't force avoidable matcher work.
			if len(id) > maxDeviceIDLength {
				return fleeterror.NewInvalidArgumentErrorf("device id too long: %d (max %d)", len(id), maxDeviceIDLength)
			}
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
	if strings.Contains(comment, pauseSilenceCommentMarker) || strings.Contains(comment, maintenanceWindowCommentMarker) {
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

// Matches the device_identifier bound in pairing.proto; caps matcher work on a direct-RPC device-scoped window.
const maxDeviceIDLength = 255

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

// Stamps Proto Fleet-created maintenance windows so List/Update/Delete don't treat an
// arbitrary operator-created Grafana silence (which may share the org matcher) as one
// we own. Like the pause marker it lives in the comment, not a matcher, so it can't
// affect which alerts the silence matches.
const maintenanceWindowCommentMarker = "[proto-fleet-mw]"

func isMaintenanceWindowSilence(sil GrafanaSilence) bool {
	return strings.HasPrefix(sil.Comment, maintenanceWindowCommentMarker)
}

// Prepends the provenance marker to the operator's reason for storage in Grafana.
func encodeMaintenanceWindowComment(comment string) string {
	if comment == "" {
		return maintenanceWindowCommentMarker
	}
	return maintenanceWindowCommentMarker + " " + comment
}

// Recovers the operator's reason from a stored comment for display.
func decodeMaintenanceWindowComment(comment string) string {
	return strings.TrimSpace(strings.TrimPrefix(comment, maintenanceWindowCommentMarker))
}

func isPauseSilenceFor(sil GrafanaSilence, wantOrgID, ruleID string) bool {
	if !isPauseSilence(sil) {
		return false
	}
	if !silenceMatchesOrg(sil, wantOrgID) {
		return false
	}
	return silenceTargetsRule(sil, ruleID)
}

func silenceTargetsRule(sil GrafanaSilence, ruleID string) bool {
	for _, m := range sil.Matchers {
		if m.Name == alertRuleUIDMatcher && m.Value == ruleID && m.IsEqual && !m.IsRegex {
			return true
		}
	}
	return false
}

const ruleLabelOrganizationID = "organization_id"

// Shared rule→alert-instance label contract; the webhook ingest and history
// rendering read the same keys, so writers must not inline the literals.
const (
	ruleLabelSeverity  = "severity"
	ruleLabelTemplate  = "template"
	ruleLabelRuleGroup = "rule_group"
	// Primary rule identity for delivery routing; generatorURL parsing is the fallback for rules compiled before it existed.
	ruleLabelRuleUID = "proto_fleet_rule_uid"
)

// Rule visibility is fail-closed and driven by proto_fleet_scope: shared rules are visible to
// every org (shared platform defaults), internal rules are hidden from all orgs (operator-only
// self-monitoring), and a rule with neither marker is visible only if it carries this org's
// organization_id label. An unmarked, unlabeled rule is hidden so it can't leak across orgs.
const (
	ruleLabelScope    = "proto_fleet_scope"
	ruleScopeShared   = "shared"
	ruleScopeInternal = "internal"
)

const silenceLabelOrganizationID = "organization_id"

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

func ruleVisibleToOrg(r GrafanaAlertRule, wantOrgID string) bool {
	switch r.Labels[ruleLabelScope] {
	case ruleScopeShared:
		// Shared platform default: visible to every org.
		return true
	case ruleScopeInternal:
		// Operator-only self-monitoring: hidden from every org.
		return false
	}
	// No scope marker: visible only to the org named on the rule. Unmarked, unlabeled
	// rules are hidden (fail closed) so a tenant-specific rule provisioned without its
	// org label can't leak across orgs.
	got, ok := r.Labels[ruleLabelOrganizationID]
	return ok && got == wantOrgID
}

func grafanaRuleToDomain(orgID int64, r GrafanaAlertRule) Rule {
	out := Rule{
		ID:              r.UID,
		OrganizationID:  orgID,
		Name:            r.Title,
		Group:           r.RuleGroup,
		Enabled:         !r.IsPaused,
		DurationSeconds: parseDurationSeconds(r.For),
		Origin:          RuleOriginProvisioned,
	}
	if r.Labels != nil {
		out.Template = templateFromLabel(r.Labels[ruleLabelTemplate])
		out.Severity = r.Labels[ruleLabelSeverity]
		if r.Labels[ruleLabelOrigin] == ruleOriginUser {
			out.Origin = RuleOriginUser
		}
		// User rules live in per-rule Grafana groups (see compileUserRule); the
		// label carries the stable per-org grouping the UI sorts by.
		if group := r.Labels[ruleLabelRuleGroup]; group != "" {
			out.Group = group
		}
	}
	if r.Annotations != nil {
		out.Summary = r.Annotations["summary"]
		out.Description = r.Annotations["description"]
		if raw := r.Annotations[ruleAnnotationConfig]; raw != "" {
			var cfg RuleConfig
			err := json.Unmarshal([]byte(raw), &cfg)
			// A config that fails validation or disagrees with the template label
			// must not round-trip into the editor (the client hides Edit on nil).
			if err == nil && validateRuleConfig(cfg) == nil && cfg.Template() == out.Template {
				out.Config = &cfg
			} else {
				slog.Warn("alerts.rule_config_invalid", "rule_uid", r.UID, "error", err)
			}
		}
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
	case "mqtt-curtailment":
		return RuleTemplateMQTTCurtailment
	case "mqtt-disconnected":
		return RuleTemplateMQTTDisconnected
	}
	return ""
}

// Grafana echoes `for` as a Prometheus duration ("1d", "2w"), whose d/w/y
// units time.ParseDuration rejects; normalize them to hours before parsing.
var promLongDurationUnits = regexp.MustCompile(`(\d+)(y|w|d)`)

func parseDurationSeconds(s string) int32 {
	if s == "" {
		return 0
	}
	norm := promLongDurationUnits.ReplaceAllStringFunc(s, func(m string) string {
		sub := promLongDurationUnits.FindStringSubmatch(m)
		n, err := strconv.ParseInt(sub[1], 10, 64)
		if err != nil {
			return m
		}
		hours := map[string]int64{"y": 8760, "w": 168, "d": 24}[sub[2]]
		return strconv.FormatInt(n*hours, 10) + "h"
	})
	d, err := time.ParseDuration(norm)
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
		Comment:        decodeMaintenanceWindowComment(gs.Comment),
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
		Comment:   encodeMaintenanceWindowComment(sil.Comment),
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
