package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

type resolverFunc func(context.Context, string) ([]net.IPAddr, error)

func (f resolverFunc) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return f(ctx, host)
}

func TestControlLoopDNSFailuresRetainReports(t *testing.T) {
	for _, tc := range []struct {
		name        string
		request     *pairingpb.DiscoverRequest
		wantReports bool
		reportErr   error
	}{
		{
			name: "network resolver timeout",
			request: &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_NetworkScan{
				NetworkScan: &pairingpb.NetworkScanModeRequest{Target: "first.lan", Ports: []string{"4028"}},
			}},
		},
		{
			name:    "all list names fail",
			request: discoverIPList([]string{"first.lan", "second.lan"}, []string{"4028"}),
		},
		{
			name:        "list keeps usable addresses after DNS failure",
			request:     discoverIPList([]string{"first.lan", "10.0.0.5", "second.lan"}, []string{"4028"}),
			wantReports: true,
		},
		{
			name:        "report failure wins over DNS failure",
			request:     discoverIPList([]string{"first.lan", "10.0.0.5"}, []string{"4028"}),
			wantReports: true,
			reportErr:   errors.New("upload failed"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &RunCmd{
				discoverer: &stubDiscoverer{probes: map[string]*pb.DiscoveredDeviceReport{
					"10.0.0.5|4028": {DeviceIdentifier: "identified", DriverName: "antminer", UrlScheme: "http"},
				}},
				resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
					return nil, fmt.Errorf("resolver: %w", &net.DNSError{Name: host, Err: "i/o timeout", IsTimeout: true})
				}),
			}
			fake := &controlFakeGateway{}
			fake.setBehavior(controlFakeBehavior{reportErr: tc.reportErr})
			fake.queue(discoverPayload(t, tc.request))
			runControlLoopOnce(t, r, fake)

			if tc.wantReports {
				require.Len(t, fake.reportsCopy(), 1)
				require.Len(t, fake.reportsCopy()[0].Devices, 1)
				assert.Equal(t, "identified", fake.reportsCopy()[0].Devices[0].DeviceIdentifier)
			} else {
				assert.Empty(t, fake.reportsCopy())
			}
			require.Len(t, fake.acksCopy(), 1)
			ack := fake.acksCopy()[0]
			if tc.reportErr != nil {
				assert.Equal(t, pb.AckCode_ACK_CODE_REPORT_FAILED, ack.Code)
			} else {
				assert.Equal(t, pb.AckCode_ACK_CODE_SCAN_FAILED, ack.Code)
				assert.Contains(t, ack.ErrorMessage, "first.lan")
				assert.NotContains(t, ack.ErrorMessage, "second.lan", "retain the first DNS failure")
			}
		})
	}
}

func TestIPListDNSFailureWinsOverCommandDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &RunCmd{
			discoverer: &delayingStubDiscoverer{blockingIPs: map[string]bool{"10.0.0.5": true}},
			resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
				return nil, &net.DNSError{Name: host, Err: "i/o timeout", IsTimeout: true}
			}),
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		_, _, err := r.discoverForCommand(ctx, discoverIPList([]string{"miner.lan", "10.0.0.5"}, []string{"4028"}), discardLogger(t))
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
		var ce *commandError
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, pb.AckCode_ACK_CODE_SCAN_FAILED, ce.code)
		assert.Contains(t, ce.Error(), "miner.lan")
	})
}

func TestNetworkDNSFailureClassificationPreservesTargetPolicy(t *testing.T) {
	for _, target := range []string{"bad/target", "8.8.8.8", "public.lan"} {
		t.Run(target, func(t *testing.T) {
			r := &RunCmd{resolver: stubResolver{"public.lan": {{IP: net.ParseIP("8.8.8.8")}}}}
			_, err := r.networkScanTargets(t.Context(), &pairingpb.NetworkScanModeRequest{Target: target})
			var ce *commandError
			require.ErrorAs(t, err, &ce)
			assert.Equal(t, pb.AckCode_ACK_CODE_BAD_REQUEST, ce.code)
		})
	}
}

func TestDNSFailureDoesNotReplaceCallerCancellation(t *testing.T) {
	requests := []*pairingpb.DiscoverRequest{
		discoverIPList([]string{"miner.lan"}, []string{"4028"}),
		{Mode: &pairingpb.DiscoverRequest_NetworkScan{NetworkScan: &pairingpb.NetworkScanModeRequest{Target: "miner.lan", Ports: []string{"4028"}}}},
	}
	for _, req := range requests {
		r := &RunCmd{
			discoverer: &stubDiscoverer{},
			resolver: resolverFunc(func(_ context.Context, host string) ([]net.IPAddr, error) {
				return nil, &net.DNSError{Name: host, Err: "request canceled"}
			}),
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, _, err := r.discoverForCommand(ctx, req, discardLogger(t))
		assert.ErrorIs(t, err, context.Canceled)
	}
}
