// Pure-function coverage for the validation, dedup, read-pairing, and
// error-mapping helpers. Transactional behavior (privilege-parity,
// built-in immutability, delete-with-assignments, cross-org guard,
// deactivated-caller revocation) lives in service_integration_test.go
// against a real Postgres via testutil.GetTestDB.
package authz

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
)

func TestValidateRoleName(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantCode connect.Code
	}{
		{"valid", "Operator", 0},
		{"valid with spaces", "Site Operator", 0},
		{"empty", "", connect.CodeInvalidArgument},
		{"too long ascii", strings.Repeat("x", maxRoleNameLength+1), connect.CodeInvalidArgument},
		{"max length boundary ascii", strings.Repeat("x", maxRoleNameLength), 0},
		// Multi-byte names: the cap is character-counted, not byte-counted.
		// 130 CJK chars (each 3 bytes in UTF-8) is 390 bytes — would fail
		// a len()-based check but fits the 255-character schema and the
		// protovalidate string.max_len rune-based wire bound.
		{"130 CJK characters within rune cap", strings.Repeat("中", 130), 0},
		{"100 emoji within rune cap", strings.Repeat("😀", 100), 0},
		{"max length boundary unicode", strings.Repeat("中", maxRoleNameLength), 0},
		{"over rune cap rejected", strings.Repeat("中", maxRoleNameLength+1), connect.CodeInvalidArgument},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRoleName(tc.input)
			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			var fleetErr fleeterror.FleetError
			if !errors.As(err, &fleetErr) || fleetErr.GRPCCode != tc.wantCode {
				t.Fatalf("expected code %v, got %v (%v)", tc.wantCode, fleetErr.GRPCCode, err)
			}
		})
	}
}

func TestValidateAndDedupKeys(t *testing.T) {
	t.Run("dedup and sort", func(t *testing.T) {
		out, err := validateAndDedupKeys([]string{PermMinerReboot, PermFleetRead, PermMinerReboot, PermMinerRead})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{PermFleetRead, PermMinerRead, PermMinerReboot}
		if !sliceEq(out, want) {
			t.Fatalf("want %v, got %v", want, out)
		}
	})
	t.Run("unknown key", func(t *testing.T) {
		_, err := validateAndDedupKeys([]string{PermFleetRead, "miner:teleport"})
		var fleetErr fleeterror.FleetError
		if !errors.As(err, &fleetErr) || fleetErr.GRPCCode != connect.CodeInvalidArgument {
			t.Fatalf("want InvalidArgument, got %v", err)
		}
	})
	t.Run("empty slice", func(t *testing.T) {
		out, err := validateAndDedupKeys(nil)
		if err != nil || len(out) != 0 {
			t.Fatalf("want empty/nil, got %v err=%v", out, err)
		}
	})
}

func TestValidateReadPairing(t *testing.T) {
	t.Run("read alone is fine", func(t *testing.T) {
		if err := validateReadPairing([]string{PermFleetRead}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("miner action needs miner:read AND fleet:read", func(t *testing.T) {
		err := validateReadPairing([]string{PermMinerReboot, PermMinerRead})
		if err == nil {
			t.Fatal("expected error for missing fleet:read")
		}
		if !strings.Contains(err.Error(), PermFleetRead) {
			t.Fatalf("error should mention fleet:read: %v", err)
		}
	})
	t.Run("miner action with both reads passes", func(t *testing.T) {
		if err := validateReadPairing([]string{PermMinerReboot, PermMinerRead, PermFleetRead}); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})
	t.Run("pool:manage needs pool:read", func(t *testing.T) {
		err := validateReadPairing([]string{PermPoolManage})
		if err == nil {
			t.Fatal("expected error for missing pool:read")
		}
		if !strings.Contains(err.Error(), PermPoolRead) {
			t.Fatalf("error should mention pool:read: %v", err)
		}
	})
	t.Run("pool:manage with pool:read passes", func(t *testing.T) {
		if err := validateReadPairing([]string{PermPoolManage, PermPoolRead}); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})
	t.Run("manage-only resources skip the pair check", func(t *testing.T) {
		// role:manage and apikey:manage have no :read partner in the
		// catalog. validateReadPairing must skip the pair check for
		// them; otherwise the Roles UI cannot save any role that grants
		// either permission.
		if err := validateReadPairing([]string{PermRoleManage}); err != nil {
			t.Fatalf("role:manage should not require role:read: %v", err)
		}
		if err := validateReadPairing([]string{PermAPIKeyManage}); err != nil {
			t.Fatalf("apikey:manage should not require apikey:read: %v", err)
		}
		if err := validateReadPairing([]string{PermRoleManage, PermAPIKeyManage}); err != nil {
			t.Fatalf("combined manage-only permissions should not require partners: %v", err)
		}
	})
}

func TestMapRolePersistError(t *testing.T) {
	cases := []struct {
		name     string
		input    error
		wantCode connect.Code
		wantSub  string
	}{
		{
			"duplicate name (typed pq)",
			&pgconn.PgError{Code: "23505", ConstraintName: "uq_role_org_custom_name", Message: `duplicate key value violates unique constraint "uq_role_org_custom_name"`},
			connect.CodeInvalidArgument,
			"already exists",
		},
		{
			"reserved name (typed pq)",
			&pgconn.PgError{Code: "23514", ConstraintName: "chk_role_custom_name_not_reserved", Message: `new row violates check constraint "chk_role_custom_name_not_reserved"`},
			connect.CodeInvalidArgument,
			"reserved",
		},
		{
			"unique violation on a different constraint falls through to Internal",
			&pgconn.PgError{Code: "23505", ConstraintName: "some_other_unique_idx", Message: "different unique"},
			connect.CodeInternal,
			"persist role",
		},
		{
			"plain non-pg error",
			errors.New("connection refused"),
			connect.CodeInternal,
			"persist role",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mapRolePersistError(tc.input)
			var fleetErr fleeterror.FleetError
			if !errors.As(err, &fleetErr) || fleetErr.GRPCCode != tc.wantCode {
				t.Fatalf("want code %v, got %v (%v)", tc.wantCode, fleetErr.GRPCCode, err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected %q in %q", tc.wantSub, err.Error())
			}
		})
	}
}

func TestNullStringIfNonEmpty(t *testing.T) {
	if ns := nullStringIfNonEmpty(""); ns.Valid {
		t.Fatalf("empty string should produce Valid=false, got %+v", ns)
	}
	if ns := nullStringIfNonEmpty("ops"); !ns.Valid || ns.String != "ops" {
		t.Fatalf("non-empty should produce Valid=true with the same string, got %+v", ns)
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
