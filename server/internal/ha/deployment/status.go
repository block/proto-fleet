package deployment

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/ha"
	"github.com/block/proto-fleet/server/internal/infrastructure/db"
	"github.com/block/proto-fleet/server/internal/transportguard"
)

const localHAStatusURL = "http://" + ha.LocalStatusAddress + "/health/ha"

type StatusReport struct {
	Runtime ha.Status      `json:"runtime"`
	Control *ControlStatus `json:"control,omitempty"`
}

type ControlReasonCode string

const (
	ReasonEtcdQuorumUnavailable      ControlReasonCode = "etcd_quorum_unavailable"
	ReasonEtcdRedundancyDegraded     ControlReasonCode = "etcd_redundancy_degraded"
	ReasonWriterUnavailable          ControlReasonCode = "writer_unavailable"
	ReasonDatabaseRedundancyDegraded ControlReasonCode = "database_redundancy_degraded"
	ReasonFleetRedundancyDegraded    ControlReasonCode = "fleet_redundancy_degraded"
	ReasonFleetVersionMismatch       ControlReasonCode = "fleet_version_mismatch"
	ReasonVIPUnavailable             ControlReasonCode = "vip_unavailable"
)

type ControlStatus struct {
	ControlReady  bool                `json:"control_ready"`
	FailoverReady bool                `json:"failover_ready"`
	ReasonCodes   []ControlReasonCode `json:"reason_codes"`
}

func Status(ctx context.Context, envPath string) (StatusReport, error) {
	return statusWithDatabase(ctx, envPath, nil)
}

// StatusWithDatabase runs the same operator-facing control check while
// reusing fleet-api's existing database pool for the writer observation.
func StatusWithDatabase(ctx context.Context, envPath string, conn *sql.DB) (StatusReport, error) {
	if conn == nil {
		return StatusReport{}, errors.New("HA status requires a database connection")
	}
	return statusWithDatabase(ctx, envPath, conn)
}

func statusWithDatabase(ctx context.Context, envPath string, conn *sql.DB) (StatusReport, error) {
	client, cleanup := newProbeHTTPClient(nil, nil)
	defer cleanup()
	report, err := readLocalStatus(ctx, client, localHAStatusURL)
	if err != nil {
		return StatusReport{}, err
	}
	return checkControlPath(ctx, envPath, report, conn)
}

func readLocalStatus(ctx context.Context, client *http.Client, endpoint string) (StatusReport, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return StatusReport{}, fmt.Errorf("create local HA status request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return StatusReport{}, fmt.Errorf("read local HA status: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return StatusReport{}, fmt.Errorf("local HA status returned %s", response.Status)
	}
	var status ha.Status
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return StatusReport{}, fmt.Errorf("decode local HA status: %w", err)
	}
	return StatusReport{Runtime: status}, nil
}

