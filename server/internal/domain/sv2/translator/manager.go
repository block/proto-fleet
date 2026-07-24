package translator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/block/proto-fleet/server/internal/domain/sv2"
)

const (
	configFileName  = "tproxy.toml"
	profileFileName = "active-profile.json"
	profileVersion  = 2
)

type Config struct {
	StateDir       string        `help:"Directory shared with the pre-created SV2 translator container" default:"/var/lib/proto-fleet/sv2" env:"STATE_DIR"`
	AdvertisedHost string        `help:"Listener IP or hostname sent to SV1 miners; empty selects the route to the assigned SV2 pool" default:"" env:"ADVERTISED_HOST"`
	ConnectHost    string        `help:"Listener host Fleet probes for readiness; empty uses the advertised host" default:"" env:"CONNECT_HOST"`
	DownstreamPort uint16        `help:"SV1 listener port exposed by the translator" default:"34255" env:"DOWNSTREAM_PORT"`
	ReadyTimeout   time.Duration `help:"Maximum time to start the pre-created translator and verify its listener" default:"30s" env:"READY_TIMEOUT"`
	DockerSocket   string        `help:"Docker Engine socket used only to inspect/start/stop the fixed translator container" default:"/var/run/docker.sock" env:"DOCKER_SOCKET"`
}

type Assignment struct {
	SelectedDeviceIdentifiers   []string
	TranslatedDeviceIdentifiers []string
}

type Manager interface {
	PreviewAssignment(ctx context.Context, desired *Profile, assignment Assignment) (Endpoint, error)
	ApplyAssignment(ctx context.Context, desired *Profile, assignment Assignment) (Endpoint, error)
	Resume(ctx context.Context) error
	ActiveProfile() (Profile, Endpoint, bool)
}

type persistedProfile struct {
	Version           int      `json:"version"`
	Profile           Profile  `json:"profile"`
	Endpoint          Endpoint `json:"endpoint"`
	DeviceIdentifiers []string `json:"device_identifiers"`
}

type FileManager struct {
	config  Config
	runtime containerRuntime

	mu       sync.Mutex
	active   bool
	profile  Profile
	endpoint Endpoint
	devices  map[string]struct{}
}

type managerSnapshot struct {
	active   bool
	profile  Profile
	endpoint Endpoint
	devices  map[string]struct{}
}

var _ Manager = (*FileManager)(nil)

func normalizeAssignment(
	desired *Profile,
	assignment Assignment,
) (Profile, []string, []string, error) {
	selected, err := normalizeDeviceIdentifiers("selected", assignment.SelectedDeviceIdentifiers)
	if err != nil {
		return Profile{}, nil, nil, err
	}
	translated, err := normalizeDeviceIdentifiers("translated", assignment.TranslatedDeviceIdentifiers)
	if err != nil {
		return Profile{}, nil, nil, err
	}
	selectedSet := deviceSet(selected)
	for _, identifier := range translated {
		if _, ok := selectedSet[identifier]; !ok {
			return Profile{}, nil, nil, fmt.Errorf(
				"translated miner %q is not part of the selected assignment",
				identifier,
			)
		}
	}

	var normalized Profile
	if len(translated) > 0 {
		if desired == nil {
			return Profile{}, nil, nil, fmt.Errorf(
				"translator profile is required for translated miners",
			)
		}
		normalized, err = NormalizeProfile(*desired)
		if err != nil {
			return Profile{}, nil, nil, err
		}
	} else if desired != nil {
		return Profile{}, nil, nil, fmt.Errorf(
			"translator profile requires at least one translated miner",
		)
	}
	return normalized, selected, translated, nil
}

