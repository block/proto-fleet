package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/grpc"

	commonfleetpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	fleetmanagementv1 "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1/fleetmanagementv1connect"
)

type fakeFleetServer struct {
	fleetmanagementv1connect.UnimplementedFleetManagementServiceHandler
	apiKey string
	miners []*fleetmanagementv1.MinerStateSnapshot
	// pages > 1 splits miners across cursor pages to exercise pagination.
	pages int

	mu        sync.Mutex
	gotFilter *fleetmanagementv1.MinerListFilter
	listCalls int
}

func (s *fakeFleetServer) ListMinerStateSnapshots(
	ctx context.Context,
	req *connect.Request[fleetmanagementv1.ListMinerStateSnapshotsRequest],
) (*connect.Response[fleetmanagementv1.ListMinerStateSnapshotsResponse], error) {
	if req.Header().Get("Authorization") != "Bearer "+s.apiKey {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("invalid api key"))
	}
	s.mu.Lock()
	s.gotFilter = req.Msg.GetFilter()
	s.listCalls++
	s.mu.Unlock()

	pages := s.pages
	if pages < 1 {
		pages = 1
	}
	per := (len(s.miners) + pages - 1) / pages
	if per == 0 {
		per = 1
	}
	page := 0
	if req.Msg.GetCursor() != "" {
		n, err := strconv.Atoi(req.Msg.GetCursor())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("bad cursor"))
		}
		page = n
	}
	lo := page * per
	hi := min(lo+per, len(s.miners))
	if lo > hi {
		lo = hi
	}
	resp := &fleetmanagementv1.ListMinerStateSnapshotsResponse{Miners: s.miners[lo:hi]}
	if hi < len(s.miners) {
		resp.Cursor = strconv.Itoa(page + 1)
	}
	return connect.NewResponse(resp), nil
}

// startFakeFleet serves the FleetManagementService over Connect on a
// loopback httptest server and returns its base URL.
func startFakeFleet(t *testing.T, srv *fakeFleetServer) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(fleetmanagementv1connect.NewFleetManagementServiceHandler(srv))
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)
	return httpSrv.URL
}

// startFakeRig serves /api/v1/network + /api/v1/pairing/info plus a listener
// standing in for the telemetry gRPC port; returns (ip, apiPort, telemetryPort).
func startFakeRig(t *testing.T, hostname string) (string, string, int) {
	return startFakeRigWithSerial(t, hostname, "dev-1-serial")
}

