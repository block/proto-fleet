//go:build !windows

package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Ullaakut/nmap/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pairingpb "github.com/block/proto-fleet/server/generated/grpc/pairing/v1"
)

func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestResolveNmapPath_Default_BinaryAdjacent(t *testing.T) {
	// Arrange: owned by current uid, 0755, no group/world write.
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0o755))
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	require.NoError(t, err)

	// Act
	got := resolveNmapPath(exeDir, testLogger())

	// Assert: resolveNmapPath returns the symlink-resolved path so the
	// validated file is the same one that gets exec'd (macOS /var -> /private/var).
	assert.Equal(t, resolvedCandidate, got)
}

func TestResolveNmapPath_Default_AdjacentNotExecutableFallsBack(t *testing.T) {
	// Arrange: empty PATH keeps the fallback deterministic across machines.
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("nope"), 0o644))
	t.Setenv("PATH", t.TempDir())

	// Act
	got := resolveNmapPath(exeDir, testLogger())

	// Assert: adjacent unsafe fails closed; PATH lookup also empty.
	assert.Empty(t, got)
}

func TestResolveNmapPath_Default_AdjacentWorldWritableFailsClosed(t *testing.T) {
	// Arrange: world-writable adjacent must fail closed, not fall through to PATH.
	// Chmod after WriteFile to bypass the runner's umask.
	exeDir := t.TempDir()
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Chmod(candidate, 0o757)) //nolint:gosec // intentionally world-writable to exercise the safety check

	// Act
	got := resolveNmapPath(exeDir, testLogger())

	// Assert
	assert.Empty(t, got, "world-writable adjacent nmap must not be accepted, and we must not fall through to PATH")
}

func TestResolveNmapPath_AdjacentRejectsWritableAncestor(t *testing.T) {
	// Arrange: binary itself passes ownership+mode, but the immediate parent
	// is world-writable (no sticky bit). validatePathChain must reject the
	// chain so a co-tenant can't swap the binary after startup.
	parent := t.TempDir()
	exeDir := filepath.Join(parent, "bin")
	require.NoError(t, os.Mkdir(exeDir, 0o750))
	candidate := filepath.Join(exeDir, "nmap")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.Chmod(parent, 0o777)) //nolint:gosec // intentional, exercises ancestor-chain safety check
	t.Setenv("PATH", t.TempDir())

	// Act
	got := resolveNmapPath(exeDir, testLogger())

	// Assert
	assert.Empty(t, got, "world-writable ancestor without sticky bit must fail the chain check")
}

func TestValidateNmapTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: true},
		{name: "ipv4", input: "10.0.0.1", wantErr: false},
		{name: "ipv6", input: "2001:db8::1", wantErr: false},
		{name: "ipv4 cidr", input: "10.0.0.0/24", wantErr: false},
		{name: "ipv6 cidr", input: "2001:db8::/32", wantErr: false},
		{name: "ipv4 range", input: "10.0.0.1-50", wantErr: false},
		{name: "hostname", input: "miner-01.lan", wantErr: false},
		{name: "hostname single label", input: "miner", wantErr: false},
		{name: "leading dash flag", input: "-iL/etc/passwd", wantErr: true},
		{name: "nmap output flag", input: "-oN/tmp/loot", wantErr: true},
		{name: "embedded space", input: "10.0.0.1 -oN/tmp/x", wantErr: true},
		{name: "embedded null", input: "10.0.0.1\x00", wantErr: true},
		{name: "ipv6 with brackets", input: "[2001:db8::1]", wantErr: true},
		{name: "ipv4 range upper bound too high", input: "10.0.0.1-300", wantErr: true},
		{name: "ipv4 range bad head", input: "10.0.0.999-50", wantErr: true},
		// nmap reads "A.B.C.D-N" as last-octet D..N. A descending range
		// (N below the head's last octet) must be rejected; otherwise it
		// passes the grammar and later disables IP scope enforcement.
		{name: "ipv4 range descending", input: "192.168.1.50-10", wantErr: true},
		{name: "ipv4 range ascending", input: "192.168.1.10-50", wantErr: false},
		{name: "ipv4 range single endpoint", input: "192.168.1.50-50", wantErr: false},
		{name: "shell metacharacter semicolon", input: "10.0.0.1;rm", wantErr: true},
		{name: "shell metacharacter ampersand", input: "10.0.0.1&touch", wantErr: true},
		{name: "leading whitespace", input: " 10.0.0.1", wantErr: true},
		{name: "trailing newline", input: "10.0.0.1\n", wantErr: true},
		{name: "hostname starts with dash", input: "-bad.lan", wantErr: true},
		{name: "hostname with underscore", input: "bad_name", wantErr: true},
		// Multi-octet IPv4 ranges (nmap syntax) match the hostname grammar
		// but would bypass the /22 cap. Must be rejected explicitly.
		{name: "nmap multi-octet range", input: "10.0-255.0-255.1-254", wantErr: true},
		{name: "nmap multi-octet range two labels", input: "10.0-255", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			err := validateNmapTarget(tc.input)

			// Assert
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestResolveNmapTarget(t *testing.T) {
	t.Parallel()

	stubLookup := func(addrs ...string) func(context.Context, string) ([]net.IPAddr, error) {
		out := make([]net.IPAddr, 0, len(addrs))
		for _, s := range addrs {
			out = append(out, net.IPAddr{IP: net.ParseIP(s)})
		}
		return func(_ context.Context, _ string) ([]net.IPAddr, error) { return out, nil }
	}
	failLookup := func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return nil, errors.New("nxdomain")
	}

	cases := []struct {
		name       string
		input      string
		lookup     func(context.Context, string) ([]net.IPAddr, error)
		wantTarget string
		wantIPv6   bool
		wantErr    bool
	}{
		{name: "ipv4 literal passes through", input: "10.0.0.1", lookup: failLookup, wantTarget: "10.0.0.1"},
		{name: "ipv6 literal passes through and flags v6", input: "2001:db8::1", lookup: failLookup, wantTarget: "2001:db8::1", wantIPv6: true},
		{name: "ipv4 cidr passes through", input: "10.0.0.0/24", lookup: failLookup, wantTarget: "10.0.0.0/24"},
		{name: "ipv4 range passes through", input: "10.0.0.1-50", lookup: failLookup, wantTarget: "10.0.0.1-50"},
		{name: "ipv6 cidr rejected", input: "2001:db8::/32", lookup: failLookup, wantErr: true},
		{name: "hostname substituted with ipv4", input: "miner.lan", lookup: stubLookup("10.0.0.5"), wantTarget: "10.0.0.5"},
		{name: "dual-stack prefers ipv4", input: "miner.lan", lookup: stubLookup("2001:db8::2", "10.0.0.5"), wantTarget: "10.0.0.5"},
		{name: "ipv6-only host flips to v6", input: "miner.lan", lookup: stubLookup("2001:db8::2"), wantTarget: "2001:db8::2", wantIPv6: true},
		{name: "unresolvable hostname falls through", input: "no-such-host.invalid", lookup: failLookup, wantTarget: "no-such-host.invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Act
			got, useIPv6, err := resolveNmapTarget(context.Background(), tc.input, tc.lookup)

			// Assert
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantTarget, got)
			assert.Equal(t, tc.wantIPv6, useIPv6)
		})
	}
}

