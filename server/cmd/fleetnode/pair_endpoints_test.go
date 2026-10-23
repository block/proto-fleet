package main

import (
	"bytes"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
	"github.com/block/proto-fleet/server/internal/domain/deviceendpoints"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
)

func TestPluginPairerChecksEndpointGroupForEveryCredentialPath(t *testing.T) {
	for _, auth := range []string{"anonymous container", "supplied credentials", "default credentials"} {
		for _, tc := range []struct {
			name        string
			savedKey    string
			returnedKey string
			wantError   bool
		}{
			{name: "new group requires discovery", returnedKey: "new", wantError: true},
			{name: "changed group requires discovery", savedKey: "old", returnedKey: "new", wantError: true},
			{name: "same group", savedKey: "saved", returnedKey: "saved"},
			{name: "omitted routes preserve inventory", savedKey: "saved"},
			{name: "legacy ungrouped device"},
		} {
			t.Run(auth+"/"+tc.name, func(t *testing.T) {
				target := fakePairTarget()
				if tc.savedKey != "" {
					target.Endpoints = []*commonpb.DeviceEndpoint{{Id: "a", DeviceKey: tc.savedKey, Host: target.IpAddress, Port: 80, UrlScheme: "http", RpcPort: 9095}}
				}
				driver := &fakePairDriver{pairResult: sdk.DeviceInfo{SerialNumber: "a"}}
				if tc.returnedKey != "" {
					driver.pairResult.Endpoints = []sdk.DeviceEndpoint{{ID: "a", DeviceKey: tc.returnedKey, Host: "10.0.0.99", Port: 8443, URLScheme: "https", RPCPort: 9095}}
				}
				var creds *pairingpb.Credentials
				switch auth {
				case "anonymous container":
					target.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_CONTAINER
				case "supplied credentials":
					password := "test-password"
					creds = &pairingpb.Credentials{Username: "admin", Password: &password}
				case "default credentials":
					driver.defaults = []sdk.UsernamePassword{{Username: "admin", Password: "test-password"}}
				}
				pairer := newTestPairerWithCredentials(t, sdk.Capabilities{sdk.CapabilityPairing: true}, driver, &credentialCodec{key: bytes.Repeat([]byte{1}, credentialKeySize)})
				before, err := deviceendpoints.Encode(target.Endpoints)
				require.NoError(t, err)

				result := pairer.Pair(t.Context(), target, creds)

				require.Len(t, driver.gotBundles, 1)
				if tc.wantError {
					require.Equal(t, pb.PairOutcome_PAIR_OUTCOME_ERROR, result.Outcome)
					require.Contains(t, result.ErrorMessage, "discover the device again")
					require.Empty(t, result.SerialNumber)
					require.Nil(t, result.EncryptedCredentials)
				} else {
					require.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, result.Outcome)
				}
				after, err := deviceendpoints.Encode(target.Endpoints)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after), "pairing must retain discovered routes")
			})
		}
	}
}

func TestPluginPairerRejectsInvalidDialTargetWithoutEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name   string
		host   string
		scheme string
	}{
		{name: "public address", host: "8.8.8.8", scheme: "http"},
		{name: "link local address", host: "169.254.169.254", scheme: "http"},
		{name: "host name", host: "controller.local", scheme: "http"},
		{name: "invalid scheme", host: "10.0.0.5", scheme: "http://"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			driver := &fakePairDriver{}
			pairer := newTestPairer(t, sdk.Capabilities{sdk.CapabilityPairing: true}, driver)
			target := fakePairTarget()
			target.IpAddress, target.UrlScheme = tc.host, tc.scheme
			target.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_CONTAINER

			result := pairer.Pair(t.Context(), target, nil)

			require.Equal(t, pb.PairOutcome_PAIR_OUTCOME_ERROR, result.Outcome)
			require.NotEmpty(t, result.ErrorMessage)
			require.Empty(t, driver.gotBundles, "invalid targets must not reach the plugin")
		})
	}
}

func TestPluginPairerAcceptsDiscoverySchemes(t *testing.T) {
	for _, scheme := range []string{"stratum+tcp", ""} {
		t.Run("scheme="+scheme, func(t *testing.T) {
			driver := &fakePairDriver{pairResult: sdk.DeviceInfo{SerialNumber: "verified"}}
			pairer := newTestPairer(t, sdk.Capabilities{sdk.CapabilityPairing: true}, driver)
			target := fakePairTarget()
			target.UrlScheme = scheme
			target.DeviceClass = commonpb.DeviceClass_DEVICE_CLASS_CONTAINER

			result := pairer.Pair(t.Context(), target, nil)

			require.Equal(t, pb.PairOutcome_PAIR_OUTCOME_PAIRED, result.Outcome)
			require.Len(t, driver.gotBundles, 1, "valid discovery schemes must reach the plugin")
		})
	}
}
