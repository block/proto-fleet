package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/netutil"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/fleetnode/bootstrap"
)

const (
	controlReconnectInitial = 1 * time.Second
	controlReconnectMax     = 30 * time.Second
	// A session that survives this long resets the reconnect backoff; flapping
	// connections keep backoff growing.
	stableSessionThreshold = 30 * time.Second
	probeConcurrency       = 32
	discoveryReportTimeout = 30 * time.Second
	maxDevicesPerReport    = 1024 // server enforces max_items=1024
	maxIPsPerCommand       = discoverylimits.MaxScanTargets
	maxPortsPerIP          = discoverylimits.MaxPortsPerIP
	// Mirrors the proto cap so a verbose error doesn't fail buf-validate on the ack itself.
	maxAckErrorMessageBytes = 4096
	// commandPoolSize bounds quick per-miner commands handled concurrently per
	// session. Discovery does not draw from this pool; it has its own exclusive,
	// single-flight slot (see runControlSession). Commands past the ceiling are
	// acked BUSY.
	commandPoolSize = 16
)

// var, not const, so tests can drive the deadline-during-scan path.
var commandTimeout = 10 * time.Minute

// Matches the server's pairing.perDeviceDiscoveryTimeout; 3s wasn't enough
// for the plugin's TCP+protocol handshake on real miners. var so the
// supervisor test can shrink it.
var perProbeTimeout = 10 * time.Second

type discoverer interface {
	Probe(ctx context.Context, ipAddress, port string) (*pb.DiscoveredDeviceReport, error)
	DefaultDiscoveryPorts(ctx context.Context) []string
}

// acker keeps handleCommand testable without a real bidi stream.
type acker interface {
	Send(req *pb.ControlStreamRequest) error
}

// connect-go bidi streams are not safe for concurrent Send. The receive
// loop's busy-ack and the worker's completion ack now share a stream;
// serialize through this wrapper.
type lockedAcker struct {
	mu    sync.Mutex
	inner acker
}

func (l *lockedAcker) Send(req *pb.ControlStreamRequest) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.inner.Send(req)
}

type endpoint struct{ ip, port string }

type commandError struct {
	code pb.AckCode
	msg  string
}

func (e *commandError) Error() string { return e.msg }

func cmdErr(code pb.AckCode, format string, args ...any) *commandError {
	return &commandError{code: code, msg: fmt.Sprintf(format, args...)}
}

func (r *RunCmd) runControlLoop(ctx context.Context, client gatewayClient, st *bootstrap.State, logger *slog.Logger) error {
	loopLogger := logger.With("fleet_node_id", st.FleetNodeID)
	backoff := controlReconnectInitial
	// Per-loop rng so tests don't race on math/rand's global source.
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // jitter, not crypto
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		started := time.Now()
		err := r.runControlSession(ctx, loopLogger, client)
		if err == nil {
			return nil
		}
		if errors.Is(err, bootstrap.ErrBeginAuthRejected) || connect.CodeOf(err) == connect.CodeNotFound {
			return err
		}
		if connect.CodeOf(err) == connect.CodeUnimplemented {
			// Old server: drop to heartbeat-only instead of looping forever.
			loopLogger.Info("control stream unimplemented by server; running heartbeat-only", "err", err)
			return nil
		}
		if time.Since(started) > stableSessionThreshold {
			backoff = controlReconnectInitial
		}
		sleep := min(backoff+time.Duration(rng.Int63n(int64(backoff/2)+1)), controlReconnectMax)
		loopLogger.Warn("control stream disconnected; will reconnect", "backoff", sleep.String(), "err", err)
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		backoff = min(backoff*2, controlReconnectMax)
	}
}

