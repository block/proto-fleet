package recovery

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	minermodels "github.com/block/proto-fleet/server/internal/domain/miner/models"
	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/block/proto-fleet/server/internal/infrastructure/metrics"
)

type fakeStore struct {
	targets      []stores.FleetNodeRecoveryTarget
	listErr      error
	foundApplied bool
	authApplied  bool
	found        []string
	auth         []string
}

func (s *fakeStore) GetOfflineFleetNodeDevices(context.Context, int) ([]stores.FleetNodeRecoveryTarget, error) {
	return s.targets, s.listErr
}

func (s *fakeStore) ApplyFleetNodeRecoveredEndpoint(_ context.Context, target stores.FleetNodeRecoveryTarget, ip, port, scheme string) (bool, error) {
	s.found = append(s.found, target.DeviceIdentifier+"|"+ip+"|"+port+"|"+scheme)
	return s.foundApplied, nil
}

func (s *fakeStore) ApplyFleetNodeRecoveryAuthenticationNeeded(_ context.Context, target stores.FleetNodeRecoveryTarget) (bool, error) {
	s.auth = append(s.auth, target.DeviceIdentifier)
	return s.authApplied, nil
}

type sendFunc func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error)

func (f sendFunc) SendCommand(ctx context.Context, nodeID int64, version gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
	return f(ctx, nodeID, version, cmd)
}

type recordingInvalidator struct {
	identifiers []minermodels.DeviceIdentifier
}

func (i *recordingInvalidator) InvalidateMiner(identifier minermodels.DeviceIdentifier) {
	i.identifiers = append(i.identifiers, identifier)
}

type recordingMetrics struct {
	mu     sync.Mutex
	labels []metrics.CommandLabels
}

func (m *recordingMetrics) EmitCommand(_ context.Context, labels metrics.CommandLabels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.labels = append(m.labels, labels)
}

func testTarget(nodeID int64, identifier, serial string) stores.FleetNodeRecoveryTarget {
	return stores.FleetNodeRecoveryTarget{
		FleetNodeID: nodeID, DeviceIdentifier: identifier, OrgID: 8,
		SerialNumber: serial, MacAddress: "aa:bb:cc:dd:ee:ff", DriverName: "antminer",
		LastKnownIP: "10.0.0.9", LastKnownPort: "80", LastKnownScheme: "http",
	}
}

func ackWithResults(t *testing.T, code gatewaypb.AckCode, results ...*gatewaypb.MinerEndpointRecoveryResult) *gatewaypb.ControlAck {
	t.Helper()
	payload, err := proto.Marshal(&gatewaypb.RecoverMinerEndpointsResult{Results: results})
	require.NoError(t, err)
	return &gatewaypb.ControlAck{Code: code, Succeeded: code == gatewaypb.AckCode_ACK_CODE_OK, Payload: payload}
}

func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRunCycleGroupsByOwnerAndCapsOneCommandPerNode(t *testing.T) {
	store := &fakeStore{}
	for i := range 513 {
		store.targets = append(store.targets, testTarget(7, "node-7-"+strconv.Itoa(i), "serial"))
	}
	store.targets = append(store.targets, testTarget(9, "node-9", "serial-9"))
	var nodeIDs []int64
	var targetCounts []int
	sender := sendFunc(func(_ context.Context, nodeID int64, version gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		assert.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, version)
		envelope := &gatewaypb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), envelope))
		nodeIDs = append(nodeIDs, nodeID)
		targetCounts = append(targetCounts, len(envelope.GetRecoverMinerEndpoints().GetTargets()))
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK), nil
	})

	NewService(store, sender, nil, nil, testLogger()).RunCycle(t.Context())

	assert.Equal(t, []int64{7, 9}, nodeIDs)
	assert.Equal(t, []int{512, 1}, targetCounts)
}

func TestSelectTargetsHonorsEncodedSizeLimit(t *testing.T) {
	targets := make([]stores.FleetNodeRecoveryTarget, 512)
	for i := range targets {
		targets[i] = testTarget(7, "miner-"+strconv.Itoa(i), "serial")
		targets[i].CredentialUsername = make([]byte, 4096)
		targets[i].CredentialPassword = make([]byte, 4096)
	}

	selected, payload := selectTargets(targets)

	assert.Less(t, len(selected), 512)
	assert.LessOrEqual(t, len(payload), maxEncodedRequest)
	assert.NotEmpty(t, selected)
}

