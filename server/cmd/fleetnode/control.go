package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
	"github.com/block/proto-fleet/server/internal/domain/plugins"
	"github.com/block/proto-fleet/server/internal/fleetnodebootstrap"
	"github.com/block/proto-fleet/server/internal/infrastructure/id"
)

const (
	controlReconnectInitial = 1 * time.Second
	controlReconnectMax     = 30 * time.Second
	perProbeTimeout         = 3 * time.Second
	probeConcurrency        = 32
	discoveryReportTimeout  = 30 * time.Second
	// Server enforces max_items = 1024 on the report batch.
	maxDevicesPerReport = 1024
)

type discoverer interface {
	Probe(ctx context.Context, ipAddress, port string) (*pb.DiscoveredDeviceReport, error)
	DefaultDiscoveryPorts(ctx context.Context) []string
}

// runControlLoop opens a ControlStream and consumes server-issued
// DiscoverRequests for the lifetime of ctx. On stream error it
// reconnects with exponential backoff up to controlReconnectMax.
// Permanent server-side rejections (NotFound, ErrBeginAuthRejected)
// exit the loop so the daemon shuts down rather than spinning.
func (r *RunCmd) runControlLoop(ctx context.Context, client gatewayClient, st *fleetnodebootstrap.State, logger *slog.Logger) error {
	backoff := controlReconnectInitial
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		err := r.runControlSession(ctx, client, st, logger)
		if err == nil {
			return nil
		}
		if errors.Is(err, fleetnodebootstrap.ErrBeginAuthRejected) || connect.CodeOf(err) == connect.CodeNotFound {
			return err
		}
		logger.Warn("control stream disconnected; will reconnect", "fleet_node_id", st.FleetNodeID, "backoff", backoff.String(), "err", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > controlReconnectMax {
			backoff = controlReconnectMax
		}
	}
}

func (r *RunCmd) runControlSession(ctx context.Context, client gatewayClient, st *fleetnodebootstrap.State, logger *slog.Logger) error {
	stream := client.ControlStream(ctx)
	defer func() { _ = stream.CloseRequest(); _ = stream.CloseResponse() }()

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
	logger.Info("control stream opened", "fleet_node_id", st.FleetNodeID)

	for {
		msg, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("control stream closed by server: %w", err)
			}
			return fmt.Errorf("recv: %w", err)
		}
		cmd := msg.GetCommand()
		if cmd == nil {
			continue
		}
		r.handleCommand(ctx, client, stream, cmd, logger, st.FleetNodeID)
	}
}

func (r *RunCmd) handleCommand(ctx context.Context, client gatewayClient, stream *connect.BidiStreamForClient[pb.ControlStreamRequest, pb.ControlStreamResponse], cmd *pb.ControlCommand, logger *slog.Logger, fleetNodeID int64) {
	commandID := cmd.GetCommandId()
	logger.Info("control command received", "command_id", commandID, "payload_bytes", len(cmd.GetPayload()))

	var req pairingpb.DiscoverRequest
	if err := proto.Unmarshal(cmd.GetPayload(), &req); err != nil {
		r.sendAck(stream, commandID, false, fmt.Sprintf("decode payload: %v", err), logger)
		return
	}

	reports, err := r.discoverForCommand(ctx, &req, logger)
	if err != nil {
		r.sendAck(stream, commandID, false, err.Error(), logger)
		return
	}
	if err := r.streamReports(ctx, client, commandID, reports, logger, fleetNodeID); err != nil {
		r.sendAck(stream, commandID, false, err.Error(), logger)
		return
	}
	r.sendAck(stream, commandID, true, "", logger)
}

func (r *RunCmd) discoverForCommand(ctx context.Context, req *pairingpb.DiscoverRequest, logger *slog.Logger) ([]*pb.DiscoveredDeviceReport, error) {
	switch m := req.GetMode().(type) {
	case *pairingpb.DiscoverRequest_IpList:
		ips := m.IpList.GetIpAddresses()
		ports := m.IpList.GetPorts()
		if len(ports) == 0 {
			ports = r.discoverer.DefaultDiscoveryPorts(ctx)
		}
		if len(ips) == 0 || len(ports) == 0 {
			return nil, fmt.Errorf("ip_addresses and ports must both be non-empty")
		}
		return r.runProbes(ctx, ips, ports, logger), nil
	case *pairingpb.DiscoverRequest_Nmap:
		return r.runNmapDiscovery(ctx, m.Nmap, logger)
	case *pairingpb.DiscoverRequest_Mdns:
		return nil, fmt.Errorf("mdns mode is not supported on the fleet node agent")
	default:
		return nil, fmt.Errorf("discover request mode is required")
	}
}

func (r *RunCmd) runProbes(ctx context.Context, ips, ports []string, logger *slog.Logger) []*pb.DiscoveredDeviceReport {
	var (
		mu    sync.Mutex
		batch []*pb.DiscoveredDeviceReport
		wg    sync.WaitGroup
	)
	sem := make(chan struct{}, probeConcurrency)
	for _, ip := range ips {
		for _, port := range ports {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return batch
			}
			wg.Add(1)
			go func(ip, port string) {
				defer wg.Done()
				defer func() { <-sem }()
				probeCtx, cancel := context.WithTimeout(ctx, perProbeTimeout)
				defer cancel()
				report, err := r.discoverer.Probe(probeCtx, ip, port)
				if err != nil {
					logger.Debug("probe failed", "ip", ip, "port", port, "err", err)
					return
				}
				if report == nil {
					return
				}
				if report.GetDeviceIdentifier() == "" {
					return
				}
				mu.Lock()
				batch = append(batch, report)
				mu.Unlock()
			}(ip, port)
		}
	}
	wg.Wait()
	return batch
}