func (r *RunCmd) runControlSession(ctx context.Context, logger *slog.Logger, client gatewayClient) error {
	stream := client.ControlStream(ctx)
	// stream.Receive parks in http2.pipe on a sync.Cond ctx can't unblock;
	// the watcher below closes the stream so Ctrl+C returns. Defers run
	// LIFO: close(done) fires first so the watcher exits quietly on normal
	// return.
	defer func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() }()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.CloseRequest()
			_ = stream.CloseResponse()
		case <-done:
		}
	}()

	if err := stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Hello{Hello: &pb.ControlHello{}}}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}
	first, err := stream.Receive()
	if err != nil {
		return fmt.Errorf("await accepted: %w", err)
	}
	if first.GetAccepted() == nil {
		return fmt.Errorf("first server message was not Accepted")
	}
	logger.Info("control stream opened")

	// sessionCtx so a dropped stream cancels the in-flight scan immediately;
	// without it the agent would burn up to commandTimeout finishing an old
	// scan before opening a new session.
	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()

	// Serialize all sends on the bidi: the worker's completion ack and the
	// receive loop's busy ack would otherwise race on stream.Send.
	sender := &lockedAcker{inner: stream}

	// Two lanes per session:
	//   - discovery (and the pairing effort's future pair) is a heavy, report-bearing
	//     scan; it is single-flight per node via an exclusive slot, so a second
	//     concurrent discovery is rejected BUSY rather than doubling the scan load.
	//   - quick per-miner commands use a broader pool, so they run concurrently and a
	//     long discovery never head-of-line-blocks them.
	// Both are non-blocking acquires: parking the receive loop would hide stream
	// drops behind in-flight work, so at capacity we ack BUSY.
	discoverySlot := make(chan struct{}, 1)
	cmdSem := make(chan struct{}, commandPoolSize)
	var wg sync.WaitGroup
	defer func() {
		cancelSession() // cancel every in-flight handler's ctx
		wg.Wait()       // drain: handlers ack or abort fast on the cancelled ctx
	}()

	for {
		msg, err := stream.Receive()
		if err != nil {
			// Watcher closed the stream because ctx is done; clean shutdown.
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("control stream closed by server: %w", err)
			}
			return fmt.Errorf("recv: %w", err)
		}
		cmd := msg.GetCommand()
		if cmd == nil {
			continue
		}
		// Decode the envelope once here so we can pick a lane and handleCommand
		// need not re-parse the payload. A malformed payload is not report-bearing:
		// it takes the pool lane and handleCommand acks it BAD_REQUEST.
		env, parseErr := decodeAgentCommand(cmd.GetPayload())
		slot := cmdSem
		if parseErr == nil && (env.GetDiscover() != nil || env.GetPair() != nil) {
			slot = discoverySlot
		}
		select {
		case slot <- struct{}{}:
			wg.Add(1)
			// All loop-scoped values the handler needs are passed as arguments,
			// including the acquired lane, so each goroutine releases the same lane.
			go func(c *pb.ControlCommand, e *pb.AgentCommand, pErr error, lane chan struct{}) {
				defer wg.Done()
				defer func() { <-lane }()
				r.handleCommand(sessionCtx, client, sender, c, e, pErr, logger)
			}(cmd, env, parseErr, slot)
		default:
			logger.Warn("agent at capacity; rejecting command", "command_id", cmd.GetCommandId())
			r.sendAck(sender, cmd.GetCommandId(), pb.AckCode_ACK_CODE_BUSY, "agent at concurrency limit; retry shortly", logger)
		}
	}
}

// decodeAgentCommand unmarshals the ControlCommand.payload envelope. The receive loop
// decodes once and hands the result to handleCommand so the payload is parsed a single
// time. Discovery (and the pairing effort's future pair) is the heavy, report-bearing
// kind that takes the exclusive single-flight slot; everything else, including a
// malformed payload, takes the per-miner command pool and is acked by handleCommand.
func decodeAgentCommand(payload []byte) (*pb.AgentCommand, error) {
	env := &pb.AgentCommand{}
	if err := proto.Unmarshal(payload, env); err != nil {
		return nil, fmt.Errorf("decode AgentCommand: %w", err)
	}
	return env, nil
}

