package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// haInstance links one fake controller to its HA pair. A nil value is the
// default single-node simulator.
type haInstance struct {
	sim       *updateSim
	self      string
	peer      string
	clusterID string
	// peerHostname is reported as the peer member's observed hostname.
	peerHostname string
	// peerUnreachable makes this controller report its peer as unreachable
	// since startedAt, with stale role and reachability observations.
	peerUnreachable bool
	startedAt       time.Time
	// primaryMetrics is true for the member that owns FAKE_MINER_METRICS_FILE.
	primaryMetrics bool
}

func (h *haInstance) isPrimary() bool { return h.sim.currentPrimary() == h.self }

func (h *haInstance) role() (string, bool) {
	if h.isPrimary() {
		return "HA_ROLE_PRIMARY", true
	}
	return "HA_ROLE_STANDBY", false
}

// haPairSettings configures CT1_HA_PAIR=1 mode: two controllers of one
// container, in one process, sharing a rollout simulator that stands in for
// etcd and the root update helpers.
type haPairSettings struct {
	PeerServerID  string
	PeerGRPCPort  int
	PeerHTTPPort  int
	ClusterID     string
	PrimaryIsPeer bool
	// PeerUnreachable makes each controller report the other as unreachable.
	PeerUnreachable bool
	Update          updateSimConfig
}

func envBool(key string) bool {
	value := strings.ToLower(os.Getenv(key))
	return value == "1" || value == "true" || value == "yes"
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid %s=%q: expected nonnegative duration", key, value)
	}
	return d, nil
}

