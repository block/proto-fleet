package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	discoverymodels "github.com/block/proto-fleet/server/internal/domain/minerdiscovery/models"
)

type stubDiscoverer struct {
	probes map[string]*pb.DiscoveredDeviceReport
	ports  []string
}

func (s *stubDiscoverer) Probe(_ context.Context, ip, port string) (*pb.DiscoveredDeviceReport, error) {
	if r, ok := s.probes[ip+"|"+port]; ok {
		return r, nil
	}
	return nil, nil
}

func (s *stubDiscoverer) DefaultDiscoveryPorts(_ context.Context) []string {
	if s.ports != nil {
		return s.ports
	}
	return []string{"4028"}
}

func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	require.NoError(t, err)
	return b
}

func TestSynthesizeIdentifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		mac        string
		serial     string
		prefix     string
		prefixOnly bool
	}{
		{name: "mac wins", mac: "aa:bb:cc:dd:ee:ff", serial: "SN1", prefix: "mac:aa:bb:cc:dd:ee:ff"},
		{name: "serial when no mac", serial: "SN1", prefix: "serial:SN1"},
		{name: "auto when neither", prefix: "auto:", prefixOnly: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got := synthesizeIdentifier(tc.mac, tc.serial)

			// Assert
			if tc.prefixOnly {
				assert.True(t, len(got) > len(tc.prefix), "auto:* must include a non-empty id, got %q", got)
				assert.Equal(t, tc.prefix, got[:len(tc.prefix)])
				return
			}
			assert.Equal(t, tc.prefix, got)
		})
	}
}

func TestReportFromDiscovered_PreservesExplicitDeviceIdentifier(t *testing.T) {
	// Arrange
	dev := &discoverymodels.DiscoveredDevice{}
	dev.DeviceIdentifier = "drv-explicit-123"
	dev.MacAddress = "aa:bb:cc:dd:ee:ff"
	dev.IpAddress = "10.0.0.5"
	dev.Port = "4028"

	// Act
	got := reportFromDiscovered(dev)

	// Assert
	assert.Equal(t, "drv-explicit-123", got.GetDeviceIdentifier(), "explicit DeviceIdentifier must pass through unchanged when set by the SDK driver")
}

func TestReportFromDiscovered_CopiesFieldsAndSynthesizesIdentifier(t *testing.T) {
	// Arrange
	dev := &discoverymodels.DiscoveredDevice{}
	dev.MacAddress = "aa:bb:cc:dd:ee:ff"
	dev.IpAddress = "10.0.0.5"
	dev.Port = "4028"
	dev.UrlScheme = "http"
	dev.DriverName = "antminer"
	dev.Model = "S19"
	dev.Manufacturer = "Bitmain"
	dev.FirmwareVersion = "v1"

	// Act
	got := reportFromDiscovered(dev)

	// Assert
	assert.Equal(t, "mac:aa:bb:cc:dd:ee:ff", got.GetDeviceIdentifier())
	assert.Equal(t, "10.0.0.5", got.GetIpAddress())
	assert.Equal(t, "4028", got.GetPort())
	assert.Equal(t, "http", got.GetUrlScheme())
	assert.Equal(t, "antminer", got.GetDriverName())
	assert.Equal(t, "S19", got.GetModel())
	assert.Equal(t, "Bitmain", got.GetManufacturer())
	assert.Equal(t, "v1", got.GetFirmwareVersion())
}
