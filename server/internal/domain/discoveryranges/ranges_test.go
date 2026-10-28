package discoveryranges

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestParseNormalizesAndMerges(t *testing.T) {
	scope, err := Parse([]string{
		"192.168.1.9", "10.0.0.4-10.0.0.6", "10.0.0.1/30",
		"10.0.0.6-10.0.0.8", "10.0.0.2", "172.16.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.0-10.0.0.8", "172.16.0.1", "192.168.1.9"}
	if got := scope.Strings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Strings() = %v, want %v", got, want)
	}
}

func TestParsePrivateBoundaries(t *testing.T) {
	for _, value := range []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"10.255.255.255/32", "172.31.255.255", "192.168.255.255",
		" 10.0.0.255 - 10.0.1.1 ",
	} {
		t.Run(value, func(t *testing.T) {
			scope, err := Parse([]string{value})
			if err != nil {
				t.Fatal(err)
			}
			if scope.Empty() || len(scope.Ranges()) != 1 {
				t.Fatalf("expected one interval, got %v", scope.Ranges())
			}
			roundTrip, err := Parse(scope.Strings())
			if err != nil || !reflect.DeepEqual(roundTrip.Ranges(), scope.Ranges()) {
				t.Fatalf("normalized values did not round trip: %v, %v", roundTrip.Ranges(), err)
			}
		})
	}
}

func TestParseRejectsInvalidRanges(t *testing.T) {
	for _, value := range []string{
		"", " ", "miner.example", "10.0.0.256", "010.0.0.1", "10.0.0.1:80",
		"8.8.8.8", "127.0.0.1", "169.254.1.1", "100.64.0.1", "0.0.0.0",
		"::1", "fd00::1", "::ffff:10.0.0.1", "fd00::/8",
		"10.0.0.0/7", "172.16.0.0/11", "192.168.0.0/15", "0.0.0.0/0",
		"10.0.0.0/33", "10.0.0.0/-1", "10.0.0.1/24/24",
		"10.0.0.2-10.0.0.1", "10.0.0.1-11.0.0.1", "10.0.0.1-192.168.0.1",
		"172.15.255.255-172.16.0.1", "172.31.255.255-172.32.0.0",
		"192.168.255.255-192.169.0.0", "10.0.0.1-::1",
		"10.0.0.1-", "10.0.0.1-2", "10.0.0.1-10.0.0.2-10.0.0.3",
	} {
		t.Run(value, func(t *testing.T) {
			scope, err := Parse([]string{"10.0.0.1", value})
			if err == nil {
				t.Fatalf("accepted invalid input %q", value)
			}
			if !scope.Empty() {
				t.Fatalf("invalid input returned a partially authorized scope: %v", scope.Strings())
			}
		})
	}
}

func TestScopeContains(t *testing.T) {
	scope := mustParse(t, "10.0.0.254-10.0.1.1", "172.16.0.1")
	for _, address := range []string{"10.0.0.254", "10.0.0.255", "10.0.1.0", "10.0.1.1", "172.16.0.1"} {
		if !scope.Contains(netip.MustParseAddr(address)) || !scope.ContainsString(address) {
			t.Errorf("missing included address %s", address)
		}
	}
	for _, address := range []string{"10.0.0.253", "10.0.1.2", "172.16.0.0", "192.168.1.1", "8.8.8.8", "::1", "::ffff:10.0.0.255", "invalid"} {
		if scope.ContainsString(address) {
			t.Errorf("included out-of-scope address %s", address)
		}
	}
	if scope.Contains(netip.Addr{}) {
		t.Error("included invalid address")
	}
	// Callers must not be able to mutate the scope through returned ranges.
	ranges := scope.Ranges()
	ranges[0].Start = netip.MustParseAddr("10.0.0.1")
	if scope.ContainsString("10.0.0.1") {
		t.Error("Ranges exposed mutable scope storage")
	}
}

func TestScopeEmpty(t *testing.T) {
	for _, scope := range []Scope{{}, mustParse(t)} {
		if !scope.Empty() || len(scope.Strings()) != 0 || len(scope.Ranges()) != 0 || scope.ContainsString("10.0.0.1") {
			t.Errorf("unexpected nonempty scope: %v", scope)
		}
		for chunk := range scope.Chunks(3) {
			t.Fatalf("empty scope yielded %v", chunk)
		}
	}
}