func startFakeRigWithSerial(t *testing.T, hostname, serial string) (string, string, int) {
	t.Helper()
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/network":
			_, _ = fmt.Fprintf(w, `{"network-info":{"hostname":%q}}`, hostname)
		case "/api/v1/pairing/info":
			_, _ = fmt.Fprintf(w, `{"cb_sn":%q,"mac":"00:11:22:33:44:55"}`, serial)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(rest.Close)
	restURL, err := url.Parse(rest.URL)
	if err != nil {
		t.Fatalf("parse rest url: %v", err)
	}

	telemetryLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("telemetry listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	go func() { _ = grpcServer.Serve(telemetryLis) }()
	t.Cleanup(grpcServer.Stop)
	_, telemetryPortStr, _ := net.SplitHostPort(telemetryLis.Addr().String())
	telemetryPort, _ := strconv.Atoi(telemetryPortStr)

	return restURL.Hostname(), restURL.Port(), telemetryPort
}

func snapshotFor(id, ip, site string) *fleetmanagementv1.MinerStateSnapshot {
	return &fleetmanagementv1.MinerStateSnapshot{
		DeviceIdentifier: id,
		SerialNumber:     "dev-1-serial",
		IpAddress:        ip,
		Placement: &commonfleetpb.PlacementRefs{
			Site: &commonfleetpb.ResourceRef{Id: 1, Label: site},
		},
	}
}

func init() {
	// Tests run fake rigs on loopback; production rejects loopback targets.
	allowLoopbackTargets = true
}

func fleetTestConfig(telemetryPort int, apiPort string) *Config {
	cfg := &Config{TelemetryPort: telemetryPort}
	if n, err := strconv.Atoi(apiPort); err == nil {
		cfg.APIPort = n
	}
	cfg.applyDefaults()
	cfg.ProbeTimeoutS = 2
	return cfg
}

func TestFleetDiscoverBuildsEnrichedRigInfo(t *testing.T) {
	ip, apiPort, telemetryPort := startFakeRig(t, "proto-miner-a1b2")
	snap := snapshotFor("dev-1", ip, "stl")
	snap.Placement.Building = &commonfleetpb.ResourceRef{Id: 2, Label: "b1"}
	snap.Placement.Rack = &commonfleetpb.ResourceRef{Id: 3, Label: "r7"}
	snap.Placement.Zone = "" // unassigned: must not become an empty label
	fleetURL := startFakeFleet(t, &fakeFleetServer{apiKey: "secret", miners: []*fleetmanagementv1.MinerStateSnapshot{snap}})

	src, err := newFleetTargetSource(fleetURL, "secret", []string{"Rig"}, false)
	if err != nil {
		t.Fatalf("newFleetTargetSource: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	found, err := src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), nil)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("discovered %d rigs, want 1", len(found))
	}

	info := found[0]
	wantAddr := net.JoinHostPort(ip, strconv.Itoa(telemetryPort))
	if info.address != wantAddr {
		t.Errorf("address = %q, want %q", info.address, wantAddr)
	}
	want := map[string]string{
		"hostname":          "proto-miner-a1b2",
		"device_identifier": "dev-1",
		"rig_ip":            ip,
		"site":              "stl",
		"building":          "b1",
		"rack":              "r7",
	}
	if !labelsEqual(info.labels, want) {
		t.Errorf("labels = %v, want %v", info.labels, want)
	}
}

func TestFleetDiscoverPaginatesAndFilters(t *testing.T) {
	ip, _, _ := startFakeRig(t, "proto-miner-a1b2")
	fake := &fakeFleetServer{
		apiKey: "secret",
		miners: []*fleetmanagementv1.MinerStateSnapshot{
			snapshotFor("dev-1", ip, "stl"),
			snapshotFor("dev-2", ip, "stl"),
			snapshotFor("dev-3", ip, "stl"),
		},
		pages: 3,
	}
	fleetURL := startFakeFleet(t, fake)

	src, err := newFleetTargetSource(fleetURL, "secret", []string{"Rig"}, false)
	if err != nil {
		t.Fatalf("newFleetTargetSource: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	miners, err := src.listSnapshots(ctx)
	if err != nil {
		t.Fatalf("listSnapshots: %v", err)
	}
	if len(miners) != 3 {
		t.Fatalf("listed %d miners across pages, want 3", len(miners))
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.listCalls != 3 {
		t.Errorf("list calls = %d, want 3 (one per page)", fake.listCalls)
	}
	f := fake.gotFilter
	if f == nil || len(f.GetModels()) != 1 || f.GetModels()[0] != "Rig" ||
		len(f.GetPairingStatuses()) != 2 {
		t.Errorf("filter = %+v, want Rig model + 2 pairing statuses", f)
	}
}

func TestFleetDiscoverKeepsKnownRigOnTransientEnrichmentFailure(t *testing.T) {
	// A snapshot whose REST endpoint is dead: hostname lookup fails, but
	// the rig is already known/streaming, so its previous info is kept.
	deadPort, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	telemetryAddr := deadPort.Addr().String()
	grpcServer := grpc.NewServer()
	go func() { _ = grpcServer.Serve(deadPort) }()
	t.Cleanup(grpcServer.Stop)
	_, telemetryPortStr, _ := net.SplitHostPort(telemetryAddr)
	telemetryPort, _ := strconv.Atoi(telemetryPortStr)

	apiPort := "1" // refused: REST enrichment fails
	snap := snapshotFor("dev-1", "127.0.0.1", "stl")
	fleetURL := startFakeFleet(t, &fakeFleetServer{apiKey: "secret", miners: []*fleetmanagementv1.MinerStateSnapshot{snap}})
	src, err := newFleetTargetSource(fleetURL, "secret", []string{"Rig"}, false)
	if err != nil {
		t.Fatalf("newFleetTargetSource: %v", err)
	}

	address := net.JoinHostPort("127.0.0.1", telemetryPortStr)
	prev := &rigInfo{address: address, labels: map[string]string{"hostname": "known", "device_identifier": "dev-1"}, verifiedAt: time.Now()}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	found, err := src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), map[string]*rigInfo{address: prev})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 1 || found[0] != prev {
		t.Fatalf("found = %+v, want the previous rigInfo kept", found)
	}

	// The same failure for an UNKNOWN rig is skipped (retried next scan).
	found, err = src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), nil)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %d rigs, want 0 for unknown rig with failed enrichment", len(found))
	}

	// IP reuse: a different device at a known address must not inherit the
	// previous device's labels.
	stale := &rigInfo{address: address, labels: map[string]string{"hostname": "known", "device_identifier": "dev-OLD"}, verifiedAt: time.Now()}
	found, err = src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), map[string]*rigInfo{address: stale})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %d rigs, want 0 when the cached labels belong to another device", len(found))
	}

	// Past the grace window, unverifiable addresses drop even for the
	// same device: cached labels must not ride failures indefinitely.
	expired := &rigInfo{address: address, labels: map[string]string{"hostname": "known", "device_identifier": "dev-1"}, verifiedAt: time.Now().Add(-identityGraceWindow - time.Minute)}
	found, err = src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), map[string]*rigInfo{address: expired})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %d rigs, want 0 once the verification grace window expires", len(found))
	}
}

