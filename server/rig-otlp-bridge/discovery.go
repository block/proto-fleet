package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
)

// rigInfo identifies a discovered rig and carries the labels that will be
// stamped onto every OTLP Resource pushed downstream.
type rigInfo struct {
	// host:port of telemetry-service on this rig. Stream workers dial
	// this; the registry keys on it.
	address string
	labels  map[string]string
	// Last successful identity/enrichment probe (fleet mode); bounds how
	// long cached labels may ride out enrichment failures.
	verifiedAt time.Time
}

type registry struct {
	mu   sync.RWMutex
	rigs map[string]*rigInfo // keyed by address
}

func newRegistry() *registry {
	return &registry{rigs: make(map[string]*rigInfo)}
}

func (r *registry) replace(found []*rigInfo) (added, removed []*rigInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	next := make(map[string]*rigInfo, len(found))
	for _, info := range found {
		next[info.address] = info
	}
	for addr, info := range next {
		prev, existed := r.rigs[addr]
		if !existed {
			added = append(added, info)
			continue
		}
		// A label change restarts the worker (removed+added) so new
		// series carry the new context.
		if !labelsEqual(prev.labels, info.labels) {
			removed = append(removed, prev)
			added = append(added, info)
		}
	}
	for addr, info := range r.rigs {
		if _, kept := next[addr]; !kept {
			removed = append(removed, info)
		}
	}
	r.rigs = next
	return added, removed
}

func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func (r *registry) snapshot() []*rigInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*rigInfo, 0, len(r.rigs))
	for _, v := range r.rigs {
		out = append(out, v)
	}
	return out
}

type target struct {
	host string
	port int
}

func (t target) address() string {
	return net.JoinHostPort(t.host, strconv.Itoa(t.port))
}

// iterTargets expands subnets + literal targets into a flat list of
// telemetry-service host:port candidates. Subnet hosts and bare targets
// inherit `telemetry_port`; explicit `host:port` targets keep their port
// override so an operator can point at a non-standard deployment.
func iterTargets(cfg *Config) ([]target, error) {
	var targets []target
	for _, s := range cfg.Subnets {
		_, network, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("invalid subnet %q: %w", s, err)
		}
		networkAddr := network.IP.Mask(network.Mask)
		ones, bits := network.Mask.Size()
		skipEndpoints := bits-ones > 1
		for ip := cloneIP(networkAddr); network.Contains(ip); incIP(ip) {
			if skipEndpoints {
				if ip.Equal(networkAddr) || isBroadcast(ip, network) {
					continue
				}
			}
			targets = append(targets, target{host: ip.String(), port: cfg.TelemetryPort})
		}
	}
	for _, raw := range cfg.Targets {
		t, err := parseTarget(raw, cfg.TelemetryPort)
		if err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, nil
}

func parseTarget(raw string, defaultPort int) (target, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return target{}, fmt.Errorf("empty target")
	}
	if strings.Contains(raw, "://") {
		return target{}, fmt.Errorf("target %q must be host or host:port, not a URL", raw)
	}
	host, portString, err := net.SplitHostPort(raw)
	if err != nil {
		return target{host: raw, port: defaultPort}, nil
	}
	if host == "" {
		return target{}, fmt.Errorf("target %q has no host", raw)
	}
	port, err := net.LookupPort("tcp", portString)
	if err != nil {
		return target{}, fmt.Errorf("invalid target %q: %w", raw, err)
	}
	return target{host: host, port: port}, nil
}

func cloneIP(ip net.IP) net.IP {
	dup := make(net.IP, len(ip))
	copy(dup, ip)
	return dup
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func isBroadcast(ip net.IP, network *net.IPNet) bool {
	for i := range ip {
		if ip[i] != network.IP[i]|^network.Mask[i] {
			return false
		}
	}
	return true
}

// scanLoop periodically discovers rigs (fleet RPC when fleetSrc is set,
// else subnet probing) and reconciles per-rig stream workers.
func scanLoop(
	ctx context.Context,
	cfg *Config,
	reg *registry,
	streams *streamManager,
	fleetSrc *fleetTargetSource,
) {
	ticker := time.NewTicker(time.Duration(cfg.ScanIntervalS * float64(time.Second)))
	defer ticker.Stop()

	scan := func() {
		var found []*rigInfo
		if fleetSrc != nil {
			known := make(map[string]*rigInfo)
			for _, info := range reg.snapshot() {
				known[info.address] = info
			}
			var err error
			found, err = fleetSrc.discover(ctx, cfg, known)
			if err != nil {
				if isFleetAuthError(err) {
					// Revoked credentials must actually revoke: tear the
					// workers down instead of streaming indefinitely.
					log.Printf("scan: %v (fleet auth revoked; stopping all rigs)", err)
					found = nil
				} else {
					// A failed listing says nothing about rig reachability:
					// keep current workers and retry next tick.
					log.Printf("scan: %v (keeping current rigs)", err)
					return
				}
			}
		} else {
			found = scanTargets(ctx, cfg)
		}
		added, removed := reg.replace(found)
		log.Printf("scan: %d rigs discovered (+%d, -%d)", len(found), len(added), len(removed))
		// Stop before start: a label change lists the same address in
		// both, and start() is a no-op while the stale worker exists.
		for _, info := range removed {
			streams.stop(info.address)
		}
		for _, info := range added {
			streams.start(ctx, info)
		}
	}

	scan() // initial pass
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scan()
		}
	}
}

