package deployment

import (
	"context"
	"crypto/tls"
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
	ReasonVIPUnavailable             ControlReasonCode = "vip_unavailable"
)

type ControlStatus struct {
	ControlReady  bool                `json:"control_ready"`
	FailoverReady bool                `json:"failover_ready"`
	ReasonCodes   []ControlReasonCode `json:"reason_codes"`
}

func Status(ctx context.Context, envPath string, check bool) (StatusReport, error) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return StatusReport{}, errors.New("default HTTP transport is not configurable for HA status")
	}
	transport := defaultTransport.Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	report, err := readLocalStatus(ctx, &http.Client{Transport: transport}, localHAStatusURL)
	if err != nil {
		return StatusReport{}, err
	}
	if !check {
		return report, nil
	}
	return checkControlPath(ctx, envPath, report)
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

func checkControlPath(ctx context.Context, envPath string, report StatusReport) (StatusReport, error) {
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
		etcdStatus  etcdReadiness
		primary     int
		synchronous int
		fleetActive int
		fleetReady  bool
		writerReady bool
		vipReady    bool
		probes      sync.WaitGroup
	)
	probes.Add(5)
	go func() {
		defer probes.Done()
		etcdStatus = probeEtcdMembers(ctx, etcdConfig)
	}()
	go func() {
		defer probes.Done()
		primary, synchronous = patroniRoles(ctx, tlsConfig, config)
	}()
	go func() {
		defer probes.Done()
		writerReady = writerObservationReady(ctx, tlsConfig, password, endpoints, config)
	}()
	go func() {
		defer probes.Done()
		vipReady = endpointReady(ctx, tlsConfig, "https://"+config.VirtualIP+"/api-proxy/health/active")
	}()
	go func() {
		defer probes.Done()
		statuses := probeFleetHosts(ctx, tlsConfig, config)
		_, fleetActive = summarizeFleetHosts(statuses)
		fleetReady = fleetRedundancyReady(statuses)
	}()
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
	if !fleetReady || !localRuntimeReady {
		control.ReasonCodes = append(control.ReasonCodes, ReasonFleetRedundancyDegraded)
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
	results := make(chan etcdMemberIdentity, len(config.Endpoints))
	for _, endpoint := range config.Endpoints {
		go func() {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			memberConfig := config
			memberConfig.Endpoints = []string{endpoint}
			if config.TLS != nil {
				memberConfig.TLS = config.TLS.Clone()
			}
			client, err := clientv3.New(memberConfig)
			if err != nil {
				results <- etcdMemberIdentity{}
				return
			}
			defer client.Close()
			response, err := client.Status(probeCtx, endpoint)
			if err != nil || response.Header == nil {
				results <- etcdMemberIdentity{}
				return
			}
			if _, err := client.Get(probeCtx, patroniDCSPath, clientv3.WithPrefix(), clientv3.WithLimit(1)); err != nil {
				results <- etcdMemberIdentity{}
				return
			}
			results <- etcdMemberIdentity{clusterID: response.Header.ClusterId, memberID: response.Header.MemberId}
		}()
	}
	identities := make([]etcdMemberIdentity, 0, len(config.Endpoints))
	for range config.Endpoints {
		identities = append(identities, <-results)
	}
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
	transport := &http.Transport{TLSClientConfig: tlsConfig.Clone(), Proxy: nil}
	client := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Second, CheckRedirect: transportguard.RejectRedirect,
	}
	defer transport.CloseIdleConnections()
	type roleResult struct {
		primary     bool
		synchronous bool
	}
	results := make(chan roleResult, 2)
	for _, address := range []string{config.DatabaseAIP, config.DatabaseBIP} {
		go func() {
			baseURL := "https://" + address + ":8008"
			results <- roleResult{
				primary: endpointReadyWithClient(ctx, client, baseURL+"/primary"),
				synchronous: endpointReadyWithClient(ctx, client, baseURL+"/synchronous") &&
					endpointReadyWithClient(ctx, client, baseURL+"/readiness?lag=0&mode=apply"),
			}
		}()
	}
	for range 2 {
		result := <-results
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

func probeFleetHosts(ctx context.Context, tlsConfig *tls.Config, config NodeConfig) []fleetHostStatus {
	addresses := []string{config.DatabaseAIP, config.DatabaseBIP}
	results := make(chan fleetHostStatus, len(addresses))
	for _, address := range addresses {
		go func() {
			results <- probeFleetHost(ctx, tlsConfig, config.VirtualIP, address)
		}()
	}
	statuses := make([]fleetHostStatus, 0, len(addresses))
	for range addresses {
		statuses = append(statuses, <-results)
	}
	return statuses
}

func probeFleetHost(ctx context.Context, tlsConfig *tls.Config, virtualIP, address string) fleetHostStatus {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig.Clone(),
		Proxy:           nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(address, "443"))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: transportguard.RejectRedirect}
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

func summarizeFleetHosts(statuses []fleetHostStatus) (reachable, active int) {
	for _, status := range statuses {
		if status.reachable {
			reachable++
		}
		if status.active {
			active++
		}
	}
	return reachable, active
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

func writerObservationReady(ctx context.Context, tlsConfig *tls.Config, password string, endpoints []string, config NodeConfig) bool {
	values, err := loadFleetEnvironment(filepath.Join(config.SecretsDir, fleetEnvironmentFile))
	if err != nil {
		return false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dsn, err := hostProbeDSN(values["DB_DSN"], config.SecretsDir)
	if err != nil {
		return false
	}
	conn, err := db.ConnectToDatabase(&db.Config{
		ExplicitDSN:  dsn,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		return false
	}
	defer conn.Close()
	pinned, err := conn.Conn(probeCtx)
	if err != nil {
		return false
	}
	defer pinned.Close()
	queries := sqlc.New(pinned)
	etcd, err := ha.NewEtcdClient(clientv3.Config{
		Endpoints: endpoints, Username: "fleet-observer", Password: password,
		TLS: tlsConfig.Clone(), DialTimeout: 2 * time.Second,
	})
	if err != nil {
		return false
	}
	defer etcd.Close()
	transport := &http.Transport{TLSClientConfig: tlsConfig.Clone(), Proxy: nil}
	defer transport.CloseIdleConnections()
	observer, err := ha.NewObserver("/service/proto-fleet", etcd, queries, ha.NewPatroniHTTPClient(&http.Client{
		Transport: transport, Timeout: 2 * time.Second,
	}))
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
	return dsn.String(), nil
}

func endpointReady(ctx context.Context, tlsConfig *tls.Config, endpoint string) bool {
	transport := &http.Transport{TLSClientConfig: tlsConfig.Clone(), Proxy: nil}
	defer transport.CloseIdleConnections()
	return endpointReadyWithClient(ctx, &http.Client{
		Transport: transport, Timeout: 2 * time.Second, CheckRedirect: transportguard.RejectRedirect,
	}, endpoint)
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
