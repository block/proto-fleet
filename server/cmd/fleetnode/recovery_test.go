package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/netscan"
	"github.com/block/proto-fleet/server/internal/domain/stableidentity"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/block/proto-fleet/server/sdk/v1/mocks"
)

type recoveryTestDiscoverer struct {
	ports         []string
	identities    map[string]stableidentity.Identity
	schemes       map[string]string
	drivers       map[string]string
	probeRecovery func(context.Context, string, string) (stableidentity.Identity, string, string, error)
}

func (d *recoveryTestDiscoverer) Probe(_ context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
	return &pb.DiscoveredDeviceReport{DeviceIdentifier: "candidate", IpAddress: ip, Port: port, DriverName: "antminer", UrlScheme: d.schemes[ip+"|"+port]}, nil
}

func (d *recoveryTestDiscoverer) ProbeRecovery(ctx context.Context, ip, port string) (stableidentity.Identity, string, string, error) {
	if d.probeRecovery != nil {
		return d.probeRecovery(ctx, ip, port)
	}
	key := ip + "|" + port
	return d.identities[key], d.schemes[key], d.drivers[key], nil
}

func (d *recoveryTestDiscoverer) DefaultDiscoveryPorts(context.Context) []string {
	return d.ports
}

type recoveryTestSecrets struct {
	bundles map[string]sdk.SecretBundle
	errors  map[string]error
}

func (s recoveryTestSecrets) SecretBundle(target *pb.MinerConnectionDescriptor) (sdk.SecretBundle, error) {
	if err := s.errors[target.GetDeviceIdentifier()]; err != nil {
		return sdk.SecretBundle{}, err
	}
	return s.bundles[target.GetDeviceIdentifier()], nil
}

func (recoveryTestSecrets) Seal(sdk.SecretBundle) (*pb.EncryptedCredentials, error) { return nil, nil }

type recoveryTestDriverGetter struct{ driver sdk.Driver }

func (g recoveryTestDriverGetter) GetDriverByDriverName(string) (sdk.Driver, error) {
	return g.driver, nil
}

type recoveryCleanerFunc func(context.Context, string) error

func (f recoveryCleanerFunc) CloseDevice(ctx context.Context, deviceID string) error {
	return f(ctx, deviceID)
}

func TestCloseUncertainRecoveryDeviceRetriesEarlyNotFound(t *testing.T) {
	var calls atomic.Int32
	closeUncertainRecoveryDevice(t.Context(), recoveryCleanerFunc(func(context.Context, string) error {
		if calls.Add(1) == 1 {
			return errors.New("device not found yet")
		}
		return nil
	}), "recovery-device")

	assert.EqualValues(t, 2, calls.Load())
}

func recoveryTarget(id, serial, mac string) *pb.MinerConnectionDescriptor {
	return &pb.MinerConnectionDescriptor{
		DeviceIdentifier: id,
		DriverName:       "antminer",
		IpAddress:        "10.0.0.99",
		Port:             "80",
		UrlScheme:        "http",
		SerialNumber:     serial,
		MacAddress:       mac,
	}
}

func recoveryRunCmd(t *testing.T, identities map[string]stableidentity.Identity, scanErr error, inspect func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error)) (*RunCmd, *atomic.Int32) {
	t.Helper()
	ctrl := gomock.NewController(t)
	driver := mocks.NewMockDriver(ctrl)
	calls := &atomic.Int32{}
	driver.EXPECT().NewDevice(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ context.Context, deviceID string, endpoint sdk.DeviceInfo, bundle sdk.SecretBundle) (sdk.NewDeviceResult, error) {
			assert.True(t, strings.HasPrefix(deviceID, "endpoint-recovery-"))
			calls.Add(1)
			info, err := inspect(endpoint, bundle)
			if err != nil {
				return sdk.NewDeviceResult{}, err
			}
			device := mocks.NewMockDevice(ctrl)
			device.EXPECT().DescribeDevice(gomock.Any()).Return(info, sdk.Capabilities{}, nil)
			device.EXPECT().Close(gomock.Any()).Return(nil)
			return sdk.NewDeviceResult{Device: device}, nil
		})
	discoverer := &recoveryTestDiscoverer{
		ports:      []string{"80"},
		identities: identities,
		schemes:    map[string]string{"10.0.0.1|80": "http", "10.0.0.2|80": "http"},
		drivers:    map[string]string{"10.0.0.1|80": "antminer", "10.0.0.2|80": "antminer"},
	}
	r := &RunCmd{
		discoverer:   discoverer,
		driverGetter: recoveryTestDriverGetter{driver: driver},
		minerSecrets: recoveryTestSecrets{bundles: map[string]sdk.SecretBundle{
			"miner-1": {Kind: sdk.UsernamePassword{Username: "root", Password: "secret"}},
			"miner-2": {Kind: sdk.UsernamePassword{Username: "root", Password: "secret"}},
		}},
		localSubnets: func() ([]string, error) { return []string{"10.0.0.0/30"}, nil },
		scanner: scanFunc(func(_ context.Context, _ iter.Seq[netip.Addr], _ []uint16, emit func(netscan.HostResult) error) error {
			for _, ip := range []string{"10.0.0.1", "10.0.0.2"} {
				if err := emit(netscan.HostResult{Addr: netip.MustParseAddr(ip), OpenPorts: []uint16{80}}); err != nil {
					return err
				}
			}
			return scanErr
		}),
	}
	return r, calls
}

