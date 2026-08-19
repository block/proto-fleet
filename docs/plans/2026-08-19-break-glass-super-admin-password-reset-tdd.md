---
title: "Break-glass CLI reset of the super admin password"
date: 2026-08-19
status: implementing
type: tdd
tracker:
---

# Break-glass CLI reset of the super admin password

## Context

First-run onboarding creates one SUPER_ADMIN and is globally gated by
`HasUser()`. Supported flows cannot create another: `CreateUser` and
`UpdateUserRole` reject the SUPER_ADMIN role, and the general `AssignRole` RPC
is unimplemented. The database does not enforce singleton cardinality, so
direct SQL or corruption could still produce an invalid zero/multiple state.

The in-app password reset requires an authenticated caller who can manage the
target. Losing the sole SUPER_ADMIN password therefore leaves no supported
recovery path; today an operator must hand-edit a bcrypt hash in Postgres.

The host operator already controls the self-hosted Docker Compose deployment
and its database. A narrow host-side command does not expand that trust
boundary. It reuses the existing temporary-password behavior, while making
the break-glass audit event stricter than the current best-effort in-app event.

## Goals

- An operator locked out of the sole SUPER_ADMIN account can regain access
  with one short host command, without hand-crafted SQL or bcrypt hashes.
- The reset is safe by default: strong generated temp password, forced change
  on next login, sessions revoked, and an unmistakable audit event—all in one
  transaction.
- Bare `fleetd` and its existing flags, environment variables, and YAML config
  remain compatible.

## Non-goals

- Resetting regular users; creating, promoting, transferring, or reactivating
  a SUPER_ADMIN; forgot-password/email recovery; recovery codes.
- Repairing corrupt role assignments or database schemas.
- A documented manual-SQL fallback. The CLI is the one supported mutation
  path; a missing/corrupt image should be restored from the same release.
- JSON output, interactive prompts, or confirmation prompts.

## Design

### Invocation

The operator-facing command is a release-bundled wrapper:

```bash
./reset-super-admin-password.sh [--password-stdin]
```

- Add `deployment-files/reset-super-admin-password.sh` to release artifacts and
  mark it executable. It resolves its own deployment directory and privately
  runs the `fleet-api` image's `/app/fleetd admin reset-password` command with
  the deployment's Compose project and `.env`; piped stdin uses non-TTY mode.
  Raw Compose details are implementation, not operator documentation.
- Restructure `fleetd` into kong commands with the server as the default and an
  `admin reset-password` command. Keep the current server `Config` at the kong
  root (or map it equivalently), so bare `fleetd` and existing root-level
  `auth:`, `db:`, and `http:` YAML remain unchanged.
- The admin command uses only the existing DB config and `ConnectToDatabase`;
  it never starts the server or runs migrations. Missing schema fails clearly.

### Target selection

There is no `--username`: supported deployments have exactly one live
org-scope SUPER_ADMIN. Inside the reset transaction, query and `FOR UPDATE`
lock the matching user, assignment, and role rows, requiring all three to be
live.

- One match: proceed.
- No users: point to first-run onboarding.
- No live match with existing users, including a soft-deleted SUPER_ADMIN:
  report the invariant violation; do not reactivate or promote anyone.
- Multiple matches: report an invariant violation; do not choose between them.

The password UPDATE must affect exactly one still-live selected user. A
zero-row result aborts without revoking sessions, auditing success, or printing
a password.

### Password handling

- Default: generate a strong temp password with the existing generator in
  `server/internal/domain/auth/password.go`.
- `--password-stdin`: read a non-empty supplied password from piped stdin.
- In both cases `requires_password_change = TRUE` is set — the supplied or
  generated password is a stopgap credential either way.

### Reset transaction

Generate/read and bcrypt the password before opening the database transaction.
Then, in one transaction:

1. Select, validate, and lock the target as described above.
2. Update exactly one row's `password_hash`, set
   `requires_password_change = TRUE`, and bump
   `password_updated_at` (the login path's concurrent-rotation guard keys off
   this, so a reset is safe while the server is running).
3. Revoke all sessions for the user.
4. Insert `cli_reset_password` with `Activity.Service.LogStrict` and the
   transaction context. Audit failure rolls back the reset.

The event has `actor_type = system`, nil actor `user_id`/`username`, and the
selected organization ID. Metadata contains the target's external ID and
username, never the password.

Add the searchable display label in a new migration using
`CREATE OR REPLACE FUNCTION`; down restores migration 000114's definition. Add
matching client labels/descriptions and lock icon so the UI says “Break-glass
password reset” rather than using a fallback label.

### Output

Print the temporary password once to stdout after commit and never log it.
Failures are actionable and non-zero. There is no confirmation prompt.

### Docs

- Document only `./reset-super-admin-password.sh` and an optional stdin-pipeline
  example. Do not publish the underlying Compose command or a manual SQL
  mutation recipe.

## Alternatives considered

- **Separate binary or offline `fleetcli`:** adds another release artifact or
  gives a network client a second operating mode. Reuse the shipped `fleetd`.
- **Manual SQL fallback:** creates a second, easy-to-drift credential mutation
  path and requires external bcrypt tooling. Do not bless it in operator docs.
- **Username targeting or general reset:** unnecessary under the singleton
  invariant and broadens the bypass. Fail on zero/multiple SUPER_ADMINs.
- **Reuse `reset_password`:** hides break-glass use among routine resets. Keep
  a distinct event type.

## Risks

- **Server startup regression:** preserve root config and test existing parsing
  plus bare invocation.
- **Concurrent account mutation:** lock and revalidate the target, and require
  one updated row.
- **Audit/schema failure:** fail closed without changing credentials. Schema
  repair is outside this command.
- **Credential leakage:** output once after commit; never log or persist it;
  force change on login.
- **Host takeover:** accepted because the host operator already controls the
  application database.

## Test plan

- **Reset integration:** generated and stdin passwords; forced-change flag and
  timestamp; old password rejected; sessions revoked; strict system-attributed
  event with target metadata; new password accepted.
- **Failure/locking:** zero users, no live SUPER_ADMIN, soft-deleted account,
  multiple SUPER_ADMINs, concurrent invalidation, zero-row update, and forced
  audit failure all exit without partial changes or password output.
- **Compatibility:** bare `fleetd`, existing YAML/env/root flags, and admin
  help. Wrapper tests pin project/env resolution, argument forwarding,
  non-TTY stdin, executable release packaging, and the internal absolute binary
  path.
- **Activity:** migration up/down restores the prior function; client tests
  cover row/detail/filter labels, target username, and icon.
- **Smoke:** run `./reset-super-admin-password.sh`, log in with the printed
  password, complete forced change, and verify the activity row.