func (r *RunCmd) streamReports(ctx context.Context, client gatewayClient, commandID string, reports []*pb.DiscoveredDeviceReport, logger *slog.Logger, fleetNodeID int64) error {
	if len(reports) == 0 {
		return nil
	}
	for start := 0; start < len(reports); start += maxDevicesPerReport {
		end := start + maxDevicesPerReport
		if end > len(reports) {
			end = len(reports)
		}
		chunk := reports[start:end]
		callCtx, cancel := context.WithTimeout(ctx, discoveryReportTimeout)
		_, err := client.ReportDiscoveredDevices(callCtx, connect.NewRequest(&pb.ReportDiscoveredDevicesRequest{
			CommandId: commandID,
			Devices:   chunk,
		}))
		cancel()
		if err != nil {
			logger.Error("report failed", "command_id", commandID, "fleet_node_id", fleetNodeID, "err", err)
			return fmt.Errorf("report devices: %w", err)
		}
		logger.Info("report accepted", "command_id", commandID, "fleet_node_id", fleetNodeID, "batch_size", len(chunk))
	}
	return nil
}

func (r *RunCmd) sendAck(stream *connect.BidiStreamForClient[pb.ControlStreamRequest, pb.ControlStreamResponse], commandID string, succeeded bool, errMsg string, logger *slog.Logger) {
	if err := stream.Send(&pb.ControlStreamRequest{Kind: &pb.ControlStreamRequest_Ack{Ack: &pb.ControlAck{
		CommandId:    commandID,
		Succeeded:    succeeded,
		ErrorMessage: errMsg,
	}}}); err != nil {
		logger.Warn("send ack failed", "command_id", commandID, "err", err)
	}
}

type pluginDiscoverer struct {
	multi *plugins.MultiTypeDiscoverer
	svc   *plugins.Service
}

// pluginsDir must be an absolute path: the manager exec's every executable in
// that directory, so a relative path would resolve against the daemon's CWD
// and let anyone with write access there plant code that runs with the
// agent's privileges.
func newPluginDiscoverer(pluginsDir string) (*pluginDiscoverer, func(), error) {
	if !filepath.IsAbs(pluginsDir) {
		return nil, func() {}, fmt.Errorf("--plugins-dir must be an absolute path, got %q", pluginsDir)
	}
	manager := plugins.NewManager(&plugins.Config{
		Enabled:                    true,
		PluginsDir:                 pluginsDir,
		MaxStartupTimeSeconds:      30,
		ShutdownTimeoutSeconds:     10,
		ShutdownGracePeriodSeconds: 5,
		LogLevel:                   "info",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := manager.LoadPlugins(ctx); err != nil {
		return nil, func() {}, fmt.Errorf("load plugins: %w", err)
	}
	cleanup := func() { _ = manager.Shutdown(context.Background()) }
	return &pluginDiscoverer{
		multi: plugins.NewMultiTypeDiscoverer(manager),
		svc:   plugins.NewService(manager),
	}, cleanup, nil
}

func (p *pluginDiscoverer) Probe(ctx context.Context, ipAddress, port string) (*pb.DiscoveredDeviceReport, error) {
	dev, err := p.multi.Discover(ctx, ipAddress, port)
	if err != nil {
		return nil, err
	}
	if dev == nil {
		return nil, nil
	}
	return reportFromDiscovered(dev), nil
}

func (p *pluginDiscoverer) DefaultDiscoveryPorts(ctx context.Context) []string {
	return p.svc.GetDefaultDiscoveryPorts(ctx)
}

// Production SDK drivers leave DeviceIdentifier empty; the agent has no DB to
// reconcile against, so it synthesizes auto:* identifiers and the server
// reconciles by (fleet_node, ip, port).
func reportFromDiscovered(dev *discoverymodels.DiscoveredDevice) *pb.DiscoveredDeviceReport {
	deviceID := dev.GetDeviceIdentifier()
	if deviceID == "" {
		deviceID = synthesizeIdentifier(dev.GetMacAddress(), dev.GetSerialNumber())
	}
	return &pb.DiscoveredDeviceReport{
		DeviceIdentifier: deviceID,
		IpAddress:        dev.GetIpAddress(),
		Port:             dev.GetPort(),
		UrlScheme:        dev.GetUrlScheme(),
		DriverName:       dev.GetDriverName(),
		Model:            dev.GetModel(),
		Manufacturer:     dev.GetManufacturer(),
		FirmwareVersion:  dev.GetFirmwareVersion(),
	}
}

func synthesizeIdentifier(mac, serial string) string {
	if mac != "" {
		return "mac:" + mac
	}
	if serial != "" {
		return "serial:" + serial
	}
	return "auto:" + id.GenerateID()
}