func TestFleetDiscoverDropsIdentityMismatch(t *testing.T) {
	// The live rig reports a different serial than fleet paired: skip it,
	// and do NOT fall back to cached labels for that address.
	ip, apiPort, telemetryPort := startFakeRigWithSerial(t, "proto-miner-a1b2", "some-other-device")
	snap := snapshotFor("dev-1", ip, "stl")
	fleetURL := startFakeFleet(t, &fakeFleetServer{apiKey: "secret", miners: []*fleetmanagementv1.MinerStateSnapshot{snap}})

	src, err := newFleetTargetSource(fleetURL, "secret", []string{"Rig"}, false)
	if err != nil {
		t.Fatalf("newFleetTargetSource: %v", err)
	}

	address := net.JoinHostPort(ip, strconv.Itoa(telemetryPort))
	prev := &rigInfo{address: address, labels: map[string]string{"hostname": "known", "device_identifier": "dev-1"}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	found, err := src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), map[string]*rigInfo{address: prev})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %d rigs, want 0 on identity mismatch", len(found))
	}
}

func TestFleetDiscoverIdentityFallsBackToMAC(t *testing.T) {
	ip, apiPort, telemetryPort := startFakeRig(t, "proto-miner-a1b2")

	// No serial on the record, matching MAC (fake rig serves 00:11:...).
	byMAC := snapshotFor("dev-1", ip, "stl")
	byMAC.SerialNumber = ""
	byMAC.MacAddress = "00:11:22:33:44:55"
	fleetURL := startFakeFleet(t, &fakeFleetServer{apiKey: "secret", miners: []*fleetmanagementv1.MinerStateSnapshot{byMAC}})
	src, err := newFleetTargetSource(fleetURL, "secret", []string{"Rig"}, false)
	if err != nil {
		t.Fatalf("newFleetTargetSource: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	found, err := src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), nil)
	if err != nil || len(found) != 1 {
		t.Fatalf("MAC-verified rig: found=%d err=%v, want 1", len(found), err)
	}

	// Wrong MAC: identity mismatch.
	wrongMAC := snapshotFor("dev-1", ip, "stl")
	wrongMAC.SerialNumber = ""
	wrongMAC.MacAddress = "AA:BB:CC:DD:EE:FF"
	fleetURL = startFakeFleet(t, &fakeFleetServer{apiKey: "secret", miners: []*fleetmanagementv1.MinerStateSnapshot{wrongMAC}})
	src, _ = newFleetTargetSource(fleetURL, "secret", []string{"Rig"}, false)
	found, err = src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), nil)
	if err != nil || len(found) != 0 {
		t.Fatalf("MAC mismatch: found=%d err=%v, want 0", len(found), err)
	}

	// Neither serial nor MAC on the record: nothing to verify, fail closed.
	unverifiable := snapshotFor("dev-1", ip, "stl")
	unverifiable.SerialNumber = ""
	fleetURL = startFakeFleet(t, &fakeFleetServer{apiKey: "secret", miners: []*fleetmanagementv1.MinerStateSnapshot{unverifiable}})
	src, _ = newFleetTargetSource(fleetURL, "secret", []string{"Rig"}, false)
	found, err = src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), nil)
	if err != nil || len(found) != 0 {
		t.Fatalf("unverifiable record: found=%d err=%v, want 0", len(found), err)
	}
}