func NewManager(config Config) (*FileManager, error) {
	if !filepath.IsAbs(config.StateDir) {
		return nil, fmt.Errorf("SV2 translator state directory must be absolute")
	}
	if config.DownstreamPort == 0 {
		return nil, fmt.Errorf("SV2 translator downstream port must be non-zero")
	}
	if config.ReadyTimeout <= 0 {
		return nil, fmt.Errorf("SV2 translator ready timeout must be positive")
	}
	if !filepath.IsAbs(config.DockerSocket) {
		return nil, fmt.Errorf("SV2 translator Docker socket must be absolute")
	}
	if config.AdvertisedHost != "" {
		if _, err := normalizeAdvertisedHost(config.AdvertisedHost); err != nil {
			return nil, err
		}
	}
	if config.ConnectHost != "" {
		connectHost := strings.Trim(strings.TrimSpace(config.ConnectHost), "[]")
		if connectHost == "" || strings.ContainsAny(connectHost, "/\\") {
			return nil, fmt.Errorf("SV2 translator readiness host %q is invalid", config.ConnectHost)
		}
	}

	manager := &FileManager{
		config:  config,
		runtime: newDockerRuntime(config.DockerSocket),
		devices: make(map[string]struct{}),
	}
	if err := manager.loadActiveProfile(); err != nil {
		return nil, err
	}
	return manager, nil
}

// PreviewAssignment validates an assignment and returns the endpoint that
// queued miner payloads should use without changing persisted translator state
// or starting/stopping the container.
func (m *FileManager) PreviewAssignment(
	ctx context.Context,
	desired *Profile,
	assignment Assignment,
) (Endpoint, error) {
	normalized, _, translated, err := normalizeAssignment(desired, assignment)
	if err != nil {
		return "", err
	}
	if len(translated) == 0 {
		return "", nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active {
		if !ProfilesEqual(m.profile, normalized) {
			return "", fmt.Errorf(
				"SV2 translator profile is active; move all translated miners to non-SV2 pools before changing the pool set",
			)
		}
		return m.endpoint, nil
	}
	return m.endpointForProfile(ctx, normalized)
}

func (m *FileManager) ApplyAssignment(
	ctx context.Context,
	desired *Profile,
	assignment Assignment,
) (Endpoint, error) {
	normalized, selected, translated, err := normalizeAssignment(desired, assignment)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	remaining := cloneDeviceSet(m.devices)
	for _, identifier := range selected {
		delete(remaining, identifier)
	}
	if len(translated) == 0 {
		return m.releaseSelectedLocked(remaining)
	}
	if m.active && !ProfilesEqual(m.profile, normalized) {
		return "", fmt.Errorf(
			"SV2 translator profile is active; move all translated miners to non-SV2 pools before changing the pool set",
		)
	}

	nextDevices := remaining
	for _, identifier := range translated {
		nextDevices[identifier] = struct{}{}
	}
	if m.active && ProfilesEqual(m.profile, normalized) {
		if err := m.startAndWaitLocked(ctx, m.endpoint); err != nil {
			return "", err
		}
		if err := m.persistActiveProfile(m.profile, m.endpoint, nextDevices); err != nil {
			return "", err
		}
		m.devices = nextDevices
		return m.endpoint, nil
	}

	previous := m.snapshotLocked()
	if previous.active {
		if err := m.stopLocked(); err != nil {
			return "", err
		}
	}
	endpoint, err := m.endpointForProfile(ctx, normalized)
	if err != nil {
		_ = m.restoreLocked(previous)
		return "", err
	}
	if err := m.activateLocked(ctx, normalized, endpoint, nextDevices); err != nil {
		restoreErr := m.restoreLocked(previous)
		if restoreErr != nil {
			return "", fmt.Errorf("%v; restore previous SV2 translator profile: %w", err, restoreErr)
		}
		return "", err
	}
	return endpoint, nil
}

func (m *FileManager) Resume(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return nil
	}
	return m.startAndWaitLocked(ctx, m.endpoint)
}

func (m *FileManager) ActiveProfile() (Profile, Endpoint, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.active {
		return Profile{}, "", false
	}
	return cloneProfile(m.profile), m.endpoint, true
}

