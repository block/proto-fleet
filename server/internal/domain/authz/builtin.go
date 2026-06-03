package authz

// BuiltinKey is the stable identifier code uses for a built-in role.
// Seeded values are stored in role.builtin_key so seed reordering or
// migration replays do not break references.
type BuiltinKey string

const (
	// BuiltinKeySuperAdmin denotes the immutable, fully reconciled
	// built-in. Its permission set is always AllPermissions().
	BuiltinKeySuperAdmin BuiltinKey = "SUPER_ADMIN"

	// BuiltinKeyAdmin denotes the additive-reconciled built-in for org
	// admins. Like every built-in, it cannot be modified through the
	// authz RPCs; the additive mode only controls how the seed formula
	// grows across releases (new catalog keys are added on each boot).
	BuiltinKeyAdmin BuiltinKey = "ADMIN"

	// BuiltinKeyFieldTech denotes the additive-reconciled built-in
	// shipped for field technicians. Not editable through the RPCs;
	// see BuiltinKeyAdmin for the additive-mode rationale.
	BuiltinKeyFieldTech BuiltinKey = "FIELD_TECH"
)

// BuiltinReconcileMode controls whether reconciliation will remove
// permission rows that aren't in the seed formula. SUPER_ADMIN is the
// only role with mode=ReconcileFull. ADMIN and FIELD_TECH are
// ReconcileAdditive so seed-formula growth across releases adds keys
// without retroactively trimming any that were seeded previously.
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
			Description:     "Org admin. Built-in role; cannot be modified.",
			Mode:            ReconcileAdditive,
			SeedPermissions: adminSeedPermissions(),
		},
		{
			Key:             BuiltinKeyFieldTech,
			Name:            "FIELD_TECH",
			Description:     "Field tech. Read fleet data, blink the locator LED, download logs, manage racks. Built-in role; cannot be modified.",
			Mode:            ReconcileAdditive,
			SeedPermissions: fieldTechSeedPermissions(),
		},
	}
}

// adminSeedPermissions is the formula AllPermissions() − {role:manage}.
// ADMIN holds user:read for the team roster and user:manage to mutate
// non-elevated users. role:manage is excluded for two compounding
// reasons: it can grant arbitrary permissions (so it's the catalog's
// ultimate authority key), and the privilege-parity check in the auth
// domain layer (see requireCallerCanManageTarget) uses *org-scope
// role:manage* as the bypass for peer-tier management — only callers
// who hold it can mutate a target whose permissions equal their own.
// Without that gate, ADMIN could create new ADMINs (the target's seed
// equals the caller's) and walk away with the new account's temp
// password.
//
// Computed from the catalog so adding a new permission in catalog.go
// automatically grows the seed for *new* orgs; seed-formula changes
// that need to reach existing orgs ship as explicit one-off migrations
// (see migrations/000060_backfill_admin_user_manage.up.sql for the
// user:read/user:manage backfill).
func adminSeedPermissions() []string {
	excluded := map[string]bool{
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
