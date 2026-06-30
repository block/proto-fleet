package pairing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateReport covers the agent-report validation rules directly, without
// a database. UpsertDiscoveredDevices runs validateReport before any
// transaction, so these cases need no migrated DB; the InvalidArgument mapping
// and rollback on a rejected report are covered by
// TestUpsertDiscoveredDevices_BatchValidationErrorRollsBack.
func TestValidateReport(t *testing.T) {
	// A report that passes every rule; each case mutates one field.
	valid := DiscoveredDeviceReport{
		DeviceIdentifier: "device-1",
		IPAddress:        "10.0.0.1",
		Port:             "80",
		URLScheme:        "http",
		DriverName:       "virtual",
	}

	cases := []struct {
		name    string
		mutate  func(r *DiscoveredDeviceReport)
		wantErr bool
	}{
		{"valid private v4", func(r *DiscoveredDeviceReport) {}, false},
		{"valid RFC4193 ULA v6", func(r *DiscoveredDeviceReport) { r.IPAddress = "fd00::1" }, false},
		{"empty url scheme", func(r *DiscoveredDeviceReport) { r.URLScheme = "" }, false},
		{"virtual url scheme", func(r *DiscoveredDeviceReport) { r.URLScheme = "virtual" }, false},
		{"non-http url scheme", func(r *DiscoveredDeviceReport) { r.URLScheme = "stratum+tcp" }, false},

		{"missing device identifier", func(r *DiscoveredDeviceReport) { r.DeviceIdentifier = "" }, true},
		{"unparseable ip", func(r *DiscoveredDeviceReport) { r.IPAddress = "not-an-ip" }, true},
		{"loopback v4", func(r *DiscoveredDeviceReport) { r.IPAddress = "127.0.0.1" }, true},
		{"loopback v6", func(r *DiscoveredDeviceReport) { r.IPAddress = "::1" }, true},
		{"link-local v4", func(r *DiscoveredDeviceReport) { r.IPAddress = "169.254.1.1" }, true},
		{"link-local v6", func(r *DiscoveredDeviceReport) { r.IPAddress = "fe80::1" }, true},
		{"public v4", func(r *DiscoveredDeviceReport) { r.IPAddress = "8.8.8.8" }, true},
		{"public v6", func(r *DiscoveredDeviceReport) { r.IPAddress = "2606:4700:4700::1111" }, true},
		{"multicast v4", func(r *DiscoveredDeviceReport) { r.IPAddress = "224.0.0.1" }, true},
		{"unspecified v4", func(r *DiscoveredDeviceReport) { r.IPAddress = "0.0.0.0" }, true},
		{"port out of range", func(r *DiscoveredDeviceReport) { r.Port = "999999" }, true},
		{"port zero", func(r *DiscoveredDeviceReport) { r.Port = "0" }, true},
		{"port non-numeric", func(r *DiscoveredDeviceReport) { r.Port = "abc" }, true},
		{"injection url scheme", func(r *DiscoveredDeviceReport) { r.URLScheme = "javascript:alert(1)//" }, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Arrange
			r := valid
			tc.mutate(&r)

			// Act
			err := validateReport(r)

			// Assert
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