func checkControlPath(ctx context.Context, envPath string, report StatusReport, conn *sql.DB) (StatusReport, error) {
	config, err := loadNodeConfig(envPath)
	if err != nil {
		return StatusReport{}, err
	}
	if err := validateNodeConfig(config); err != nil {
		return StatusReport{}, fmt.Errorf("validate HA node configuration: %w", err)
	}
	if !config.isDatabaseNode() {
		return StatusReport{}, errors.New("HA control check must run on ha-a or ha-b")
	}
	tlsConfig, err := ha.LoadServiceTLS(filepath.Join(config.SecretsDir, "service-ca.crt"))
	if err != nil {
		return StatusReport{}, err
	}
	password, err := readPassword(filepath.Join(config.SecretsDir, fleetEtcdPasswordFile))
	if err != nil {
		return StatusReport{}, fmt.Errorf("read Fleet etcd password: %w", err)
	}
	endpoints := []string{
		"https://" + config.DatabaseAIP + ":2379",
		"https://" + config.DatabaseBIP + ":2379",
		"https://" + config.WitnessIP + ":2379",
	}
	etcdConfig := clientv3.Config{
		Endpoints: endpoints, Username: "fleet-observer", Password: password,
		TLS: tlsConfig.Clone(), DialTimeout: 2 * time.Second,
	}

	var (
		etcdStatus         etcdReadiness
		primary            int
		synchronous        int
		fleetActive        int
		fleetRedundant     bool
		fleetReady         bool
		fleetVersionsMatch bool
		writerReady        bool
		vipReady           bool
		probes             sync.WaitGroup
	)
	probes.Go(func() {
		etcdStatus = probeEtcdMembers(ctx, etcdConfig)
	})
	probes.Go(func() {
		primary, synchronous = patroniRoles(ctx, tlsConfig, config)
	})
	probes.Go(func() {
		writerReady = writerObservationReady(ctx, etcdConfig, config, conn)
	})
	probes.Go(func() {
		client, cleanup := newProbeHTTPClient(tlsConfig, nil)
		defer cleanup()
		vipReady = endpointReadyWithClient(ctx, client, "https://"+config.VirtualIP+"/api-proxy/health/active")
	})
	probes.Go(func() {
		statuses := gather([]string{config.DatabaseAIP, config.DatabaseBIP}, func(address string) fleetHostStatus {
			return probeFleetHost(ctx, tlsConfig, config.VirtualIP, address)
		})
		for _, status := range statuses {
			if status.active {
				fleetActive++
			}
		}
		fleetRedundant = fleetRedundancyReady(statuses)
		fleetVersionsMatch = matchingFleetVersions(statuses)
		fleetReady = fleetRedundant && fleetVersionsMatch
	})
	probes.Wait()

	localRuntimeReady := report.Runtime.Observation == ha.ObservationCurrent &&
		(report.Runtime.Role == ha.RoleActive || report.Runtime.Role == ha.RolePassive)
	controlReady := etcdStatus.quorum && primary == 1 && writerReady && vipReady && fleetActive == 1
	failoverReady := controlReady && etcdStatus.redundant && synchronous == 1 && fleetReady && localRuntimeReady
	control := &ControlStatus{ControlReady: controlReady, FailoverReady: failoverReady}
	if !etcdStatus.quorum {
		control.ReasonCodes = append(control.ReasonCodes, ReasonEtcdQuorumUnavailable)
	} else if !etcdStatus.redundant {
		control.ReasonCodes = append(control.ReasonCodes, ReasonEtcdRedundancyDegraded)
	}
	if primary != 1 || !writerReady {
		control.ReasonCodes = append(control.ReasonCodes, ReasonWriterUnavailable)
	}
	if synchronous != 1 {
		control.ReasonCodes = append(control.ReasonCodes, ReasonDatabaseRedundancyDegraded)
	}
	if !fleetRedundant || !localRuntimeReady {
		control.ReasonCodes = append(control.ReasonCodes, ReasonFleetRedundancyDegraded)
	} else if !fleetVersionsMatch {
		control.ReasonCodes = append(control.ReasonCodes, ReasonFleetVersionMismatch)
	}
	if !vipReady {
		control.ReasonCodes = append(control.ReasonCodes, ReasonVIPUnavailable)
	}
	report.Control = control
	return report, nil
}

type etcdMemberIdentity struct {
	clusterID uint64
	memberID  uint64
}

type etcdReadiness struct {
	quorum    bool
	redundant bool
}

func probeEtcdMembers(ctx context.Context, config clientv3.Config) etcdReadiness {
	identities := gather(config.Endpoints, func(endpoint string) etcdMemberIdentity {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		memberConfig := config
		memberConfig.Endpoints = []string{endpoint}
		if config.TLS != nil {
			memberConfig.TLS = config.TLS.Clone()
		}
		client, err := clientv3.New(memberConfig)
		if err != nil {
			return etcdMemberIdentity{}
		}
		defer client.Close()
		response, err := client.Status(probeCtx, endpoint)
		if err != nil || response.Header == nil {
			return etcdMemberIdentity{}
		}
		if _, err := client.Get(probeCtx, patroniDCSPath, clientv3.WithPrefix(), clientv3.WithLimit(1)); err != nil {
			return etcdMemberIdentity{}
		}
		return etcdMemberIdentity{clusterID: response.Header.ClusterId, memberID: response.Header.MemberId}
	})
	return summarizeEtcdMembers(identities, len(config.Endpoints))
}

func summarizeEtcdMembers(identities []etcdMemberIdentity, expected int) etcdReadiness {
	clusters := make(map[uint64]map[uint64]struct{})
	for _, identity := range identities {
		if identity.clusterID == 0 || identity.memberID == 0 {
			continue
		}
		if clusters[identity.clusterID] == nil {
			clusters[identity.clusterID] = make(map[uint64]struct{})
		}
		clusters[identity.clusterID][identity.memberID] = struct{}{}
	}
	readiness := etcdReadiness{}
	for _, members := range clusters {
		if len(members) >= 2 {
			readiness.quorum = true
		}
		if len(clusters) == 1 && len(members) == expected {
			readiness.redundant = true
		}
	}
	return readiness
}

func patroniRoles(ctx context.Context, tlsConfig *tls.Config, config NodeConfig) (primary, synchronous int) {
	client, cleanup := newProbeHTTPClient(tlsConfig, nil)
	defer cleanup()
	type roleResult struct {
		primary     bool
		synchronous bool
	}
	results := gather([]string{config.DatabaseAIP, config.DatabaseBIP}, func(address string) roleResult {
		baseURL := "https://" + address + ":8008"
		return roleResult{
			primary: endpointReadyWithClient(ctx, client, baseURL+"/primary"),
			synchronous: endpointReadyWithClient(ctx, client, baseURL+"/synchronous") &&
				endpointReadyWithClient(ctx, client, baseURL+"/readiness?lag=1MB&mode=apply"),
		}
	})
	for _, result := range results {
		if result.primary {
			primary++
		}
		if result.synchronous {
			synchronous++
		}
	}
	return primary, synchronous
}

