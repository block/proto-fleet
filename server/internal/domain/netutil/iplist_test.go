package netutil

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubResolver struct {
	addrs map[string][]net.IPAddr
	err   error
}

func (s stubResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if s.err != nil {
		return nil, s.err
	}
	if a, ok := s.addrs[host]; ok {
		return a, nil
	}
	return nil, &net.DNSError{Err: "not found", Name: host, IsNotFound: true}
}

func TestNormalizeIPListEntry(t *testing.T) {
	t.Parallel()

	resolver := stubResolver{
		addrs: map[string][]net.IPAddr{
			"dual.example": {{IP: net.ParseIP("2001:db8::5")}, {IP: net.ParseIP("10.0.0.5")}},
			"v6only.example": {
				{IP: net.ParseIP("fe80::1")}, // link-local skipped
				{IP: net.ParseIP("2001:db8::1")},
			},
			"linklocalonly.example": {{IP: net.ParseIP("fe80::1")}},
		},
	}

	cases := []struct {
		name      string
		input     string
		want      string
		wantErr   error
		errSubstr string
	}{
		{name: "empty rejected", input: "", wantErr: ErrEmptyTarget},
		{name: "scoped ipv6 rejected", input: "fe80::1%eth0", wantErr: ErrScopedIPv6},
		{name: "link-local ipv6 rejected", input: "fe80::1", wantErr: ErrLinkLocalIPv6},
		{name: "ipv4 passes through", input: "10.0.0.1", want: "10.0.0.1"},
		{name: "ipv6 canonicalized", input: "2001:0DB8::1", want: "2001:db8::1"},
		{name: "loopback ipv4", input: "127.0.0.1", want: "127.0.0.1"},
		{name: "hostname prefers ipv4", input: "dual.example", want: "10.0.0.5"},
		{name: "hostname falls back to non-link-local v6", input: "v6only.example", want: "2001:db8::1"},
		{name: "hostname with only link-local v6 unresolved", input: "linklocalonly.example", wantErr: ErrHostnameUnresolved},
		{name: "unknown hostname surfaces resolver error", input: "missing.example", errSubstr: "resolve missing.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got, err := NormalizeIPListEntry(context.Background(), tc.input, resolver)

			// Assert
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tc.wantErr), "want %v, got %v", tc.wantErr, err)
				return
			}
			if tc.errSubstr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNormalizeIPListEntry_DefaultResolverSatisfiesInterface(t *testing.T) {
	// Compile-time check that *net.Resolver satisfies IPListResolver. If a
	// future Go release narrows or widens the method set, this test fails
	// at build time and signals callers to update.
	var _ IPListResolver = net.DefaultResolver
}