func TestRecoverMinerEndpointsMatchesStableIdentityAndDeduplicatesCredentialProbes(t *testing.T) {
	r, calls := recoveryRunCmd(t, nil, nil, func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		if endpoint.Host == "10.0.0.1" {
			return sdk.DeviceInfo{SerialNumber: "SERIAL-1", MacAddress: "AA-BB-CC-DD-EE-01"}, nil
		}
		return sdk.DeviceInfo{SerialNumber: "SERIAL-2", MacAddress: "AA-BB-CC-DD-EE-02"}, nil
	})
	targets := []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
		recoveryTarget("miner-2", "", "aa:bb:cc:dd:ee:02"),
	}

	results, partial, err := r.recoverMinerEndpoints(t.Context(), targets, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.False(t, partial)
	require.Len(t, results, 2)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, results[0].GetOutcome())
	assert.Equal(t, "10.0.0.1", results[0].GetIpAddress())
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, results[1].GetOutcome())
	assert.Equal(t, "10.0.0.2", results[1].GetIpAddress())
	assert.EqualValues(t, 2, calls.Load(), "the shared credential should be tried once per endpoint")
}

func TestRecoverMinerEndpointsInspectsCandidatesConcurrentlyBeforeDeciding(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	r, _ := recoveryRunCmd(t, nil, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		started <- struct{}{}
		select {
		case <-release:
			return sdk.DeviceInfo{SerialNumber: "DUPLICATE"}, nil
		case <-ctx.Done():
			return sdk.DeviceInfo{}, ctx.Err()
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		results, partial, err := r.recoverMinerEndpoints(ctx, []*pb.MinerConnectionDescriptor{
			recoveryTarget("miner-1", "DUPLICATE", ""),
		}, []string{"80"}, discardLogger(t))
		assert.NoError(t, err)
		assert.False(t, partial)
		if assert.Len(t, results, 1) {
			assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AMBIGUOUS, results[0].GetOutcome())
		}
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Error("both candidate inspections must start before either finishes")
		}
	}
	close(release)
	<-done
}

func TestRecoverMinerEndpointsInspectsNonOverlappingPartialIdentity(t *testing.T) {
	r, calls := recoveryRunCmd(t, map[string]stableidentity.Identity{
		"10.0.0.1|80": stableidentity.New("DISCOVERED-SERIAL", ""),
	}, nil, func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		if endpoint.Host == "10.0.0.1" {
			return sdk.DeviceInfo{SerialNumber: "DISCOVERED-SERIAL", MacAddress: "AA:BB:CC:DD:EE:01"}, nil
		}
		return sdk.DeviceInfo{SerialNumber: "OTHER", MacAddress: "AA:BB:CC:DD:EE:02"}, nil
	})

	results, partial, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "", "aa:bb:cc:dd:ee:01"),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.False(t, partial)
	require.Len(t, results, 1)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, results[0].GetOutcome())
	assert.Equal(t, "10.0.0.1", results[0].GetIpAddress())
	assert.EqualValues(t, 2, calls.Load())
}