func (m *FileManager) loadActiveProfile() error {
	data, err := os.ReadFile(m.profilePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read persisted SV2 translator profile: %w", err)
	}
	if len(data) > 64*1024 {
		return fmt.Errorf("persisted SV2 translator profile is too large")
	}

	var persisted persistedProfile
	if err := json.Unmarshal(data, &persisted); err != nil {
		return fmt.Errorf("decode persisted SV2 translator profile: %w", err)
	}
	if persisted.Version != profileVersion {
		return fmt.Errorf("unsupported persisted SV2 translator profile version %d", persisted.Version)
	}
	normalized, err := NormalizeProfile(persisted.Profile)
	if err != nil {
		return fmt.Errorf("validate persisted SV2 translator profile: %w", err)
	}
	if !ProfilesEqual(normalized, persisted.Profile) {
		return fmt.Errorf("persisted SV2 translator profile is not normalized")
	}
	if err := validateEndpoint(persisted.Endpoint, m.config.DownstreamPort); err != nil {
		return fmt.Errorf("validate persisted SV2 translator endpoint: %w", err)
	}
	deviceIdentifiers, err := normalizeDeviceIdentifiers("persisted", persisted.DeviceIdentifiers)
	if err != nil {
		return fmt.Errorf("validate persisted SV2 translator devices: %w", err)
	}
	if len(deviceIdentifiers) == 0 {
		return fmt.Errorf("persisted SV2 translator profile has no translated miners")
	}
	if !equalStrings(deviceIdentifiers, persisted.DeviceIdentifiers) {
		return fmt.Errorf("persisted SV2 translator devices are not normalized")
	}

	m.active = true
	m.profile = cloneProfile(normalized)
	m.endpoint = persisted.Endpoint
	m.devices = deviceSet(deviceIdentifiers)
	return nil
}

func (m *FileManager) endpointForProfile(ctx context.Context, profile Profile) (Endpoint, error) {
	host := m.config.AdvertisedHost
	if host == "" {
		selected, err := routeIP(ctx, profile.Upstreams[0].URL)
		if err != nil {
			return "", err
		}
		host = selected.String()
	}
	host, err := normalizeAdvertisedHost(host)
	if err != nil {
		return "", err
	}
	return Endpoint("stratum+tcp://" + net.JoinHostPort(host, strconv.Itoa(int(m.config.DownstreamPort)))), nil
}

