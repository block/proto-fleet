package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"sort"
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
		// Saved channel: the receiver already lives in Grafana, so test it by name
		// with its stored settings (full URL + secure fields). The echoed-back
		// payload is redacted and would probe the wrong target.
		_, ownedCP, err := s.findOwnedChannel(ctx, orgID, c.ID)
		if err != nil {
			return false, 0, "", err
		}
		res, err := s.grafana.TestReceiverIntegration(ctx, ownedCP.Name, ownedCP.Type, ownedCP.Settings)
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
		if delErr := s.grafana.DeleteContactPoint(ctx, created.UID); delErr != nil {
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
