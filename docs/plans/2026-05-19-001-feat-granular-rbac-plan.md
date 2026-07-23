---
title: Granular RBAC with scoped role assignments and custom roles
date: 2026-05-19
status: accepted
type: plan
---

## Summary

Proto Fleet today has two hardcoded roles — `SUPER_ADMIN` and `ADMIN` — gated
by a single `RequireAdmin` middleware. Any user that isn't ADMIN/SUPER_ADMIN
has no usable place in the app. We want to add a third built-in role
(`FIELD_TECH`) that can see fleet data and perform a curated set of physical
actions (starting with `BlinkLED`), and we want admins to be able to define
their own roles by picking from a catalog of resource:action permissions.

This plan replaces the hardcoded role check with a permission-based model:

- A **permission catalog** of ~25 resource:action verbs
  (`miner:read`, `miner:blink_led`, `miner:reboot`, `user:manage`,
  `site:manage`, …).
- A **role** is a named bag of permissions. Three roles ship seeded:
  SUPER_ADMIN (every permission, **immutable**), ADMIN (everything
  except user/role management, **editable** by SUPER_ADMIN), FIELD_TECH
  (fleet read + rack management + a small physical-action set,
  **editable** by SUPER_ADMIN). Custom roles are rows created by users
  holding `role:manage` (SUPER_ADMIN by default) that pick permissions
  from the catalog.
- A **role assignment** binds a user to a role within a **scope** — org
  or site. Existing assignments are migrated as org-scope; site-scope
  is available immediately because multi-site Phase 1 already shipped.
  Building-scope is deferred to a follow-up plan.
- Server-side enforcement moves from `RequireAdmin(ctx, action)` to
  `RequirePermission(ctx, "miner:reboot", resourceCtx)`. Every guarded RPC
  declares the permission(s) it needs; the resolver evaluates the user's
  assignments against the resource's scope.
- The client receives `permissions: string[]` in `UserInfo` and gates UI
  through a `hasPermission(p)` helper instead of comparing role names.

Out of scope: any ABAC policy DSL, time- or IP-based attributes, plugin-side
authorization (plugins continue to receive no user context), audit-log
overhaul beyond the existing `record_use` shape, SSO/OIDC.

**Multi-site precondition (already met):** Multi-site Phase 1
([plan](./2026-05-05-multi-site-support-plan.md)) is already on `main` —
migrations `000043_create_site_table` through
`000049_add_site_id_to_device_set_rack` shipped. Both `site` and
`building` use a `(id, organization_id)` composite-unique constraint that
this plan's `user_organization_role.scope_id` FKs target. Per-site RBAC
was a non-goal of that plan and is delivered here.

---

## Goals

- An admin can grant a teammate read-only fleet access plus the ability to
  `BlinkLED` on miners, without granting any other write capability. That
  user lands on the dashboard with the same data views as today; reboot,
  firmware update, and pool/cooling controls are absent from their UI and
  rejected at the server if attempted.
- A user with `role:manage` (SUPER_ADMIN by default) can create a
  custom role named e.g. "Pool Operator" by picking permissions
  (`miner:read`, `miner:update_pools`) from a catalog, and assign that
  role to a user. The built-in ADMIN role does not include `role:manage`;
  org owners can grant it via a custom role if they want to delegate.
- A role assignment can be scoped to an organization or to a specific
  site. A field tech assigned at "Site A" sees and acts on Site-A
  miners only. Building-level scoping is deferred to a follow-up plan
  once site-scope is operator-validated as insufficient.
- The two existing roles (`SUPER_ADMIN`, `ADMIN`) keep their current
  behavior. No existing user loses access; no manual re-assignment is
  required.
- A single permission-check code path replaces the scattered
  `RequireAdmin(ctx, "<verb>")` calls. Adding a new RPC means declaring
  its required permission in one place.

## Non-goals

### Deferred for later

- Permission inheritance / hierarchies between roles. Each role has a flat
  permission set; if a custom role should "be like ADMIN but lose X" it
  must be enumerated explicitly.
- Per-field permissions (e.g. "read miner serial but not hashrate"). The
  catalog is action-level.
- Time-bounded or network-bounded role assignments.
- Audit log redesign. We continue to use existing structured logs and the
  `apikey` `record_use` table; we add log lines on permission denials and
  on role/assignment mutations but do not build a UI for them.
- Self-service role requests, role approval workflows.

### Outside this product's identity

- ABAC policy languages (OPA, Cedar, Casbin). Resource-scoped RBAC covers
  the intended use cases at this product's scale.
- Plugins receiving auth context. The plugin boundary stays clean; server
  enforces permissions before invoking plugin methods.
- Federated identity / SSO.

---

## High-Level Technical Design

*This section illustrates the intended approach and is directional guidance
for review, not implementation specification.*

### Data model shape

```
permission                role                    user_organization_role
----------                ----                    ----------------------
id                        id                      id
key (miner:reboot)        name                    user_id
description               description             organization_id
                          is_builtin              role_id
                          builtin_key             scope_type   (org|site)
                                                  scope_id     (nullable;
                                                                composite FK
                                                                to site(id,
                                                                organization_id)
                                                                when site)

role_permission
---------------
role_id
permission_id
```

`user_organization.role_id` is dropped after data is migrated into
`user_organization_role`. Built-in roles are insert-once on migration
and carry a stable `builtin_key` (`SUPER_ADMIN`, `ADMIN`, `FIELD_TECH`)
so code can resolve them by key, not by primary key. SUPER_ADMIN cannot
be edited or deleted at runtime; ADMIN and FIELD_TECH can be edited
through the same RPC as custom roles (only the SUPER_ADMIN row is
guarded with `BUILTIN_ROLE_IMMUTABLE`), but neither can be deleted —
every org always has a named ADMIN and FIELD_TECH to assign.

### Resolver flow

```
request arrives
  → AuthInterceptor populates session.Info (BOTH session AND API-key paths)
  → PermissionResolver.LoadEffective(userID, orgID)
      • fetch all (role, scope_type, scope_id) assignments
      • fetch permissions per role
      • stash result on context; cached for request lifetime
  → handler verifies resource.organization_id == session.Info.OrgID
    (cross-org IDOR guard) BEFORE building ResourceContext
  → handler calls RequirePermission(ctx, "miner:reboot", ResourceContext{site_id})
      • pick the narrowest matching assignments for the resource's scope:
        if any site-scoped assignment matches the resource's site, only
        those site-scoped assignments are consulted; otherwise fall back
        to org-scoped assignments
      • return ALLOW if any consulted role covers the action
      • DENY by default; log denial with action + scope at warn
      • FAIL-CLOSED: if the procedure is authenticated but not in any
        allowlist AND not in the permission map, the middleware returns
        PermissionDenied. The CI contract test is the audit signal, not
        the gate of last resort.
```

Scope containment rule: `org` ⊃ `site`. An assignment at org scope grants
the action everywhere within that org; an assignment at a site grants it
for resources at that site. A resource without a site (org-scoped action,
e.g. `user:manage`) is only satisfied by an org-scoped assignment. The
two-level model is deliberate for v1 — building-level scope is deferred
to a follow-up once site-scope is operator-validated.

**Org-scope-plus-site-scope semantics: narrowing
(intersection-on-overlap).** When a user has BOTH an org-scoped
assignment AND a site-scoped assignment, the site-scoped assignment
overrides the org-scoped grant at that site, while the org-scoped
grant continues to apply at every other site. This lets an admin
grant broad org-level access and then narrow a user at a specific
site by adding a smaller site-scoped role, without first removing the
org-scoped assignment. The behavior is surfaced in the RoleEditor UI
and the assignment confirmation dialog.

**Synthesized sessions (scheduler).** Internal callers with
`Actor=ActorScheduler` short-circuit the resolver and `RequirePermission`
returns ALLOW for any catalog key. These paths have no UserID in the
assignment table; they are trusted by virtue of running in-process.

**Denial payload shape.** The structured `PermissionDenied` response
carries exactly `{"required": "<key>", "scope": <caller-supplied
ResourceContext>}`. The scope field echoes the caller's request input
only; it never includes server-side assignment IDs, role names, or the
caller's effective permission list.

**API-key path.** Both `authenticateWithSession` and
`authenticateWithApiKey` call `LoadEffective` and stash the result on
the context. API keys inherit the user's *current* effective permission
set on every request (live inheritance). Snapshot-at-issue and per-key
permission declaration are deferred. The resolver is fail-closed: if
`EffectivePermissions` is absent from context when `RequirePermission`
is invoked, the call returns `Internal` (never ALLOW).

### UserInfo response shape

```proto
message UserInfo {
  // existing fields …
  string role = 5;                   // kept; reports primary role name
  repeated string permissions = 7;   // new — effective permission keys
                                     //  across all current assignments
}
```

For the resource-scope picture, the client doesn't need full assignment
detail in MVP: it gates UI on whether the permission appears at all and
lets the server reject scoped misuse. A later iteration can return a richer
`permissions_by_scope` map if the UI grows scope-aware affordances.

### Permission catalog (initial)

