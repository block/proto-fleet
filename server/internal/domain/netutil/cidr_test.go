package netutil

import (
	"errors"
	"testing"
)

func TestParseCIDROrIP(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
		errIs   error
	}{
		{name: "ipv4 CIDR", raw: "10.0.0.0/24", want: "10.0.0.0/24"},
		{name: "ipv4 CIDR masked", raw: "10.0.0.5/24", want: "10.0.0.0/24"},
		{name: "ipv4 bare", raw: "10.0.0.5", want: "10.0.0.5/32"},
		{name: "ipv6 CIDR", raw: "2001:db8::/32", want: "2001:db8::/32"},
		{name: "ipv6 bare", raw: "2001:db8::1", want: "2001:db8::1/128"},
		{name: "empty", raw: "", wantErr: true, errIs: ErrEmptyCIDR},
		{name: "garbage IP", raw: "not-an-ip", wantErr: true},
		{name: "garbage CIDR", raw: "10.0.0.0/abc", wantErr: true},
		{name: "ipv4 too-many-bits", raw: "10.0.0.0/33", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCIDROrIP(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				if tc.errIs != nil && !errors.Is(err, tc.errIs) {
					t.Fatalf("expected errors.Is(%v, %v), got %v", err, tc.errIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tc.want {
				t.Fatalf("got %q, want %q", got.String(), tc.want)
			}
		})
	}
}