func TestBuildNmapOptions_SubstitutesHostnameWithResolvedIP(t *testing.T) {
	// Arrange
	r := &RunCmd{
		nmapPath:   "/usr/bin/nmap",
		discoverer: &stubDiscoverer{},
		resolver: stubResolver{
			"miner.lan": {{IP: net.ParseIP("10.0.0.42")}},
		},
	}
	req := &pairingpb.NmapModeRequest{Target: "miner.lan", Ports: []string{"4028"}}

	// Act
	opts, err := r.buildNmapOptions(context.Background(), req, req.Ports)
	require.NoError(t, err)
	scanner, err := nmap.NewScanner(context.Background(), opts...)
	require.NoError(t, err)

	// Assert: nmap sees the resolved IP, not the hostname.
	args := scanner.Args()
	assert.True(t, slices.Contains(args, "10.0.0.42"), "expected resolved IP in argv: %v", args)
	assert.False(t, slices.Contains(args, "miner.lan"), "hostname must be substituted out of argv: %v", args)
}

type stubResolver map[string][]net.IPAddr

func (s stubResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if addrs, ok := s[host]; ok {
		return addrs, nil
	}
	return nil, errors.New("nxdomain")
}

func TestBuildNmapOptions_AddsIPv6Scanning(t *testing.T) {
	// Arrange
	r := &RunCmd{nmapPath: "/usr/bin/nmap", discoverer: &stubDiscoverer{}}
	v4 := &pairingpb.NmapModeRequest{Target: "10.0.0.1", Ports: []string{"4028"}}
	v6 := &pairingpb.NmapModeRequest{Target: "2001:db8::1", Ports: []string{"4028"}}

	// Act
	v4Opts, err := r.buildNmapOptions(context.Background(), v4, v4.Ports)
	require.NoError(t, err)
	v6Opts, err := r.buildNmapOptions(context.Background(), v6, v6.Ports)
	require.NoError(t, err)

	// Assert via argv: slice-length math would silently pass if a refactor swapped two options for one.
	v4Scanner, err := nmap.NewScanner(context.Background(), v4Opts...)
	require.NoError(t, err)
	v6Scanner, err := nmap.NewScanner(context.Background(), v6Opts...)
	require.NoError(t, err)
	assert.False(t, slices.Contains(v4Scanner.Args(), "-6"), "IPv4 target must not carry -6")
	assert.True(t, slices.Contains(v6Scanner.Args(), "-6"), "IPv6 target must carry -6")
}

func TestDiscoverForCommand_NmapPathEmptyFailsClosed(t *testing.T) {
	// Arrange
	r := &RunCmd{nmapPath: "", discoverer: &stubDiscoverer{}}
	req := &pairingpb.DiscoverRequest{Mode: &pairingpb.DiscoverRequest_Nmap{
		Nmap: &pairingpb.NmapModeRequest{Target: "10.0.0.1", Ports: []string{"4028"}},
	}}

	// Act
	_, _, err := r.discoverForCommand(context.Background(), req, testLogger())

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nmap binary not available")
}