// haPairSettingsFromEnv returns false when CT1_HA_PAIR is not set.
//
//	CT1_HA_PAIR=1                     enable the pair
//	CT1_HA_PEER_SERVER_ID             second controller's ID (default <SERVER_ID>-b)
//	CT1_HA_PEER_GRPC_PORT/_HTTP_PORT  second controller's ports (default primary+1; 0 stays ephemeral)
//	CT1_HA_CLUSTER_ID                 durable cluster UUID (default derived from the two IDs)
//	CT1_UPDATE_ETCD_CLUSTER_ID        etcd cluster ID update support reports (default derived from the cluster UUID)
//	CT1_HA_PRIMARY                    "a" (default) or "b"
//	CT1_HA_FAULT_PEER_UNREACHABLE     each controller reports its peer unreachable
//	CT1_UPDATE_TRUSTED_KEY_FILES      comma-separated PEM P-256 release public keys
//	CT1_UPDATE_STEP                   duration of each scripted rollout phase (default 2s)
//	CT1_UPDATE_SPOOL_FREE_BYTES       advertised free spool space
//	CT1_UPDATE_FAULT_*                REMOTE_TRIGGER_DISABLED, SELF_COORDINATION_UNSUPPORTED,
//	                                  ETCD_UNHEALTHY, HELPER_UNAVAILABLE, OUTSIDE_WINDOW,
//	                                  STALE_CONTACT, DRIVER_REBOOT (booleans),
//	                                  MIGRATE_FAILURES (count), COOLDOWN (duration)
//	CT1_PROTOOS_TRUSTED_KEY_FILES     comma-separated PEM P-256 ProtoOS release public keys
//	CT1_PROTOOS_FAULT_*               SELF_UNSUPPORTED, INSTALL_FAILED (booleans)
func haPairSettingsFromEnv(cfg ContainerConfig, grpcPort, httpPort int) (haPairSettings, bool, error) {
	if !envBool("CT1_HA_PAIR") {
		return haPairSettings{}, false, nil
	}
	settings := haPairSettings{
		PeerServerID:    getEnv("CT1_HA_PEER_SERVER_ID", cfg.ServerID+"-b"),
		PeerUnreachable: envBool("CT1_HA_FAULT_PEER_UNREACHABLE"),
	}
	if settings.PeerServerID == cfg.ServerID {
		return settings, false, fmt.Errorf("CT1_HA_PEER_SERVER_ID must differ from SERVER_ID")
	}
	next := func(port int) int {
		if port == 0 {
			return 0
		}
		return port + 1
	}
	settings.PeerGRPCPort = getEnvPort("CT1_HA_PEER_GRPC_PORT", next(grpcPort))
	settings.PeerHTTPPort = getEnvPort("CT1_HA_PEER_HTTP_PORT", next(httpPort))
	settings.ClusterID = getEnv("CT1_HA_CLUSTER_ID", uuid.NewSHA1(uuid.NameSpaceOID, []byte(cfg.ServerID+"|"+settings.PeerServerID)).String())
	if parsed, err := uuid.Parse(settings.ClusterID); err != nil || parsed == uuid.Nil || parsed.String() != settings.ClusterID {
		return settings, false, fmt.Errorf("invalid CT1_HA_CLUSTER_ID=%q: expected a lowercase UUID", settings.ClusterID)
	}
	switch strings.ToLower(getEnv("CT1_HA_PRIMARY", "a")) {
	case "a":
	case "b":
		settings.PrimaryIsPeer = true
	default:
		return settings, false, fmt.Errorf("invalid CT1_HA_PRIMARY: expected a or b")
	}
	primary := cfg.ServerID
	if settings.PrimaryIsPeer {
		primary = settings.PeerServerID
	}
	step, err := envDuration("CT1_UPDATE_STEP", defaultUpdateStep)
	if err != nil {
		return settings, false, err
	}
	cooldown, err := envDuration("CT1_UPDATE_FAULT_COOLDOWN", 0)
	if err != nil {
		return settings, false, err
	}
	keys, err := loadTrustedKeys(envList("CT1_UPDATE_TRUSTED_KEY_FILES"))
	if err != nil {
		return settings, false, err
	}
	protoOSKeys, err := loadTrustedKeys(envList("CT1_PROTOOS_TRUSTED_KEY_FILES"))
	if err != nil {
		return settings, false, err
	}
	spool := int64(0)
	if value := os.Getenv("CT1_UPDATE_SPOOL_FREE_BYTES"); value != "" {
		if spool, err = strconv.ParseInt(value, 10, 64); err != nil || spool < 0 {
			return settings, false, fmt.Errorf("invalid CT1_UPDATE_SPOOL_FREE_BYTES=%q", value)
		}
	}
	failures := 0
	if value := os.Getenv("CT1_UPDATE_FAULT_MIGRATE_FAILURES"); value != "" {
		if failures, err = strconv.Atoi(value); err != nil || failures < 0 {
			return settings, false, fmt.Errorf("invalid CT1_UPDATE_FAULT_MIGRATE_FAILURES=%q", value)
		}
	}
	// Real Frigate reports the etcd cluster ID in update support, a different
	// identifier from the HA cluster UUID in GetHAStatus, so the simulator does
	// too.
	etcdCluster := getEnv("CT1_UPDATE_ETCD_CLUSTER_ID", fmt.Sprintf("%x", sha256.Sum256([]byte(settings.ClusterID)))[:16])
	settings.Update = updateSimConfig{
		Primary: primary, EtcdClusterID: etcdCluster, StepDelay: step, SpoolFreeBytes: spool, TrustedKeys: keys, ProtoOSTrustedKeys: protoOSKeys,
		Faults: updateFaults{
			RemoteTriggerDisabled:       envBool("CT1_UPDATE_FAULT_REMOTE_TRIGGER_DISABLED"),
			SelfCoordinationUnsupported: envBool("CT1_UPDATE_FAULT_SELF_COORDINATION_UNSUPPORTED"),
			EtcdUnhealthy:               envBool("CT1_UPDATE_FAULT_ETCD_UNHEALTHY"),
			HelperUnavailable:           envBool("CT1_UPDATE_FAULT_HELPER_UNAVAILABLE"),
			OutsideWindow:               envBool("CT1_UPDATE_FAULT_OUTSIDE_WINDOW"),
			StaleContact:                envBool("CT1_UPDATE_FAULT_STALE_CONTACT"),
			DriverReboot:                envBool("CT1_UPDATE_FAULT_DRIVER_REBOOT"),
			FirmwareActive:              envBool("CT1_UPDATE_FAULT_FIRMWARE_ACTIVE"),
			MigrateFailures:             failures,
			Cooldown:                    cooldown,
			ProtoOSSelfUnsupported:      envBool("CT1_PROTOOS_FAULT_SELF_UNSUPPORTED"),
			ProtoOSInstallFails:         envBool("CT1_PROTOOS_FAULT_INSTALL_FAILED"),
		},
	}
	return settings, true, nil
}