func (m *FileManager) waitReady(ctx context.Context, endpoint Endpoint) error {
	connectHost := m.config.ConnectHost
	if connectHost == "" {
		parsed, err := url.Parse(endpoint.String())
		if err != nil {
			return fmt.Errorf("parse SV2 translator endpoint: %w", err)
		}
		connectHost = parsed.Hostname()
	}
	address := net.JoinHostPort(connectHost, strconv.Itoa(int(m.config.DownstreamPort)))
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		conn, err := (&net.Dialer{Timeout: 250 * time.Millisecond}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		state, stateErr := m.runtime.State(ctx)
		if stateErr == nil && state.Exists && !state.Running {
			return fmt.Errorf("SV2 translator stopped before its listener became ready: %s", state.Detail)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for SV2 translator listener %s: %w", address, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (m *FileManager) releaseSelectedLocked(remaining map[string]struct{}) (Endpoint, error) {
	if !m.active {
		return "", nil
	}
	if len(remaining) > 0 {
		if equalDeviceSets(remaining, m.devices) {
			return m.endpoint, nil
		}
		if err := m.persistActiveProfile(m.profile, m.endpoint, remaining); err != nil {
			return "", err
		}
		m.devices = remaining
		return m.endpoint, nil
	}
	if err := m.stopLocked(); err != nil {
		return "", err
	}
	profileErr := removeIfExists(m.profilePath())
	configErr := removeIfExists(m.configPath())
	m.active = false
	m.profile = Profile{}
	m.endpoint = ""
	m.devices = make(map[string]struct{})
	if profileErr != nil {
		return "", profileErr
	}
	if configErr != nil {
		return "", configErr
	}
	return "", nil
}

func (m *FileManager) activateLocked(
	ctx context.Context,
	profile Profile,
	endpoint Endpoint,
	devices map[string]struct{},
) error {
	rendered, err := renderConfig(profile, m.config.DownstreamPort)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.config.StateDir, 0o700); err != nil {
		return fmt.Errorf("create SV2 translator state directory: %w", err)
	}
	if err := atomicWrite(m.configPath(), rendered, 0o600); err != nil {
		return err
	}
	if err := m.persistActiveProfile(profile, endpoint, devices); err != nil {
		_ = os.Remove(m.configPath())
		return err
	}
	if err := m.startAndWaitLocked(ctx, endpoint); err != nil {
		return err
	}
	m.active = true
	m.profile = cloneProfile(profile)
	m.endpoint = endpoint
	m.devices = cloneDeviceSet(devices)
	return nil
}

func (m *FileManager) persistActiveProfile(
	profile Profile,
	endpoint Endpoint,
	devices map[string]struct{},
) error {
	record, err := json.Marshal(persistedProfile{
		Version:           profileVersion,
		Profile:           profile,
		Endpoint:          endpoint,
		DeviceIdentifiers: sortedDeviceIdentifiers(devices),
	})
	if err != nil {
		return fmt.Errorf("encode SV2 translator profile: %w", err)
	}
	return atomicWrite(m.profilePath(), record, 0o600)
}

func (m *FileManager) startAndWaitLocked(ctx context.Context, endpoint Endpoint) error {
	startupCtx, cancel := context.WithTimeout(ctx, m.config.ReadyTimeout)
	defer cancel()
	if err := m.runtime.EnsureStarted(startupCtx); err != nil {
		return err
	}
	return m.waitReady(startupCtx, endpoint)
}

func (m *FileManager) stopLocked() error {
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return m.runtime.Stop(stopCtx)
}

func (m *FileManager) snapshotLocked() managerSnapshot {
	return managerSnapshot{
		active:   m.active,
		profile:  cloneProfile(m.profile),
		endpoint: m.endpoint,
		devices:  cloneDeviceSet(m.devices),
	}
}

func (m *FileManager) restoreLocked(previous managerSnapshot) error {
	if err := m.stopLocked(); err != nil {
		return err
	}
	if !previous.active {
		_ = removeIfExists(m.profilePath())
		_ = removeIfExists(m.configPath())
		m.active = false
		m.profile = Profile{}
		m.endpoint = ""
		m.devices = make(map[string]struct{})
		return nil
	}
	restoreCtx, cancel := context.WithTimeout(context.Background(), m.config.ReadyTimeout)
	defer cancel()
	return m.activateLocked(
		restoreCtx,
		previous.profile,
		previous.endpoint,
		previous.devices,
	)
}

func (m *FileManager) configPath() string {
	return filepath.Join(m.config.StateDir, configFileName)
}

func (m *FileManager) profilePath() string {
	return filepath.Join(m.config.StateDir, profileFileName)
}

func renderConfig(profile Profile, downstreamPort uint16) ([]byte, error) {
	var upstreams strings.Builder
	for _, upstream := range profile.Upstreams {
		parsed, err := url.Parse(upstream.URL)
		if err != nil {
			return nil, fmt.Errorf("parse SV2 translator upstream: %w", err)
		}
		port, err := strconv.ParseUint(parsed.Port(), 10, 16)
		if err != nil || port == 0 {
			return nil, fmt.Errorf("SV2 translator upstream %q has an invalid port", upstream.URL)
		}
		authority, err := sv2.CanonicalAuthorityPublicKeyFromURL(upstream.URL)
		if err != nil {
			return nil, fmt.Errorf("normalize SV2 authority key: %w", err)
		}
		addressJSON, err := json.Marshal(parsed.Hostname())
		if err != nil {
			return nil, fmt.Errorf("encode SV2 translator upstream address: %w", err)
		}
		authorityJSON, err := json.Marshal(authority)
		if err != nil {
			return nil, fmt.Errorf("encode SV2 translator authority key: %w", err)
		}
		usernameJSON, err := json.Marshal(upstream.Username)
		if err != nil {
			return nil, fmt.Errorf("encode SV2 translator username: %w", err)
		}
		fmt.Fprintf(&upstreams, `
[[upstreams]]
address = %s
port = %d
authority_pubkey = %s
user_identity = %s
`, addressJSON, port, authorityJSON, usernameJSON)
	}

	config := fmt.Sprintf(`downstream_address = "0.0.0.0"
downstream_port = %d
max_supported_version = 2
min_supported_version = 2
downstream_extranonce2_size = 4
verify_payout = false
aggregate_channels = true
supported_extensions = [0x0002]
required_extensions = []

[downstream_difficulty_config]
min_individual_miner_hashrate = 10_000_000_000_000.0
shares_per_minute = 6.0
enable_vardiff = true
job_keepalive_interval_secs = 60
%s`, downstreamPort, upstreams.String())
	return []byte(config), nil
}

func routeIP(ctx context.Context, upstreamURL string) (net.IP, error) {
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("parse SV2 upstream for listener selection: %w", err)
	}
	address := net.JoinHostPort(parsed.Hostname(), parsed.Port())
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", address)
	if err != nil {
		return nil, fmt.Errorf("select miner-reachable SV2 translator address: %w", err)
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("select miner-reachable SV2 translator address: unexpected local address")
	}
	return privateIP("automatically selected", local.IP.String(), false)
}

