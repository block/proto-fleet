package ct1

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mdk "github.com/block/proto-fleet/sdk/mdk/generated/mdk/v2"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const DefaultRPCPort int32 = 9095
const maxReceiveBytes = 8 << 20

// Client owns one controller connection. Independent reads share it; controls
// retain their serialized read/write/readback sequence.
type Client struct {
	closeOnce sync.Once
	closeErr  error
	conn      *grpc.ClientConn
	endpoint  sdk.DeviceInfo
	memberID  string
	system    mdk.SystemServiceClient
	boards    mdk.HashboardServiceClient
	mining    mdk.MiningServiceClient
	telemetry mdk.TelemetryServiceClient
	power     mdk.PowerServiceClient
	firmware  mdk.FirmwareServiceClient
	pollMu    sync.Mutex
}

func NewClient(info sdk.DeviceInfo, _ sdk.UsernamePassword) (*Client, error) {
	if info.Host == "" || info.Port <= 0 || info.Port > 65535 || (info.URLScheme != "http" && info.URLScheme != "https") {
		return nil, fmt.Errorf("invalid CT1 web endpoint")
	}
	port := info.RPCPort
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid CT1 RPC port")
	}
	if os.Getenv("CT1_RPC_TOKEN_FILE") != "" {
		return nil, fmt.Errorf("CT1_RPC_TOKEN_FILE is unsupported; authenticated CT1 RPC connections are not supported")
	}
	var transport credentials.TransportCredentials = insecure.NewCredentials()
	if info.RPCTLS {
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: info.Host})
	}
	// Disable configured application retry policies. gRPC's transparent retries
	// are limited to attempts known not to have reached application processing.
	opts := []grpc.DialOption{grpc.WithTransportCredentials(transport), grpc.WithDisableRetry(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxReceiveBytes))}
	// Preserve Frigate audit attribution across the transport change. This is not authentication.
	opts = append(opts, grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
		return invoke(metadata.AppendToOutgoingContext(ctx, "x-frigate-actor", "proto-fleet"), method, req, reply, cc, options...)
	}))
	conn, err := grpc.NewClient("dns:///"+net.JoinHostPort(info.Host, strconv.Itoa(int(port))), opts...)
	if err != nil {
		return nil, fmt.Errorf("create CT1 connection: %w", err)
	}
	return &Client{firmware: mdk.NewFirmwareServiceClient(conn), endpoint: info, conn: conn, system: mdk.NewSystemServiceClient(conn), boards: mdk.NewHashboardServiceClient(conn), mining: mdk.NewMiningServiceClient(conn), telemetry: mdk.NewTelemetryServiceClient(conn), power: mdk.NewPowerServiceClient(conn)}, nil
}
func (c *Client) Close() error {
	c.closeOnce.Do(func() { c.closeErr = c.conn.Close() })
	return c.closeErr
}

func (c *Client) Discover(ctx context.Context, info sdk.DeviceInfo) (sdk.DeviceInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result, err := c.system.GetSystemInfo(ctx, &mdk.GetSystemInfoRequest{})
	if err != nil {
		return sdk.DeviceInfo{}, err
	}
	v := result.GetInfo()
	if v == nil || strings.TrimSpace(v.Id) == "" || (!strings.EqualFold(v.Product, "Frigate") && !strings.EqualFold(v.Product, "CT1") && !strings.EqualFold(v.Model, "CT1")) {
		return sdk.DeviceInfo{}, fmt.Errorf("endpoint is not a CT1 container")
	}
	if info.SerialNumber != "" && info.SerialNumber != v.Id {
		return sdk.DeviceInfo{}, fmt.Errorf("CT1 controller identity mismatch")
	}
	report, key, err := c.readHA(ctx, v.Id, "")
	if err != nil {
		return sdk.DeviceInfo{}, err
	}
	if key != "" {
		if strings.TrimSpace(report.PeerServerId) == "" || report.PeerServerId == v.Id {
			return sdk.DeviceInfo{}, fmt.Errorf("invalid CT1 HA membership")
		}
		info.Endpoints = []sdk.DeviceEndpoint{{ID: v.Id, DeviceKey: key, MemberIDs: []string{v.Id, report.PeerServerId}, Host: c.endpoint.Host, Port: c.endpoint.Port, URLScheme: c.endpoint.URLScheme, RPCPort: c.endpoint.RPCPort, RPCTLS: c.endpoint.RPCTLS}}
		if _, err := sdk.DeviceEndpointKey(info.Endpoints); err != nil {
			return sdk.DeviceInfo{}, err
		}
	}
	info.DeviceClass = sdk.DeviceClassContainer
	if info.SerialNumber == "" {
		info.SerialNumber = v.Id
	}
	info.Model = "CT1"
	info.Manufacturer = "Proto"
	info.FirmwareVersion = v.Version
	info.RPCPort = c.endpoint.RPCPort
	return info, nil
}

