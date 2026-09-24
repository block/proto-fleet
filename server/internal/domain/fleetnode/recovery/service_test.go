package recovery

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

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

func (s *fakeStore) GetOfflineFleetNodeDevices(context.Context) ([]stores.FleetNodeRecoveryTarget, error) {
	return append([]stores.FleetNodeRecoveryTarget(nil), s.targets...), s.listErr
}

func (s *fakeStore) ApplyFleetNodeRecoveredEndpoint(_ context.Context, target stores.FleetNodeRecoveryTarget, ip, port, scheme string) (bool, error) {
	s.found = append(s.found, target.DeviceIdentifier+"|"+ip+"|"+port+"|"+scheme)
	return s.foundApplied, nil
}

func (s *fakeStore) ApplyFleetNodeRecoveryAuthenticationNeeded(_ context.Context, target stores.FleetNodeRecoveryTarget, ip, port, scheme string) (bool, error) {
	s.auth = append(s.auth, target.DeviceIdentifier+"|"+ip+"|"+port+"|"+scheme)
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
	labels []metrics.CommandLabels
}

func (m *recordingMetrics) EmitCommand(_ context.Context, labels metrics.CommandLabels) {
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

func TestRunCycleGroupsByOwnerCapsAndAdvancesBatches(t *testing.T) {
	store := &fakeStore{}
	for i := range 1024 {
		target := testTarget(7, "node-7-"+strconv.Itoa(i), "serial")
		store.targets = append(store.targets, target)
	}
	node9 := testTarget(9, "node-9", "serial-9")
	store.targets = append(store.targets, node9)
	type batch struct {
		nodeID int64
		count  int
	}
	var batches []batch
	var node7FirstTargets []string
	var mu sync.Mutex
	sender := sendFunc(func(_ context.Context, nodeID int64, version gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, gatewaypb.CommandProtocolVersion_COMMAND_PROTOCOL_VERSION_V1, version)
		envelope := &gatewaypb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), envelope))
		targets := envelope.GetRecoverMinerEndpoints().GetTargets()
		batches = append(batches, batch{nodeID, len(targets)})
		if nodeID == 7 {
			node7FirstTargets = append(node7FirstTargets, targets[0].GetDeviceIdentifier())
		}
		results := make([]*gatewaypb.MinerEndpointRecoveryResult, len(targets))
		for i, target := range targets {
			results[i] = &gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: target.GetDeviceIdentifier(), Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_NOT_FOUND}
		}
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK, results...), nil
	})
	service := NewService(store, sender, nil, nil, testLogger())

	service.RunCycle(t.Context())
	service.RunCycle(t.Context())

	assert.ElementsMatch(t, []batch{{7, 512}, {9, 1}, {7, 512}, {9, 1}}, batches)
	assert.Equal(t, []string{"node-7-0", "node-7-512"}, node7FirstTargets)
	assert.Equal(t, "node-7-0", service.nextTarget[7])
}

func TestRunCycleRotatesAfterIncompleteAttempts(t *testing.T) {
	for _, outcome := range []string{"partial prefix", "empty partial", "incomplete OK", "timeout"} {
		t.Run(outcome, func(t *testing.T) {
			store := &fakeStore{targets: []stores.FleetNodeRecoveryTarget{
				testTarget(7, "a", "serial-a"),
				testTarget(7, "b", "serial-b"),
				testTarget(7, "c", "serial-c"),
			}}
			var firstTargets []string
			sender := sendFunc(func(_ context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				envelope := &gatewaypb.AgentCommand{}
				require.NoError(t, proto.Unmarshal(cmd.GetPayload(), envelope))
				first := envelope.GetRecoverMinerEndpoints().GetTargets()[0].GetDeviceIdentifier()
				firstTargets = append(firstTargets, first)
				switch outcome {
				case "partial prefix":
					return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_PARTIAL,
						&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: first, Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_NOT_FOUND}), nil
				case "empty partial":
					return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_PARTIAL), nil
				case "incomplete OK":
					return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK), nil
				default:
					return nil, context.DeadlineExceeded
				}
			})
			service := NewService(store, sender, nil, nil, testLogger())
			for range 4 {
				service.RunCycle(t.Context())
			}
			assert.Equal(t, []string{"a", "b", "c", "a"}, firstTargets)
		})
	}
}

func TestRunCycleBoundsConcurrentNodesAndCancels(t *testing.T) {
	store := &fakeStore{}
	for i := range maxConcurrentNodes + 1 {
		store.targets = append(store.targets, testTarget(int64(i+1), strconv.Itoa(i), "serial"))
	}
	started := make(chan int64, len(store.targets))
	sender := sendFunc(func(ctx context.Context, nodeID int64, _ gatewaypb.CommandProtocolVersion, _ *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		started <- nodeID
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		NewService(store, sender, nil, nil, testLogger()).RunCycle(ctx)
	}()
	for range maxConcurrentNodes {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("stalled node blocked another node's recovery")
		}
	}
	select {
	case <-started:
		t.Fatal("exceeded concurrent node limit")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cycle did not stop after cancellation")
	}
}