| Key | Notes |
|---|---|
| `fleet:read` | Required for any list/dashboard view (miner list, telemetry). Implicit floor — a role with any action permission must also hold `fleet:read`; enforced at role save (see U8). |
| `miner:read` | Per-row detail view, status snapshot, error history. Required alongside any `miner:*` action permission. |
| `miner:blink_led` | Visual locate. **Required for FIELD_TECH minimum.** |
| `miner:reboot` | |
| `miner:start_mining` / `miner:stop_mining` | |
| `miner:update_pools` | |
| `miner:update_worker_names` | |
| `miner:rename` / `miner:delete` | |
| `miner:set_cooling_mode` / `miner:set_power_target` | |
| `miner:firmware_update` | |
| `miner:download_logs` | |
| `miner:update_password` | Device-local web UI password |
| `miner:unpair` / `miner:pair` | |
| `miner:export_csv` | |
| `rack:read` / `rack:manage` | List racks at a site / create, rename, delete racks and move miners between them. Site-scoped resource. |
| `site:read` / `site:manage` | CRUD on sites/buildings |
| `serverlog:read` | View server-side logs (today's `serverlog` admin gate) |
| `curtailment:read` / `curtailment:manage` | View curtailment policies and preview impact / create-edit-delete policies |
| `fleetnode:read` / `fleetnode:manage` | View fleet-node state / fleet-node admin operations (today's `fleetnodeadmin` admin gate) |
| `apikey:manage` | List, create, and revoke API keys (scoped to issuer's caller). No separate `apikey:read` — the API Keys page lives under the route-guarded Settings area, so a viewer-only role couldn't reach it; the split has no usable surface. |
| `user:read` / `user:manage` | List users, create/reset/deactivate. SUPER_ADMIN only by default. |
| `role:manage` | Create/edit/delete custom roles **and** edit the ADMIN/FIELD_TECH built-ins. SUPER_ADMIN only by default. |

**Read pairing rule (enforced at role save).** Every action permission has
a corresponding read permission that must be present in the same role:
- any `miner:*` action requires `miner:read` and `fleet:read`
- `rack:manage` requires `rack:read` and `fleet:read`
- `site:manage` requires `site:read` and `fleet:read`
- `curtailment:manage` requires `curtailment:read`
- `fleetnode:manage` requires `fleetnode:read`
- `user:manage` requires `user:read`

This rule is implemented in U8's role-save handler. It prevents the
footgun of a custom role that grants `miner:reboot` without `miner:read`,
which would let a user reboot miners but see no listing to find one.

**List-endpoint semantics: filter, don't reject.** A user holding
`fleet:read` at site-scope (and not org-scope) gets a list endpoint
response **filtered** to the sites where they have the permission, not a
page-level `PermissionDenied`. Per-row gating (e.g. hiding a Reboot
button on a miner where the caller lacks the action) is still enforced
at the handler.

**Administrative permission set.** The following five keys define the
"administrative" surface — anything that lives under `/settings/*` in
the client:

```
ADMIN_PERMISSIONS = {
  user:manage, role:manage, site:manage, apikey:manage, serverlog:read
}
```

Top-nav items (Dashboard, Curtailment, Fleet Nodes, etc.) have their
own per-item permission gates and are **not** part of this set.
Curtailment and fleet-node admin live in the top nav, so their `:read`
keys are not administrative; a "Curtailment Viewer" custom role works
without granting Settings access.

**Primary-nav and route-guard rules.** Every primary-nav entry has a
required permission predicate; the entry is hidden when the predicate
is false, AND the corresponding route is redirected at the layout
level when the predicate is false. Nav and route always agree — there
is no "hidden in nav but reachable by direct link" state.

| Primary nav | Required permission(s) | Behavior when false |
|---|---|---|
| Dashboard / Fleet | any `fleet:read` assignment | Nav hidden; `/` redirects to first available primary nav, or to the "no resources in scope" terminal page if none. |
| Curtailment | `curtailment:read` (any scope) | Nav hidden; `/curtailment/*` redirects. |
| Fleet Nodes | `fleetnode:read` (any scope) | Nav hidden; `/fleet-nodes/*` redirects. |
| Settings | any key in `ADMIN_PERMISSIONS` | Nav hidden; `/settings/*` redirects. |

The redirect target is the first available primary nav for the user
(in declared order). If literally none are available, the user lands
on the "no resources in scope" terminal page, which surfaces a
contact-your-admin affordance and a sign-out button.

The server-side per-RPC permission gates remain the security boundary
— the redirect is purely UX hygiene. A user who reaches a route via
back-button or stale URL still hits the server-side `PermissionDenied`
on any guarded RPC.

Final list lands in U1; the catalog is a code constant, not user-editable.

### Built-in role permission sets

- **SUPER_ADMIN** — every key in the catalog. Computed as "all" at seed
  time and **fully reconciled on every startup** so the set stays
  correct when the catalog grows. Not editable at runtime — its
  definition is "everything," and that invariant is load-bearing for
  the org-scope-SUPER_ADMIN floor (U8) and the privilege-parity rule.
- **ADMIN** — seeded as every key except `user:manage`, `user:read`,
  `role:manage`. Editable at runtime by any user with `role:manage`
  (SUPER_ADMIN by default). See "Built-in editability" below.
- **FIELD_TECH** — seeded with `fleet:read`, `miner:read`,
  `miner:blink_led`, `miner:download_logs`, `rack:read`, `rack:manage`.
  Lets a tech identify a miner physically, inspect what's wrong, and
  organize the physical layout (create/rename racks, move miners
  between them) without changing miner state. Editable at runtime by
  any user with `role:manage`. Widening the *seed* to include
  `miner:reboot`, `miner:start_mining`, and `miner:stop_mining` is
  **Open Question Q1**; until Q1 lands, an operator can add those
  permissions to their org's FIELD_TECH via the role editor.

### Built-in editability

SUPER_ADMIN is immutable. ADMIN and FIELD_TECH are **editable** through
the same `UpdateCustomRole` RPC used for custom roles; the handler
distinguishes only SUPER_ADMIN as the locked row. This lets operators
tune the practical scope of their team's roles without minting a custom
role-of-roles. To keep edits from being silently overwritten, startup
reconciliation is **additive-only** for ADMIN and FIELD_TECH:

- New catalog keys added in a future release are appended to ADMIN
  (unless excluded from its seed formula) and to a `field_tech_seed`
  marker — never removed.
- Existing `role_permission` rows on ADMIN/FIELD_TECH are left alone.
- SUPER_ADMIN remains fully reconciled (additions and removals) — its
  contract is "everything in the current catalog."

Two distinct error codes are reserved for built-in mutation attempts:
`BUILTIN_ROLE_IMMUTABLE` for any update or delete on SUPER_ADMIN, and
`BUILTIN_ROLE_NON_DELETABLE` for a delete attempt on ADMIN or
FIELD_TECH (which are editable but not deletable).

---

## Output Structure

New paths created or expanded by this plan:

```
server/
  migrations/
    000NNN_create_permission_tables.up.sql
    000NNN_create_permission_tables.down.sql
    000NNN_seed_builtin_roles.up.sql
    000NNN_seed_builtin_roles.down.sql
    000NNN_migrate_user_organization_to_assignments.up.sql
    000NNN_migrate_user_organization_to_assignments.down.sql
  sqlc/queries/
    permission.sql
    role.sql                       (extended)
    user_organization_role.sql     (new)
  internal/domain/
    authz/
      catalog.go                   permission key constants + ALL set
      resolver.go                  PermissionResolver
      service.go                   custom-role CRUD domain
      builtin.go                   built-in role definitions
  internal/handlers/
    middleware/
      permission.go                RequirePermission, ResourceContext
      rpc_permissions.go           Connect-procedure → permission map
    role/
      service.go                   role/permission management RPC handlers
  cmd/fleetd/
    main.go                        (+ startup role reconciliation call)
proto/
  authz/v1/
    authz.proto                    Role/Permission/Assignment messages + service
  auth/v1/
    auth.proto                     (UserInfo gains permissions[])
client/src/protoFleet/
  store/slices/authSlice.ts        (+ permissions, setPermissions)
  store/hooks/useAuth.ts           (+ usePermissions, useHasPermission)
  store/hooks/useAuthentication.ts (call setPermissions on auth)
  features/settings/
    components/
      Roles.tsx                    list + create/edit/delete custom roles
      RoleEditor.tsx               permission checklist modal
      PermissionPicker.tsx
```

This tree is a scope declaration of the expected output shape, not a
constraint — per-unit `**Files:**` lists are authoritative.

---

## Implementation Units

### U1. Permission catalog

**Goal:** Define the full list of permission keys as a code constant.

**Requirements:** All subsequent units reference this catalog. Custom roles
in U9 select from it; the resolver in U6 looks up keys from it; the
frontend in U11 displays the same labels.

**Dependencies:** None.

**Files:**
- `server/internal/domain/authz/catalog.go` (new)
- `proto/authz/v1/authz.proto` (new — defines `Permission` message with
  `key` and `description`)

**Approach:**
- Define each permission as a string constant (e.g.
  `const PermMinerBlinkLED = "miner:blink_led"`).
- Export `AllPermissions()` returning the canonical slice.
- Group permissions by resource for UI display
  (`fleet`, `miner`, `site`, `apikey`, `user`, `role`).
- Provide a `human-readable` description for each, used by the catalog
  RPC and the admin UI.

**Patterns to follow:** Existing `server/internal/domain/auth/service.go`
constant pattern (`SuperAdminRoleName`, `AdminRoleName`).

**Test scenarios:**
- `AllPermissions()` returns no duplicates and covers every constant
  defined in the file (reflection or string-table check).
- Every permission key matches the regex `^[a-z_]+:[a-z_]+$`.
- `Test expectation: catalog completeness` — the test fails if a new
  constant is added but not registered in `AllPermissions()`.

**Verification:** Catalog is importable from `domain/authz`; `just gen`
regenerates `authz.pb.go` without errors.

---

### U2. Schema migration: permissions, role join, multi-assignment

**Goal:** Add the database tables for permissions, role-to-permission join,
and user-to-role-with-scope assignments. Mark the existing `role` table as
builtin-aware.

**Requirements:** Foundation for everything below.

**Dependencies:** U1 (catalog drives the seed of `permission` rows in U4).

**Files:**
- `server/migrations/000NNN_create_permission_tables.up.sql` (new)
- `server/migrations/000NNN_create_permission_tables.down.sql` (new)

**Approach:**

```
ALTER TABLE role
  ADD COLUMN is_builtin BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN builtin_key VARCHAR(64) NULL,
  ADD CONSTRAINT uq_role_builtin_key UNIQUE (builtin_key);

CREATE TABLE permission (
  id BIGSERIAL PRIMARY KEY,
  key VARCHAR(128) NOT NULL,
  description TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT uq_permission_key UNIQUE (key)
);

CREATE TABLE role_permission (
  role_id BIGINT NOT NULL REFERENCES role(id) ON DELETE CASCADE,
  permission_id BIGINT NOT NULL REFERENCES permission(id) ON DELETE RESTRICT,
  PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE user_organization_role (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  organization_id BIGINT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  role_id BIGINT NOT NULL REFERENCES role(id) ON DELETE RESTRICT,
  scope_type VARCHAR(16) NOT NULL CHECK (scope_type IN ('org','site')),
  scope_id BIGINT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMPTZ NULL,
  CONSTRAINT uq_user_org_role_scope UNIQUE
    (user_id, organization_id, role_id, scope_type, scope_id),
  CONSTRAINT chk_scope_id_matches_type CHECK (
    (scope_type = 'org'  AND scope_id IS NULL) OR
    (scope_type = 'site' AND scope_id IS NOT NULL)
  ),
  -- Composite FK uses the (id, organization_id) unique key on `site`
  -- shipped by multi-site Phase 1 migration 000043. Ensures the scoped
  -- site belongs to the same org as the assignment — DB-enforced
  -- tenant isolation, not application-layer only.
  CONSTRAINT fk_user_org_role_site FOREIGN KEY (scope_id, organization_id)
    REFERENCES site(id, organization_id) ON DELETE CASCADE
    DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX idx_user_organization_role_user_org
  ON user_organization_role(user_id, organization_id)
  WHERE deleted_at IS NULL;
```

The FK is `DEFERRABLE INITIALLY DEFERRED` so a transactional re-assignment
that drops then inserts inside one tx doesn't fail mid-transaction; the
constraint is evaluated at commit. Building-scope is deferred to a
follow-up plan; when it ships, this migration's CHECK constraint is
relaxed to include `'building'` and a second composite FK to
`building(id, organization_id)` is added.

`user_organization.role_id` is **not** dropped in this migration. U5
renames it to `role_id_deprecated_do_not_use` and adds a trigger that
raises on any INSERT/UPDATE referencing it. U12 then drops the column
cleanly once no readers remain.

**Patterns to follow:** Existing migration style in
`server/migrations/000002_create_core_tables.up.sql` — use `update_updated_at_column()`
trigger for the assignment table.

**Test scenarios:**
- Apply migration on a clean DB; tables exist with expected columns,
  indexes, and constraints (golang-migrate dry run + introspection).
- Roll back the migration; tables drop cleanly; `role` retains its
  original columns.
- The unique constraint `uq_user_org_role_scope` rejects duplicate
  (user, org, role, scope) assignments.
- `chk_scope_id_matches_type` rejects `('org', 42)` and `('site', NULL)`.
- The composite site FK rejects a row where `scope_id` is a site
  belonging to a different organization (cross-org isolation enforced
  at the DB layer).
- `Test expectation: integration` — a separate test under the existing
  migration-test harness exercises up+down idempotency.

**Verification:** `just gen` regenerates sqlc artifacts cleanly; `just dev`
applies the migration; `psql` confirms schema.

---

### U3. sqlc queries for permissions and assignments

**Goal:** Generate Go bindings for reading the new tables and managing
custom roles + assignments.

**Requirements:** Used by U6 (resolver), U8 (role mgmt RPC), U9 (custom
role CRUD).

**Dependencies:** U2.

**Files:**
- `server/sqlc/queries/permission.sql` (new)
- `server/sqlc/queries/role.sql` (extended — add `ListBuiltinRoles`,
  `ListCustomRoles`, `CreateCustomRole`, `UpdateCustomRoleName`,
  `SoftDeleteCustomRole`, `ListRolePermissions`, `ReplaceRolePermissions`)
- `server/sqlc/queries/user_organization_role.sql` (new — `AssignRole`,
  `UnassignRole`, `ListAssignmentsForUser`, `ListAssignmentsForRole`)
- `server/generated/sqlc/**` (regenerated; do not hand-edit per AGENTS.md)

**Approach:**
- Read queries return joined permission rows alongside role rows in a
  single query where it avoids N+1.
- Mutations on custom-role CRUD reject only the SUPER_ADMIN built-in at
  the query level via `WHERE builtin_key IS DISTINCT FROM 'SUPER_ADMIN'`.
  ADMIN and FIELD_TECH are editable through the same path as custom
  roles; the domain layer (U8) enforces the SUPER_ADMIN-only lock and
  surfaces `BUILTIN_ROLE_IMMUTABLE` on a SUPER_ADMIN edit attempt.
- `ReplaceRolePermissions` is a transaction: delete current rows, insert
  new set.

**Patterns to follow:** Existing `server/sqlc/queries/role.sql` and
`user_organization.sql` styles.

**Test scenarios:** Covered indirectly by U6/U9 unit tests; this unit
itself is just generated bindings. `Test expectation: none — generated
bindings exercised by U6 and U9.`

**Verification:** `just gen` succeeds; new helpers compile.

---

### U4. Seed permissions and built-in roles

**Goal:** On every migration run, ensure `permission` rows match the
catalog and `role` rows for SUPER_ADMIN, ADMIN, FIELD_TECH exist with the
correct `role_permission` set.

**Requirements:** Built-in roles must always reflect the current catalog
without manual intervention.

**Dependencies:** U1, U2, U3.

**Files:**
- `server/migrations/000NNN_seed_builtin_roles.up.sql` (new — seeds initial
  set on first run)
- `server/migrations/000NNN_seed_builtin_roles.down.sql` (new — clears
  seed)
- `server/internal/domain/authz/builtin.go` (new — defines the
  permission set for each built-in role in code)
- `server/cmd/fleetd/main.go` (modify — on startup, after
  `db.ConnectAndMigrate` and after the `authz` store is constructed,
  reconcile built-in role permission sets against `builtin.go`)

**Approach:**
- The seed migration inserts the initial `permission` rows (catalog as of
  this release) and creates the three built-in `role` rows with
  `is_builtin=TRUE` and the appropriate `builtin_key`.
- A **startup reconciliation step** runs after migrations on server boot:
  for each built-in role defined in `builtin.go`, upsert any missing
  `permission` rows from the in-code catalog, then converge
  `role_permission` rows according to per-role policy:
  - **SUPER_ADMIN**: fully reconciled to `AllPermissions()` — both adds
    new catalog keys and removes any obsolete `role_permission` rows.
    Its contract is "everything in the current catalog."
  - **ADMIN / FIELD_TECH**: **additive-only.** Catalog keys present in
    the role's *seed formula* but absent from `role_permission` are
    inserted; nothing is ever removed. Operators who edited the role
    keep their edits across upgrades.
  This keeps the catalog and SUPER_ADMIN in sync as the catalog grows
  without requiring a new migration per catalog change, and respects
  per-org customizations on the editable built-ins.
- **Race-free reconciliation.** The whole reconciliation runs inside a
  transaction that first acquires
  `pg_advisory_xact_lock(<stable-bigint-key>)`. Concurrent boots
  (rolling deploy, autoscaler) serialize on the lock; only one wins,
  the others observe the converged state and exit fast. Within the
  transaction, `role_permission` is converged with idempotent
  `INSERT … ON CONFLICT DO NOTHING` + `DELETE … WHERE permission_id NOT
  IN (…)` rather than a wholesale delete-then-insert, so there is no
  window where SUPER_ADMIN has zero rows.
- **Catalog shrinkage.** When a permission is removed from the catalog,
  reconciliation **does not** drop the obsolete `permission` row — it
  simply stops referencing it from any `role_permission`. This avoids
  the revoke-on-rollback hazard if the deploy is reverted, and prevents
  orphan FK errors from any user-cached state. Obsolete-row cleanup is
  a deliberate manual migration step when the team is certain.
- ADMIN's **seed formula** is
  `AllPermissions() − {user:read, user:manage, role:manage}` in code.
  This formula is consulted only for additive reconciliation (and on
  fresh-install seeding); it does not overwrite operator edits.
- FIELD_TECH's seed formula is the explicit set
  `{fleet:read, miner:read, miner:blink_led, miner:download_logs,
  rack:read, rack:manage}`. New catalog keys are added to FIELD_TECH
  only if the formula is updated in a future release — by default,
  catalog growth does not silently widen FIELD_TECH.
- SUPER_ADMIN's set is `AllPermissions()` and is fully reconciled on
  every boot.

**Patterns to follow:** Existing seed pattern in
`000002_create_core_tables.up.sql` for the ADMIN role; startup reconciliation
runs as a free function called from `server/cmd/fleetd/main.go` after
`db.ConnectAndMigrate` and before `NewAuthInterceptor` so no request can
be served before built-in roles are reconciled.

**Test scenarios:**
- Fresh install: after migrations and startup reconciliation, all three
  built-in roles exist with `is_builtin=TRUE` and their full permission
  sets.
- Catalog grows: add a new permission key to the catalog, restart server,
  reconciliation inserts the new `permission` row and adds it to
  SUPER_ADMIN (and ADMIN if its seed formula includes it). FIELD_TECH
  is unchanged unless its explicit seed set was updated.
- Reconciliation is idempotent: running it twice produces the same row
  state, no duplicate inserts.
- **Operator edits to ADMIN survive restart.** An operator removes
  `miner:firmware_update` from ADMIN via `UpdateCustomRole`; restart
  reconciliation leaves the row deleted (additive-only). A later
  catalog addition of `miner:new_action` is added to ADMIN on the
  following restart without restoring the removed firmware-update row.
- **Operator edits to FIELD_TECH survive restart** under the same
  additive-only contract.
- **SUPER_ADMIN tampering is repaired on restart.** A row deleted from
  SUPER_ADMIN's `role_permission` is restored on next startup (full
  reconciliation). A row inserted into SUPER_ADMIN for a permission
  not in the current catalog is removed.
- Concurrent reconciliation: two goroutines invoking the reconciler in
  parallel against the same DB observe a single committed converged
  state; no row count goes negative (instrument with a snapshot read
  inside the second goroutine after the first commits).

**Verification:** On `just dev`, three built-in role rows exist with their
declared permission sets; SQL inspection confirms.

---

### U5. Migrate existing user_organization rows to assignments

**Goal:** Backfill `user_organization_role` from the existing
`user_organization.role_id` column without changing any user's effective
access.

**Requirements:** Existing users must keep their access. No flag day, no
re-login required.

**Dependencies:** U2, U4.

**Files:**
- `server/migrations/000NNN_migrate_user_organization_to_assignments.up.sql`
  (new)
- `server/migrations/000NNN_migrate_user_organization_to_assignments.down.sql`
  (new — repopulates `user_organization.role_id` from the assignment
  table, picks any one row if multiple exist)

**Approach:**
- `INSERT INTO user_organization_role (user_id, organization_id, role_id,
  scope_type, scope_id) SELECT user_id, organization_id, role_id, 'org',
  NULL FROM user_organization WHERE deleted_at IS NULL`.
- **Neutralize the legacy column in the same migration.** Rename
  `user_organization.role_id` to `role_id_deprecated_do_not_use` and add
  a trigger that raises `EXCEPTION` on any INSERT or UPDATE that sets
  the column to a non-NULL value. Any code path still reading or
  writing the legacy column fails loudly at runtime, rather than
  silently bypassing the new gate during the soak window before U12.
- The column itself is dropped in U12 once the audit confirms zero
  callers. This split (neutralize-then-drop) gives us the soak signal
  without the silent-bypass risk.

**Test scenarios:**
- Pre-existing org with one SUPER_ADMIN and two ADMINs: after migration,
  three `user_organization_role` rows exist, all `scope_type='org'`,
  pointing at the correct `role_id`.
- Soft-deleted `user_organization` rows are not copied.
- Down migration restores `user_organization.role_id` for each user/org
  pair, deterministically when multiple assignments exist (prefer the
  most-permissive built-in; document the rule in the migration comment).
- Writing to `role_id_deprecated_do_not_use` raises an exception; the
  test asserts the message is clear (`do not use; see
  user_organization_role`).
- A grep test in CI fails if any non-migration source file in the repo
  references `role_id` against the `user_organization` table.

**Verification:** Local DB inspection shows assignment row counts match
expected; existing login flows still work end-to-end.

---

### U6. Permission resolver and `RequirePermission` middleware

**Goal:** Replace `RequireAdmin` with a permission-based gate that
understands scope. Per-request, compute the caller's effective permission
set and offer a `RequirePermission(ctx, key, ResourceContext)` API.

**Requirements:** Every guarded handler in the codebase routes through
this gate; no handler keeps its own role-string check.

**Dependencies:** U1, U2, U3, U4.

**Execution note:** Implement test-first. The permission check is the
security boundary of the application; the test cases drive the
implementation, not the other way around.

**Files:**
- `server/internal/domain/authz/resolver.go` (new — `PermissionResolver`,
  `LoadEffective(userID, orgID) → EffectivePermissions`)
- `server/internal/handlers/middleware/permission.go` (new —
  `RequirePermission(ctx, key, ResourceContext)`, `ResourceContext` type)
- `server/internal/handlers/middleware/admin.go` (delete after callers
  migrated in U7 — keep through U6 to avoid a giant cross-cutting diff)
- `server/internal/handlers/interceptors/authentication.go` (modify —
  BOTH the `authenticateWithSession` and `authenticateWithApiKey`
  branches call `resolver.LoadEffective` and stash the result on the
  context after `session.Info` is populated)
- `server/internal/domain/session/info.go` (unchanged — `Info` keeps its
  current fields; `EffectivePermissions` lives on the context under its
  own key, not on `Info`. This avoids mutating a struct that is also
  synthesized by non-interceptor paths like the scheduler.)

**Approach:**
- `LoadEffective` runs a single query that joins
  `user_organization_role → role → role_permission → permission` and
  returns a slice of `(permission_key, scope_type, scope_id)` triples.
- `EffectivePermissions.Has(key, ResourceContext)` walks the triples and
  returns true on the first match where the assignment's scope contains
  the resource's scope (containment rule from the technical design above).
- `ResourceContext` is a small struct: `{ SiteID *int64 }`. Org-only
  actions pass the zero value. Building-level scoping is deferred; when
  it lands, this struct gains a `BuildingID` field and the resolver
  gains the third containment level.
- `RequirePermission` returns a Connect `PermissionDenied` error with a
  structured payload `{"required":"miner:reboot","scope":<caller's
  ResourceContext>}`. The `scope` field echoes the caller's request
  input only — never server-side assignment IDs, role names, or the
  caller's effective permission list.
- **Fail-closed default.** `RequirePermission` invoked without
  `EffectivePermissions` on the context returns Connect `Internal` (not
  ALLOW, not `PermissionDenied`). The middleware also returns
  `PermissionDenied` when invoked on an authenticated procedure whose
  Connect procedure name is not in any allowlist AND not in the
  permission map declared by U7 — the CI contract test is the audit
  signal, not the gate of last resort.
- **API-key callers.** Both `authenticateWithSession` and
  `authenticateWithApiKey` in the AuthInterceptor call `LoadEffective`.
  API keys inherit the user's *current* effective set on every request
  (live inheritance); revocation propagates on next request. See
  Adversarial open question (deferred) for snapshot-at-issue.
- **Scheduler / synthesized-actor paths.** When `session.Info.Actor`
  is `ActorScheduler` (or any future internal actor), `RequirePermission`
  short-circuits to ALLOW; `LoadEffective` is not invoked. Internal
  callers have no UserID in the assignment table and are trusted by
  virtue of running in-process.
- **Revocation latency.** The per-request cache means an in-flight RPC
  acts under the permission set loaded at the start of the request.
  For short unary RPCs this window is sub-second. Long-running RPCs
  (`FirmwareUpdate`, `DownloadLogs`, streaming responses) should
  re-invoke `RequirePermission` between significant side-effects
  (e.g. between firmware chunks); document this convention in
  resolver godoc. Hard revocation requires session/key revocation,
  not just role unassignment.

**Patterns to follow:** Existing `session.GetInfo(ctx)` accessor pattern;
existing Connect error helpers in
`server/internal/domain/fleeterror/`.

**Test scenarios:**
- SUPER_ADMIN with org-scope assignment: `Has("miner:reboot", any
  ResourceContext)` returns true.
- FIELD_TECH with org-scope: `Has("miner:blink_led", ...)` true,
  `Has("miner:reboot", ...)` false, `Has("user:manage", ...)` false.
- FIELD_TECH scoped to Site-A: `Has("miner:blink_led", {SiteID: A})`
  true; `Has("miner:blink_led", {SiteID: B})` false; `Has("miner:blink_led",
  {SiteID: nil})` (org-level resource) false.
- User with two assignments (ADMIN @ Site-A, FIELD_TECH @ Site-B):
  `Has("miner:reboot", {SiteID: A})` true; `Has("miner:reboot",
  {SiteID: B})` false.
- **Narrowing semantics test:** user with both `ADMIN @ org` AND
  `FIELD_TECH @ Site-A`: `Has("miner:reboot", {SiteID: A})` false
  (site-A narrower assignment wins and FIELD_TECH lacks
  `miner:reboot`); `Has("miner:reboot", {SiteID: B})` true (org
  assignment applies where no narrower assignment exists);
  `Has("user:manage", {SiteID: nil})` true (org-scoped action
  satisfied by the org assignment, no site-scope narrowing applies).
- Soft-deleted assignment is ignored.
- **API-key path:** request authenticated by API key tied to an
  ADMIN-roled user gets the ADMIN permission set, not a stale single-role
  string. `LoadEffective` is invoked exactly once per request from
  either auth branch.
- **Fail-closed:** `RequirePermission` called with no
  `EffectivePermissions` on context returns Connect `Internal`. A
  synthetic procedure not in any allowlist and not in the permission
  map gets `PermissionDenied` from the middleware.
- **Scheduler short-circuit:** `session.Info{Actor: ActorScheduler}`
  context: `RequirePermission` returns ALLOW without calling
  `LoadEffective` (test asserts zero DB calls).
- **Denial payload shape:** for `RequirePermission(ctx, "miner:reboot",
  ResourceContext{SiteID: &42})` denied, payload is exactly
  `{"required":"miner:reboot","scope":{"site_id":42}}`. Assert no other
  fields. With caller passing zero `ResourceContext`, payload is
  `{"required":"...","scope":{}}` (no nulls, no role names).
- Unauthenticated context: returns `Unauthenticated`, not
  `PermissionDenied`.
- Resolver caches within a request: hitting `Has` ten times triggers one
  DB query (instrument with a query counter).
- Covers AE: a FIELD_TECH can call `BlinkLED` and not `Reboot`.

**Verification:** All unit and integration tests in
`server/internal/domain/authz/` and
`server/internal/handlers/middleware/` pass.

---

### U7. Replace every `RequireAdmin` call site with `RequirePermission`

**Goal:** Migrate every handler in the codebase from `RequireAdmin` (or
ad-hoc role checks) to `RequirePermission`, threading the right
`ResourceContext` from request payloads.

**Requirements:** No handler bypasses the gate; every guarded RPC has
exactly one permission requirement.

**Dependencies:** U1, U6.

**Files (re-audited against the codebase — corrects the earlier draft):**
- `server/internal/handlers/buildings/` — site/building CRUD →
  `site:manage` or `site:read`. Swap existing `RequireAdmin` calls.
- `server/internal/handlers/sites/` — same. Swap existing `RequireAdmin`.
- `server/internal/handlers/fleetmanagement/` — list miners requires
  `fleet:read`; the handler **filters** the result set to the sites
  where the caller holds `fleet:read` (org-scope users see everything;
  site-scope users see only their sites; a caller with zero matching
  scope gets an empty list, not a 403). Single-miner read requires
  `miner:read` plus the miner's `site_id` in `ResourceContext`.
  Mutations (delete, rename, worker names) → their respective
  miner-mutation permissions, threading the miner's current `site_id`
  into `ResourceContext`. **No current gate** — U7 introduces
  authorization here for the first time.
- `server/internal/handlers/deviceset/` (racks) — list racks at a site
  requires `rack:read` (site-scoped filter on the same rule as miner
  lists); create/rename/delete rack and move-miner-into-rack require
  `rack:manage` with the site's `ResourceContext`. **No current gate** —
  U7 introduces authorization here for the first time.
- `server/internal/handlers/command/handler.go` — `BlinkLED`, `Reboot`,
  `StopMining`, `StartMining`, `SetCoolingMode`, `SetPowerTarget`,
  `UpdateMiningPools`, `UpdateMinerPassword`, `FirmwareUpdate`,
  `DownloadLogs`, `Unpair` → matching catalog keys with miner-scoped
  `ResourceContext`. **No current gate** — U7 introduces authorization
  here for the first time. This is a security-boundary addition, not a
  relocation; the security-critical PR 2 should call this out for
  reviewers.
- `server/internal/handlers/auth/` — user CRUD → `user:manage`. Swap
  inline role-string checks.
- `server/internal/handlers/apikey/handler.go` — local `requireAdmin`
  helper deleted; every endpoint (list, create, revoke) gated by
  `apikey:manage`, org-scoped. No `apikey:read` split — the surface
  lives under route-guarded Settings, so a viewer-only role has no
  reachable UI.
- `server/internal/handlers/curtailment/handler.go` — local
  `requireAdminFromContext` deleted; read endpoints (list, preview)
  gated by `curtailment:read`, mutations by `curtailment:manage`.
- `server/internal/handlers/fleetnodeadmin/handler.go` — inline
  `info.Role != ...` check replaced; read endpoints gated by
  `fleetnode:read`, mutations by `fleetnode:manage`.
- `server/internal/handlers/serverlog/handler.go` — inline check
  replaced with `RequirePermission(ctx, authz.PermServerlogRead,
  ResourceContext{})`.
- `server/internal/domain/auth/service.go` — `checkCanManageUser` is
  the domain-layer companion to `user:manage`; it now reads the
  caller's `EffectivePermissions` from context rather than comparing
  role-name strings.
- `server/internal/handlers/middleware/admin.go` (delete in this unit; no
  remaining callers).

**Approach:**
- Each call site changes from
  `info, err := middleware.RequireAdmin(ctx, "reboot miners")` to
  `info, err := middleware.RequirePermission(ctx, authz.PermMinerReboot,
   middleware.MinerResourceContext(miner))`.
- Helper `middleware.MinerResourceContext(miner)` extracts `{SiteID}`
  from a miner row. Equivalents for sites and buildings handle the
  parent-resource case.
- The previous human-readable action string (`"reboot miners"`) becomes
  the permission key. The Forbidden response carries the structured
  payload from U6.
- **Cross-org IDOR safeguard.** Before building `ResourceContext`, every
  migrated handler validates `resource.organization_id ==
  session.Info.OrgID`. If not, return `NotFound` (don't leak whether the
  resource exists in another org). This is the per-handler equivalent of
  the DB-level composite FK from U2 — defense in depth against handlers
  that accept resource IDs from request payloads.
- A new file `server/internal/handlers/middleware/rpc_permissions.go`
  documents (in a Go map) the permission for each Connect procedure name
  — consumed by the contract test introduced in U13. The file header
  notes that this map and the corresponding `RequirePermission` call
  in each handler are two sources of truth that must stay in sync per
  new RPC.

**Patterns to follow:** The mechanical replacement mirrors today's
existing `RequireAdmin` call sites — keep the same early-return shape.

**Test scenarios:**
- For each migrated handler, a unit test asserts:
  - SUPER_ADMIN succeeds.
  - ADMIN succeeds for everything except user/role management.
  - FIELD_TECH succeeds only for the six permissions in its built-in
    seed (`fleet:read`, `miner:read`, `miner:blink_led`,
    `miner:download_logs`, `rack:read`, `rack:manage`) and fails for
    all others.
  - A FIELD_TECH user with the seeded permission set can list racks
    and create one at their assigned site; the same call at a
    different site returns `PermissionDenied`.
  - Anonymous/expired session returns `Unauthenticated`.
- A contract test scans the Connect server's registered procedures and
  asserts that every non-allowlisted procedure has an entry in
  `rpc_permissions.go`. Adding a new RPC without registering it fails
  the test loudly.
- Covers AE: end-to-end, a FIELD_TECH user can call `BlinkLED` for a
  miner at their assigned site and is rejected on `Reboot` for the same
  miner.
- **Cross-org IDOR:** a SUPER_ADMIN of Org A calls a miner-targeting
  RPC with a miner ID that belongs to Org B. The handler's pre-resolve
  org check returns `NotFound` before `RequirePermission` runs. Without
  the org check (regression test), the cross-org call would succeed
  because the org-scoped SUPER_ADMIN assignment matches any
  `ResourceContext`.
- **command/ handlers (new gate, not relocation):** unauthenticated
  request rejected by AuthInterceptor; authenticated user with no
  matching permission gets `PermissionDenied`; SUPER_ADMIN succeeds.
  Confirm regression coverage exists for the legitimate flows that
  used to be ungated.

**Verification:** All existing handler tests still pass; the new contract
test passes; `grep -r RequireAdmin server/` returns nothing.

---

### U8. Role and assignment management RPCs

**Goal:** Expose proto-level RPCs for listing permissions, listing roles
(built-in + custom), creating/updating/deleting custom roles, and
assigning/unassigning roles to users with a scope.

**Requirements:** Custom roles are admin-manageable through the API.
ADMIN and FIELD_TECH built-ins are editable through the same API as
custom roles; only SUPER_ADMIN is immutable.

**Dependencies:** U1, U2, U3, U4, U6.

**Files:**
- `proto/authz/v1/authz.proto` (new) — `AuthzService` with:
  - `ListPermissions(Empty) → ListPermissionsResponse` (catalog)
  - `ListRoles(Empty) → ListRolesResponse` (built-in + custom, with their
    permission keys)
  - `CreateCustomRole(name, description, permission_keys)` →
    new role
  - `UpdateCustomRole(role_id, name, description, permission_keys)` →
    full replace
  - `DeleteCustomRole(role_id)` → soft delete; rejects if assignments
    still reference it (must unassign first)
  - `AssignRole(user_id, role_id, scope_type, scope_id)` → assignment
  - `UnassignRole(assignment_id)`
  - `ListUserAssignments(user_id) → ListUserAssignmentsResponse`
- `server/internal/domain/authz/service.go` (new) — domain logic for the
  above, including the safety rules below.
- `server/internal/handlers/role/service.go` (new) — Connect handler.
- `server/internal/handlers/interceptors/config.go` (modify — add new
  RPCs to `SessionOnlyProcedures`; mutations gated by `role:manage`).

**Approach:**

Safety rules enforced in the service layer:

- Custom-role mutations require `role:manage` (SUPER_ADMIN only by
  default).
- **Privilege parity (universal).** A caller cannot create, update, OR
  assign a role whose permission set contains any permission the caller
  does not themselves currently hold. This covers three vectors with
  one rule:
  - `CreateCustomRole` with permissions exceeding the caller's set →
    rejected.
  - `UpdateCustomRole` that adds a permission the caller doesn't hold →
    rejected.
  - `AssignRole(role_id=X)` where the caller does not hold every
    permission in role X (built-in or custom) → rejected.
  This closes the "pre-loaded definition" path: a custom role created
  while the caller held elevated permissions cannot later be assigned
  by a caller who has since lost those permissions, even if they still
  hold `role:manage`.
- `UpdateCustomRole` accepts the ADMIN and FIELD_TECH built-in rows
  (`builtin_key IN ('ADMIN','FIELD_TECH')`) and applies the same
  privilege-parity check as for custom roles. `UpdateCustomRole`
  rejects only the SUPER_ADMIN row (`builtin_key='SUPER_ADMIN'`) with
  `BUILTIN_ROLE_IMMUTABLE`. `DeleteCustomRole` rejects SUPER_ADMIN
  with `BUILTIN_ROLE_IMMUTABLE` and rejects ADMIN and FIELD_TECH with
  a distinct `BUILTIN_ROLE_NON_DELETABLE` — the latter two can be
  edited but not deleted, so the org always has a named
  ADMIN/FIELD_TECH to assign. Two error codes so the UI can render
  separate copy ("this role can't be modified" vs "this role can be
  edited but not deleted").
- **Read-pairing validation.** `CreateCustomRole` and `UpdateCustomRole`
  reject a permission set that contains an action permission without
  its required read partners (table in the catalog section above). The
  error code is `INVALID_PERMISSION_SET` and the response payload lists
  the missing reads so the UI can prompt the admin to add them.
- `AssignRole` validates the `(scope_type, scope_id)` pair: `org`
  requires `scope_id` IS NULL; `site` requires the FK row to exist and
  belong to the caller's org (the U2 composite FK is the structural
  guarantee; the application-layer check produces a friendlier error).
- An org must always have at least one user with the SUPER_ADMIN
  assignment at org scope. `UnassignRole` and `DeactivateUser` reject if
  removing the last such assignment. Both checks query
  `user_organization_role`, not the legacy single-role column.
- All `DeactivateUser` and last-SUPER_ADMIN checks run against
  `user_organization_role`; if any path still reads from the now-renamed
  `role_id_deprecated_do_not_use`, it raises and the surrounding test
  fails.

**Patterns to follow:** Existing
`server/internal/handlers/auth/service.go` for handler shape and error
construction; existing apikey lifecycle service for soft-delete +
record-use idioms.

**Test scenarios:**
- `ListPermissions` returns the full catalog in stable order.
- `ListRoles` returns the three built-ins + any custom roles in the org,
  each with its permission key list.
- `CreateCustomRole` succeeds for SUPER_ADMIN; rejected for ADMIN with
  `PermissionDenied`.
- `CreateCustomRole` rejects an attempt to include `role:manage` for a
  caller who doesn't already have it.
- **AssignRole parity:** a caller with `role:manage` but missing
  `user:manage` cannot `AssignRole` SUPER_ADMIN (or any role whose
  permission set contains `user:manage`) to any user. Reject with the
  same error code as the create/update parity rejection.
- **Pre-loaded-definition attack:** a custom role `"Backdoor"` was
  previously created with `{user:manage, role:manage}` by a caller who
  had both. Later, a different caller holding only `role:manage`
  attempts `AssignRole(Backdoor)` and is rejected by the
  AssignRole-parity rule.
- `DeactivateUser` reads the new assignment table: deactivating the
  org's last user with a SUPER_ADMIN assignment at org scope fails
  with `FailedPrecondition`.
- `UpdateCustomRole` on SUPER_ADMIN returns `BUILTIN_ROLE_IMMUTABLE`
  and does not mutate anything.
- `UpdateCustomRole` on ADMIN succeeds for a SUPER_ADMIN caller: a
  SUPER_ADMIN can remove `miner:firmware_update` from ADMIN, save, and
  reloading the role shows the permission gone. The change is also
  visible in the next `Authenticate` for any user holding ADMIN.
- `UpdateCustomRole` on FIELD_TECH succeeds for a SUPER_ADMIN caller
  adding `miner:reboot`. The change covers Q1 without a code release.
- `DeleteCustomRole` on ADMIN or FIELD_TECH returns
  `BUILTIN_ROLE_NON_DELETABLE` (delete is locked even though edit is
  not), distinct from the `BUILTIN_ROLE_IMMUTABLE` returned for
  SUPER_ADMIN.
- **Read-pairing rejection:** `CreateCustomRole` with permissions
  `{miner:reboot}` (no `miner:read`, no `fleet:read`) returns
  `INVALID_PERMISSION_SET` with the missing reads enumerated in the
  payload. The same role saved with `{miner:reboot, miner:read,
  fleet:read}` succeeds.
- **Read-pairing on update:** `UpdateCustomRole` removing `miner:read`
  from a role that still holds `miner:reboot` returns
  `INVALID_PERMISSION_SET`. Removing both in the same update succeeds.
- `DeleteCustomRole` on a role with active assignments returns
  `FailedPrecondition` listing the offending assignment count.
- `AssignRole` with `scope_type='site'` and a `scope_id` from another
  org returns `NotFound` (org isolation).
- `AssignRole` with `scope_type='building'` is rejected with
  `InvalidArgument` (deferred from v1; the CHECK constraint also
  rejects at the DB layer).
- `UnassignRole` of the last org-scope SUPER_ADMIN assignment returns
  `FailedPrecondition`.
- Two-assignment scenario: assigning the same role twice at the same
  scope is rejected by the unique constraint, surfaced as
  `AlreadyExists`.

**Verification:** All handler tests pass; the new contract test from U7
includes the new RPCs in its permission map.

---

### U9. Expose effective permissions in `UserInfo`

**Goal:** The client receives the caller's effective permission keys in
the `UserInfo` it already gets back from `Authenticate` and (where
applicable) on subsequent identity refresh.

**Requirements:** Frontend gates UI on permissions instead of role name.

**Dependencies:** U6, U8.

**Files:**
- `proto/auth/v1/auth.proto` — `UserInfo` gains
  `repeated string permissions = 7;`. Keep `role` for now; phase it out
  in Deferred to Follow-Up Work after the client migration in U10 is
  complete and stable.
- `server/internal/handlers/auth/service.go` — populate `permissions`
  from the resolver on every response that returns `UserInfo`.
- Generated files regenerated via `just gen`.

**Approach:**
- A small helper in the auth handler builds `UserInfo` and now also
  reads `resolver.LoadEffective(...)`. The permission slice is sorted
  for deterministic UX.
- `permissions` is the *union* of permission keys across all current
  assignments — sufficient for show/hide gating. Scope-aware
  affordances (e.g. "show reboot button only on miners at sites where
  the user has the permission") are still server-rejected at the
  handler level; the UI gates at the coarser level.

**Test scenarios:**
- `Authenticate` for an ADMIN returns `permissions` containing all
  catalog keys except `user:*` and `role:manage`.
- `Authenticate` for a FIELD_TECH returns the six expected keys
  (`fleet:read`, `miner:blink_led`, `miner:download_logs`,
  `miner:read`, `rack:manage`, `rack:read`), in sorted order.
- Backward-compat: the deprecated `role` field returns the user's
  *primary* role, computed as: (1) if any built-in is among their
  assignments, the highest-privilege built-in
  (SUPER_ADMIN > ADMIN > FIELD_TECH); (2) otherwise, the custom role
  from their oldest non-deleted assignment by `created_at ASC`
  (assignment-id ASC as a tiebreaker). The tie-break is intentionally
  not alphabetical: rename of a custom role must not flip a user's
  reported `role`, and an attacker with `role:manage` must not be able
  to influence the reported field for other users by naming a new role
  `"AAA"`. Document the rule in `auth/service.go`.
- A user with no assignments at all returns `permissions: []` and the
  AuthInterceptor still succeeds (they can log in but can't do anything
  beyond `fleet:read` if even that isn't granted — UI handles the
  empty-state).

**Verification:** Hitting `Authenticate` via the dev server returns the
new field; TypeScript types regenerate cleanly.

---

### U10. Client auth context exposes `permissions` and `hasPermission`

**Goal:** Replace every `role === "ADMIN"` / `role === "SUPER_ADMIN"`
check in the React client with `hasPermission("…")`.

**Requirements:** UI shows controls a user can actually use and hides the
rest. A FIELD_TECH sees the dashboard, miner detail, and the Blink LED
button on miner detail; nothing else.

**Dependencies:** U9.

**Files:**
- `client/src/protoFleet/store/slices/authSlice.ts` (modify) — `AuthState`
  gains `permissions: string[]` and a `setPermissions` action paralleling
  the existing `setRole`.
- `client/src/protoFleet/store/hooks/useAuth.ts` (modify) — new selectors
  `usePermissions()` and `useHasPermission(key)` paralleling the existing
  `useRole()`/`useIsAuthenticated()` shape.
- `client/src/protoFleet/store/hooks/useAuthentication.ts` (modify) —
  call `setPermissions` alongside `setRole` after `Authenticate`
  resolves.
- `client/src/protoFleet/features/settings/components/Team.tsx` (modify)
  — replace `currentUserRole === "SUPER_ADMIN"` with
  `useHasPermission("user:manage")`.
- `client/src/protoFleet/features/settings/components/ApiKeys.tsx`
  (modify) — replace role check with
  `useHasPermission("apikey:manage")`.
- `client/src/protoFleet/features/minerDetail/components/*` (modify) —
  every action button (`BlinkLED`, `Reboot`, `FirmwareUpdate`, etc.)
  wraps its render in `useHasPermission(...)`.
- `client/src/protoFleet/features/minerList/components/*` (modify) —
  bulk-action menu items gated by their respective permissions.
- `client/src/protoFleet/navigation/*` (modify) — primary-nav and
  settings sub-nav filter by required permission per item; primary-nav
  entries are hidden when their predicate is false.
- `client/src/protoFleet/routes/*` (modify — or whichever module wires
  route layouts) — add layout-level redirects for `/`,
  `/curtailment/*`, `/fleet-nodes/*`, and `/settings/*` matching the
  primary-nav predicates above. Redirect target is the first available
  primary nav in declared order; degenerate case routes to the
  "no resources in scope" terminal page.
- `client/src/protoFleet/features/welcome/NoAccess.tsx` (new) — the
  terminal landing page rendered when a user has no available primary
  nav. Surfaces the org name, a contact-your-admin message, and a
  sign-out button. Does not render the chrome (no nav, no header
  actions) — there is nothing for them to do.

**Approach:**
- `useHasPermission(key)` is a thin selector over the auth store; no
  network involvement once `Authenticate` populates the store.
- For controls that should hide entirely (vs. show-but-disable), prefer
  hide. Tooltips on disabled controls leak the existence of features the
  user can't access.
- Settings sub-nav items become an array of
  `{label, path, permission}` and the renderer filters by
  `useHasPermission`. The existing `SecondaryNavItem.allowedRoles` field
  is replaced by `requiredPermission: string`. Existing items that
  previously listed `allowedRoles: ["SUPER_ADMIN","ADMIN"]` map to
  their new catalog key: `api-keys → apikey:manage`, `server-logs →
  serverlog:read`, etc.
- **Primary-nav and route-guard parity.** Each primary-nav entry has
  a predicate (see the catalog notes' route-guard table). The nav
  hides when the predicate is false AND the corresponding route
  redirects at the layout level — there is no state where a direct
  link reaches a route whose nav entry is hidden. Predicates:
  Dashboard → `hasPermission("fleet:read")`; Curtailment →
  `hasPermission("curtailment:read")`; Fleet Nodes →
  `hasPermission("fleetnode:read")`; Settings →
  `hasAnyPermission(ADMIN_PERMISSIONS)`.
- **Landing-page fallback.** A `useDefaultRoute()` selector returns the
  first available primary-nav route for the current user (in declared
  order: Dashboard → Curtailment → Fleet Nodes → Settings). Login
  redirects there; the bare `/` route also redirects there. If the
  selector returns nothing — no available primary nav at all — the
  user lands on `<NoAccess>`.
- **Live-session refresh (Q3).** `UserInfo.permissions` is refreshed
  on: (a) window focus event (re-fetch via a lightweight
  `GetSessionUserInfo` RPC), and (b) any `PermissionDenied` response
  to a previously-allowed action (the response handler triggers a
  refresh then shows the denial toast). No periodic poll. Q3 covers
  whether a coarser refresh model is preferred.

**Test scenarios:**
- Component-level test (Vitest + React Testing Library): mount miner
  detail with `permissions=["fleet:read","miner:read","miner:blink_led"]`;
  assert Blink LED button is visible, Reboot button is not in the DOM.
- Mount Team page with `permissions=[]` (omitting `user:manage`); assert
  it renders an empty-state, not a list of teammates.
- Mount Team page with `permissions=["user:manage"]`; assert the full
  list renders and the "Add teammate" action is enabled.
- Snapshot: settings sub-nav for FIELD_TECH renders zero settings items,
  confirming no accidental admin-page exposure.
- **Settings primary nav hidden for FIELD_TECH:** mount the chrome with
  the FIELD_TECH permission set; assert the Settings entry is not in
  the primary nav.
- **Settings route guard:** navigate FIELD_TECH directly to
  `/settings/api-keys`; assert the router redirects to `/` (dashboard)
  and the API Keys component never mounts.
- **Dashboard hide + landing fallback:** a user with permissions
  `["curtailment:read"]` (no `fleet:read`, no admin set) lands on
  `/curtailment` after login; the Dashboard nav entry is not rendered;
  navigating directly to `/` redirects to `/curtailment`.
- **No-access terminal page:** a user with `permissions=[]` lands on
  `<NoAccess>`; the page contains a sign-out button; primary nav is
  not rendered.
- Covers AE: a FIELD_TECH user logging in lands on the dashboard, can
  open miner detail, and sees Blink LED but not Reboot.

**Verification:** `just lint` and frontend test suite pass; manual
walkthrough of the dev client logged in as each built-in role.

---

### U11. Settings → Roles admin UI

**Goal:** A new Settings page where admins (anyone with `role:manage`)
can list roles, inspect built-in role permissions, create/edit/delete
custom roles, and assign roles to users.

**Requirements:** The custom-role story is reachable without raw API
calls.

**Dependencies:** U8, U10.

**Files:**
- `client/src/protoFleet/features/settings/components/Roles.tsx` (new)
- `client/src/protoFleet/features/settings/components/RoleEditor.tsx`
  (new)
- `client/src/protoFleet/features/settings/components/PermissionPicker.tsx`
  (new)
- `client/src/protoFleet/features/settings/components/Team.tsx` (modify)
  — the per-user row gains a "Roles" cell summarizing assignments with a
  modal to edit them.
- `client/src/protoFleet/api/authz.ts` (new) — Connect-RPC client wrapper
  for the new RPCs (auto-generated from proto via existing pipeline).
- `client/src/protoFleet/navigation/*` (modify) — add Roles entry,
  gated by `role:manage`.

**Approach:**
- Roles page shape: three columns — name + description, permission count
  with a tooltip listing every permission key in the role (no
  "first few" truncation — the full list is short enough), builtin-or-custom
  badge. Tooltip is hover for pointer devices; tap-to-open on touch.
- RoleEditor modal: name, description, grouped permission checklist.
  - **Accessibility:** each resource group renders as
    `<fieldset><legend>` so screen readers announce group context;
    keyboard navigation uses roving tabindex within a group (arrow
    keys), Tab moves between groups. The modal traps focus per the
    existing settings-modal pattern.
  - **Built-in roles read-only:** the modal still opens (admins want
    to inspect the permission set), but the form is disabled and a
    full-width info banner at the top reads "Built-in role — permissions
    are managed by the application." Save button replaced with Close.
  - **Save states:** Save disables and shows a spinner during the
    in-flight RPC. On `PermissionDenied` (parity rejection), inline
    error appears above the checklist naming the offending permission
    key. On `InvalidArgument` (empty name, etc.), inline field-level
    errors. On `Internal`, generic toast error and modal stays open.
- PermissionPicker groups by resource and renders catalog descriptions
  next to each checkbox.
- **First-run empty state.** When no custom roles exist, the Roles page
  shows the three built-in role rows, a section divider "Custom roles",
  and a primary CTA "Create role". No empty-state illustration; the
  built-in rows already orient the user.
- Assignment editing happens in the Team page's per-user row to keep the
  "who can do what" surface in one place. The modal lists current
  assignments and lets the admin add new ones with a scope picker (org /
  site dropdown). Scope dropdowns populate from existing site list API.
  - **Loading state:** site dropdown shows a spinner while
    `ListSites` is in flight; disabled until populated.
  - **Error state:** if `ListSites` fails, the dropdown disables with
    an inline retry link; admin cannot save a site-scoped assignment
    until it succeeds. Org-scope assignment remains saveable.
  - **Live propagation:** after saving an assignment, the assigned user's
    next request (or focus event in their browser, per U10) refreshes
    their `permissions` array; the admin is not blocked.

**Patterns to follow:** Existing settings modal pattern (Team modal,
ApiKeys modal); existing form-validation library used by the
`ApiKeyForm`.

**Test scenarios:**
- Roles page renders the three built-ins plus any custom roles; built-in
  rows are non-clickable into edit mode (or render the read-only banner).
- Create-role flow: pick three permissions, save, new row appears with
  correct permission count; refresh confirms persistence.
- Edit custom role: rename, change permissions, save; assignments to
  that role gain/lose access on next request without re-login.
- Delete custom role with active assignments: confirmation surfaces the
  count and offers a "remove all assignments and delete" affordance; if
  the user declines, the role is preserved.
- Assignment scope picker: selecting "Org" hides the site picker;
  selecting "Site" surfaces the site dropdown.
- `ListSites` failure: the scope picker disables the site option with
  an inline retry link; org-scope save still works.
- Accessibility: a keyboard-only user can navigate every resource group
  in the PermissionPicker with arrow keys, and the active group's
  `<legend>` is announced by VoiceOver/NVDA on focus entry.
- Settings primary nav hidden for a FIELD_TECH (no `requiredPermission`
  matches any of the sub-items); the user does not see "Settings" in
  the primary nav at all.
- Covers AE: an admin creates a "Pool Operator" custom role with
  `fleet:read`, `miner:read`, `miner:update_pools`, assigns it to a
  teammate scoped to Site-A, and the teammate can update pools on Site-A
  miners but not Site-B miners.

**Verification:** Manual walkthrough plus the component tests above.

---

### U12. Drop legacy `user_organization.role_id` column

**Goal:** Remove the now-renamed legacy column and refactor the
interface methods that still encode the single-role model, after U5's
neutralization has soaked for at least one release.

**Requirements:** Schema and Go interfaces reflect the actual
multi-assignment model.

**Dependencies:** U5, U7, plus a soak release. The neutralization
trigger from U5 must have logged zero raised exceptions during the
soak window (audit step).

**Files:**
- `server/migrations/000NNN_drop_user_organization_role_id.up.sql` (new
  — drops the trigger added in U5, then
  `ALTER TABLE user_organization DROP COLUMN role_id_deprecated_do_not_use;`)
- `server/migrations/000NNN_drop_user_organization_role_id.down.sql`
  (new — re-add column and backfill from `user_organization_role` with
  the same precedence rule documented in U5's down migration; re-add
  the U5 trigger)
- `server/sqlc/queries/user_organization.sql` (modify — remove any
  remaining references)
- `server/internal/domain/stores/interfaces/user.go` (modify) — the
  three single-role interface methods are rewritten:
  - `CreateUserOrganizationRole(ctx, userID, orgID, roleID)` becomes
    `CreateUserOrganizationRole(ctx, userID, orgID, roleID, scopeType,
    scopeID)` writing into `user_organization_role`. Existing
    bootstrap and create-user callers pass `('org', nil)`.
  - `CreateAdminUserWithOrganization(ctx, ..., roleName, roleDescription)`
    bootstrap path is unchanged for the role row but the assignment
    write goes through the new table. Confirm the org's first
    SUPER_ADMIN assignment is created and the "at least one
    SUPER_ADMIN at org scope" invariant from U8 holds from the very
    first byte of org state.
  - `GetUserRoleName(ctx, userID, orgID)` no longer reads the dropped
    column. It is rewritten to return the user's *primary* role per
    the rule in U9 (highest-privilege built-in among assignments;
    else oldest custom by created_at). All callers of this method
    are audited; the AuthInterceptor's per-request usage is removed
    entirely once `LoadEffective` is the source of truth.
- `server/internal/domain/auth/service.go` (modify) — `CreateUser`,
  `ResetUserPassword`, `DeactivateUser` paths confirmed to write/read
  exclusively through `user_organization_role`.

**Approach:** Schema cleanup is mechanical; the interface refactor is
the real work. The audit step before merging: (a) grep the codebase
for `role_id` against `user_organization`, (b) inspect U5's trigger
log for any exceptions during soak, (c) confirm `GetUserRoleName` and
the three interface methods have one consistent implementation each.

**Test scenarios:**
- Apply migration on a DB with active assignments; column is gone;
  application boot succeeds; logging in still returns the correct
  `permissions`.
- Down migration repopulates `role_id` with the documented precedence
  rule and the application boots against the rolled-back schema.

**Verification:** No references remain in code or fixtures.

---

### U13. End-to-end and contract test coverage

**Goal:** A test suite that proves the security boundary holds and that
adding new RPCs without registering them fails loudly.

**Requirements:** Confidence in the gate before shipping.

**Dependencies:** All preceding units.

**Files:**
- `tests/e2e-fleet/granular-rbac/*` (new — Playwright E2E flows: log in
  as FIELD_TECH, attempt blocked actions, verify rejections).
- `server/internal/handlers/middleware/rpc_permissions_test.go` (new —
  contract test that every Connect procedure either appears in the
  allow/agent/session lists or in the permission map).
- `server/internal/domain/authz/resolver_test.go` (already added in U6
  — extended here with cross-org isolation cases).

**Approach:**
- E2E covers the three built-in roles + a custom "Pool Operator" role.
- Cross-org isolation test: a SUPER_ADMIN of Org A cannot act on Org B's
  miners. Both the DB-level composite FK (U2) and the per-handler
  pre-resolve org check (U7) enforce this; this test confirms both
  layers and that removing either breaks the test (regression cover for
  defense-in-depth).
- Pre-loaded-definition attack: a `Backdoor` custom role created under
  elevated permissions cannot be assigned by a caller who has since
  lost those permissions, even with `role:manage`. Cover this in both
  handler unit (U8) and E2E.
- Permission-map drift: a synthetic Connect procedure not in any
  allowlist and not in `rpc_permissions.go` returns
  `PermissionDenied` at the middleware (fail-closed default), AND the
  contract test fails CI for the same procedure.

**Test scenarios:**
- (above)

**Verification:** `just test-e2e-fleet` green; the contract test fails
when a synthetic "unregistered" RPC is added (and passes after it's
added to the map).

---

## Key Technical Decisions

- **One assignment row per (user, role, scope).** Originally considered a
  single row with arrays of scopes; rejected because it complicates the
  containment query and the unique constraint. The 1-row-per-scope shape
  lets the DB enforce uniqueness and makes audit-log entries trivial.
- **`scope_id` carries a composite DB-level FK to `site(id,
  organization_id)`.** Multi-site Phase 1 is on `main` and the
  composite-unique key it published is the right FK target — it
  encodes tenant isolation at the DB layer. The FK is `DEFERRABLE
  INITIALLY DEFERRED` so transactional re-assignments don't fail
  mid-transaction. Building-level FK is deferred along with
  building-scope itself.
- **Built-in roles: SUPER_ADMIN locked and fully reconciled; ADMIN and
  FIELD_TECH editable with additive-only reconciliation.** SUPER_ADMIN
  is treated as immutable at the API layer and its `role_permission`
  rows are fully reconciled on every boot to `AllPermissions()`
  (additions *and* removals). ADMIN and FIELD_TECH share the same
  `UpdateCustomRole` API as custom roles so operators can tune them
  per org; startup reconciliation only **adds** missing seed
  permissions to these two and never removes a row, so operator edits
  survive upgrades. Built-ins are identified by `builtin_key`, not by
  primary key. Reconciliation runs under `pg_advisory_xact_lock` so
  concurrent boots serialize cleanly, and uses idempotent upserts (no
  delete-then-insert window).
- **`UserInfo.permissions` is a flat union across all assignments.** The
  client gates on coarse "has the permission anywhere" presence; the
  server still rejects scope-violating calls. A richer
  `permissions_by_scope` map is a follow-up only if UI grows scope-aware
  needs that today's gating model can't express.
- **Universal privilege parity on create, update, AND assign.** A
  caller cannot create, update, or assign a role whose permission set
  contains any permission the caller does not currently hold. The
  AssignRole leg closes the "pre-loaded definition" attack where a
  permissive role minted under elevated permissions sits dormant until
  later assigned by a less-privileged caller. See U8.
- **Middleware fail-closed default.** The `AuthInterceptor` returns
  `PermissionDenied` for any authenticated procedure not in any
  allowlist AND not in the U7 permission map. The CI contract test is
  an audit signal — the runtime middleware is the gate of last resort.
- **Live API-key inheritance.** API keys carry the user's *current*
  effective permission set on every request; revocation propagates on
  next request. Snapshot-at-issue and per-key permission declaration
  are deferred follow-ups, not v1.
- **Narrowing (intersection-on-overlap) scope semantics, made visible.**
  When a user has both an org-scope and a site-scope assignment, the
  site-scope assignment overrides the org-scope grant at that site; the
  org-scope grant continues to apply at every other site. The UI flags
  this in the RoleEditor and assignment confirmation so admins know
  that adding a site-scoped role to an org-scoped user narrows them at
  that site. Pure union was considered and rejected (see Q2).
- **Multi-site Phase 1 is a precondition, already met.** Migrations
  000043–000049 shipped on `main`; site and building tables with their
  `(id, org_id)` composite-unique keys are the structural anchor for
  this plan's scope FK. Building-level scope is deferred to a follow-up
  plan.
- **Primary-nav and route-guard parity.** Every primary-nav entry has
  a permission predicate; the nav hides AND the route redirects when
  the predicate is false. No "hidden in nav but reachable via direct
  link" state. The administrative set
  `{user:manage, role:manage, site:manage, apikey:manage,
  serverlog:read}` defines Settings visibility; Dashboard requires
  `fleet:read`; Curtailment and Fleet Nodes are top-nav and require
  their own `:read`. Users with no available primary nav land on a
  `<NoAccess>` terminal page. Server-side per-RPC gates remain the
  security boundary; the client redirect is UX hygiene.

## Requirements Trace

The user's stated requirements:
- "Add field tech role" → U4 (seed FIELD_TECH built-in), U10 (UI hides
  controls), U13 (E2E proves it).
- "Field tech can view data and blink LED at minimum" → U1 catalog keys
  `fleet:read`, `miner:read`, `miner:blink_led`, `miner:download_logs`;
  U6 enforcement; U10 button visibility.
- "Field tech can manage racks" → U1 catalog keys `rack:read`,
  `rack:manage`; FIELD_TECH seed includes both; U7 gates the
  `deviceset` handlers; U13 covers rack create/move at the tech's
  assigned site.
- "Super admin can modify the ADMIN role after the initial migration"
  → built-in editability rule (only SUPER_ADMIN locked); U3 query
  filter scoped to SUPER_ADMIN; U4 additive-only reconciliation for
  ADMIN/FIELD_TECH; U8 `UpdateCustomRole` accepts ADMIN/FIELD_TECH.
- "Allow creating custom roles by selecting from a set of attributes" →
  U1 catalog, U8 RPCs, U11 admin UI.
- "End to end technical design doc" → this document.

## System-Wide Impact

- **Server domain:** new `authz` package; existing `auth` package keeps
  its session/user-management responsibilities and gains a thin call
  into `authz` to populate `UserInfo`.
- **Server handlers:** every guarded handler updated; net code change is
  small (one-line gate swap per handler) but touches many files.
- **Database:** four new tables (`permission`, `role_permission`,
  `user_organization_role`, plus the eventual drop of
  `user_organization.role_id`), two seed steps, one data backfill.
- **Proto surface:** new `authz.v1` package; `auth.v1.UserInfo` adds one
  field. Backward-compatible — old clients still get `role`.
- **Frontend:** auth store gains a derived selector; every role-string
  comparison becomes a permission check. New Settings → Roles page.
- **Plugins:** untouched. Plugin boundary stays clean per Goals.
- **Tests:** new authz unit tests, new handler permission tests, new E2E
  flows, contract test that pins the RPC↔permission map.

## Risks and Mitigations

- **Risk: privilege escalation via custom role creation OR assignment.**
  Mitigation: universal parity rule (U8) — caller must hold every
  permission they put into OR assign through a role, closing the
  pre-loaded-definition variant.
- **Risk: API-key auth path bypasses the new resolver.** Mitigation:
  U6/U7 explicitly require both `authenticateWithSession` and
  `authenticateWithApiKey` to invoke `LoadEffective`; the resolver
  returns `Internal` (fail-closed) if `EffectivePermissions` is absent
  from context.
- **Risk: a new RPC ships without a registered permission and is
  unguarded.** Mitigation: middleware-level fail-closed default
  (U6/U7) returns `PermissionDenied` for any authenticated procedure
  not in any allowlist AND not in the permission map; CI contract
  test (U13) makes drift loud but is not the only line of defense.
- **Risk: org lockout from removing the last SUPER_ADMIN.** Mitigation:
  refuse to delete the last org-scope SUPER_ADMIN assignment (U8); same
  rule for `DeactivateUser`. Both checks run against
  `user_organization_role`.
- **Risk: operator cripples ADMIN or FIELD_TECH by editing it.**
  Editable built-ins introduce the possibility that a SUPER_ADMIN
  removes critical permissions (e.g., strips `miner:read` and
  `fleet:read` from ADMIN), leaving every non-SUPER_ADMIN user unable
  to perform their job. Mitigations: (a) privilege-parity rule
  (U8) prevents demotion below the caller's own set, so a non-SUPER
  caller can't ratchet ADMIN down beyond what they themselves hold;
  (b) the read-pairing rule (U8) prevents saving an internally
  inconsistent set (action without its read); (c) recovery path —
  any SUPER_ADMIN can re-edit the role at any time, no DB
  intervention required; (d) additive-only startup reconciliation
  (U4) is **not** a safety net here because it never removes rows,
  so a stripped permission stays stripped until a SUPER_ADMIN
  restores it. The product surface should make this clear: the
  RoleEditor renders an explicit "you are editing a built-in role"
  banner on ADMIN and FIELD_TECH so the operator can't mistake it
  for a custom role tweak.
- **Risk: cross-org IDOR via resource ID in request payloads.**
  Mitigation: defense in depth — DB-level composite FK on
  `(scope_id, organization_id)` (U2) plus per-handler pre-resolve
  org-scope check (U7) plus cross-org regression test (U7, U13).
- **Risk: in-flight long-running RPC continues to act after a
  permission revoke.** Mitigation: documented revocation latency
  bound (one request duration for short RPCs); long-running RPCs
  (`FirmwareUpdate`, `DownloadLogs`, streams) re-invoke
  `RequirePermission` between significant side-effects.
- **Risk: stale built-in role definitions in the DB after a catalog
  change.** Mitigation: startup reconciliation in U4, hardened with
  `pg_advisory_xact_lock` and idempotent upsert; obsolete-permission
  rows preserved to survive rollbacks.
- **Risk: orphan assignment rows after a site is deleted.** Mitigation:
  composite FK has `ON DELETE CASCADE`, so deleting a site removes
  its scoped assignments at the DB layer. Operators are warned in the
  site-delete confirmation dialog when scoped assignments exist; a
  building deletion follow-up will mirror this when building-scope
  ships.
- **Risk: a downstream consumer reads `user_organization.role_id`
  during the soak window between U5 and U12.** Mitigation: U5 renames
  the column to `role_id_deprecated_do_not_use` AND installs a trigger
  that raises on any write referencing it; any in-repo reader fails
  loudly; out-of-repo readers are detected via the soak audit.
- **Risk: a deny-rule feature ask after launch can't compose with the
  first-match-ALLOW resolver.** Acceptable for v1; documented in the
  resolver godoc that adding deny rules would require either a
  two-pass evaluator or a separate deny table; not gating today.

## Phased Delivery

This plan deliberately sequences as a single coherent shipment to a
**branch**, then merges to main once verified. No per-phase user-visible
rollout is needed — the migration is invisible to users (existing roles
preserved), and the new affordances are non-destructive additions.

Suggested PR sequencing within the branch:

1. **PR 1** — U1, U2, U3, U4, U5 (schema + seeds + data migration; no
   behavior change yet).
2. **PR 2** — U6, U7 (resolver + middleware swap; old `RequireAdmin`
   deleted). This is the security-critical PR; merit-eligible for an
   extra round of review (security-reviewer, dhh-rails-equivalent for
   Go).
3. **PR 3** — U8, U9 (role management RPCs + `UserInfo.permissions`).
4. **PR 4** — U10, U11 (frontend permissions + admin UI).
5. **PR 5** — U13 (E2E + contract tests).
6. **PR 6** — U12 (drop `role_id` column) after PRs 1–5 have soaked.

## Scope Boundaries

### Deferred to Follow-Up Work

- **Building-level scope.** v1 ships `scope_type IN ('org','site')`. A
  follow-up plan adds `'building'`, the second composite FK to
  `building(id, organization_id)`, the third containment level in the
  resolver, and the building dropdown in the UI scope picker. Defer
  pending evidence that site-scope is insufficient.
- **Snapshot-at-issue API keys.** v1 uses live inheritance — keys reflect
  the user's current effective permissions on every request. A follow-up
  may add an issue-time snapshot mode and/or per-key permission
  declaration where the issuer picks which subset of their permissions
  the key carries.
- **Drop the renamed legacy column** (U12 — deliberately split from
  the U5 neutralization so it can soak).
- **Replace `UserInfo.role`** with a richer primary-role selection
  once the client no longer reads the field. Today the deprecated
  field uses oldest-by-created_at on the custom-role tie-break.
- **Per-scope permission map in `UserInfo`** if UI needs scope-aware
  affordances beyond show/hide.
- **Self-service access requests / approvals UI.**
- **Audit log table for permission denials.** Today denials surface
  in warn-level logs; a structured table with query affordances is a
  separate product surface.
- **Deny rules.** First-match-ALLOW resolver works for v1; if deny
  semantics ever land, the resolver needs a two-pass evaluator or a
  separate deny table.
- **Catalog description i18n.** Plain string today; localized catalog
  when the rest of the client adopts i18n.

### Outside this product's identity

- ABAC policy languages, time- or network-based attributes.
- Plugin-side authorization.
- Federated identity / SSO / OIDC integration.
- Audit log redesign as a product surface.

## Open Questions

These came out of the doc-review pass and need explicit decisions
before, or shortly after, the security-critical PR (PR 2) merges. Each
has a working default so implementation isn't blocked, but the default
should be confirmed.

**Q1 — Widen FIELD_TECH's seeded surface?** The current seed
(`fleet:read`, `miner:read`, `miner:blink_led`, `miner:download_logs`,
`rack:read`, `rack:manage`) lets a tech *find* a broken miner and
reorganize the rack layout, but not act on the miner itself. The
product-lens review argues a tech who can't `Reboot` will be routed
around on first real use. Because ADMIN and FIELD_TECH are now editable
at runtime, this question is only about what ships **by default** —
operators can widen FIELD_TECH per-org without a code release. Options:
  - **(a) Keep the read-only-plus-racks floor.** Operators add
    remediation per-org. Bets that the role editor is good enough that
    a per-org tweak is acceptable on day one.
  - **(b) Widen the seed** to include `miner:reboot`,
    `miner:start_mining`, `miner:stop_mining`. Closes the obvious
    workflow dead-end out of the box.
  - **(c) Ship both.** FIELD_TECH stays minimal; add a second built-in
    `FIELD_TECH_PLUS` seeded with the remediation set.
  Plan default while undecided: **(a)** — minimal seed, operators
  widen via the role editor when needed.

**Q2 — Resolved.** Narrowing (intersection-on-overlap) is the chosen
semantic. When a user has `ADMIN @ org` AND a narrower scoped
assignment, the site-scoped assignment overrides the org grant at
that site, while the org grant still applies at every other site.
The resolver implements the narrower-than precedence step (U6); the
RoleEditor and assignment confirmation flag this (U11) so admins know
adding a site-scoped role narrows the user at that site. Pure union
is the rejected alternative — recorded here so reviewers see what was
considered, not as an open question.

**Q3 — Live-session permission refresh trigger.** The plan currently
refreshes `UserInfo.permissions` on (a) window focus event and (b) any
`PermissionDenied` response to a previously-allowed action. Alternative
options: periodic polling (every N minutes), no refresh (force
re-login), explicit pub/sub via server-sent events. The chosen default
trades a small extra RPC per focus for "good enough" perceived
correctness without infra additions.

**Q4 — Resolved.** Settings primary nav is hidden AND the
`/settings/*` routes are layout-guarded with the same predicate
(`hasAnyPermission(ADMIN_PERMISSIONS)`). Dashboard nav and route get
the same treatment under `fleet:read`. Users with no available primary
nav land on a `<NoAccess>` terminal page. See the route-guard table in
the catalog section and the U10 file list.

**Q5 — Split FIELD_TECH from the architecture refactor?** Product-lens
suggests two plans: (a) Add FIELD_TECH as a third hardcoded role
behind the existing `RequireAdmin` pattern, shipped in one PR — fast
user-visible delivery; (b) This plan as the architecture refactor,
shipped on its own schedule. The current plan ships one coherent
shipment. Splitting trades coherence for time-to-user-value. Plan
default while undecided: **single shipment**; the trade is worth
naming explicitly if FIELD_TECH adoption is urgent.

**Q6 — Drop U11 admin UI in favor of a YAML/CLI seed path?**
Scope-guardian + product-lens both flagged U11 as heavy for a
SUPER_ADMIN-only audience. A `roles.yaml` loaded at startup, paired
with the existing `roles.yaml`-style seed reconciliation in U4, would
deliver the operator capability without three new React components.
Assignment editing stays in the Team page either way. Plan default
while undecided: **keep U11** because it matches the user's stated
"admins define custom roles" requirement; revisit if scope pressure
materializes.

### Lower-stakes follow-ups (decide during implementation)

- Whether the catalog `description` should be sourced from a single Go
  string constant or i18n-ready from day one. Default: plain string;
  the catalog is small and the project doesn't yet have i18n
  infrastructure.
- Whether `PermissionPicker` should support "include all by group"
  one-click affordances (e.g. "all miner:*"). Default: no — explicit
  selection makes the permission set auditable. Revisit if admins
  complain about repetitive checkbox toggling.
- Whether to record permission denials in a structured audit log table
  vs. existing `record_use` shape vs. warn-level logs only. Default:
  warn-level logs only; structured audit table is a follow-up.
