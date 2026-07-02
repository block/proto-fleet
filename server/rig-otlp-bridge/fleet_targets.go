package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
)

// fleetTargetSource discovers rigs and their enrichment labels via
// proto-fleet's ListMinerStateSnapshots RPC (Connect protocol, API key).
type fleetTargetSource struct {
	client fleetmanagementv1connect.FleetManagementServiceClient
	apiKey string
	models []string
}

const (
	fleetPageSize = 1000
	// fleetMaxPages bounds the pagination loop against a misbehaving
	// server; 100 pages = 100k devices, far beyond any single site.
	fleetMaxPages = 100
	// identityGraceWindow caps how long cached labels survive failed
	// identity probes before the rig is dropped until re-verified.
	identityGraceWindow = 10 * time.Minute
)

func newFleetTargetSource(apiURL, apiKey string, models []string, allowInsecureHTTP bool) (*fleetTargetSource, error) {
	u, err := url.Parse(apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid fleet_api_url %q: %w", apiURL, err)
	}
	switch u.Scheme {
	case "https":
	case "http":
		// The API key rides every request: plaintext is loopback-only
		// unless the operator explicitly accepts the local-link risk.
		if !allowInsecureHTTP && !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf(
				"fleet_api_url %q: plain http is loopback-only; use https or set fleet_api_insecure_http", apiURL)
		}
	default:
		return nil, fmt.Errorf("fleet_api_url %q: scheme must be http (loopback) or https", apiURL)
	}
	return &fleetTargetSource{
		client: fleetmanagementv1connect.NewFleetManagementServiceClient(&http.Client{}, apiURL),
		apiKey: apiKey,
		models: models,
	}, nil
}

// isFleetAuthError reports a revoked/denied credential — deterministic, so
// the scan loop must stop exporting rather than ride it out as transient.
func isFleetAuthError(err error) bool {
	code := connect.CodeOf(err)
	return code == connect.CodeUnauthenticated || code == connect.CodePermissionDenied
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}

// listSnapshots pages through ListMinerStateSnapshots for paired,
// directly-dialable proto rigs.
func (f *fleetTargetSource) listSnapshots(ctx context.Context) ([]*fleetmanagementv1.MinerStateSnapshot, error) {
	var out []*fleetmanagementv1.MinerStateSnapshot
	cursor := ""
	for range fleetMaxPages {
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		req := connect.NewRequest(&fleetmanagementv1.ListMinerStateSnapshotsRequest{
			PageSize: fleetPageSize,
			Cursor:   cursor,
			Filter: &fleetmanagementv1.MinerListFilter{
				Models: f.models,
				PairingStatuses: []fleetmanagementv1.PairingStatus{
					fleetmanagementv1.PairingStatus_PAIRING_STATUS_PAIRED,
					fleetmanagementv1.PairingStatus_PAIRING_STATUS_DEFAULT_PASSWORD,
				},
			},
		})
		req.Header().Set("Authorization", "Bearer "+f.apiKey)
		resp, err := f.client.ListMinerStateSnapshots(callCtx, req)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("fleet target listing failed: %w", err)
		}
		out = append(out, resp.Msg.GetMiners()...)
		cursor = resp.Msg.GetCursor()
		if cursor == "" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("fleet target listing failed: pagination exceeded %d pages", fleetMaxPages)
}

// discover turns fleet's snapshot list into rigInfos. Rigs that fail the
// probe or hostname lookup keep their previous rigInfo when already known
// (a transient enrichment failure must not stop a healthy stream); unknown
// ones are skipped and retried next scan. A listing failure is an error,
// not an empty set.
func (f *fleetTargetSource) discover(ctx context.Context, cfg *Config, known map[string]*rigInfo) ([]*rigInfo, error) {
	miners, err := f.listSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(cfg.ProbeTimeoutS * float64(time.Second))
	work := make(chan *fleetmanagementv1.MinerStateSnapshot, len(miners))
	for _, m := range miners {
		work <- m
	}
	close(work)

	workerCount := cfg.Workers
	if len(miners) < workerCount {
		workerCount = len(miners)
	}

	var (
		mu         sync.Mutex
		discovered []*rigInfo
		wg         sync.WaitGroup
	)
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range work {
				info, foreignDevice := probeFleetTarget(ctx, m, cfg, timeout)
				// A confirmed identity mismatch must not fall back to the
				// cached info: the address no longer belongs to this rig.
				if info == nil && !foreignDevice {
					address := net.JoinHostPort(m.GetIpAddress(), strconv.Itoa(cfg.TelemetryPort))
					// Same device only, and only within the verification
					// grace window: unverifiable addresses must not stream
					// under cached labels indefinitely.
					if prev, ok := known[address]; ok &&
						prev.labels["device_identifier"] == m.GetDeviceIdentifier() &&
						time.Since(prev.verifiedAt) <= identityGraceWindow {
						info = prev
					}
				}
				if info != nil {
					mu.Lock()
					discovered = append(discovered, info)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	// Deterministic order: when two targets share an IP the same one wins
	// every scan, instead of flapping the worker on label changes.
	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].labels["device_identifier"] < discovered[j].labels["device_identifier"]
	})
	return discovered, nil
}