func TestRecoverMinerEndpointsTriesDistinctCredentialsSeparately(t *testing.T) {
	r, calls := recoveryRunCmd(t, nil, nil, func(_ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{}, nil
	})
	secrets, ok := r.minerSecrets.(recoveryTestSecrets)
	require.True(t, ok)
	secrets.bundles["miner-2"] = sdk.SecretBundle{Kind: sdk.UsernamePassword{Username: "root", Password: "different"}}
	r.minerSecrets = secrets

	_, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
		recoveryTarget("miner-2", "SERIAL-2", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.EqualValues(t, 4, calls.Load(), "distinct credentials must each be tested against every unidentified endpoint")
}

func TestRecoverMinerEndpointsDoesNotReuseInspectionAcrossSchemes(t *testing.T) {
	r, calls := recoveryRunCmd(t, nil, nil, func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{SerialNumber: endpoint.URLScheme}, nil
	})
	discoverer, ok := r.discoverer.(*recoveryTestDiscoverer)
	require.True(t, ok)
	discoverer.schemes = nil
	httpTarget := recoveryTarget("miner-1", "http", "")
	httpsTarget := recoveryTarget("miner-2", "https", "")
	httpsTarget.UrlScheme = "https"

	_, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{httpTarget, httpsTarget}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.EqualValues(t, 4, calls.Load(), "the effective URL scheme is part of an endpoint inspection")
}

func TestRecoverMinerEndpointsOnlyReportsAuthenticationFailureAfterCredentialFreeMatch(t *testing.T) {
	authErr := sdk.SDKError{Code: sdk.ErrCodeAuthenticationFailed, Message: "rejected"}
	r, calls := recoveryRunCmd(t, map[string]stableidentity.Identity{
		"10.0.0.1|80": stableidentity.New("SERIAL-1", ""),
	}, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{}, authErr
	})

	results, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
		recoveryTarget("miner-2", "SERIAL-2", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AUTHENTICATION_FAILED, results[0].GetOutcome())
	assert.Equal(t, "10.0.0.1", results[0].GetIpAddress())
	assert.Equal(t, "80", results[0].GetPort())
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_NOT_FOUND, results[1].GetOutcome())
	assert.EqualValues(t, 2, calls.Load())
}

func TestRecoverMinerEndpointsDoesNotDecideAfterFailedUnidentifiedInspection(t *testing.T) {
	r, _ := recoveryRunCmd(t, nil, nil, func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		if endpoint.Host == "10.0.0.1" {
			return sdk.DeviceInfo{}, errors.New("temporary read failure")
		}
		return sdk.DeviceInfo{SerialNumber: "SERIAL-1"}, nil
	})

	results, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR, results[0].GetOutcome())
}

func TestRecoverMinerEndpointsDoesNotSendCredentialsToUnrecognizedEndpoints(t *testing.T) {
	r, calls := recoveryRunCmd(t, nil, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		t.Fatal("unrecognized endpoints must not reach credential-backed inspection")
		return sdk.DeviceInfo{}, nil
	})
	discoverer, ok := r.discoverer.(*recoveryTestDiscoverer)
	require.True(t, ok)
	discoverer.probeRecovery = func(_ context.Context, ip, _ string) (stableidentity.Identity, string, string, error) {
		if ip == "10.0.0.1" {
			return stableidentity.Identity{}, "", "", errors.New("not a supported miner")
		}
		return stableidentity.Identity{}, "http", "proto", nil
	}

	results, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_NOT_FOUND, results[0].GetOutcome())
	assert.Zero(t, calls.Load())
}

func TestRecoverMinerEndpointsTreatsDistinctIdentityBoundEndpointsAsAmbiguous(t *testing.T) {
	authErr := sdk.SDKError{Code: sdk.ErrCodeAuthenticationFailed, Message: "rejected"}
	r, _ := recoveryRunCmd(t, map[string]stableidentity.Identity{
		"10.0.0.1|80": stableidentity.New("SERIAL-1", ""),
	}, nil, func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		if endpoint.Host == "10.0.0.1" {
			return sdk.DeviceInfo{}, authErr
		}
		return sdk.DeviceInfo{SerialNumber: "SERIAL-1"}, nil
	})

	results, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AMBIGUOUS, results[0].GetOutcome())
}

func TestRecoverMinerEndpointsRejectsUnstableAndAmbiguousMatches(t *testing.T) {
	r, _ := recoveryRunCmd(t, nil, nil, func(_ sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{SerialNumber: "DUPLICATE"}, nil
	})

	results, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "", ""),
		recoveryTarget("miner-2", "DUPLICATE", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_UNRECOVERABLE_IDENTITY, results[0].GetOutcome())
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_AMBIGUOUS, results[1].GetOutcome())
}

