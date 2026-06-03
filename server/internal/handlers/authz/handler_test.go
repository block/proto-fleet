package authz

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestParseRoleID(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantID  int64
		wantErr bool
	}{
		{"plain decimal", "42", 42, false},
		{"max int64", "9223372036854775807", 9223372036854775807, false},

		// Rejection cases — the wire round-trip must be byte-for-byte
		// reversible. strconv.ParseInt would happily accept several of
		// these and silently canonicalize, so any caller passing a
		// non-canonical value would never see the same id back.
		{"empty", "", 0, true},
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"plus prefix", "+123", 0, true},
		{"leading zero", "01", 0, true},
		{"leading whitespace", " 42", 0, true},
		{"trailing whitespace", "42 ", 0, true},
		{"hex prefix", "0x10", 0, true},
		{"unicode digits", "٠١", 0, true}, // Arabic-Indic 0,1
		{"not a number", "abc", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := parseRoleID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got id=%d", tc.input, id)
				}
				var fleetErr fleeterror.FleetError
				if !errors.As(err, &fleetErr) || fleetErr.GRPCCode != connect.CodeInvalidArgument {
					t.Fatalf("expected InvalidArgument, got %v", err)
				}
				if !strings.Contains(err.Error(), "invalid role_id") {
					t.Fatalf("expected 'invalid role_id' in error, got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if id != tc.wantID {
				t.Fatalf("want id=%d, got %d", tc.wantID, id)
			}
		})
	}
}