func (r *RunCmd) handleCommand(ctx context.Context, client gatewayClient, stream acker, cmd *pb.ControlCommand, env *pb.AgentCommand, parseErr error, logger *slog.Logger) {
	commandID := cmd.GetCommandId()
	// Drop silently if command_id is itself unsafe to echo in an ack; the
	// gateway would reject the ack and close the stream. Server retries.
	if vErr := protovalidate.Validate(cmd); vErr != nil {
		if commandID == "" || len(commandID) > 128 {
			logger.Warn("dropping inbound ControlCommand: command_id violates proto contract, cannot ack safely", "err", vErr)
			return
		}
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, fmt.Sprintf("invalid ControlCommand: %v", vErr), logger)
		return
	}
	logger.Info("control command received", "command_id", commandID, "payload_bytes", len(cmd.GetPayload()))

	if parseErr != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, parseErr.Error(), logger)
		return
	}
	switch k := env.GetCommand().(type) {
	case *pb.AgentCommand_Discover:
		r.handleDiscover(ctx, client, stream, commandID, k.Discover, logger)
	case *pb.AgentCommand_MinerCommand:
		r.handleMinerCommand(ctx, stream, commandID, k.MinerCommand, logger)
	case *pb.AgentCommand_Pair:
		r.handlePairCommand(ctx, client, stream, commandID, k.Pair, logger)
	default:
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_BAD_REQUEST, "AgentCommand has no recognized command kind", logger)
	}
}

// handleDiscover scans the local network for the requested targets, streams the
// discovered devices back via ReportDiscoveredDevices, and acks OK/PARTIAL/failure.
func (r *RunCmd) handleDiscover(ctx context.Context, client gatewayClient, stream acker, commandID string, req *pairingpb.DiscoverRequest, logger *slog.Logger) {
	cmdCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	reports, truncated, err := r.discoverForCommand(cmdCtx, req, logger)
	if err != nil {
		code := pb.AckCode_ACK_CODE_INTERNAL
		var ce *commandError
		if errors.As(err, &ce) {
			code = ce.code
		}
		r.sendAck(stream, commandID, code, err.Error(), logger)
		return
	}
	// Stream on parent ctx, not cmdCtx: if the scan hit commandTimeout,
	// cmdCtx is dead and partial reports would never upload. Each batch is
	// still bounded by discoveryReportTimeout.
	if err := r.streamReports(ctx, client, commandID, reports, logger); err != nil {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_REPORT_FAILED, err.Error(), logger)
		return
	}
	// Two PARTIAL sources: cmdCtx deadline (commandTimeout) or fanOutProbes
	// supervisor (a probe ignored ctx). Either way reports already uploaded.
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_PARTIAL, fmt.Sprintf("scan exceeded command deadline (%s); %d partial report(s) uploaded", commandTimeout, len(reports)), logger)
		return
	}
	if truncated {
		r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_PARTIAL, fmt.Sprintf("probe supervisor budget exceeded; %d report(s) uploaded, some endpoints not probed", len(reports)), logger)
		return
	}
	r.sendAck(stream, commandID, pb.AckCode_ACK_CODE_OK, "", logger)
}

func (r *RunCmd) discoverForCommand(ctx context.Context, req *pairingpb.DiscoverRequest, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, bool, error) {
	if req.GetMode() == nil {
		return nil, false, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "discover request mode is required")
	}
	switch m := req.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		ips := m.IpList.GetIpAddresses()
		if len(ips) == 0 {
			return nil, false, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "ip_addresses must be non-empty")
		}
		if len(ips) > maxIPsPerCommand {
			return nil, false, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "too many ip_addresses: %d exceeds the limit of %d", len(ips), maxIPsPerCommand)
		}
		ports, err := r.resolveAndValidatePorts(ctx, m.IpList.GetPorts())
		if err != nil {
			return nil, false, err
		}
		normalized := make([]string, 0, len(ips))
		for _, raw := range ips {
			n, err := netutil.NormalizeIPListEntry(ctx, raw, net.DefaultResolver)
			if err != nil {
				logger.Debug("skipping ipList entry", "input", raw, "err", err)
				continue
			}
			normalized = append(normalized, n)
		}
		if len(normalized) == 0 {
			return nil, false, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "no usable ip_addresses after normalization (scoped/link-local IPv6 and unresolvable hostnames are skipped)")
		}
		reports, truncated := r.probeIPsAndPorts(ctx, normalized, ports, logger)
		return reports, truncated, nil
	case *pairingpb.DiscoverRequest_IpRange:
		ports, err := r.resolveAndValidatePorts(ctx, m.IpRange.GetPorts())
		if err != nil {
			return nil, false, err
		}
		ips, err := expandIPv4Range(m.IpRange.GetStartIp(), m.IpRange.GetEndIp(), maxIPsPerCommand)
		if err != nil {
			return nil, false, err
		}
		reports, truncated := r.probeIPsAndPorts(ctx, ips, ports, logger)
		return reports, truncated, nil
	case *pairingpb.DiscoverRequest_Nmap:
		ports, err := r.resolveAndValidatePorts(ctx, m.Nmap.GetPorts())
		if err != nil {
			return nil, false, err
		}
		return r.runNmapDiscovery(ctx, m.Nmap, ports, logger)
	case *pairingpb.DiscoverRequest_Mdns:
		return nil, false, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "mdns mode is not supported on the fleet node agent")
	default:
		return nil, false, cmdErr(pb.AckCode_ACK_CODE_AGENT_INCAPABLE, "agent does not support requested discover mode")
	}
}