func TestScopeIntersect(t *testing.T) {
	scope := mustParse(t, "10.0.0.1-10.0.0.5", "10.0.0.8-10.0.0.12", "192.168.1.1")
	for _, tc := range []struct {
		name, start, end string
		want             []Range
	}{
		{"clips intervals and retains gap", "10.0.0.3", "10.0.0.9", []Range{ipRange("10.0.0.3", "10.0.0.5"), ipRange("10.0.0.8", "10.0.0.9")}},
		{"inclusive singleton", "10.0.0.5", "10.0.0.5", []Range{ipRange("10.0.0.5", "10.0.0.5")}},
		{"gap", "10.0.0.6", "10.0.0.7", nil},
		{"outside", "172.16.0.1", "172.31.255.255", nil},
		{"reversed", "10.0.0.9", "10.0.0.3", nil},
		{"IPv6", "::1", "::2", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := scope.Intersect(netip.MustParseAddr(tc.start), netip.MustParseAddr(tc.end))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Intersect() = %v, want %v", got, tc.want)
			}
		})
	}
	if got := scope.Intersect(netip.Addr{}, netip.MustParseAddr("10.0.0.1")); len(got) != 0 {
		t.Fatalf("invalid interval returned %v", got)
	}
}

func TestScopeIntersectScope(t *testing.T) {
	left := mustParse(t, "10.0.0.1-10.0.0.5", "10.0.0.8-10.0.0.12", "172.16.0.1")
	right := mustParse(t, "10.0.0.3-10.0.0.9", "192.168.1.1")
	want := []string{"10.0.0.3-10.0.0.5", "10.0.0.8-10.0.0.9"}
	for _, result := range []Scope{left.IntersectScope(right), right.IntersectScope(left)} {
		if got := result.Strings(); !reflect.DeepEqual(got, want) {
			t.Fatalf("IntersectScope() = %v, want %v", got, want)
		}
	}
	if !left.IntersectScope(Scope{}).Empty() || !left.IntersectScope(mustParse(t, "192.168.1.1")).Empty() {
		t.Fatal("intersection with empty or disjoint scope was nonempty")
	}
}

func TestScopeChunksUniqueAcrossIntervals(t *testing.T) {
	scope := mustParse(t, "10.0.0.2-10.0.0.4", "10.0.0.1-10.0.0.3", "10.0.0.8-10.0.0.10")
	var got [][]string
	for chunk := range scope.Chunks(3) {
		got = append(got, chunk)
	}
	want := [][]string{{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, {"10.0.0.4", "10.0.0.8", "10.0.0.9"}, {"10.0.0.10"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Chunks() = %v, want %v", got, want)
	}
}

func TestLargeScopeChunksStopEarly(t *testing.T) {
	scope := mustParse(t, "10.0.0.0/8", "172.16.0.0/12")
	want := []Range{ipRange("10.0.0.0", "10.255.255.255"), ipRange("172.16.0.0", "172.31.255.255")}
	if got := scope.Ranges(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Ranges() = %v, want %v", got, want)
	}
	chunks := 0
	for chunk := range scope.Chunks(2) {
		if !reflect.DeepEqual(chunk, []string{"10.0.0.0", "10.0.0.1"}) {
			t.Fatalf("unexpected first chunk %v", chunk)
		}
		chunks++
		break
	}
	if chunks != 1 {
		t.Fatalf("got %d chunks", chunks)
	}
	// A narrow scan of a large configuration must intersect before enumeration.
	clipped := scope.IntersectScope(mustParse(t, "10.255.255.254-10.255.255.255"))
	var got []string
	for chunk := range clipped.Chunks(10) {
		got = append(got, chunk...)
	}
	if want := []string{"10.255.255.254", "10.255.255.255"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("clipped chunks = %v, want %v", got, want)
	}
}

func mustParse(t *testing.T, values ...string) Scope {
	t.Helper()
	scope, err := Parse(values)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func ipRange(start, end string) Range {
	return Range{Start: netip.MustParseAddr(start), End: netip.MustParseAddr(end)}
}
