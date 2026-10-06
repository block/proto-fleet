package sites

import (
	"fmt"
	"strings"
	"testing"
)

func TestCanonicalizeInfrastructureControlSubnetsSortsAndMasks(t *testing.T) {
	t.Parallel()

	got, err := CanonicalizeInfrastructureControlSubnets([]string{
		" fd12:3456::1/128 ",
		"192.168.8.99/24",
		"10.2.3.4/32",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSubnets := []string{
		"10.2.3.4/32",
		"192.168.8.0/24",
		"fd12:3456::1/128",
	}
	if strings.Join(got.Subnets, "\n") != strings.Join(wantSubnets, "\n") {
		t.Fatalf("unexpected canonical subnets: got %v want %v", got.Subnets, wantSubnets)
	}
	if got.Canonical != strings.Join(wantSubnets, "\n") {
		t.Fatalf("unexpected storage text: %q", got.Canonical)
	}
}

func TestCanonicalizeInfrastructureControlSubnetsAcceptsEmptyDecommission(t *testing.T) {
	t.Parallel()

	got, err := CanonicalizeInfrastructureControlSubnets(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Canonical != "" || len(got.Subnets) != 0 {
		t.Fatalf("empty replacement must decommission: %+v", got)
	}
}

func TestCanonicalizeInfrastructureControlSubnetsRejectsUnsafePrefixesWithoutEcho(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subnets []string
		secrets []string
	}{
		{
			name:    "public IPv4",
			subnets: []string{"203.0.113.44/32"},
			secrets: []string{"203.0.113.44"},
		},
		{
			name:    "IPv4 broader than slash 20",
			subnets: []string{"10.48.0.0/19"},
			secrets: []string{"10.48.0.0"},
		},
		{
			name:    "IPv6 subnet instead of host",
			subnets: []string{"fd12:3456::/64"},
			secrets: []string{"fd12:3456"},
		},
		{
			name:    "loopback",
			subnets: []string{"127.0.0.1/32"},
			secrets: []string{"127.0.0.1"},
		},
		{
			name:    "link local",
			subnets: []string{"169.254.10.20/32"},
			secrets: []string{"169.254.10.20"},
		},
		{
			name:    "unspecified",
			subnets: []string{"0.0.0.0/32"},
			secrets: []string{"0.0.0.0"},
		},
		{
			name:    "malformed",
			subnets: []string{"secret-control-host"},
			secrets: []string{"secret-control-host"},
		},
		{
			name:    "overlap",
			subnets: []string{"10.64.0.0/24", "10.64.0.42/32"},
			secrets: []string{"10.64.0.0", "10.64.0.42"},
		},
		{
			name:    "duplicate",
			subnets: []string{"192.168.20.0/24", "192.168.20.0/24"},
			secrets: []string{"192.168.20.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := CanonicalizeInfrastructureControlSubnets(tt.subnets)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			message := err.Error()
			if !strings.Contains(message, "infrastructure_control_subnets") ||
				!strings.Contains(message, "line") {
				t.Fatalf("error must identify the field and line without topology: %q", message)
			}
			for _, secret := range tt.secrets {
				if strings.Contains(message, secret) {
					t.Fatalf("error echoed submitted OT topology %q: %q", secret, message)
				}
			}
		})
	}
}

func TestCanonicalizeInfrastructureControlSubnetsCapsEntries(t *testing.T) {
	t.Parallel()

	subnets := make([]string, MaxInfrastructureControlSubnets+1)
	for i := range subnets {
		subnets[i] = fmt.Sprintf("10.%d.%d.%d/32", i/65536, (i/256)%256, i%256)
	}

	_, err := CanonicalizeInfrastructureControlSubnets(subnets)
	if err == nil {
		t.Fatal("expected over-cap validation error, got nil")
	}
	if !strings.Contains(err.Error(), "infrastructure_control_subnets") {
		t.Fatalf("error must identify field: %q", err)
	}
}
