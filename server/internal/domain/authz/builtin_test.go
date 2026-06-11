package authz

import "testing"

func TestBuiltinRoles_AdminSeedsFirmwareUpdateAndReboot(t *testing.T) {
	admin := builtinRoleByKey(t, BuiltinKeyAdmin)
	perms := permissionSet(admin.SeedPermissions)

	for _, key := range []string{PermMinerFirmwareUpdate, PermMinerReboot} {
		if !perms[key] {
			t.Fatalf("ADMIN seed permissions missing %q", key)
		}
	}
}

func TestBuiltinRoles_FieldTechDoesNotSeedFirmwareUpdateOrReboot(t *testing.T) {
	fieldTech := builtinRoleByKey(t, BuiltinKeyFieldTech)
	perms := permissionSet(fieldTech.SeedPermissions)

	for _, key := range []string{PermMinerFirmwareUpdate, PermMinerReboot} {
		if perms[key] {
			t.Fatalf("FIELD_TECH seed permissions unexpectedly include %q", key)
		}
	}
}

func builtinRoleByKey(t *testing.T, key BuiltinKey) BuiltinRoleSpec {
	t.Helper()
	for _, role := range BuiltinRoles() {
		if role.Key == key {
			return role
		}
	}
	t.Fatalf("builtin role %q not found", key)
	return BuiltinRoleSpec{}
}

func permissionSet(keys []string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, key := range keys {
		out[key] = true
	}
	return out
}
