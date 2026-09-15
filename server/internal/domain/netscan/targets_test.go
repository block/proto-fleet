package netscan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/block/proto-fleet/server/internal/domain/discoverylimits"
)

func TestTargetGrammar(t *testing.T) {
	for _, raw := range []string{
		"10.0.0.0", "10.0.0.1", "::ffff:10.0.0.1", "fd00::1", "2001:db8::1",
		"miner-01.lan", "miner", "123", "10.0.0.0-255", "10.0.0.5-5", LocalSubnetTarget,
		fmt.Sprintf("10.0.0.0/%d", discoverylimits.MinIPv4PrefixBits), "10.0.0.0/31", "10.0.0.1/32",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseBoundedTarget(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, raw := range []string{
		"", "-iL", "10.0.0.1;touch", "10.0.0.1 10.0.0.2", "miner_name", "miner.",
		"256.0.0.1", "10.0.0.1-256", "10.0.0.10-9", "10.0-255.0-255.1-254", "10.*.0.1",
		"10.0.0.0/33", "10.0.0.0/-1", "10.0.0.0/0", "fd00::/120", "::ffff:10.0.0.1/128",
		"fe80::1", "fd00::1%eth0", "fe80::1%eth0", fmt.Sprintf("10.0.0.0/%d", discoverylimits.MinIPv4PrefixBits-1),
	} {
		t.Run("reject_"+raw, func(t *testing.T) {
			if _, err := ParseBoundedTarget(raw); err == nil {
				t.Fatal("accepted invalid target")
			}
		})
	}
}

func TestTargetCIDRUsableHosts(t *testing.T) {
	for _, test := range []struct {
		raw   string
		want  []string
		count uint64
	}{
		{"10.0.0.3/30", []string{"10.0.0.1", "10.0.0.2"}, 2},
		{"10.0.0.1/31", []string{"10.0.0.0", "10.0.0.1"}, 2},
		{"10.0.0.0/32", []string{"10.0.0.0"}, 1},
		{"10.0.0.1", []string{"10.0.0.1"}, 1},
		{"::ffff:10.0.0.1", []string{"10.0.0.1"}, 1},
		{"fd00::1", []string{"fd00::1"}, 1},
	} {
		t.Run(test.raw, func(t *testing.T) {
			target, err := ParseBoundedTarget(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := addressStrings(target); !slices.Equal(got, test.want) {
				t.Fatalf("addresses = %v, want %v", got, test.want)
			}
			if target.Count() != test.count {
				t.Fatalf("count = %d, want %d", target.Count(), test.count)
			}
			for _, addr := range test.want {
				if !target.Contains(netip.MustParseAddr(addr)) {
					t.Fatalf("target excludes yielded address %s", addr)
				}
			}
		})
	}
	target := mustTarget(t, fmt.Sprintf("10.0.0.3/%d", discoverylimits.MinIPv4PrefixBits))
	if got, want := target.Count(), uint64(discoverylimits.MaxScanTargets-2); got != want {
		t.Fatalf("broadest prefix count = %d, want %d", got, want)
	}
	for _, raw := range []string{"10.0.0.255", "10.0.1.0", "10.0.1.1"} {
		if !target.Contains(netip.MustParseAddr(raw)) {
			t.Errorf("CIDR wrongly excludes interior address %s", raw)
		}
	}
	if target.Contains(netip.MustParseAddr("10.0.0.0")) || target.Contains(netip.MustParseAddr("fd00::1")) {
		t.Fatal("CIDR includes its network address or another address family")
	}
	full := mustTarget(t, "10.0.0.1/0")
	if full.Count() != (uint64(1)<<32)-2 || full.Contains(netip.MustParseAddr("0.0.0.0")) || full.Contains(netip.MustParseAddr("255.255.255.255")) {
		t.Fatalf("broad server-local prefix count = %d", full.Count())
	}
	count := 0
	for range full.Addresses() {
		count++
		break
	}
	if count != 1 {
		t.Fatal("broad prefix iterator did not stop at consumer boundary")
	}
}

func TestExplicitRangesIncludeAllAddresses(t *testing.T) {
	target, err := Range("10.0.0.255", "10.0.1.2")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.255", "10.0.1.0", "10.0.1.1", "10.0.1.2"}
	if got := addressStrings(target); !slices.Equal(got, want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	if got := addressStrings(mustTarget(t, "10.0.0.0-2")); !slices.Equal(got, []string{"10.0.0.0", "10.0.0.1", "10.0.0.2"}) {
		t.Fatalf("last-octet range addresses = %v", got)
	}
	last := mustTarget(t, "255.255.255.255-255")
	if got := addressStrings(last); !slices.Equal(got, []string{"255.255.255.255"}) {
		t.Fatalf("maximum address iteration = %v", got)
	}
	full, err := Range("0.0.0.0", "255.255.255.255")
	if err != nil || full.Count() != uint64(1)<<32 {
		t.Fatalf("full IPv4 count = %d, error = %v", full.Count(), err)
	}
	count := 0
	for range full.Addresses() {
		count++
		break
	}
	if count != 1 {
		t.Fatal("iterator did not stop at consumer boundary")
	}
	for _, test := range [][2]string{{"10.0.0.2", "10.0.0.1"}, {"fd00::1", "fd00::2"}, {"bad", "10.0.0.1"}, {"10.0.0.1", "bad"}, {"::ffff:10.0.0.1%eth0", "10.0.0.2"}} {
		if _, err := Range(test[0], test[1]); err == nil {
			t.Fatalf("accepted range %v", test)
		}
	}
}

func TestTargetPrivateScope(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want bool
	}{
		{"10.0.0.0/24", true}, {"172.16.0.0-255", true}, {"192.168.0.1", true},
		{"8.8.8.0/24", false}, {"8.8.8.10-50", false},
		{"fd00::1", true}, {"miner.lan", true}, {"8.8.8.8", false}, {"127.0.0.1", false}, {"2001:db8::1", false},
	} {
		if got := mustTarget(t, test.raw).IsPrivate(); got != test.want {
			t.Errorf("IsPrivate(%s) = %v, want %v", test.raw, got, test.want)
		}
	}
	for _, test := range []struct {
		start, end string
		want       bool
	}{
		{"10.0.0.0", "10.255.255.255", true},
		{"10.0.0.0", "192.168.0.1", false},
		{"172.16.0.0", "172.32.0.0", false},
	} {
		target, err := Range(test.start, test.end)
		if err != nil {
			t.Fatal(err)
		}
		if got := target.IsPrivate(); got != test.want {
			t.Errorf("IsPrivate(%s..%s) = %v, want %v", test.start, test.end, got, test.want)
		}
	}
	var zero Target
	if zero.Count() != 0 || zero.IsPrivate() || zero.Contains(netip.MustParseAddr("10.0.0.1")) || len(addressStrings(zero)) != 0 {
		t.Fatal("zero target is not empty")
	}
	host := mustTarget(t, "miner.lan")
	if host.Count() != 0 || len(addressStrings(host)) != 0 || !host.Contains(netip.MustParseAddr("10.0.0.1")) {
		t.Fatal("unresolved hostname scope or enumeration is incorrect")
	}
}

func TestResolveFiltersBeforeAddressFamilyPreference(t *testing.T) {
	for _, test := range []struct {
		name        string
		answers     []net.IPAddr
		privateOnly bool
		want        string
	}{
		{"private IPv6 after public IPv4", dnsAnswers("8.8.8.8", "fd00::1"), true, "fd00::1"},
		{"private IPv4 after public IPv4", dnsAnswers("8.8.8.8", "10.0.0.1"), true, "10.0.0.1"},
		{"IPv4 preferred over IPv6", dnsAnswers("fd00::1", "10.0.0.1"), true, "10.0.0.1"},
		{"server retains public IPv4", dnsAnswers("fd00::1", "8.8.8.8"), false, "8.8.8.8"},
		{"link local skipped", dnsAnswers("fe80::1", "fd00::2"), false, "fd00::2"},
		{"mapped IPv4 normalized", dnsAnswers("::ffff:10.0.0.1"), true, "10.0.0.1"},
		{"invalid answer skipped", []net.IPAddr{{}, {IP: net.ParseIP("10.0.0.1")}}, true, "10.0.0.1"},
		{"scoped answer skipped", []net.IPAddr{{IP: net.ParseIP("fd00::1"), Zone: "eth0"}, {IP: net.ParseIP("fd00::2")}}, true, "fd00::2"},
		{"all answers forbidden", dnsAnswers("8.8.8.8", "fe80::1"), true, ""},
		{"empty answers", nil, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &targetResolver{answers: test.answers}
			target, err := mustTarget(t, "miner.lan").Resolve(context.Background(), resolver, test.privateOnly)
			if resolver.calls != 1 || resolver.host != "miner.lan" {
				t.Fatalf("DNS calls = %d, hostname = %q", resolver.calls, resolver.host)
			}
			if test.want == "" {
				if err == nil {
					t.Fatal("expected unusable hostname error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if target.Count() != 1 || !slices.Equal(addressStrings(target), []string{test.want}) {
				t.Fatalf("resolved addresses = %v, want %s", addressStrings(target), test.want)
			}
			if _, err := target.Resolve(context.Background(), resolver, test.privateOnly); err != nil || resolver.calls != 1 {
				t.Fatalf("resolved target unexpectedly repeats DNS: calls = %d, error = %v", resolver.calls, err)
			}
		})
	}
}

func TestParseAddrTarget(t *testing.T) {
	for _, test := range []struct {
		raw      string
		addr     string
		hostname string
	}{
		{raw: "10.0.0.1", addr: "10.0.0.1"},
		{raw: "::ffff:10.0.0.1", addr: "10.0.0.1"},
		{raw: "8.8.8.8", addr: "8.8.8.8"},
		{raw: "fd00::1", addr: "fd00::1"},
		{raw: "2001:db8::1", addr: "2001:db8::1"},
		{raw: "miner-01.lan", hostname: "miner-01.lan"},
		{raw: "miner", hostname: "miner"},
	} {
		t.Run(test.raw, func(t *testing.T) {
			target, err := ParseAddrTarget(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if test.hostname != "" {
				if target.hostname != test.hostname || target.Count() != 0 {
					t.Fatalf("hostname should remain unresolved: %+v", target)
				}
			} else if got := addressStrings(target); !slices.Equal(got, []string{test.addr}) {
				t.Fatalf("addresses = %v, want %s", got, test.addr)
			}
		})
	}
	for _, raw := range []string{
		"", "10.0.0.0/24", "fd00::/120", "10.0.0.1-2", "10.0.0.1-1",
		"10.0-1.0-1.1", "10.0.0.1-10.0.1.2", "bad/host", "256.0.0.1",
		"fe80::1", "fd00::1%eth0",
	} {
		t.Run("reject_"+raw, func(t *testing.T) {
			if _, err := ParseAddrTarget(raw); err == nil {
				t.Fatal("accepted invalid IP-list target")
			}
		})
	}
}

func TestResolveAddrAndCancellation(t *testing.T) {
	resolver := &targetResolver{answers: dnsAnswers("10.0.0.1")}
	for _, raw := range []string{"10.0.0.1", "::ffff:10.0.0.1"} {
		addr, err := ResolveAddr(context.Background(), raw, resolver, true)
		if err != nil || addr.String() != "10.0.0.1" || resolver.calls != 0 {
			t.Fatalf("ResolveAddr(%q) = %s, %v; DNS calls = %d", raw, addr, err, resolver.calls)
		}
	}
	for _, raw := range []string{"", "10.0.0.0/24", "10.0.0.1-2", "bad/host", "fe80::1", "fd00::1%eth0", "8.8.8.8"} {
		if _, err := ResolveAddr(context.Background(), raw, resolver, true); err == nil {
			t.Errorf("ResolveAddr accepted %q", raw)
		}
	}
	if _, err := ResolveAddr(context.Background(), "8.8.8.8", resolver, false); err != nil {
		t.Fatalf("server policy rejected public literal: %v", err)
	}
	resolver.err = context.Canceled
	if _, err := ResolveAddr(context.Background(), "miner.lan", resolver, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("DNS error lost cancellation: %v", err)
	}
}

func TestPorts(t *testing.T) {
	ports, err := Ports([]string{"80", "080", "+80"}, nil)
	if err != nil || !slices.Equal(ports, []uint16{80}) {
		t.Fatalf("normalize ports: %v, %v", ports, err)
	}

	for _, test := range []struct {
		name          string
		raw, defaults []string
		want          []uint16
	}{
		{"normalize sort and dedupe", []string{"4028", "080", "80", "1", "65535"}, nil, []uint16{1, 80, 4028, 65535}},
		{"default ports", nil, []string{"4028", "80"}, []uint16{80, 4028}},
		{"explicit overrides defaults", []string{"22"}, []string{"4028", "80"}, []uint16{22}},
		{"at raw cap", strings.Fields(strings.Repeat("80 ", discoverylimits.MaxPortsPerIP)), nil, []uint16{80}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Ports(test.raw, test.defaults)
			if err != nil || !slices.Equal(got, test.want) {
				t.Fatalf("ports = %v, %v; want %v", got, err, test.want)
			}
		})
	}
	for _, raw := range [][]string{
		nil, {"0"}, {"65536"}, {"-1"}, {" 80"}, {"80 "}, {"http"},
		strings.Fields(strings.Repeat("80 ", discoverylimits.MaxPortsPerIP+1)),
	} {
		if _, err := Ports(raw, nil); err == nil {
			t.Errorf("accepted invalid ports %v", raw)
		}
	}
}

type targetResolver struct {
	answers []net.IPAddr
	err     error
	calls   int
	host    string
}

func (r *targetResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.calls++
	r.host = host
	return r.answers, r.err
}

func dnsAnswers(raw ...string) []net.IPAddr {
	var answers []net.IPAddr
	for _, value := range raw {
		answers = append(answers, net.IPAddr{IP: net.ParseIP(value)})
	}
	return answers
}

func mustTarget(t *testing.T, raw string) Target {
	t.Helper()
	target, err := ParseTarget(raw)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func addressStrings(target Target) []string {
	var result []string
	for addr := range target.Addresses() {
		result = append(result, addr.String())
	}
	return result
}