func TestFleetDiscoverErrorsOnBadToken(t *testing.T) {
	ip, apiPort, telemetryPort := startFakeRig(t, "proto-miner-a1b2")
	fleetURL := startFakeFleet(t, &fakeFleetServer{apiKey: "secret", miners: []*fleetmanagementv1.MinerStateSnapshot{snapshotFor("dev-1", ip, "stl")}})

	src, err := newFleetTargetSource(fleetURL, "wrong-token", []string{"Rig"}, false)
	if err != nil {
		t.Fatalf("newFleetTargetSource: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// A listing failure must surface as an error — not an empty list —
	// so the scan loop keeps current workers streaming.
	found, err := src.discover(ctx, fleetTestConfig(telemetryPort, apiPort), nil)
	if err == nil {
		t.Fatal("discover succeeded with bad token, want error")
	}
	if len(found) != 0 {
		t.Fatalf("discovered %d rigs with bad token, want 0", len(found))
	}
	// ...except auth errors, which the scan loop treats as revocation.
	if !isFleetAuthError(err) {
		t.Errorf("bad-token error not classified as auth error: %v", err)
	}
	if isFleetAuthError(fmt.Errorf("dial tcp: connection refused")) {
		t.Error("transient error misclassified as auth error")
	}
}

func TestValidateAcceptsFleetModeWithoutSubnets(t *testing.T) {
	cfg := &Config{FleetAPIURL: "http://127.0.0.1:4000", FleetAPIToken: "secret"}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Source and token checks run only after env/flag overrides, so a
	// tuning-only config file loads even when fleet mode comes from env.
	empty := &Config{}
	empty.applyDefaults()
	if err := empty.validate(); err != nil {
		t.Fatalf("validate should defer source checks to overrides: %v", err)
	}
	if err := empty.validateTargetSource(); err == nil {
		t.Fatal("validateTargetSource accepted a config with no fleet URL, subnets, or targets")
	}

	tokenless := &Config{FleetAPIURL: "http://127.0.0.1:4000"}
	tokenless.applyDefaults()
	if err := tokenless.validateTargetSource(); err == nil {
		t.Fatal("validateTargetSource accepted fleet mode without a token")
	}
}

func TestMergeLabelsDropsDuplicateBridgeOwnedKeys(t *testing.T) {
	dup := func(v string) *commonpb.KeyValue {
		return &commonpb.KeyValue{Key: "hostname", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
	}
	merged := mergeLabels(
		[]*commonpb.KeyValue{dup("spoof-1"), dup("spoof-2"), {Key: "custom", Value: dup("keep").Value}},
		map[string]string{"hostname": "from-bridge"},
	)
	var hostnames []string
	for _, kv := range merged {
		if kv.GetKey() == "hostname" {
			hostnames = append(hostnames, kv.GetValue().GetStringValue())
		}
	}
	if len(hostnames) != 1 || hostnames[0] != "from-bridge" {
		t.Fatalf("hostname attrs = %v, want exactly [from-bridge]", hostnames)
	}
	if len(merged) != 2 {
		t.Fatalf("merged = %d attrs, want 2 (custom + hostname)", len(merged))
	}

	// Bridge-owned keys are dropped even when the bridge sets no value:
	// an unracked rig must not supply its own placement labels.
	spoofSite := &commonpb.KeyValue{Key: "site", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "spoofed"}}}
	merged = mergeLabels([]*commonpb.KeyValue{spoofSite}, map[string]string{"hostname": "from-bridge"})
	for _, kv := range merged {
		if kv.GetKey() == "site" {
			t.Fatalf("rig-supplied site label survived: %q", kv.GetValue().GetStringValue())
		}
	}
}

func TestFetchHostnameRejectsInvalidAndOversized(t *testing.T) {
	cases := map[string]string{
		"too-long":  strings.Repeat("a", 300),
		"bad-chars": "rig{one}\u0000",
		"spacey":    "rig one",
	}
	for name, hostname := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"network-info": map[string]any{"hostname": hostname}})
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if got, err := fetchRESTHostnameURL(ctx, srv.URL); err == nil {
				t.Fatalf("accepted invalid hostname: %q", got)
			}
		})
	}
}