func TestRunCyclePersistsValidatedResultsAndInvalidatesFoundMiner(t *testing.T) {
	store := &fakeStore{
		targets: []stores.FleetNodeRecoveryTarget{
			testTarget(7, "found", "serial-found"),
			testTarget(7, "auth", "serial-auth"),
		},
		foundApplied: true,
		authApplied:  true,
	}
	invalidator := &recordingInvalidator{}
	emitter := &recordingMetrics{}
	sender := sendFunc(func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK,
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "found", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, IpAddress: "10.0.0.20", Port: "80", UrlScheme: "http", SerialNumber: "serial-found"},
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "auth", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED, SerialNumber: "serial-auth"},
		), nil
	})

	NewService(store, sender, invalidator, emitter, testLogger()).RunCycle(t.Context())

	assert.Equal(t, []string{"found|10.0.0.20|80|http"}, store.found)
	assert.Equal(t, []string{"auth"}, store.auth)
	assert.Equal(t, []minermodels.DeviceIdentifier{"found"}, invalidator.identifiers)
	require.Len(t, emitter.labels, 2)
	assert.Equal(t, metrics.ResultSuccess, emitter.labels[0].Result)
	assert.Equal(t, metrics.ResultSuccess, emitter.labels[1].Result)
}

func TestRunCycleRejectsUntrustedAcknowledgementWithoutWrites(t *testing.T) {
	store := &fakeStore{targets: []stores.FleetNodeRecoveryTarget{testTarget(7, "miner", "serial")}}
	sender := sendFunc(func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		duplicate := &gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "miner", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, IpAddress: "10.0.0.20", Port: "80", UrlScheme: "http", SerialNumber: "serial"}
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK, duplicate, proto.CloneOf(duplicate)), nil
	})

	NewService(store, sender, nil, nil, testLogger()).RunCycle(t.Context())

	assert.Empty(t, store.found)
	assert.Empty(t, store.auth)
}

func TestRunCycleRejectsMismatchedIdentityAndPublicEndpoint(t *testing.T) {
	store := &fakeStore{targets: []stores.FleetNodeRecoveryTarget{
		testTarget(7, "mismatch", "serial-1"),
		testTarget(7, "public", "serial-2"),
	}}
	sender := sendFunc(func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK,
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "mismatch", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED, SerialNumber: "other"},
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "public", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, IpAddress: "8.8.8.8", Port: "80", UrlScheme: "http", SerialNumber: "serial-2"},
		), nil
	})

	NewService(store, sender, nil, nil, testLogger()).RunCycle(t.Context())

	assert.Empty(t, store.found)
	assert.Empty(t, store.auth)
}

func TestRunCycleLeavesStateUntouchedForVersionMismatchAndDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name string
		ack  *gatewaypb.ControlAck
		err  error
	}{
		{name: "unsupported", ack: &gatewaypb.ControlAck{Code: gatewaypb.AckCode_ACK_CODE_UNIMPLEMENTED}},
		{name: "disconnect", err: errors.New("stream closed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{targets: []stores.FleetNodeRecoveryTarget{testTarget(7, "miner", "serial")}}
			sender := sendFunc(func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				return tc.ack, tc.err
			})

			NewService(store, sender, nil, nil, testLogger()).RunCycle(t.Context())

			assert.Empty(t, store.found)
			assert.Empty(t, store.auth)
		})
	}
}

func TestRunCycleStalePersistenceDoesNotInvalidate(t *testing.T) {
	store := &fakeStore{targets: []stores.FleetNodeRecoveryTarget{testTarget(7, "miner", "serial")}, foundApplied: false}
	invalidator := &recordingInvalidator{}
	sender := sendFunc(func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_PARTIAL,
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "miner", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, IpAddress: "10.0.0.20", Port: "80", UrlScheme: "http", SerialNumber: "serial"},
		), nil
	})

	NewService(store, sender, invalidator, nil, testLogger()).RunCycle(t.Context())

	require.Len(t, store.found, 1)
	assert.Empty(t, invalidator.identifiers)
}