func TestCompleteBatchResumesAtPortLimitOmission(t *testing.T) {
	store := &fakeStore{}
	for i := range 12 {
		target := testTarget(7, strconv.Itoa(i), "serial")
		target.LastKnownPort = strconv.Itoa(8000 + i%11)
		store.targets = append(store.targets, target)
	}
	sender := sendFunc(func(_ context.Context, _ int64, _ gatewaypb.CommandProtocolVersion, cmd *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		envelope := &gatewaypb.AgentCommand{}
		require.NoError(t, proto.Unmarshal(cmd.GetPayload(), envelope))
		var results []*gatewaypb.MinerEndpointRecoveryResult
		for _, target := range envelope.GetRecoverMinerEndpoints().GetTargets() {
			results = append(results, &gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: target.GetDeviceIdentifier(), Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_NOT_FOUND})
		}
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK, results...), nil
	})
	service := NewService(store, sender, nil, nil, testLogger())
	service.RunCycle(t.Context())
	assert.Equal(t, "10", service.nextTarget[7])
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

func TestSelectTargetsCapsDistinctScanPorts(t *testing.T) {
	targets := make([]stores.FleetNodeRecoveryTarget, 11)
	for i := range targets {
		targets[i] = testTarget(7, "miner-"+strconv.Itoa(i), "serial")
		targets[i].LastKnownPort = strconv.Itoa(8000 + i)
	}

	selected, payload := selectTargets(targets)
	envelope := &gatewaypb.AgentCommand{}
	require.NoError(t, proto.Unmarshal(payload, envelope))

	assert.Len(t, selected, 10)
	assert.Len(t, envelope.GetRecoverMinerEndpoints().GetScanPorts(), 10)
	assert.Equal(t, "8000", envelope.GetRecoverMinerEndpoints().GetScanPorts()[0])
	assert.Equal(t, "8009", envelope.GetRecoverMinerEndpoints().GetScanPorts()[9])
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
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "auth", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED, IpAddress: "10.0.0.21", Port: "80", UrlScheme: "http", SerialNumber: "serial-auth"},
		), nil
	})

	NewService(store, sender, invalidator, emitter, testLogger()).RunCycle(t.Context())

	assert.Equal(t, []string{"found|10.0.0.20|80|http"}, store.found)
	assert.Equal(t, []string{"auth|10.0.0.21|80|http"}, store.auth)
	assert.Equal(t, []minermodels.DeviceIdentifier{"found", "auth"}, invalidator.identifiers)
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

func TestRunCycleRejectsMismatchedIdentityAndInvalidEndpoints(t *testing.T) {
	store := &fakeStore{targets: []stores.FleetNodeRecoveryTarget{
		testTarget(7, "mismatch", "serial-1"),
		testTarget(7, "public", "serial-2"),
		testTarget(7, "zero-port", "serial-3"),
	}}
	sender := sendFunc(func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
		return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK,
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "mismatch", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED, SerialNumber: "other"},
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "public", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, IpAddress: "8.8.8.8", Port: "80", UrlScheme: "http", SerialNumber: "serial-2"},
			&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "zero-port", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, IpAddress: "10.0.0.20", Port: "0", UrlScheme: "http", SerialNumber: "serial-3"},
		), nil
	})

	NewService(store, sender, nil, nil, testLogger()).RunCycle(t.Context())

	assert.Empty(t, store.found)
	assert.Empty(t, store.auth)
}

func TestRunCycleRequiresValidEndpoint(t *testing.T) {
	for _, tc := range []struct {
		scheme string
		port   string
		valid  bool
	}{
		{scheme: "http", port: "80", valid: true},
		{scheme: "https", port: "80", valid: true},
		{scheme: "tcp", port: "80", valid: true},
		{scheme: "virtual", port: "80", valid: true},
		{scheme: "", port: "80"},
		{scheme: "ssh", port: "80"},
		{scheme: "http", port: "1", valid: true},
		{scheme: "http", port: "65535", valid: true},
		{scheme: "http", port: "+80"},
		{scheme: "http", port: "080"},
		{scheme: "http", port: "0"},
		{scheme: "http", port: "65536"},
		{scheme: "http", port: "-1"},
		{scheme: "http", port: ""},
	} {
		t.Run(tc.scheme+"/"+tc.port, func(t *testing.T) {
			store := &fakeStore{
				targets: []stores.FleetNodeRecoveryTarget{
					testTarget(7, "found", "serial-found"),
					testTarget(7, "auth", "serial-auth"),
				},
				foundApplied: true,
				authApplied:  true,
			}
			invalidator := &recordingInvalidator{}
			sender := sendFunc(func(context.Context, int64, gatewaypb.CommandProtocolVersion, *gatewaypb.ControlCommand) (*gatewaypb.ControlAck, error) {
				return ackWithResults(t, gatewaypb.AckCode_ACK_CODE_OK,
					&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "found", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, IpAddress: "10.0.0.20", Port: tc.port, UrlScheme: tc.scheme, SerialNumber: "serial-found"},
					&gatewaypb.MinerEndpointRecoveryResult{DeviceIdentifier: "auth", Outcome: gatewaypb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED, IpAddress: "10.0.0.21", Port: tc.port, UrlScheme: tc.scheme, SerialNumber: "serial-auth"},
				), nil
			})

			NewService(store, sender, invalidator, nil, testLogger()).RunCycle(t.Context())

			if tc.valid {
				assert.Equal(t, []string{"found|10.0.0.20|" + tc.port + "|" + tc.scheme}, store.found)
				assert.Equal(t, []string{"auth|10.0.0.21|" + tc.port + "|" + tc.scheme}, store.auth)
				assert.Equal(t, []minermodels.DeviceIdentifier{"found", "auth"}, invalidator.identifiers)
			} else {
				assert.Empty(t, store.found)
				assert.Empty(t, store.auth)
				assert.Empty(t, invalidator.identifiers)
			}
		})
	}
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