func (c *Client) Collect(ctx context.Context, id string) (sdk.DeviceMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	c.pollMu.Lock()
	defer c.pollMu.Unlock()
	var wire *mdk.GetContainerMapResponse
	var mining *mdk.GetMiningStatusResponse
	var plantWire *mdk.GetContainerHealthResponse
	var controlsWire *mdk.GetContainerControlsResponse
	var err, miningErr, plantErr, controlsErr error
	var reads sync.WaitGroup
	reads.Add(4)
	go func() { defer reads.Done(); wire, err = c.boards.GetContainerMap(ctx, &mdk.GetContainerMapRequest{}) }()
	go func() {
		defer reads.Done()
		sub, done := supplementContext(ctx)
		defer done()
		mining, miningErr = c.mining.GetMiningStatus(sub, &mdk.GetMiningStatusRequest{})
	}()
	go func() {
		defer reads.Done()
		sub, done := supplementContext(ctx)
		defer done()
		plantWire, plantErr = c.telemetry.GetContainerHealth(sub, &mdk.GetContainerHealthRequest{})
	}()
	go func() {
		defer reads.Done()
		sub, done := supplementContext(ctx)
		defer done()
		controlsWire, controlsErr = c.power.GetContainerControls(sub, &mdk.GetContainerControlsRequest{})
	}()
	reads.Wait()
	if err != nil {
		return sdk.DeviceMetrics{}, err
	}
	observed := time.Now().UTC()
	topology, err := snapshot(mapResponse(wire), observed)
	if err != nil {
		return sdk.DeviceMetrics{}, err
	}
	topology.HashboardFirmwareVersion, topology.HashboardFirmwareMixed = observedHashboardFirmware(wire, topology, observed)
	result := sdk.DeviceMetrics{DeviceID: id, Timestamp: observed, Container: topology, Health: sdk.HealthWarning}
	if err := miningErr; err != nil {
		slog.WarnContext(ctx, "CT1 mining totals unavailable", "device_id", id, "error", err)
	} else {
		result.Health = sdk.HealthHealthyInactive
		if mining.Mining {
			result.Health = sdk.HealthHealthyActive
		}
	}
	if !topology.TopologyComplete {
		result.Health = sdk.HealthWarning
	}
	for _, tank := range topology.Tanks {
		for _, m := range tank.Modules {
			if m.State != sdk.MinerStatusActive && m.State != sdk.MinerStatusInactive && m.State != sdk.MinerStatusMaintenance {
				result.Health = sdk.HealthWarning
			}
		}
	}
	plant := plantObservation(plantWire)
	if err := plantErr; err != nil {
		plant = plantResponse{}
		slog.WarnContext(ctx, "CT1 plant observation unavailable", "device_id", id, "error", err)
	} else if topology.Plant = plant.observation(time.Now().UTC()); topology.Plant != nil {
		p := topology.Plant
		if p.AlarmActive || p.CriticalAlarmActive {
			result.Health = sdk.HealthWarning
		}
	}
	// A successful stopped status must not become Active via the server's
	// Warning mapping. Detailed module, plant and coverage warnings remain available.
	if miningErr == nil && !mining.Mining {
		result.Health = sdk.HealthHealthyInactive
	}
	// Fans have their own source observation time and do not depend on plant
	// aggregate availability. A failed read leaves them absent.
	topology.Fans = plant.fanObservations(time.Now().UTC())
	if err := controlsErr; err != nil {
		slog.WarnContext(ctx, "CT1 equipment controls unavailable", "device_id", id, "error", err)
	} else {
		topology.Controls = controlObservation(controlsWire).observation(time.Now().UTC())
	}
	collectRollups(topology, plantWire, plantErr == nil)
	sdk.CalculateContainerRollups(topology)
	result.HashrateHS = metric(topology.Metrics.HashrateHS)
	result.PowerW = metric(topology.Metrics.PowerW)
	result.TempC = metric(topology.Metrics.TemperatureC)
	result.EfficiencyJH = metric(topology.Metrics.EfficiencyJH)
	if err := ctx.Err(); err != nil {
		return sdk.DeviceMetrics{}, fmt.Errorf("collect CT1: %w", err)
	}
	return result, nil
}

// Reserve time for the accepted core result to cross the plugin RPC.
func supplementContext(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := time.Second
	if deadline, ok := ctx.Deadline(); ok {
		budget = min(budget, time.Until(deadline)-500*time.Millisecond)
	}
	return context.WithTimeout(ctx, max(budget, 0))
}

func metric(value *float64) *sdk.MetricValue {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return &sdk.MetricValue{Value: *value, Kind: sdk.MetricKindGauge}
}
