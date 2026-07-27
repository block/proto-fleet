package models

import "testing"

func TestGridCapacity(t *testing.T) {
	cases := []struct {
		aisles, racksPerAisle int32
		want                  int64
	}{
		{0, 0, 0},
		{3, 0, 0},
		{0, 4, 0},
		{3, 4, 12},
	}
	for _, c := range cases {
		if got := GridCapacity(c.aisles, c.racksPerAisle); got != c.want {
			t.Errorf("GridCapacity(%d,%d) = %d, want %d", c.aisles, c.racksPerAisle, got, c.want)
		}
	}
}

func TestRackCapacityExceeded(t *testing.T) {
	cases := []struct {
		name                  string
		aisles, racksPerAisle int32
		resulting             int64
		want                  bool
	}{
		{"unconfigured grid is never exceeded", 0, 0, 100, false},
		{"at capacity fits", 3, 4, 12, false},
		{"over capacity exceeds", 3, 4, 13, true},
		{"under capacity fits", 3, 4, 5, false},
	}
	for _, c := range cases {
		if got := RackCapacityExceeded(c.aisles, c.racksPerAisle, c.resulting); got != c.want {
			t.Errorf("%s: RackCapacityExceeded(%d,%d,%d) = %v, want %v", c.name, c.aisles, c.racksPerAisle, c.resulting, got, c.want)
		}
	}
}