type fleetHostStatus struct {
	reachable bool
	active    bool
	passive   bool
	version   string
}

func probeFleetHost(ctx context.Context, tlsConfig *tls.Config, virtualIP, address string) fleetHostStatus {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	client, cleanup := newProbeHTTPClient(tlsConfig, func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, net.JoinHostPort(address, "443"))
	})
	defer cleanup()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+virtualIP+"/api-proxy/health", nil)
	if err != nil {
		return fleetHostStatus{}
	}
	response, err := client.Do(request)
	if err != nil {
		return fleetHostStatus{}
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fleetHostStatus{}
	}
	return fleetHostStatus{
		reachable: true,
		active:    endpointReadyWithClient(ctx, client, "https://"+virtualIP+"/api-proxy/health/active"),
		passive:   endpointReadyWithClient(ctx, client, "https://"+virtualIP+"/api-proxy/health/passive"),
		version:   response.Header.Get("X-Proto-Fleet-Version"),
	}
}

func fleetRedundancyReady(statuses []fleetHostStatus) bool {
	active, passive := 0, 0
	for _, status := range statuses {
		if !status.reachable || status.active == status.passive {
			return false
		}
		if status.active {
			active++
		} else {
			passive++
		}
	}
	return len(statuses) == 2 && active == 1 && passive == 1
}

func matchingFleetVersions(statuses []fleetHostStatus) bool {
	return len(statuses) == 2 && statuses[0].version != "" && statuses[0].version == statuses[1].version
}

func writerObservationReady(ctx context.Context, etcdConfig clientv3.Config, config NodeConfig, conn *sql.DB) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if conn == nil {
		values, err := loadFleetEnvironment(filepath.Join(config.SecretsDir, fleetEnvironmentFile))
		if err != nil {
			return false
		}
		dsn, err := hostProbeDSN(values["DB_DSN"], config.SecretsDir)
		if err != nil {
			return false
		}
		opened, err := db.ConnectToDatabase(&db.Config{
			ExplicitDSN:  dsn,
			MaxOpenConns: 1,
			MaxIdleConns: 1,
		})
		if err != nil {
			return false
		}
		defer opened.Close()
		conn = opened
	}
	pinned, err := conn.Conn(probeCtx)
	if err != nil {
		return false
	}
	defer pinned.Close()
	queries := sqlc.New(pinned)
	if etcdConfig.TLS != nil {
		etcdConfig.TLS = etcdConfig.TLS.Clone()
	}
	etcd, err := ha.NewEtcdClient(etcdConfig)
	if err != nil {
		return false
	}
	defer etcd.Close()
	client, cleanup := newProbeHTTPClient(etcdConfig.TLS, nil)
	defer cleanup()
	observer, err := ha.NewObserver("/service/proto-fleet", etcd, queries, ha.NewPatroniHTTPClient(client))
	if err != nil {
		return false
	}
	_, err = observer.Observe(probeCtx)
	return err == nil
}

func hostProbeDSN(raw, secretsDir string) (string, error) {
	dsn, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse Fleet database DSN: %w", err)
	}
	query := dsn.Query()
	query.Set("sslrootcert", filepath.Join(secretsDir, "service-ca.crt"))
	query.Set("default_query_exec_mode", "cache_statement")
	dsn.RawQuery = query.Encode()
	probeDSN := dsn.String()
	if err := (&db.Config{ExplicitDSN: probeDSN}).ValidateHA(); err != nil {
		return "", fmt.Errorf("validate HA database probe: %w", err)
	}
	return probeDSN, nil
}

func gather[T, R any](items []T, probe func(T) R) []R {
	results := make(chan R, len(items))
	for _, item := range items {
		go func(item T) {
			results <- probe(item)
		}(item)
	}
	gathered := make([]R, 0, len(items))
	for range items {
		gathered = append(gathered, <-results)
	}
	return gathered
}

func newProbeHTTPClient(tlsConfig *tls.Config, dialContext func(context.Context, string, string) (net.Conn, error)) (*http.Client, func()) {
	transport := &http.Transport{Proxy: nil, DialContext: dialContext}
	if tlsConfig != nil {
		transport.TLSClientConfig = tlsConfig.Clone()
	}
	client := &http.Client{
		Transport:     transport,
		Timeout:       2 * time.Second,
		CheckRedirect: transportguard.RejectRedirect,
	}
	return client, transport.CloseIdleConnections
}

func endpointReadyWithClient(ctx context.Context, client *http.Client, endpoint string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}
