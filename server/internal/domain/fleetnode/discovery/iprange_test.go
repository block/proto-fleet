package discovery

import (
	"slices"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatedIPv4Range(t *testing.T) {
	for _, tc := range []struct {
		start, end string
		count      int
	}{
		{"10.0.0.5", "10.0.0.7", 3},
		{"10.0.0.0", "10.0.15.255", 4096},
		{"10.0.0.0", "10.0.0.4", 5},
		{"10.0.0.0", "10.0.0.1", 2},
		{"192.168.1.5", "192.168.1.5", 1},
	} {
		t.Run(tc.start+"-"+tc.end, func(t *testing.T) {
			target, err := validatedIPv4Range(tc.start, tc.end)
			require.NoError(t, err)
			addrs := slices.Collect(target.Addresses())
			require.Len(t, addrs, tc.count)
			assert.Equal(t, tc.start, addrs[0].String())
			assert.Equal(t, tc.end, addrs[len(addrs)-1].String())
		})
	}
}

func TestValidatedIPv4RangeRejectsInvalidScope(t *testing.T) {
	for _, tc := range []struct{ start, end, message string }{
		{"not-an-ip", "10.0.0.5", "start_ip"},
		{"2001:db8::1", "2001:db8::5", "start_ip"},
		{"10.0.0.10", "10.0.0.5", ">="},
		{"10.0.0.0", "10.0.16.0", "exceeds"},
		{"8.8.8.8", "8.8.8.10", "private"},
	} {
		t.Run(tc.message, func(t *testing.T) {
			_, err := validatedIPv4Range(tc.start, tc.end)
			require.Error(t, err)
			assert.True(t, fleeterror.IsInvalidArgumentError(err))
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}