func privateIP(label, raw string, allowLoopback bool) (net.IP, error) {
	ip := net.ParseIP(strings.Trim(strings.TrimSpace(raw), "[]"))
	if ip == nil || ip.IsUnspecified() || (!allowLoopback && ip.IsLoopback()) ||
		(!ip.IsPrivate() && !(allowLoopback && ip.IsLoopback())) {
		return nil, fmt.Errorf("SV2 translator %s host %q must be a private IP", label, raw)
	}
	return ip, nil
}

func normalizeAdvertisedHost(raw string) (string, error) {
	host := strings.Trim(strings.TrimSpace(raw), "[]")
	if ip := net.ParseIP(host); ip != nil {
		validated, err := privateIP("advertised", host, false)
		if err != nil {
			return "", err
		}
		return validated.String(), nil
	}
	host = strings.ToLower(host)
	if !validHostname(host) {
		return "", fmt.Errorf(
			"SV2 translator advertised host %q must be a private IP or valid hostname",
			raw,
		)
	}
	return host, nil
}

func validHostname(host string) bool {
	if host == "" || len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	allNumeric := true
	for _, char := range host {
		if (char < '0' || char > '9') && char != '.' {
			allNumeric = false
			break
		}
	}
	if allNumeric {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') &&
				(char < '0' || char > '9') &&
				char != '-' {
				return false
			}
		}
	}
	return true
}

func validateEndpoint(endpoint Endpoint, expectedPort uint16) error {
	parsed, err := url.Parse(endpoint.String())
	if err != nil {
		return fmt.Errorf("parse persisted SV2 translator endpoint: %w", err)
	}
	if parsed.Scheme != "stratum+tcp" {
		return fmt.Errorf("unexpected scheme %q", parsed.Scheme)
	}
	if _, err := normalizeAdvertisedHost(parsed.Hostname()); err != nil {
		return err
	}
	if parsed.Port() != strconv.Itoa(int(expectedPort)) {
		return fmt.Errorf("unexpected port %q", parsed.Port())
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".sv2-*")
	if err != nil {
		return fmt.Errorf("create temporary SV2 translator state: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("set SV2 translator state permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write SV2 translator state: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync SV2 translator state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close SV2 translator state: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish SV2 translator state: %w", err)
	}
	return nil
}

func cloneProfile(profile Profile) Profile {
	return Profile{Upstreams: append([]Upstream(nil), profile.Upstreams...)}
}

func normalizeDeviceIdentifiers(label string, identifiers []string) ([]string, error) {
	normalized := make([]string, 0, len(identifiers))
	seen := make(map[string]struct{}, len(identifiers))
	for _, raw := range identifiers {
		identifier := strings.TrimSpace(raw)
		if identifier == "" {
			return nil, fmt.Errorf("%s miner identifier must not be empty", label)
		}
		if _, ok := seen[identifier]; ok {
			continue
		}
		seen[identifier] = struct{}{}
		normalized = append(normalized, identifier)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func deviceSet(identifiers []string) map[string]struct{} {
	devices := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		devices[identifier] = struct{}{}
	}
	return devices
}

func cloneDeviceSet(devices map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(devices))
	for identifier := range devices {
		cloned[identifier] = struct{}{}
	}
	return cloned
}

func sortedDeviceIdentifiers(devices map[string]struct{}) []string {
	identifiers := make([]string, 0, len(devices))
	for identifier := range devices {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	return identifiers
}

func equalDeviceSets(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for identifier := range left {
		if _, ok := right[identifier]; !ok {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove SV2 translator state %s: %w", filepath.Base(path), err)
	}
	return nil
}