func (r *RunCmd) probeIPsAndPorts(ctx context.Context, ips []string, ports []string, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, bool) {
	endpoints := make([]endpoint, 0, len(ips)*len(ports))
	for _, ip := range ips {
		for _, port := range ports {
			endpoints = append(endpoints, endpoint{ip: ip, port: port})
		}
	}
	return fanOutProbes(ctx, endpoints, probeConcurrency, r.discoverer.Probe, logger)
}

// Single decimal port only; range/comma syntax would let one entry bypass
// maxPortsPerIP. Plugin defaults pass through the same validator.
func (r *RunCmd) resolveAndValidatePorts(ctx context.Context, supplied []string) ([]string, error) {
	ports := supplied
	if len(ports) == 0 {
		ports = r.discoverer.DefaultDiscoveryPorts(ctx)
	}
	if len(ports) == 0 {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "ports must be non-empty (no defaults available)")
	}
	if len(ports) > maxPortsPerIP {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "too many ports: %d exceeds the limit of %d", len(ports), maxPortsPerIP)
	}
	// Emit canonical form so "+80"/"080" don't reach the gateway's ^[1-9][0-9]*$ check.
	seen := make(map[string]struct{}, len(ports))
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "invalid port %q: must be decimal 1-65535 (ranges, commas, and protocol prefixes are not allowed)", p)
		}
		canonical := strconv.Itoa(n)
		if _, dup := seen[canonical]; dup {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	return out, nil
}

// Skips .0/.1 at the range start to match server's DiscoverWithIPRange,
// except inside 127.0.0.0/8 where dev fixtures bind.
func expandIPv4Range(startStr, endStr string, maxCount int) ([]string, error) {
	startAddr, err := netutil.ParseIPv4(startStr)
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "start_ip %q: %s", startStr, err)
	}
	endAddr, err := netutil.ParseIPv4(endStr)
	if err != nil {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "end_ip %q: %s", endStr, err)
	}
	startU := netutil.IPv4ToUint32(startAddr)
	endU := netutil.IPv4ToUint32(endAddr)
	if endU < startU {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "end_ip %q must be >= start_ip %q", endStr, startStr)
	}
	startU = netutil.AdjustIPv4RangeStart(startU)
	if endU < startU {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "range %q-%q only covers network/gateway addresses", startStr, endStr)
	}
	size := int(endU - startU + 1)
	if size > maxCount {
		return nil, cmdErr(pb.AckCode_ACK_CODE_BAD_REQUEST, "ip range expands to %d addresses, exceeds the limit of %d", size, maxCount)
	}
	out := make([]string, 0, size)
	for v := startU; ; v++ {
		out = append(out, netutil.Uint32ToIPv4(v))
		if v == endU {
			break
		}
	}
	return out, nil
}

