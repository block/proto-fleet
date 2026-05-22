package authz

// BuiltinKey is the stable identifier code uses for a built-in role.
// Seeded values are stored in role.builtin_key so seed reordering or
// migration replays do not break references.
type BuiltinKey string

const (
	// BuiltinKeySuperAdmin denotes the immutable, fully reconciled
	// built-in. Its permission set is always AllPermissions().
	BuiltinKeySuperAdmin BuiltinKey = "SUPER_ADMIN"

	// BuiltinKeyAdmin denotes the editable, additive-only built-in.
	// Operator edits survive restart; new catalog keys in the seed
	// formula are added on each boot.
	BuiltinKeyAdmin BuiltinKey = "ADMIN"

	// BuiltinKeyFieldTech denotes the editable, additive-only built-in
	// shipped for field technicians.
	BuiltinKeyFieldTech BuiltinKey = "FIELD_TECH"
)

// BuiltinReconcileMode controls whether reconciliation will remove
// permission rows that aren't in the seed formula. SUPER_ADMIN is the
// only role with mode=ReconcileFull. ADMIN and FIELD_TECH are
// ReconcileAdditive so operator edits persist.
type BuiltinReconcileMode int

const (
	ReconcileFull BuiltinReconcileMode = iota
	ReconcileAdditive
)

// BuiltinRoleSpec is the in-code definition of a built-in role. The
// reconciler in reconcile.go converges the database state to match
// these specs at every startup.
type BuiltinRoleSpec struct {
	Key         BuiltinKey
	Name        string
	Description string
	Mode        BuiltinReconcileMode

	// SeedPermissions is the set of keys the reconciler will ensure are
	// present on the role. For ReconcileFull, anything outside this set
	// is removed; for ReconcileAdditive, missing keys are added but
	// extras (operator additions) are left alone.
	SeedPermissions []string
}

// BuiltinRoles returns the canonical specs in display order. The
// returned slice is a fresh copy on every call.
func BuiltinRoles() []BuiltinRoleSpec {
	return []BuiltinRoleSpec{
		{
			Key:             BuiltinKeySuperAdmin,
			Name:            "SUPER_ADMIN",
			Description:     "Full system access. Cannot be modified.",
			Mode:            ReconcileFull,
			SeedPermissions: AllPermissions(),
		},
		{
			Key:             BuiltinKeyAdmin,
			Name:            "ADMIN",
			Description:     "Org admin. Editable by a SUPER_ADMIN.",
			Mode:            ReconcileAdditive,
			SeedPermissions: adminSeedPermissions(),
		},
		{
			Key:             BuiltinKeyFieldTech,
			Name:            "FIELD_TECH",
			Description:     "Field tech. Read fleet data, blink the locator LED, download logs, manage racks. Editable by a SUPER_ADMIN.",
			Mode:            ReconcileAdditive,
			SeedPermissions: fieldTechSeedPermissions(),
		},
	}
}

// adminSeedPermissions is the formula AllPermissions() − {user:manage,
// role:manage}. ADMIN holds user:read so an org admin can view the team
// roster, but cannot create/reset/deactivate users — those gates require
// SUPER_ADMIN. Computed from the catalog so adding a new permission in
// catalog.go automatically grows ADMIN (subject to the additive-only
// contract — existing operator edits are preserved).
func adminSeedPermissions() []string {
	excluded := map[string]bool{
		PermUserManage: true,
		PermRoleManage: true,
	}
	all := AllPermissions()
	out := make([]string, 0, len(all))
	for _, key := range all {
		if !excluded[key] {
			out = append(out, key)
		}
	}
	return out
}

// fieldTechSeedPermissions is an explicit set. Unlike ADMIN, catalog
// growth does NOT silently widen FIELD_TECH — operators must opt in
// to new permissions by editing the role or by a future release
// updating this list.
func fieldTechSeedPermissions() []string {
	return []string{
		PermFleetRead,
		PermMinerRead,
		PermMinerBlinkLED,
		PermMinerDownloadLogs,
		PermRackRead,
		PermRackManage,
	}
}