// allowLoopbackTargets is a test hook: production always rejects loopback.
var allowLoopbackTargets = false

// routableTargetIP rejects addresses a poisoned fleet record must not make
// the host-networked bridge dial (loopback/link-local/multicast/unspecified).
func routableTargetIP(ipAddress string) bool {
	addr, err := netip.ParseAddr(ipAddress)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return allowLoopbackTargets
	}
	return !(addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsUnspecified())
}

// probeFleetTarget returns the enriched rigInfo, or nil on failure; the
// second result reports a confirmed identity mismatch at the address.
func probeFleetTarget(ctx context.Context, m *fleetmanagementv1.MinerStateSnapshot, cfg *Config, timeout time.Duration) (*rigInfo, bool) {
	if !routableTargetIP(m.GetIpAddress()) {
		log.Printf("skipping %s: address %q is not a routable rig address", m.GetDeviceIdentifier(), m.GetIpAddress())
		return nil, false
	}
	if !cfg.fleetTargetAllowed(m.GetIpAddress()) {
		log.Printf("skipping %s: address %q is outside fleet_target_cidrs", m.GetDeviceIdentifier(), m.GetIpAddress())
		return nil, false
	}
	address := net.JoinHostPort(m.GetIpAddress(), strconv.Itoa(cfg.TelemetryPort))

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, false
	}
	defer conn.Close()
	if !waitForReady(probeCtx, conn) {
		return nil, false
	}

	// Identity check: the live rig must match the serial (or, failing
	// that, the MAC) fleet paired, or a reused IP could stream under
	// another device's labels. No verifiable identity = fail closed.
	base := fmt.Sprintf("%s://%s", cfg.APIScheme, net.JoinHostPort(m.GetIpAddress(), strconv.Itoa(cfg.APIPort)))
	if m.GetSerialNumber() == "" && m.GetMacAddress() == "" {
		log.Printf("skipping %s: fleet record has no serial or MAC to verify the rig's identity", m.GetDeviceIdentifier())
		return nil, false
	}
	idCtx, idCancel := context.WithTimeout(ctx, timeout)
	defer idCancel()
	serial, mac, err := fetchRESTPairingInfo(idCtx, base)
	if err != nil {
		log.Printf("identity lookup via REST failed for %s (%s): %v", m.GetDeviceIdentifier(), m.GetIpAddress(), err)
		return nil, false
	}
	switch {
	case m.GetSerialNumber() != "":
		if serial != m.GetSerialNumber() {
			log.Printf("skipping %s: rig at %s reports serial %q, fleet has %q (identity mismatch)",
				m.GetDeviceIdentifier(), m.GetIpAddress(), serial, m.GetSerialNumber())
			return nil, true
		}
	default:
		if !strings.EqualFold(mac, m.GetMacAddress()) {
			log.Printf("skipping %s: rig at %s reports mac %q, fleet has %q (identity mismatch)",
				m.GetDeviceIdentifier(), m.GetIpAddress(), mac, m.GetMacAddress())
			return nil, true
		}
	}

	// Fresh budget: the gRPC probe must not starve the hostname fetch.
	restCtx, restCancel := context.WithTimeout(ctx, timeout)
	defer restCancel()
	hostname, err := fetchRESTHostnameURL(restCtx, base)
	if err != nil {
		log.Printf("hostname lookup via REST failed for %s (%s): %v", m.GetDeviceIdentifier(), m.GetIpAddress(), err)
		return nil, false
	}

	labels := map[string]string{
		"hostname":          hostname,
		"device_identifier": m.GetDeviceIdentifier(),
	}
	if ip := net.ParseIP(m.GetIpAddress()); ip != nil {
		labels["rig_ip"] = ip.String()
	}
	placement := m.GetPlacement()
	for key, value := range map[string]string{
		"site":     placement.GetSite().GetLabel(),
		"building": placement.GetBuilding().GetLabel(),
		"rack":     placement.GetRack().GetLabel(),
		"zone":     placement.GetZone(),
	} {
		if value != "" {
			labels[key] = value
		}
	}

	return &rigInfo{
		address:    address,
		labels:     labels,
		verifiedAt: time.Now(),
	}, false
}

// fetchRESTPairingInfo reads the rig's serial + MAC from the unauthenticated
// pairing-info endpoint, so targets can be identity-checked against fleet.
func fetchRESTPairingInfo(ctx context.Context, baseURL string) (serial, mac string, err error) {
	u := baseURL + "/api/v1/pairing/info"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := rigAPIHTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("GET %s returned %s", u, resp.Status)
	}
	var decoded struct {
		CbSn string `json:"cb_sn"`
		Mac  string `json:"mac"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxNetworkInfoBytes)).Decode(&decoded); err != nil {
		return "", "", err
	}
	serial = strings.TrimSpace(decoded.CbSn)
	mac = strings.TrimSpace(decoded.Mac)
	if serial == "" && mac == "" {
		return "", "", fmt.Errorf("GET %s returned neither cb_sn nor mac", u)
	}
	return serial, mac, nil
}