func TestRoutableTargetIP(t *testing.T) {
	allowLoopbackTargets = false
	defer func() { allowLoopbackTargets = true }()
	for ip, want := range map[string]bool{
		"172.16.2.42": true, "2001:db8::1": true,
		"127.0.0.1": false, "169.254.169.254": false, "0.0.0.0": false,
		"224.0.0.1": false, "fe80::1": false, "not-an-ip": false,
	} {
		if got := routableTargetIP(ip); got != want {
			t.Errorf("routableTargetIP(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestUploaderFlushesOnPendingCapBetweenTicks(t *testing.T) {
	posts := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		posts <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	up := newMetricsUploader(srv.URL, 4, false)
	up.flushInterval = time.Hour // the count cap, not the ticker, must flush
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go up.run(ctx)

	payload := mustMarshalMetrics(t, "mcdd", nil)
	info := &rigInfo{address: "rig-1:2123", labels: map[string]string{"hostname": "rig-1"}}
	for range 8 {
		up.enqueue(info, payload)
	}

	select {
	case <-posts:
	case <-time.After(5 * time.Second):
		t.Fatal("no flush before the ticker despite pending cap being reached")
	}
}

func TestNewFleetTargetSourceSchemes(t *testing.T) {
	for _, u := range []string{"http://127.0.0.1:4000", "http://localhost:4000", "https://fleet.example"} {
		if _, err := newFleetTargetSource(u, "k", []string{"Rig"}, false); err != nil {
			t.Errorf("newFleetTargetSource(%q): %v", u, err)
		}
	}
	// Plain http off loopback leaks the API key: opt-in only.
	if _, err := newFleetTargetSource("http://fleet:4000", "k", []string{"Rig"}, false); err == nil {
		t.Error("non-loopback plain http accepted without opt-in")
	}
	if _, err := newFleetTargetSource("http://fleet:4000", "k", []string{"Rig"}, true); err != nil {
		t.Error("insecure-http opt-in rejected")
	}
	if _, err := newFleetTargetSource("grpc://fleet:4000", "k", []string{"Rig"}, false); err == nil {
		t.Error("non-http(s) scheme accepted")
	}
}

func TestValidateRejectsOversizedScanSubnets(t *testing.T) {
	small := &Config{Subnets: []string{"172.16.2.0/24", "2001:db8::/120"}}
	small.applyDefaults()
	if err := small.validateTargetSource(); err != nil {
		t.Fatalf("small subnets rejected: %v", err)
	}
	for _, s := range []string{"10.0.0.0/8", "2001:db8::/64", "not-a-cidr"} {
		big := &Config{Subnets: []string{s}}
		big.applyDefaults()
		if err := big.validateTargetSource(); err == nil {
			t.Errorf("subnet %q accepted, want error", s)
		}
	}

	// Malformed standalone targets fail fast instead of nulling every scan.
	for _, target := range []string{"http://rig:2123", ":2123", "rig:not-a-port"} {
		bad := &Config{Targets: []string{target}}
		bad.applyDefaults()
		if err := bad.validateTargetSource(); err == nil {
			t.Errorf("target %q accepted, want error", target)
		}
	}

	// Fleet mode ignores subnets/targets: a migrated scan config must boot.
	migrated := &Config{FleetAPIURL: "http://fleet", FleetAPIToken: "fleet_x_x", Subnets: []string{"10.0.0.0/8"}, Targets: []string{"http://rig:2123"}}
	migrated.applyDefaults()
	if err := migrated.validateTargetSource(); err != nil {
		t.Errorf("fleet mode rejected leftover scan config: %v", err)
	}
}

func TestFleetTargetCIDRAllowlist(t *testing.T) {
	cfg := &Config{
		FleetAPIURL:      "http://fleet",
		FleetAPIToken:    "fleet_x_x",
		FleetTargetCIDRs: []string{"172.16.0.0/12", " 2001:db8::/32 "},
	}
	if err := cfg.validateTargetSource(); err != nil {
		t.Fatalf("valid CIDR list rejected: %v", err)
	}
	for ip, want := range map[string]bool{
		"172.16.2.42": true, "2001:db8::1": true, "::ffff:172.20.0.9": true,
		"10.0.0.1": false, "8.8.8.8": false, "2001:db9::1": false, "bogus": false,
	} {
		if got := cfg.fleetTargetAllowed(ip); got != want {
			t.Errorf("fleetTargetAllowed(%q) = %v, want %v", ip, got, want)
		}
	}

	// No explicit allowlist: fleet mode fails closed to private ranges.
	open := &Config{FleetAPIURL: "http://fleet", FleetAPIToken: "fleet_x_x"}
	if err := open.validateTargetSource(); err != nil {
		t.Fatalf("empty CIDR list rejected: %v", err)
	}
	if open.fleetTargetAllowed("8.8.8.8") {
		t.Error("default allowlist must reject public addresses")
	}
	if !open.fleetTargetAllowed("192.168.4.20") || !open.fleetTargetAllowed("fd00::1") {
		t.Error("default allowlist must accept private-range addresses")
	}

	bad := &Config{FleetAPIURL: "http://fleet", FleetAPIToken: "fleet_x_x", FleetTargetCIDRs: []string{"172.16.0.0"}}
	if err := bad.validateTargetSource(); err == nil {
		t.Error("bare IP without prefix length must be rejected")
	}
}

func TestUploaderEnqueueRespectsByteBudget(t *testing.T) {
	up := newMetricsUploader("http://unused", 8, false)
	info := &rigInfo{address: "rig-1:2123", labels: map[string]string{"hostname": "rig-1"}}
	up.enqueue(info, make([]byte, maxQueuedBytes+1))
	if len(up.queue) != 0 {
		t.Fatal("payload above the queue byte budget must be dropped")
	}
	half := make([]byte, maxQueuedBytes/2)
	for range 3 {
		up.enqueue(info, half)
	}
	if len(up.queue) != 2 {
		t.Fatalf("queue holds %d batches, want 2 (third exceeds the byte budget)", len(up.queue))
	}
}

func TestUploaderTickDrainRespectsFlushBudget(t *testing.T) {
	up := newMetricsUploader("http://unused", 8, false)
	payload := make([]byte, maxPendingFlushBytes/2)
	info := &rigInfo{address: "rig-1:2123", labels: map[string]string{"hostname": "rig-1"}}
	for range 3 {
		up.enqueue(info, payload)
	}

	pending, pendingBytes, more := up.drainBounded(nil, 0)
	if !more || len(pending) != 2 || pendingBytes < maxPendingFlushBytes {
		t.Fatalf("first drain: %d batches %d bytes more=%v; want 2 budget-capped batches with more queued",
			len(pending), pendingBytes, more)
	}
	pending, pendingBytes, more = up.drainBounded(pending[:0], 0)
	if more || len(pending) != 1 || pendingBytes != len(payload) {
		t.Fatalf("second drain: %d batches %d bytes more=%v; want the final batch and an empty queue",
			len(pending), pendingBytes, more)
	}
}

func TestRegistryReplaceRestartsOnLabelChange(t *testing.T) {
	reg := newRegistry()
	first := &rigInfo{address: "10.0.0.1:2123", labels: map[string]string{"site": "stl"}}
	added, removed := reg.replace([]*rigInfo{first})
	if len(added) != 1 || len(removed) != 0 {
		t.Fatalf("initial replace: +%d -%d, want +1 -0", len(added), len(removed))
	}

	unchanged := &rigInfo{address: "10.0.0.1:2123", labels: map[string]string{"site": "stl"}}
	added, removed = reg.replace([]*rigInfo{unchanged})
	if len(added) != 0 || len(removed) != 0 {
		t.Fatalf("no-op replace: +%d -%d, want +0 -0", len(added), len(removed))
	}

	moved := &rigInfo{address: "10.0.0.1:2123", labels: map[string]string{"site": "markham"}}
	added, removed = reg.replace([]*rigInfo{moved})
	if len(added) != 1 || len(removed) != 1 {
		t.Fatalf("label-change replace: +%d -%d, want +1 -1 (restart)", len(added), len(removed))
	}
}
