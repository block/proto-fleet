package antminer

import (
	"context"
	"time"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/rpc"
	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	"github.com/block/proto-fleet/server/sdk/v1"
)

// AntminerClient defines the interface for communicating with Antminer devices
// This interface allows for easy mocking and testing
//
//go:generate go run go.uber.org/mock/mockgen -source=interface.go -destination=mocks/mock_client.go -package=mocks AntminerClient
//nolint:interfacebloat // This interface represents a complete device client with necessary operations
type AntminerClient interface {
	// RPC operations
	GetVersion(ctx context.Context) (*rpc.VersionResponse, error)
	GetSummary(ctx context.Context) (*rpc.SummaryResponse, error)
	GetDevs(ctx context.Context) (*rpc.DevsResponse, error)
	GetPools(ctx context.Context) (*rpc.PoolsResponse, error)

	// Web API operations
	GetStatsInfo(ctx context.Context) (*web.StatsInfo, error)

	// High-level operations
	GetDeviceInfo(ctx context.Context) (*DeviceInfo, error)
	GetStatus(ctx context.Context) (*Status, error)
	GetTelemetry(ctx context.Context) (*Telemetry, error)
	GetLogs(ctx context.Context, since *time.Time, maxLines int) (string, bool, error)
	Pair(ctx context.Context, credentials sdk.UsernamePassword) error
	StopMining(ctx context.Context) error
	StartMining(ctx context.Context) error

	// Configuration and management
	SetCredentials(credentials sdk.UsernamePassword) error
	SetCoolingMode(ctx context.Context, mode web.CoolingMode) error
	UpdatePools(ctx context.Context, pools []Pool) error
	GetMinerConfig(ctx context.Context) (*web.MinerConfig, error)
	SetMinerConfig(ctx context.Context, config *web.MinerConfig) error
	BlinkLED(ctx context.Context, duration time.Duration) error
	Reboot(ctx context.Context) error
	ChangePassword(ctx context.Context, currentPassword, newPassword string) error
	UploadFirmware(ctx context.Context, firmware sdk.FirmwareFile) error

	// Lifecycle
	Close()
}

// Ensure Client implements AntminerClient
var _ AntminerClient = (*Client)(nil)
