package netutil

import (
	"context"
	"errors"
	"net"
	"net/netip"
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
		{name: "empty rejected", input: "", wantErr: errEmptyTarget},
		{name: "scoped ipv6 rejected", input: "fe80::1%eth0", wantErr: errScopedIPv6},
		{name: "link-local ipv6 rejected", input: "fe80::1", wantErr: errLinkLocalIPv6},
		{name: "ipv4 passes through", input: "10.0.0.1", want: "10.0.0.1"},
		{name: "ipv6 canonicalized", input: "2001:0DB8::1", want: "2001:db8::1"},
		{name: "loopback ipv4", input: "127.0.0.1", want: "127.0.0.1"},
		{name: "hostname prefers ipv4", input: "dual.example", want: "10.0.0.5"},
		{name: "hostname falls back to non-link-local v6", input: "v6only.example", want: "2001:db8::1"},
		{name: "hostname with only link-local v6 unresolved", input: "linklocalonly.example", wantErr: errHostnameUnresolved},
		{name: "unknown hostname surfaces resolver error", input: "missing.example", errSubstr: "resolve missing.example"},
		// No implicit TrimSpace: whitespace-bearing entries fall through to the resolver.
		{name: "ipv4 trailing whitespace falls through to resolver", input: "10.0.0.1 ", errSubstr: "resolve 10.0.0.1"},
		// v4-only checks must not be bypassed via the v6 wire format.
		{name: "ipv4-mapped ipv6 collapses to ipv4", input: "::ffff:10.0.0.1", want: "10.0.0.1"},
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

func TestParseIPv4(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "ipv4 literal", input: "10.0.0.1", want: "10.0.0.1"},
		{name: "loopback", input: "127.0.0.1", want: "127.0.0.1"},
		{name: "ipv4-mapped ipv6 unmaps to ipv4", input: "::ffff:10.0.0.1", want: "10.0.0.1"},
		{name: "pure ipv6 rejected", input: "2001:db8::1", wantErr: true},
		{name: "junk rejected", input: "not an ip", wantErr: true},
		{name: "empty rejected", input: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got, err := ParseIPv4(tc.input)

			// Assert
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
		})
	}
}

func TestIPv4ToUint32_KnownValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  uint32
	}{
		// Arrange + Act inlined per case.
		{input: "0.0.0.0", want: 0},
		{input: "0.0.0.1", want: 1},
		{input: "127.0.0.1", want: 0x7f000001},
		{input: "10.0.0.0", want: 0x0a000000},
		{input: "255.255.255.255", want: 0xffffffff},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			// Act
			addr, err := ParseIPv4(tc.input)
			require.NoError(t, err)

			// Assert
			assert.Equal(t, tc.want, IPv4ToUint32(addr))
		})
	}
}

func TestAdjustIPv4RangeStart(t *testing.T) {
	t.Parallel()

	mustU32 := func(s string) uint32 {
		a, err := ParseIPv4(s)
		require.NoError(t, err)
		return IPv4ToUint32(a)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: ".0 outside loopback skips to .2", in: "10.0.0.0", want: "10.0.0.2"},
		{name: ".1 outside loopback skips to .2", in: "10.0.0.1", want: "10.0.0.2"},
		{name: ".2 outside loopback unchanged", in: "10.0.0.2", want: "10.0.0.2"},
		{name: ".128 outside loopback unchanged", in: "192.168.1.128", want: "192.168.1.128"},
		{name: "loopback .0 preserved (dev fixtures bind there)", in: "127.0.0.0", want: "127.0.0.0"},
		{name: "loopback .1 preserved", in: "127.0.0.1", want: "127.0.0.1"},
		{name: "loopback .2 preserved", in: "127.0.0.2", want: "127.0.0.2"},
		{name: "non-loopback that happens to start with 7 not carved out", in: "70.0.0.0", want: "70.0.0.2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got := AdjustIPv4RangeStart(mustU32(tc.in))

			// Assert
			assert.Equal(t, tc.want, Uint32ToIPv4(got))
		})
	}
}

func TestUint32ToIPv4_KnownValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   uint32
		want string
	}{
		{in: 0, want: "0.0.0.0"},
		{in: 1, want: "0.0.0.1"},
		{in: 0x7f000001, want: "127.0.0.1"},
		{in: 0xffffffff, want: "255.255.255.255"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			// Act
			got := Uint32ToIPv4(tc.in)

			// Assert
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsIPv4NetworkOrGateway(t *testing.T) {
	t.Parallel()

	mustAddr := func(s string) netip.Addr {
		a, err := ParseIPv4(s)
		require.NoError(t, err)
		return a
	}

	cases := []struct {
		in   string
		want bool
	}{
		{in: "192.168.1.0", want: true},
		{in: "192.168.1.1", want: true},
		{in: "192.168.1.2", want: false},
		{in: "192.168.1.254", want: false},
		// Loopback carve-out: dev fixtures bind .0/.1 in 127/8.
		{in: "127.0.0.0", want: false},
		{in: "127.0.0.1", want: false},
		{in: "10.0.0.0", want: true},
		{in: "10.0.0.1", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			// Act + Assert
			assert.Equal(t, tc.want, IsIPv4NetworkOrGateway(mustAddr(tc.in)))
		})
	}
}