// Returns (reports, truncated). Supervisor caps wg.Wait at perProbeTimeout*2
// so a plugin Probe that ignores ctx can't pin the agent; truncated=true
// lets the caller ack PARTIAL.
func fanOutProbes(ctx context.Context, endpoints []endpoint, concurrency int, probe func(context.Context, string, string) (*pb.DiscoveredDeviceReport, error), logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, bool) {
	if len(endpoints) == 0 {
		return nil, false
	}
	var (
		mu      sync.Mutex
		reports []*pb.DiscoveredDeviceReport
		wg      sync.WaitGroup
	)
	sem := make(chan struct{}, concurrency)
	for _, e := range endpoints {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			out, _ := waitSupervisor(&wg, &mu, &reports, perProbeTimeout*2, "probe", logger)
			return out, true
		}
		wg.Add(1)
		go func(ip, port string) {
			defer wg.Done()
			defer func() { <-sem }()
			probeCtx, cancel := context.WithTimeout(ctx, perProbeTimeout)
			defer cancel()
			report, err := probe(probeCtx, ip, port)
			if err != nil {
				logger.Debug("probe failed", "ip", ip, "port", port, "err", err)
				return
			}
			if report == nil || report.GetDeviceIdentifier() == "" {
				return
			}
			// Plugins can return any IpAddress/Port in their DiscoveredDevice;
			// a buggy or hostile plugin would otherwise let us upload a
			// spoofed endpoint and poison the server's discovery state.
			// Override with what we actually probed before validating.
			report.IpAddress = ip
			report.Port = port
			// One device that violates the gateway's buf-validate rules
			// (oversized model string, wrong url_scheme, etc.) would fail
			// the whole ReportDiscoveredDevices batch server-side and lose
			// every other device in it. Validate per-device here and drop
			// the bad one instead.
			if vErr := protovalidate.Validate(report); vErr != nil {
				logger.Warn("dropping device report that fails gateway validation",
					"ip", ip, "port", port,
					"device_id", report.GetDeviceIdentifier(),
					"err", vErr)
				return
			}
			mu.Lock()
			reports = append(reports, report)
			mu.Unlock()
		}(e.ip, e.port)
	}
	return waitSupervisor(&wg, &mu, &reports, perProbeTimeout*2, "probe", logger)
}

// waitSupervisor caps wg.Wait at maxWait so a plugin call that ignores ctx can't
// pin the agent; truncated=true lets the caller ack PARTIAL. noun names the work
// for the budget-exceeded log line. Shared by the discovery and pair fan-outs.
func waitSupervisor[T any](wg *sync.WaitGroup, mu *sync.Mutex, results *[]T, maxWait time.Duration, noun string, logger *slog.Logger) ([]T, bool) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	truncated := false
	select {
	case <-done:
	case <-timer.C:
		truncated = true
		logger.Warn(noun+" wait exceeded supervisor budget; returning partial batch", "max_wait", maxWait.String())
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]T, len(*results))
	copy(out, *results)
	return out, truncated
}

func (r *RunCmd) streamReports(ctx context.Context, client gatewayClient, commandID string, reports []*pb.DiscoveredDeviceReport, logger *slog.Logger) error {
	for chunk := range slices.Chunk(reports, maxDevicesPerReport) {
		callCtx, cancel := context.WithTimeout(ctx, discoveryReportTimeout)
		_, err := client.ReportDiscoveredDevices(callCtx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
			CommandId: commandID,
			Devices:   chunk,
		}))
		cancel()
		if err != nil {
			logger.Error("report failed", "command_id", commandID, "err", err)
			return fmt.Errorf("report devices: %w", err)
		}
		logger.Info("report accepted", "command_id", commandID, "batch_size", len(chunk))
	}
	return nil
}

func (r *RunCmd) sendAck(stream acker, commandID string, code pb.AckCode, errMsg string, logger *slog.Logger) {
	errMsg = truncateUTF8(errMsg, maxAckErrorMessageBytes)
	if err := stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{
		CommandId:    commandID,
		Succeeded:    code == pb.AckCode_ACK_CODE_OK,
		ErrorMessage: errMsg,
		Code:         code,
	}}}); err != nil {
		logger.Warn("send ack failed", "command_id", commandID, "err", err)
	}
}

type pluginDiscoverer struct {
	multi *plugins.MultiTypeDiscoverer
	svc   *plugins.Service
	// fleetNodeID is folded into synthesized auto: identifiers (see synthesizeIdentifier).
	fleetNodeID int64
}

