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
	grafana     *Grafana
	channels    ChannelStore
	routes      RouteStore
	configs     RuleConfigStore
	windows     MaintenanceWindowStore
	crypto      Cipher
	tester      ChannelTester
	scopeLookup ScopeLookup
	policy      DestinationPolicy
	now         func() time.Time
	// Serializes user-rule creation so the quota read-then-create can't race.
	userRuleMu sync.Mutex
}

type DestinationPolicy struct {
	AllowPrivateDestinations bool `help:"Allow alert destinations (webhook URLs, SMTP hosts) that resolve to loopback, link-local, or private network ranges. Enable for dev stacks or deployments whose relays live on internal addresses." default:"false" env:"ALLOW_PRIVATE_DESTINATIONS"`
}

func NewService(g *Grafana, channels ChannelStore, routes RouteStore, configs RuleConfigStore, windows MaintenanceWindowStore, crypto Cipher, tester ChannelTester, scopeLookup ScopeLookup, policy DestinationPolicy) *Service {
	return &Service{grafana: g, channels: channels, routes: routes, configs: configs, windows: windows, crypto: crypto, tester: tester, scopeLookup: scopeLookup, policy: policy, now: time.Now}
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
func parseRowID(id string) (int64, error) {
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
	id, err := parseRowID(c.ID)
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
	n, err := parseRowID(id)
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
		id, err := parseRowID(c.ID)
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
	// Fail closed (unlike requireRule, which backs pause/maintenance actions that must survive a
	// store outage): rendering routed rules as default or scoped rules as org-wide misleads operators.
	if err := s.attachConfigs(ctx, orgID, out); err != nil {
		return nil, err
	}
	s.sweepRuleConfigsBestEffort(ctx, orgID, out)
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

// attachConfigBestEffort decorates a mutation response like attachRoutingBestEffort: the mutation
// is already committed, so a config-read hiccup degrades the response rather than failing the action.
func (s *Service) attachConfigBestEffort(ctx context.Context, orgID int64, rule *Rule) {
	if s.configs == nil {
		return
	}
	cfg, err := s.configs.GetConfig(ctx, orgID, rule.ID)
	if err != nil {
		// Flag rather than silently omit: an absent config also means "not
		// editable", so the client must know to keep its last-known value.
		rule.ConfigUnknown = true
		slog.Warn("alerts.rule_config_decorate", "org_id", orgID, "rule_id", rule.ID, "error", err)
		return
	}
	if cfg == nil {
		cfg = legacyRuleConfig(ctx, orgID, rule.ID, rule.Template, rule.legacyConfigJSON)
	}
	rule.Config = cfg
	markConfigOutOfSync(ctx, orgID, rule)
}

// markConfigOutOfSync flags an interrupted save; it compares every compiled field, not just SQL (name/duration
// live only in Title/For; 1 PH/s and 1000 TH/s share SQL), on every decoration path so a later mutation can't erase it.
func markConfigOutOfSync(ctx context.Context, orgID int64, rule *Rule) {
	if rule.Config == nil {
		return
	}
	// "" means no refId-A rawSql was found — a config always compiles to non-empty SQL,
	// so that is itself a divergence, not a reason to skip the comparison.
	if rule.CompiledSQL == "" {
		rule.ConfigOutOfSync = true
		slog.WarnContext(ctx, "alerts.rule_config_out_of_sync", "org_id", orgID, "rule_id", rule.ID, "reason", "no_compiled_sql")
		return
	}
	sql, summary, description := compileTemplate(orgID, *rule.Config)
	if sql == rule.CompiledSQL &&
		strings.TrimSpace(rule.Config.Name) == rule.Name &&
		rule.Config.DurationSeconds == rule.DurationSeconds &&
		summary == rule.Summary &&
		description == rule.Description {
		return
	}
	rule.ConfigOutOfSync = true
	slog.WarnContext(ctx, "alerts.rule_config_out_of_sync", "org_id", orgID, "rule_id", rule.ID)
}

// attachConfigs overlays stored rule configs, falling back to the legacy annotation until a rule's first
// update writes a row. Fails closed: a rule rendered without its config would misreport an org-wide scope.
func (s *Service) attachConfigs(ctx context.Context, orgID int64, rules []Rule) error {
	if s.configs == nil {
		return nil
	}
	cfgs, err := s.configs.ListConfigs(ctx, orgID, ruleUIDs(rules))
	if err != nil {
		return fmt.Errorf("list rule configs: %w", err)
	}
	for i := range rules {
		cfg, ok := cfgs[rules[i].ID]
		if !ok {
			rules[i].Config = legacyRuleConfig(ctx, orgID, rules[i].ID, rules[i].Template, rules[i].legacyConfigJSON)
			markConfigOutOfSync(ctx, orgID, &rules[i])
			continue
		}
		rules[i].Config = &cfg
		markConfigOutOfSync(ctx, orgID, &rules[i])
	}
	return nil
}

func ruleUIDs(rules []Rule) []string {
	uids := make([]string, len(rules))
	for i := range rules {
		uids[i] = rules[i].ID
	}
	return uids
}

// sweepRuleConfigsBestEffort reclaims rows whose rule Grafana no longer lists (ambiguous create failures
// keep theirs; see CreateRule). rules is authoritative, and the store spares recent rows for in-flight creates.
func (s *Service) sweepRuleConfigsBestEffort(ctx context.Context, orgID int64, rules []Rule) {
	if s.configs == nil {
		return
	}
	n, err := s.configs.SweepConfigs(ctx, orgID, ruleUIDs(rules))
	if err != nil {
		slog.Warn("alerts.rule_config_sweep", "org_id", orgID, "error", err)
		return
	}
	if n > 0 {
		slog.InfoContext(ctx, "alerts.rule_config_sweep", "org_id", orgID, "reclaimed", n)
	}
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
		// The no-op response is upserted by the client like any other: without decoration its
		// nil routing serializes as an explicit DEFAULT and overwrites the real policy client-side.
		s.attachRoutingBestEffort(ctx, orgID, rule)
		s.attachConfigBestEffort(ctx, orgID, rule)
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
	s.attachConfigBestEffort(ctx, orgID, &out)
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
	s.attachConfigBestEffort(ctx, orgID, updated)
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
		if !timeRangeActive(sil.StartsAt, sil.EndsAt, now) {
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
	recs, err := s.windows.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]MaintenanceWindow, 0, len(recs))
	for _, rec := range recs {
		out = append(out, maintenanceWindowFromRecord(rec, now))
	}
	return out, nil
}

func (s *Service) CreateMaintenanceWindow(ctx context.Context, orgID int64, w MaintenanceWindow) (*MaintenanceWindow, error) {
	now := s.now()
	rec, err := s.resolveMaintenanceWindowRecord(ctx, orgID, w, now)
	if err != nil {
		return nil, err
	}
	// Prune-on-create: creation is the only path that grows the table, so reclaiming the org's
	// stale expired rows here bounds the list without a background job. Best-effort — a failed
	// prune must not block the window an operator needs right now.
	if _, err := s.windows.DeleteExpiredBefore(ctx, orgID, now.Add(-maintenanceWindowRetention)); err != nil {
		slog.Warn("alerts.maintenance_window_prune_failed", "org_id", orgID, "error", err)
	}
	if err := s.requireWindowQuota(ctx, orgID, now, 0); err != nil {
		return nil, err
	}
	stored, err := s.windows.Insert(ctx, *rec)
	if err != nil {
		return nil, err
	}
	out := maintenanceWindowFromRecord(stored, now)
	return &out, nil
}

func (s *Service) UpdateMaintenanceWindow(ctx context.Context, orgID int64, w MaintenanceWindow) (*MaintenanceWindow, error) {
	if w.ID == "" {
		return nil, errors.New("maintenance window id is required for update")
	}
	id, err := parseRowID(w.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rec, err := s.resolveMaintenanceWindowRecord(ctx, orgID, w, now)
	if err != nil {
		return nil, err
	}
	rec.ID = id
	// Accepted writes always end in the future, so an update can revive an expired row —
	// hold it to the create quota (see requireWindowQuota).
	if err := s.requireWindowQuota(ctx, orgID, now, id); err != nil {
		return nil, err
	}
	// The store update leaves created_by/created_at untouched, so the audit owner survives edits.
	stored, err := s.windows.Update(ctx, *rec)
	if err != nil {
		return nil, err
	}
	out := maintenanceWindowFromRecord(stored, now)
	return &out, nil
}

// requireWindowQuota rejects a write that would leave the org with more than
// maxMaintenanceWindowsPerOrg unexpired windows. excludingID names the row being rewritten so
// an edit of an at-cap active window still saves while reviving an expired row is held to the
// cap; 0 on create. Soft cap (read-then-write, like the user-rule quota but without its mutex):
// racing writes can overshoot by a few rows, which is fine for a growth bound.
func (s *Service) requireWindowQuota(ctx context.Context, orgID int64, now time.Time, excludingID int64) error {
	unexpired, err := s.windows.CountUnexpired(ctx, orgID, now, excludingID)
	if err != nil {
		return err
	}
	if unexpired >= maxMaintenanceWindowsPerOrg {
		return fleeterror.NewFailedPreconditionErrorf(
			"maintenance window limit reached (%d active or scheduled); delete one first", maxMaintenanceWindowsPerOrg)
	}
	return nil
}

func (s *Service) DeleteMaintenanceWindow(ctx context.Context, orgID int64, id string) error {
	if err := requireOrg(orgID); err != nil {
		return err
	}
	n, err := parseRowID(id)
	if err != nil {
		return err
	}
	return s.windows.Delete(ctx, orgID, n)
}

// resolveMaintenanceWindowRecord validates a submitted window and resolves its rule and channel
// targets into the persisted form, shared by create and update.
func (s *Service) resolveMaintenanceWindowRecord(ctx context.Context, orgID int64, w MaintenanceWindow, now time.Time) (*MaintenanceWindowRecord, error) {
	if err := requireOrg(orgID); err != nil {
		return nil, err
	}
	if err := validateMaintenanceWindowTimes(w.StartsAt, w.EndsAt, now); err != nil {
		return nil, err
	}
	ruleUIDs, err := s.resolveVisibleRuleIDs(ctx, orgID, w.RuleIDs)
	if err != nil {
		return nil, err
	}
	channelIDs, err := s.resolveWindowChannelIDs(ctx, orgID, w.ChannelIDs)
	if err != nil {
		return nil, err
	}
	return &MaintenanceWindowRecord{
		OrganizationID: orgID,
		RuleUIDs:       ruleUIDs,
		ChannelIDs:     channelIDs,
		StartsAt:       w.StartsAt,
		EndsAt:         w.EndsAt,
		Comment:        w.Comment,
		CreatedBy:      w.CreatedBy,
	}, nil
}

// resolveVisibleRuleIDs dedupes the requested rule ids, requiring each to name a rule the caller
// can see (PauseRule's visibility gate, minus the silence read so windows stay writable during a
// silence-API outage). Empty means every rule and skips the Grafana read entirely.
func (s *Service) resolveVisibleRuleIDs(ctx context.Context, orgID int64, ruleIDs []string) ([]string, error) {
	if len(ruleIDs) == 0 {
		return nil, nil
	}
	if len(ruleIDs) > maxMaintenanceWindowTargets {
		return nil, fleeterror.NewInvalidArgumentErrorf("too many rule_ids: %d (max %d)", len(ruleIDs), maxMaintenanceWindowTargets)
	}
	rules, err := s.visibleRulesNoPauseState(ctx, orgID)
	if err != nil {
		return nil, err
	}
	visible := make(map[string]bool, len(rules))
	for _, r := range rules {
		visible[r.ID] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		// Uniform NotFound (like requireRule) so id scans aren't an existence oracle.
		if !visible[id] {
			return nil, ErrNotFound
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// resolveWindowChannelIDs is resolveOrgChannelIDs behind the window's "empty means every channel"
// contract, bounded like the rule list.
func (s *Service) resolveWindowChannelIDs(ctx context.Context, orgID int64, channelIDs []string) ([]int64, error) {
	if len(channelIDs) == 0 {
		return nil, nil
	}
	if len(channelIDs) > maxMaintenanceWindowTargets {
		return nil, fleeterror.NewInvalidArgumentErrorf("too many channel_ids: %d (max %d)", len(channelIDs), maxMaintenanceWindowTargets)
	}
	return s.resolveOrgChannelIDs(ctx, orgID, channelIDs)
}

func maintenanceWindowFromRecord(rec MaintenanceWindowRecord, now time.Time) MaintenanceWindow {
	channelIDs := make([]string, len(rec.ChannelIDs))
	for i, id := range rec.ChannelIDs {
		channelIDs[i] = strconv.FormatInt(id, 10)
	}
	w := MaintenanceWindow{
		ID:             strconv.FormatInt(rec.ID, 10),
		OrganizationID: rec.OrganizationID,
		RuleIDs:        rec.RuleUIDs,
		ChannelIDs:     channelIDs,
		StartsAt:       rec.StartsAt,
		EndsAt:         rec.EndsAt,
		Comment:        rec.Comment,
		CreatedBy:      rec.CreatedBy,
		CreatedAt:      rec.CreatedAt,
	}
	w.Active = timeRangeActive(w.StartsAt, w.EndsAt, now)
	return w
}

// Maintenance windows are finite and forward-looking: the UI enforces this, but a direct RPC
// could omit ends_at (which would compile to the far-future sentinel and silence alerts for
// decades), pass an end at/before the start, or write an already-ended window — which mutes
// nothing but would bloat the table beyond the unexpired-count quota's reach. Indefinite
// suppression is only available via PauseRule; ending a window early is done by deleting it.
func validateMaintenanceWindowTimes(startsAt, endsAt, now time.Time) error {
	if startsAt.IsZero() {
		return fleeterror.NewInvalidArgumentError("starts_at is required for a maintenance window")
	}
	if endsAt.IsZero() {
		return fleeterror.NewInvalidArgumentError("ends_at is required for a maintenance window")
	}
	if !endsAt.After(startsAt) {
		return fleeterror.NewInvalidArgumentError("ends_at must be after starts_at")
	}
	if !endsAt.After(now) {
		return fleeterror.NewInvalidArgumentError("ends_at must be in the future; to end a window early, delete it")
	}
	return nil
}

// Per-list bound on a window's rule and channel targets, matching the wire validation; a
// hostile direct caller can't inflate the row.
const maxMaintenanceWindowTargets = 100

// Per-org bound on active-or-scheduled windows; expired history never counts against it
// (it is pruned instead), so the cap can't wedge creation shut over time.
const maxMaintenanceWindowsPerOrg = 100

// How long an expired window stays listable as audit history before the creation-time prune
// reclaims it.
const maintenanceWindowRetention = 90 * 24 * time.Hour

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
		out.legacyConfigJSON = r.Annotations[ruleAnnotationConfig]
	}
	out.CompiledSQL = ruleRawSQL(r.Data)
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

// timeRangeActive reports whether [startsAt, endsAt) covers now; a zero endsAt (or the
// far-future pause sentinel) reads as indefinite.
func timeRangeActive(startsAt, endsAt, now time.Time) bool {
	if now.Before(startsAt) {
		return false
	}
	if endsAt.IsZero() {
		return true
	}
	return now.Before(endsAt)
}