func TestRecoverMinerEndpointsOmitsResultsAfterPartialScan(t *testing.T) {
	r, calls := recoveryRunCmd(t, nil, errors.New("scanner interrupted"), func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{SerialNumber: "SERIAL-1", Host: endpoint.Host}, nil
	})

	results, partial, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
	}, []string{"80"}, discardLogger(t))

	require.ErrorContains(t, err, "scanner interrupted")
	assert.True(t, partial)
	assert.Empty(t, results)
	assert.Zero(t, calls.Load())
}

func TestRecoverMinerEndpointsRedactsMalformedCiphertext(t *testing.T) {
	r, _ := recoveryRunCmd(t, nil, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		t.Fatal("malformed credentials must not reach a driver")
		return sdk.DeviceInfo{}, nil
	})
	r.minerSecrets = recoveryTestSecrets{errors: map[string]error{"miner-1": fmt.Errorf("ciphertext contained secret-value")}}

	results, _, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR, results[0].GetOutcome())
	assert.Equal(t, "invalid encrypted credentials", results[0].GetErrorMessage())
	assert.NotContains(t, results[0].GetErrorMessage(), "secret-value")
}

func TestHandleRecoverMinerEndpointsReturnsEmptyPartialOnTimeout(t *testing.T) {
	r, _ := recoveryRunCmd(t, nil, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{}, nil
	})
	r.scanner = scanFunc(func(ctx context.Context, _ iter.Seq[netip.Addr], _ []uint16, _ func(netscan.HostResult) error) error {
		<-ctx.Done()
		return ctx.Err()
	})
	previousTimeout := commandTimeout
	commandTimeout = time.Millisecond
	t.Cleanup(func() { commandTimeout = previousTimeout })
	ack := &capturingAcker{}
	r.handleRecoverMinerEndpoints(t.Context(), ack, "recover-timeout", &pb.RecoverMinerEndpointsRequest{
		Targets:   []*pb.MinerConnectionDescriptor{recoveryTarget("miner-1", "SERIAL-1", "")},
		ScanPorts: []string{"80"},
	}, discardLogger(t))

	require.Len(t, ack.sent, 1)
	got := ack.sent[0].GetAck()
	assert.Equal(t, pb.AckCode_ACK_CODE_PARTIAL, got.GetCode())
	result := &pb.RecoverMinerEndpointsResult{}
	require.NoError(t, proto.Unmarshal(got.GetPayload(), result))
	assert.Empty(t, result.GetResults())
}

func TestRecoverMinerEndpointsStopsInspectingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var inspections atomic.Int32
	r, calls := recoveryRunCmd(t, map[string]stableidentity.Identity{
		"10.0.0.1|80": stableidentity.New("SERIAL-1", ""),
		"10.0.0.2|80": stableidentity.New("SERIAL-2", ""),
	}, nil, func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		if inspections.Add(1) == 2 {
			cancel()
		}
		return sdk.DeviceInfo{SerialNumber: strings.Replace(endpoint.Host, "10.0.0.", "SERIAL-", 1)}, nil
	})

	results, partial, err := r.recoverMinerEndpoints(ctx, []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "SERIAL-1", ""),
		recoveryTarget("miner-2", "SERIAL-2", ""),
	}, []string{"80"}, discardLogger(t))

	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, partial)
	require.Len(t, results, 1)
	assert.Equal(t, "miner-1", results[0].GetDeviceIdentifier())
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, results[0].GetOutcome())
	assert.EqualValues(t, 2, calls.Load())
}

func TestRecoverMinerEndpointsKeepsCompletedPrefixWhenInspectionIsStuck(t *testing.T) {
	previousTimeout := perProbeTimeout
	perProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { perProbeTimeout = previousTimeout })
	release, finished := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		close(release)
		<-finished
	})
	r, _ := recoveryRunCmd(t, nil, nil, func(endpoint sdk.DeviceInfo, _ sdk.SecretBundle) (sdk.DeviceInfo, error) {
		if endpoint.Host == "10.0.0.2" {
			defer close(finished)
			<-release
			return sdk.DeviceInfo{}, errors.New("inspection did not complete")
		}
		return sdk.DeviceInfo{SerialNumber: "SERIAL-2"}, nil
	})

	results, partial, err := r.recoverMinerEndpoints(t.Context(), []*pb.MinerConnectionDescriptor{
		recoveryTarget("miner-1", "", ""),
		recoveryTarget("miner-2", "SERIAL-2", ""),
	}, []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.True(t, partial)
	require.Len(t, results, 1, "an unfinished candidate could make the apparent match ambiguous")
	assert.Equal(t, "miner-1", results[0].GetDeviceIdentifier())
}