// newPluginComponents builds the discoverer and pairer over one shared manager
// so the node loads plugins only once.
func newPluginComponents(parent context.Context, pluginsDir string, fleetNodeID int64) (*pluginDiscoverer, *pluginPairer, func(), error) {
	// Manager.Shutdown waits the full grace period even when a plugin already
	// exited, so keep it tight; a stuck plugin still gets killed.
	manager := plugins.NewManager(&plugins.Config{
		Enabled:                    true,
		PluginsDir:                 pluginsDir,
		MaxStartupTimeSeconds:      30,
		ShutdownTimeoutSeconds:     10,
		ShutdownGracePeriodSeconds: 2,
		LogLevel:                   "info",
	})
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	if err := manager.LoadPlugins(ctx); err != nil {
		// LoadPlugins can leave partial subprocesses on error; reap them so
		// the agent doesn't exit with orphans behind it.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = manager.Shutdown(shutdownCtx)
		shutdownCancel()
		return nil, nil, func() {}, fmt.Errorf("load plugins: %w", err)
	}
	// Parent ctx is typically already cancelled by a signal when cleanup
	// runs; use a fresh background ctx bounded by the same 10s budget.
	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = manager.Shutdown(shutdownCtx)
	}
	prr := newPluginPairer(manager)
	disc := &pluginDiscoverer{
		multi:       plugins.NewMultiTypeDiscoverer(manager),
		svc:         plugins.NewService(manager),
		fleetNodeID: fleetNodeID,
	}
	return disc, prr, cleanup, nil
}

func (p *pluginDiscoverer) Probe(ctx context.Context, ipAddress, port string) (*pb.DiscoveredDeviceReport, error) {
	dev, err := p.multi.Discover(ctx, ipAddress, port)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, nil
	}
	return reportFromDiscovered(dev, ipAddress, port, p.fleetNodeID), nil
}

func (p *pluginDiscoverer) DefaultDiscoveryPorts(ctx context.Context) []string {
	return p.svc.GetDefaultDiscoveryPorts(ctx)
}

// SDK drivers often leave DeviceIdentifier empty; the agent has no DB so it
// synthesizes a stable identifier. ip/port are the trusted probed endpoint
// (what fanOutProbes also stamps onto the report), not the plugin-reported one,
// which may be blank or wrong.
func reportFromDiscovered(dev *discoverymodels.DiscoveredDevice, ip, port string, fleetNodeID int64) *pb.DiscoveredDeviceReport {
	deviceID := dev.GetDeviceIdentifier()
	if deviceID == "" {
		deviceID = synthesizeIdentifier(dev, ip, port, fleetNodeID)
	}
	return &pb.DiscoveredDeviceReport{
		DeviceIdentifier: deviceID,
		IpAddress:        ip,
		Port:             port,
		UrlScheme:        dev.GetUrlScheme(),
		DriverName:       dev.GetDriverName(),
		Model:            dev.GetModel(),
		Manufacturer:     dev.GetManufacturer(),
		FirmwareVersion:  dev.GetFirmwareVersion(),
	}
}

// synthesizeIdentifier derives a stable identifier for a device the driver left
// unidentified. MAC/serial are preferred; absent both, it hashes the fleet node
// id + probed endpoint + type, so the same device re-keys identically across
// scans while distinct endpoints stay distinct (no duplicate rows, no
// server-side reconciliation). The fleet node id is in the hash so two nodes on
// overlapping RFC1918 space don't mint the same auto: key for distinct miners.
func synthesizeIdentifier(dev *discoverymodels.DiscoveredDevice, ip, port string, fleetNodeID int64) string {
	if mac := dev.GetMacAddress(); mac != "" {
		return "mac:" + mac
	}
	if serial := dev.GetSerialNumber(); serial != "" {
		return "serial:" + serial
	}
	node := strconv.FormatInt(fleetNodeID, 10)
	sum := sha256.Sum256([]byte(strings.Join([]string{node, ip, port, dev.GetDriverName(), dev.GetModel()}, "\x00")))
	return "auto:" + hex.EncodeToString(sum[:8])
}