func scanTargets(ctx context.Context, cfg *Config) []*rigInfo {
	targets, err := iterTargets(cfg)
	if err != nil {
		log.Printf("iterTargets: %v", err)
		return nil
	}

	work := make(chan target, len(targets))
	for _, t := range targets {
		work <- t
	}
	close(work)

	var (
		mu         sync.Mutex
		discovered []*rigInfo
		wg         sync.WaitGroup
	)
	timeout := time.Duration(cfg.ProbeTimeoutS * float64(time.Second))
	workerCount := cfg.Workers
	if len(targets) < workerCount {
		workerCount = len(targets)
	}
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range work {
				if info := probeRig(ctx, t, cfg.Site, cfg.APIScheme, cfg.APIPort, timeout); info != nil {
					mu.Lock()
					discovered = append(discovered, info)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return discovered
}

// probeRig dials plaintext gRPC to a candidate's telemetry-service port.
// A successful connection means the target is reachable enough for stream
// workers to try. The stream worker performs the actual RPC and reconnects
// with backoff if the service is not ready yet.
func probeRig(ctx context.Context, t target, site, apiScheme string, apiPort int, timeout time.Duration) *rigInfo {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := grpc.NewClient(
		t.address(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil
	}
	defer conn.Close()
	if !waitForReady(probeCtx, conn) {
		return nil
	}

	labels, err := buildLabels(probeCtx, t.host, site, apiScheme, apiPort)
	if err != nil {
		log.Printf("hostname lookup via REST failed for %s:%d: %v", t.host, apiPort, err)
		return nil
	}

	return &rigInfo{
		address: t.address(),
		labels:  labels,
	}
}

func waitForReady(ctx context.Context, conn *grpc.ClientConn) bool {
	conn.Connect()
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return true
		}
		if !conn.WaitForStateChange(ctx, state) {
			return false
		}
	}
}

// buildLabels assembles the discovery labels that get stamped onto every OTLP
// Resource. `hostname` must come from the rig REST API before streaming starts.
func buildLabels(ctx context.Context, targetHost, site, apiScheme string, apiPort int) (map[string]string, error) {
	labels := make(map[string]string, 3)
	hostname, err := fetchRESTHostname(ctx, targetHost, apiScheme, apiPort)
	if err != nil {
		return nil, err
	}
	if hostname == "" {
		return nil, fmt.Errorf("GET %s://%s/api/v1/network returned empty hostname", apiScheme, net.JoinHostPort(targetHost, strconv.Itoa(apiPort)))
	}
	labels["hostname"] = hostname
	if ip := net.ParseIP(targetHost); ip != nil {
		labels["rig_ip"] = ip.String()
	}
	if site != "" {
		labels["site"] = site
	}
	return labels, nil
}

type networkInfoResponse struct {
	NetworkInfo *struct {
		Hostname string `json:"hostname"`
	} `json:"network-info"`
}

func fetchRESTHostname(ctx context.Context, host, apiScheme string, apiPort int) (string, error) {
	base := url.URL{
		Scheme: apiScheme,
		Host:   net.JoinHostPort(host, strconv.Itoa(apiPort)),
	}
	return fetchRESTHostnameURL(ctx, base.String())
}

// rigAPIHTTPClient skips TLS verification: rigs serve self-signed certs
// and are discovered as https, matching the proto plugin and minerproxy.
var rigAPIHTTPClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- rigs use self-signed certs on the site LAN.
	},
}

// fetchRESTHostnameURL reads the rig hostname from baseURL/api/v1/network.
// An empty hostname is an error: empty labels are invisible to dashboards.
func fetchRESTHostnameURL(ctx context.Context, baseURL string) (string, error) {
	u := baseURL + "/api/v1/network"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}

	resp, err := rigAPIHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET %s returned %s", u, resp.Status)
	}

	var decoded networkInfoResponse
	// The rig is untrusted: bound the response body before decoding.
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxNetworkInfoBytes)).Decode(&decoded); err != nil {
		return "", err
	}
	if decoded.NetworkInfo == nil {
		return "", fmt.Errorf("GET %s did not include network-info", u)
	}
	hostname := strings.TrimSpace(decoded.NetworkInfo.Hostname)
	if hostname == "" {
		return "", fmt.Errorf("GET %s returned empty hostname", u)
	}
	if len(hostname) > maxHostnameLen || !validHostname(hostname) {
		return "", fmt.Errorf("GET %s returned invalid hostname %q", u, truncateForLog(hostname))
	}
	return hostname, nil
}

const (
	// maxNetworkInfoBytes bounds the untrusted /api/v1/network response.
	maxNetworkInfoBytes = 64 << 10
	// maxHostnameLen matches the DNS name length limit; longer values are
	// label abuse, not hostnames.
	maxHostnameLen = 253
)

// validHostname allows DNS-style names only, so a malicious rig cannot
// smuggle arbitrary bytes into a promoted Prometheus label.
func validHostname(hostname string) bool {
	for _, r := range hostname {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
		default:
			return false
		}
	}
	return true
}

func truncateForLog(s string) string {
	if len(s) > 64 {
		return s[:64] + "..."
	}
	return s
}