// envList splits a comma-separated variable and drops empty items.
func envList(key string) []string {
	var items []string
	for _, item := range strings.Split(os.Getenv(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// peerConfig derives the second controller: the same container identity under
// a different server ID.
func (s haPairSettings) peerConfig(cfg ContainerConfig) ContainerConfig {
	peer := cfg
	peer.ServerID = s.PeerServerID
	peer.Hostname = "fake-ct1-" + s.PeerServerID
	return peer
}

// newHAPair links two states to a fresh shared simulator.
func newHAPair(a, b *ContainerState, settings haPairSettings) *updateSim {
	sim := newUpdateSim(settings.Update)
	sim.attach(a)
	sim.attach(b)
	startedAt := time.Now()
	a.ha = &haInstance{
		sim: sim, self: a.ServerID, peer: b.ServerID, clusterID: settings.ClusterID, peerHostname: b.Hostname,
		peerUnreachable: settings.PeerUnreachable, startedAt: startedAt, primaryMetrics: true,
	}
	b.ha = &haInstance{
		sim: sim, self: b.ServerID, peer: a.ServerID, clusterID: settings.ClusterID, peerHostname: a.Hostname,
		peerUnreachable: settings.PeerUnreachable, startedAt: startedAt,
	}
	return sim
}

// runHAPair serves both controllers until one stops or fails.
func runHAPair(a, b *ContainerState, sim *updateSim, address string, aGRPC, aHTTP, bGRPC, bHTTP int, webRoot string) error {
	defer sim.close()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, member := range []struct {
		state      *ContainerState
		grpc, http int
	}{{a, aGRPC, aHTTP}, {b, bGRPC, bHTTP}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runContainer(member.state, address, member.grpc, member.http, webRoot); err != nil {
				errs <- fmt.Errorf("%s: %w", member.state.ServerID, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		log.Printf("HA pair member stopped: %v", err)
		return err
	}
	return nil
}

func (a *grpcAPI) GetHAStatus(context.Context, *mdk.GetHAStatusRequest) (*mdk.GetHAStatusResponse, error) {
	h := a.state.ha
	if h == nil {
		return nil, status.Error(codes.Unimplemented, "method GetHAStatus not implemented")
	}
	now := timestamppb.Now()
	report := &mdk.HAStatus{
		Enabled: true, ReportingServerId: h.self, PeerServerId: h.peer, ClusterId: h.clusterID,
		IdentityState:  mdk.HAIdentityState_HA_IDENTITY_STATE_CONFIRMED,
		InventoryReady: true, ObservedAt: now,
	}
	fresh := func(source string) *mdk.HAObservation {
		return &mdk.HAObservation{SourceServerId: source, ObservedAt: now, LastSuccessfulAt: now, Freshness: mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_FRESH}
	}
	for _, id := range []string{h.self, h.peer} {
		primary := h.sim.currentPrimary() == id
		role := mdk.HARole_HA_ROLE_STANDBY
		if primary {
			role = mdk.HARole_HA_ROLE_PRIMARY
		}
		hostname := a.state.Hostname
		if id != h.self {
			hostname = h.peerHostname
		}
		member := &mdk.HAMemberStatus{
			Id: id, Kind: mdk.HAMemberRole_HA_MEMBER_ROLE_FRIGATE, Local: id == h.self, Hostname: hostname,
			HaRole: role, ContainerWritePrimary: &primary, FrigateReachable: new(true),
			Recovery:           &mdk.HARecoveryStatus{State: mdk.HARecoveryState_HA_RECOVERY_STATE_STEADY},
			RoleObservation:    fresh(id),
			FrigateObservation: fresh(h.self),
		}
		if id != h.self && h.peerUnreachable {
			// The last successful probe predates the fault; the role and write
			// claim are what the peer last reported, and recovery is unknown.
			lastSuccess := timestamppb.New(h.startedAt)
			member.FrigateReachable = new(false)
			member.Recovery = nil
			member.FrigateObservation = &mdk.HAObservation{
				SourceServerId: h.self, ObservedAt: now, LastSuccessfulAt: lastSuccess,
				Freshness: mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_STALE,
				Detail:    "peer did not answer the latest probe",
			}
			member.RoleObservation = &mdk.HAObservation{
				SourceServerId: id, ObservedAt: lastSuccess, LastSuccessfulAt: lastSuccess,
				Freshness: mdk.HAObservationFreshness_HA_OBSERVATION_FRESHNESS_STALE,
			}
		}
		report.Members = append(report.Members, member)
	}
	return &mdk.GetHAStatusResponse{Status: report}, nil
}
