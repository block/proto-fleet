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

func TestBuiltinRoles_AdminDoesNotSeedInstanceUpdate(t *testing.T) {
	admin := builtinRoleByKey(t, BuiltinKeyAdmin)
	perms := permissionSet(admin.SeedPermissions)

	if perms[PermInstanceUpdate] {
		t.Fatalf("ADMIN seed permissions unexpectedly include %q", PermInstanceUpdate)
	}
}

func TestBuiltinRoles_SuperAdminSeedsInstanceUpdate(t *testing.T) {
	superAdmin := builtinRoleByKey(t, BuiltinKeySuperAdmin)
	perms := permissionSet(superAdmin.SeedPermissions)

	if !perms[PermInstanceUpdate] {
		t.Fatalf("SUPER_ADMIN seed permissions missing %q", PermInstanceUpdate)
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

func TestBuiltinRoles_AdminAndFieldTechSeedMaintenancePermissions(t *testing.T) {
	for _, roleKey := range []BuiltinKey{BuiltinKeyAdmin, BuiltinKeyFieldTech} {
		role := builtinRoleByKey(t, roleKey)
		perms := permissionSet(role.SeedPermissions)
		for _, permission := range []string{PermMaintenanceRead, PermMaintenanceManage} {
			if !perms[permission] {
				t.Errorf("%s seed permissions missing %q", roleKey, permission)
			}
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
