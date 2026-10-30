package discoveryranges

import (
	"net/netip"
	"reflect"
	"testing"
)

func TestParseNormalizesAndMerges(t *testing.T) {
	// Act
	scope, err := Parse([]string{
		"192.168.1.9", "10.0.0.4-10.0.0.6", "10.0.0.1/30",
		"10.0.0.6-10.0.0.8", "10.0.0.2", "172.16.0.1",
	})

	// Assert
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
			// Act
			scope, err := Parse([]string{value})
			roundTrip, roundTripErr := Parse(scope.Strings())

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if scope.Empty() || len(scope.Ranges()) != 1 {
				t.Fatalf("expected one interval, got %v", scope.Ranges())
			}
			if roundTripErr != nil || !reflect.DeepEqual(roundTrip.Ranges(), scope.Ranges()) {
				t.Fatalf("normalized values did not round trip: %v, %v", roundTrip.Ranges(), roundTripErr)
			}
		})
	}
}

func TestParseRejectsInvalidRanges(t *testing.T) {
	for _, value := range []string{
		"", "miner.example", "010.0.0.1",
		"8.8.8.8", "127.0.0.1", "100.64.0.1",
		"fd00::1", "::ffff:10.0.0.1", "fd00::/8",
		"10.0.0.0/7", "172.16.0.0/11", "192.168.0.0/15",
		"10.0.0.0/33",
		"10.0.0.2-10.0.0.1", "10.0.0.1-11.0.0.1", "10.0.0.1-192.168.0.1",
		"172.15.255.255-172.16.0.1", "172.31.255.255-172.32.0.0",
		"192.168.255.255-192.169.0.0", "10.0.0.1-::1",
		"10.0.0.1-",
	} {
		t.Run(value, func(t *testing.T) {
			// Act
			scope, err := Parse([]string{"10.0.0.1", value})

			// Assert
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

	t.Run("included addresses", func(t *testing.T) {
		for _, address := range []string{"10.0.0.254", "10.0.0.255", "10.0.1.0", "10.0.1.1", "172.16.0.1"} {
			// Act
			contains, containsString := scope.Contains(netip.MustParseAddr(address)), scope.ContainsString(address)

			// Assert
			if !contains || !containsString {
				t.Errorf("missing included address %s", address)
			}
		}
	})

	t.Run("excluded addresses", func(t *testing.T) {
		for _, address := range []string{"10.0.0.253", "10.0.1.2", "172.16.0.0", "192.168.1.1", "8.8.8.8", "::1", "::ffff:10.0.0.255", "invalid"} {
			// Act
			contains := scope.ContainsString(address)

			// Assert
			if contains {
				t.Errorf("included out-of-scope address %s", address)
			}
		}
	})

	t.Run("returned ranges are copies", func(t *testing.T) {
		// Arrange
		ranges := scope.Ranges()

		// Act
		ranges[0].Start = netip.MustParseAddr("10.0.0.1")

		// Assert
		if scope.ContainsString("10.0.0.1") {
			t.Error("Ranges exposed mutable scope storage")
		}
	})
}

func TestScopeEmpty(t *testing.T) {
	for _, scope := range []Scope{{}, mustParse(t)} {
		// Act
		empty := scope.Empty() && len(scope.Strings()) == 0 && len(scope.Ranges()) == 0 && !scope.ContainsString("10.0.0.1")

		// Assert
		if !empty {
			t.Errorf("unexpected nonempty scope: %v", scope)
		}
		for chunk := range scope.Chunks(3) {
			t.Fatalf("empty scope yielded %v", chunk)
		}
	}
}

func TestScopeIntersectScope(t *testing.T) {
	// Arrange
	left := mustParse(t, "10.0.0.1-10.0.0.5", "10.0.0.8-10.0.0.12", "172.16.0.1")
	right := mustParse(t, "10.0.0.3-10.0.0.9", "192.168.1.1")
	want := []string{"10.0.0.3-10.0.0.5", "10.0.0.8-10.0.0.9"}

	t.Run("overlap is symmetric", func(t *testing.T) {
		for _, result := range []Scope{left.IntersectScope(right), right.IntersectScope(left)} {
			// Assert
			if got := result.Strings(); !reflect.DeepEqual(got, want) {
				t.Fatalf("IntersectScope() = %v, want %v", got, want)
			}
		}
	})

	t.Run("empty or disjoint scope", func(t *testing.T) {
		// Act
		withEmpty := left.IntersectScope(Scope{})
		withDisjoint := left.IntersectScope(mustParse(t, "192.168.1.1"))

		// Assert
		if !withEmpty.Empty() || !withDisjoint.Empty() {
			t.Fatal("intersection with empty or disjoint scope was nonempty")
		}
	})
}

func TestScopeChunksUniqueAcrossIntervals(t *testing.T) {
	// Arrange
	scope := mustParse(t, "10.0.0.2-10.0.0.4", "10.0.0.1-10.0.0.3", "10.0.0.8-10.0.0.10")

	// Act
	var got [][]string
	for chunk := range scope.Chunks(3) {
		got = append(got, chunk)
	}

	// Assert
	want := [][]string{{"10.0.0.1", "10.0.0.2", "10.0.0.3"}, {"10.0.0.4", "10.0.0.8", "10.0.0.9"}, {"10.0.0.10"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Chunks() = %v, want %v", got, want)
	}
}

func TestLargeScopeChunksStopEarly(t *testing.T) {
	// Arrange
	scope := mustParse(t, "10.0.0.0/8", "172.16.0.0/12")

	t.Run("breaking yields only the first chunk", func(t *testing.T) {
		// Act
		var got [][]string
		for chunk := range scope.Chunks(2) {
			got = append(got, chunk)
			break
		}

		// Assert
		want := [][]string{{"10.0.0.0", "10.0.0.1"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Chunks() = %v, want %v", got, want)
		}
	})

	t.Run("narrow scan intersects before enumeration", func(t *testing.T) {
		// Arrange
		clipped := scope.IntersectScope(mustParse(t, "10.255.255.254-10.255.255.255"))

		// Act
		var got []string
		for chunk := range clipped.Chunks(10) {
			got = append(got, chunk...)
		}

		// Assert
		if want := []string{"10.255.255.254", "10.255.255.255"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("clipped chunks = %v, want %v", got, want)
		}
	})
}

func mustParse(t *testing.T, values ...string) Scope {
	t.Helper()
	scope, err := Parse(values)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