func TestScanRecoveryEndpointsPrioritizesBoundedRequestPorts(t *testing.T) {
	r, _ := recoveryRunCmd(t, nil, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{}, nil
	})
	discoverer, ok := r.discoverer.(*recoveryTestDiscoverer)
	require.True(t, ok)
	discoverer.ports = []string{"80", "81", "82"}
	r.scanner = scanFunc(func(_ context.Context, _ iter.Seq[netip.Addr], ports []uint16, _ func(netscan.HostResult) error) error {
		require.Equal(t, []uint16{80, 1000, 1001, 1002, 1003, 1004, 1005, 1006, 1007, 1008}, ports)
		return nil
	})

	_, _, err := r.scanRecoveryEndpoints(t.Context(), []string{"1000", "1001", "1002", "1003", "1004", "1005", "1006", "1007", "1008"}, discardLogger(t))

	require.NoError(t, err)
}

func TestScanRecoveryEndpointsSupervisorReturnsPartialOnStuckProbe(t *testing.T) {
	previousTimeout := perProbeTimeout
	perProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { perProbeTimeout = previousTimeout })

	r, _ := recoveryRunCmd(t, nil, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{}, nil
	})
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	discoverer, ok := r.discoverer.(*recoveryTestDiscoverer)
	require.True(t, ok)
	discoverer.identities = map[string]stableidentity.Identity{
		"10.0.0.1|80": stableidentity.New("SERIAL-1", ""),
	}
	discoverer.probeRecovery = func(_ context.Context, ip, port string) (stableidentity.Identity, string, string, error) {
		if ip == "10.0.0.2" {
			<-stuck
		}
		key := ip + "|" + port
		return discoverer.identities[key], discoverer.schemes[key], discoverer.drivers[key], nil
	}

	start := time.Now()
	endpoints, partial, err := r.scanRecoveryEndpoints(t.Context(), []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.True(t, partial)
	assert.LessOrEqual(t, time.Since(start), perProbeTimeout*2+time.Second)
	require.Len(t, endpoints, 1)
	assert.Equal(t, "SERIAL-1", endpoints[0].identity.SerialNumber)
}

func TestScanRecoveryEndpointsReturnsPartialWhenProbeDeadlineIsObserved(t *testing.T) {
	previousTimeout := perProbeTimeout
	perProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { perProbeTimeout = previousTimeout })

	r, _ := recoveryRunCmd(t, nil, nil, func(sdk.DeviceInfo, sdk.SecretBundle) (sdk.DeviceInfo, error) {
		return sdk.DeviceInfo{}, nil
	})
	discoverer, ok := r.discoverer.(*recoveryTestDiscoverer)
	require.True(t, ok)
	discoverer.probeRecovery = func(ctx context.Context, ip, _ string) (stableidentity.Identity, string, string, error) {
		if ip == "10.0.0.1" {
			<-ctx.Done()
			return stableidentity.Identity{}, "", "", ctx.Err()
		}
		return stableidentity.New("SERIAL-2", ""), "http", "antminer", nil
	}

	endpoints, partial, err := r.scanRecoveryEndpoints(t.Context(), []string{"80"}, discardLogger(t))

	require.NoError(t, err)
	assert.True(t, partial)
	require.Len(t, endpoints, 1)
	assert.Equal(t, "SERIAL-2", endpoints[0].identity.SerialNumber)
}

func TestRecoveryResultIsolatesInvalidPluginEvidence(t *testing.T) {
	target := recoveryTarget("miner-1", "SERIAL-1", "")
	invalid := recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, recoveryEndpoint{
		ip: "10.0.0.1", port: "80", urlScheme: "http",
		identity: stableidentity.New(strings.Repeat("x", 256), ""),
	}, "")
	valid := recoveryResult(target, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, recoveryEndpoint{
		ip: "10.0.0.2", port: "80", urlScheme: "http",
		identity: stableidentity.New("SERIAL-1", ""),
	}, "")

	require.NoError(t, protovalidate.Validate(&pb.RecoverMinerEndpointsResult{Results: []*pb.MinerEndpointRecoveryResult{invalid, valid}}))
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_ERROR, invalid.GetOutcome())
	assert.Equal(t, "plugin returned invalid recovery evidence", invalid.GetErrorMessage())
	assert.Equal(t, pb.MinerEndpointRecoveryOutcome_MINER_ENDPOINT_RECOVERY_OUTCOME_FOUND, valid.GetOutcome())
}
