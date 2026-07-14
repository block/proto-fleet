package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// perSendTimeout bounds a single destination POST so one slow channel can't stall the whole batch.
const perSendTimeout = 10 * time.Second

// maxDeliveryConcurrency bounds in-flight sends per org so one slow channel can't starve the rest.
const maxDeliveryConcurrency = 8

// Alert is one alert instance from a Grafana webhook batch, reduced to what delivery needs.
type Alert struct {
	Status      string
	Labels      map[string]string
	Annotations map[string]string
}

// Deliverer fans a webhook batch out to each org's channels, re-checking each destination against the SSRF policy at send time.
type Deliverer struct {
	channels   ChannelStore
	crypto     Cipher
	devices    DeviceIdentityLookup
	httpClient *http.Client
	policy     DestinationPolicy
	publicURL  string
}

func NewDeliverer(channels ChannelStore, crypto Cipher, devices DeviceIdentityLookup, policy DestinationPolicy, publicURL string) *Deliverer {
	return &Deliverer{
		channels:   channels,
		crypto:     crypto,
		devices:    devices,
		httpClient: newDeliveryHTTPClient(policy),
		policy:     policy,
		publicURL:  strings.TrimRight(publicURL, "/"),
	}
}

// newDeliveryHTTPClient pins the resolved+validated IP into the dial so a DNS rebind between
// the preflight check and the actual connection can't reach an internal address, and refuses to
// follow redirects so a 3xx can't leak the secret-bearing channel URL (via Referer/Authorization)
// to another host or bounce the request onto an internal one.
func newDeliveryHTTPClient(policy DestinationPolicy) *http.Client {
	dialer := &net.Dialer{Timeout: perSendTimeout}
	transport, _ := http.DefaultTransport.(*http.Transport)
	transport = transport.Clone()
	// No proxy: a proxy would resolve+connect the final host itself, bypassing the pinned dial
	// below and reopening the DNS-rebind SSRF gap. This egress client must reach destinations directly.
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("split destination address: %w", err)
		}
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve destination: %w", err)
		}
		lastErr := errors.New("destination has no dialable address")
		for _, ip := range ips {
			if !destinationIPAllowed(policy, ip) {
				lastErr = errors.New("destination resolves to a private or internal address")
				continue
			}
			// Dial the validated IP directly; TLS SNI/verification still uses the original host.
			conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if derr != nil {
				lastErr = derr
				continue
			}
			return conn, nil
		}
		return nil, lastErr
	}
	return &http.Client{
		Timeout:   perSendTimeout,
		Transport: transport,
		// Don't follow redirects: an alert destination is an exact endpoint, and following a 3xx
		// would forward the secret channel URL to the redirect target. Surface the 3xx as a failed send.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Deliver groups the batch by org (dropping operator-only internal and org-less alerts) and sends each org's alerts to its channels; errors are logged, never returned.
func (d *Deliverer) Deliver(ctx context.Context, alerts []Alert) {
	byOrg := map[int64][]Alert{}
	for _, a := range alerts {
		// Operator-only self-monitoring alerts reach history only, never an org's channel.
		if a.Labels[ruleLabelScope] == ruleScopeInternal {
			continue
		}
		// Grafana's synthetic evaluation-failure alerts inherit the rule's static
		// labels (incl. organization_id); they are operator signal, not tenant alerts.
		if name := a.Labels["alertname"]; name == "DatasourceError" || name == "DatasourceNoData" {
			continue
		}
		orgID, err := strconv.ParseInt(a.Labels[ruleLabelOrganizationID], 10, 64)
		if err != nil || orgID == 0 {
			continue
		}
		byOrg[orgID] = append(byOrg[orgID], a)
	}
	for orgID, orgAlerts := range byOrg {
		d.deliverOrg(ctx, orgID, orgAlerts)
	}
}

func (d *Deliverer) deliverOrg(ctx context.Context, orgID int64, orgAlerts []Alert) {
	recs, err := d.channels.List(ctx, orgID)
	if err != nil {
		slog.Error("alerts.deliver_list_channels_failed", "org", orgID, "err", err)
		return
	}
	if len(recs) == 0 {
		return
	}
	identities := d.resolveDevices(ctx, orgID, orgAlerts)
	// Deliver channels concurrently (bounded) so one slow destination can't delay the others.
	sem := make(chan struct{}, maxDeliveryConcurrency)
	var wg sync.WaitGroup
	for _, rec := range recs {
		wg.Add(1)
		sem <- struct{}{}
		go func(rec ChannelRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			d.deliverChannel(ctx, orgID, rec, orgAlerts, identities)
		}(rec)
	}
	wg.Wait()
}

func (d *Deliverer) deliverChannel(ctx context.Context, orgID int64, rec ChannelRecord, orgAlerts []Alert, identities map[string]DeviceIdentity) {
	cfg, err := decodeChannelConfig(d.crypto, rec.EncryptedConfig)
	if err != nil {
		slog.Error("alerts.deliver_decode_failed", "org", orgID, "channel", rec.ID, "err", err)
		return
	}
	body, err := d.render(rec.Kind, orgID, orgAlerts, identities)
	if err != nil {
		slog.Error("alerts.deliver_render_failed", "org", orgID, "channel", rec.ID, "err", err)
		return
	}
	if err := d.send(ctx, rec.Kind, cfg, body); err != nil {
		slog.Error("alerts.deliver_send_failed", "org", orgID, "channel", rec.ID, "kind", rec.Kind, "err", err)
	}
}

// resolveDevices looks up friendly name+MAC for the batch's device_ids in one query; a failure degrades to raw ids.
func (d *Deliverer) resolveDevices(ctx context.Context, orgID int64, alerts []Alert) map[string]DeviceIdentity {
	seen := map[string]bool{}
	var ids []string
	for _, a := range alerts {
		if id := a.Labels["device_id"]; id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	m, err := d.devices.DeviceIdentities(ctx, orgID, ids)
	if err != nil {
		slog.Warn("alerts.deliver_device_lookup_failed", "org", orgID, "err", err)
		return nil
	}
	return m
}

func (d *Deliverer) render(kind ChannelKind, orgID int64, alerts []Alert, identities map[string]DeviceIdentity) ([]byte, error) {
	var payload any
	switch kind {
	case ChannelKindSlack:
		payload = renderSlack(d.publicURL, alerts, identities)
	case ChannelKindWebhook:
		payload = renderWebhook(orgID, alerts, identities)
	default:
		return nil, fmt.Errorf("unsupported channel kind %q", kind)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", kind, err)
	}
	return b, nil
}

func (d *Deliverer) send(ctx context.Context, kind ChannelKind, cfg channelConfig, body []byte) error {
	bearer := ""
	if kind == ChannelKindWebhook {
		bearer = cfg.Bearer
	}
	return d.post(ctx, cfg.URL, bearer, body)
}

// SendTest posts a synthetic notification and reports whether it was accepted (implements ChannelTester).
func (d *Deliverer) SendTest(ctx context.Context, kind ChannelKind, url, bearer string) (bool, string, error) {
	sample := []Alert{{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "Proto Fleet test alert", "severity": "info"},
		Annotations: map[string]string{"summary": "This is a test notification from Proto Fleet."},
	}}
	body, err := d.render(kind, 0, sample, nil)
	if err != nil {
		return false, "", err
	}
	sendBearer := ""
	if kind == ChannelKindWebhook {
		sendBearer = bearer
	}
	if err := d.post(ctx, url, sendBearer, body); err != nil {
		// Surface the failure to the caller as a message, scrubbed of any echoed secret.
		return false, scrubSecretSubstrings(err.Error()), nil
	}
	return true, "", nil
}

func (d *Deliverer) post(ctx context.Context, rawURL, bearer string, body []byte) error {
	// Re-check at send time (the actual egress): policy or DNS may have changed since the write-time validation.
	if err := checkDestinationURL(ctx, d.policy, rawURL, "channel"); err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, perSendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		// The transport error can quote the destination URL (capability token); scrub it.
		return fmt.Errorf("post: %s", scrubSecretSubstrings(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("destination returned status %d", resp.StatusCode)
	}
	return nil
}

// firing/resolved partition, stable by alertname then device for a deterministic message.
func partitionAlerts(alerts []Alert) (firing, resolved []Alert) {
	for _, a := range alerts {
		if a.Status == "resolved" {
			resolved = append(resolved, a)
		} else {
			firing = append(firing, a)
		}
	}
	sortAlerts(firing)
	sortAlerts(resolved)
	return firing, resolved
}

func sortAlerts(alerts []Alert) {
	sort.SliceStable(alerts, func(i, j int) bool {
		if alerts[i].Labels["alertname"] != alerts[j].Labels["alertname"] {
			return alerts[i].Labels["alertname"] < alerts[j].Labels["alertname"]
		}
		return alerts[i].Labels["device_id"] < alerts[j].Labels["device_id"]
	})
}

// deviceSuffix renders " — <name> (<MAC>)" for an alert's device, falling back to the raw id.
func deviceSuffix(a Alert, identities map[string]DeviceIdentity) string {
	id := a.Labels["device_id"]
	if id == "" {
		return ""
	}
	ident := identities[id]
	name := escapeMrkdwn(strings.TrimSpace(ident.Name))
	mac := escapeMrkdwn(ident.MAC)
	switch {
	case name != "" && mac != "":
		return fmt.Sprintf(" — %s (%s)", name, mac)
	case name != "":
		return " — " + name
	case mac != "":
		return " — " + mac
	default:
		return " — " + escapeMrkdwn(id)
	}
}
