# Hosted account permissions

Hosted accounts separate reporting allocation, private account access and physical
fleet operations. Capability names in the client describe the existing server
bits; changing a label does not change authorization.

## Capabilities and roles

| Bit | Server capability | Client label               | Meaning                                                                                                                                                                               |
| --- | ----------------- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `View`            | View account data          | Read the selected account's permitted data. Authentication method also limits which endpoints can be called.                                                                          |
| 2   | `ManagePools`     | Manage pool configurations | Create, revise and archive saved pool configurations.                                                                                                                                 |
| 4   | `ChangePools`     | Select stored pool profile | Select or clear the stored profile's pinned pool revision using a Fleet session and the current login password.                                                                       |
| 8   | `ManageMembers`   | Manage memberships         | Manage the account's team, invitations and explicit ancestor grants; also rename the account and manage site policy within the permitted scope.                                       |
| 16  | `Delegate`        | Allocate and recall assets | Manage permitted child accounts and reporting allocations. Customer administrators allocate directly held miners or hashboards (including module selections) to subclients and recall those allocations to the parent. |

Viewer supplies bit 1, Pool operator supplies bits 1, 2 and 4, and Account
administrator supplies all five bits. Effective capabilities combine direct
membership and explicit host/parent grants, then intersect them with every account
ceiling on the path. Suspension anywhere on that path denies account access.
Live organization affiliation and user/organization status also apply.

Saved pool configurations and stored profile selection are separate permissions.
Neither operation applies pool settings to existing or newly assigned mining
assets. Allocation moves reporting responsibility; it does not move physical
equipment, apply pools or enable firmware commands.

## Hashboard allocations

CT1 allocation uses stable hashboard IDs. A module selection supplies the exact
reviewed board IDs and revisions. The server assigns, delegates, or recalls that
set in one transaction. It rejects changed membership or allocation revisions.
A conventional miner remains one whole-device allocation.

A module appears only when its three slots establish a nonempty membership and
the account owns every installed board. Confirmed empty slots are permitted;
unknown or conflicting slots do not establish membership. Parent accounts can
see a module whose boards belong to different subclients. Each partial owner
sees only its boards. Discovery gaps and module retirement preserve allocations.

Inventory updates detect a replacement when a different board appears in the same
slot of a module with a confirmed PSU identity. The new board inherits the old
board's client and subclient allocation. The old board returns to the host.
The update records both changes and a system activity event in one transaction.
History changes at the accepted observation time; delayed confirmation does not
backfill ownership. Empty or unknown slots preserve the old allocation.

Automatic transfer requires an unallocated replacement and an old board that has
not moved elsewhere. The source map must be complete. Every other registered CT1 controller must provide complete
inventory accepted after the candidate installation, with an observation time at
least as recent. Missing, incomplete, or stale peer reports defer the transfer;
later source polls retry it automatically. This preserves ownership when the
destination reports a moved board after the source reports its replacement.
Unresolved identities, later manual allocation decisions,
suspended accounts, and destination site restrictions prevent the transfer.
The database records decision order separately from device timestamps and physical
moves. Replacements already pending before migration 1049 require the manual
fallback because their decision order cannot be reconstructed safely.

As a manual fallback, host operators can use **Individual hashboards** in a
client's Mining assets page to select an old board and **Replace hashboard**.
The replacement must be available and unallocated. The operation preserves the
client/subclient chain and checks both physical read authority and destination
site policy. This action requires a selected host account and a Fleet session.
Customers and account API keys cannot perform it.
The replacement picker resolves the reviewed board's full owner path, including
subclient policies and parent site ceilings, before filtering and paging spares.
The transfer checks this policy again in case it changed after selection.

Manual changes use one history timestamp for every affected miner and board.
It is no earlier than database time or any selected asset's recorded history,
including closed periods. This keeps recall, delegation, reassignment, and
replacement valid when an accepted inventory timestamp is ahead of the database.

Current summaries and allocation counts use miners and hashboards. Modules remain
inventory groups with separately labeled module/PSU power in their detail view.
Account totals never substitute module power for missing board measurements.
New historical reporting follows board allocation periods and uses measured board
power. Earlier module periods remain labeled legacy module history. No past board
ownership is inferred from current membership.
History buckets split at board allocation changes, including migration and board
replacement. Sequential equipment is not added as simultaneous capacity.

## Host and parent access

**Host and parent access** is the UI name for explicit ancestor grants. A grant
does not add a team membership. A named hosting root requires the operator's live
organization `account:manage` authority; a non-root ancestor requires live
membership in that ancestor. Granted capabilities remain bounded by the user's
source role, the grant and the account ceilings. Direct membership can coexist
with granted access, and the displayed membership role remains that direct role.
When no direct role exists, the UI identifies the access as Host and parent access.

Physical custody and membership in a parent do not by themselves grant private
child access. Host or direct-parent authority can manage permitted allocations
and child ceilings without access to the child's private pools, team or keys.
The host's physical fleet operations continue under organization permissions.

The Access page can create a grant to the direct parent and edit existing
ancestor grants. A subclient's first hosting-root grant cannot be created there
because the API does not expose the required root metadata.

## Endpoint and authentication matrix

The following names are methods on `HostedAccountService`. A capability is
necessary but does not replace live account, site or resource authorization.
Host recovery and direct-parent management paths have additional checks.

| Endpoints                                                                                      | Required capability                                                    | Fleet session                    | Account API key                            |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- | -------------------------------- | ------------------------------------------ |
| `GetAccount`, `GetSitePolicy`                                                                  | `View`                                                                 | Yes                              | Yes, for the key's selected target account |
| `ListPoolConfigurations`, `GetPoolRevision`, `GetIncomingPoolProfile`                          | `View`                                                                 | Yes                              | Yes                                        |
| `CreatePoolConfiguration`, `RevisePoolConfiguration`, `ArchivePoolConfiguration`               | `ManagePools`                                                          | Yes                              | Yes                                        |
| `SetIncomingPoolProfile`                                                                       | `ChangePools`                                                          | Yes, with current login password | No                                         |
| `GetAccountDashboard`, `GetAccountMiningSummary`, `ListAccountAssets`, `GetAccountAssetDetail` | `View`                                                                 | Yes                              | No                                         |
| `ListAccountActivities`, `ExportAccountActivities`                                             | `View`                                                                 | Yes                              | No                                         |
| `AssignAccountAssets`, `ReplaceAccountHashboard`                                                                          | `Delegate`                                                             | Yes                              | No                                         |
| `ListChildren`, `CreateAccount`, `ConfigureAccount`                                            | `Delegate` or checked host management authority                        | Yes                              | No                                         |
| `RenameAccount`, `ListMembers`, `SetMemberRole`, `RevokeMember`                                | `ManageMembers`                                                        | Yes                              | No                                         |
| `ListAncestorGrants`, `SetAncestorGrant`, `RevokeAncestorGrant`, `SetSitePolicy`               | `ManageMembers`, with checked host/parent alternatives where supported | Yes                              | No                                         |
| `ListInvitations`, `CreateInvitation`, `RevokeInvitation`                                      | `ManageMembers`                                                        | Yes                              | No                                         |
| `CreateAccountKey`, `ListAccountKeys`, `RevokeAccountKey`                                      | `View`, with issuer/administrator restrictions                         | Yes                              | No                                         |
| `ListAccounts`                                                                                 | Live identity and account discovery authority                          | Yes                              | No                                         |

Account keys therefore do **not** provide dashboard, inventory, historical or
activity reporting, even when their `View` bit is set. Organization API keys
cannot access customer accounts. Account keys cannot borrow the issuer's host,
sibling-account or other organization permissions.

## API-key scope and lifecycle

The key form requires View and offers optional Manage pool configurations. It
does not offer stored profile selection. The server's existing machine capability
mask still accepts `View | ManagePools | ChangePools` (bits 1–7), but a stored
`ChangePools` bit does not authorize profile selection: the authentication
allowlist excludes that RPC, and the domain operation independently requires a
session and password verification. Existing keys and capability values retain
their current behavior.

Key creation requires a customer or subclient account, View, and requested bits
within the issuer's current effective authority. A key is account-only by default.
A customer can select up to 100 accessible direct subclients or opt into all
current and future direct subclients; a subclient cannot include descendants.
The scope itself grants no membership or account access.

Each request rechecks the credential and intersects its stored mask with the
issuer's live effective capabilities on both the issuing account and the selected
target. Role changes, grant revocation, account ceilings, suspension on either
path, user/organization removal, key revocation and expiry can narrow or deny
access. Selecting future subclients never bypasses these checks. Requests must
select the target account consistently with `X-Fleet-Account-ID` and the RPC body.

Members can list and revoke their own keys; users with Manage memberships can
list and revoke all keys in the account. Key secrets are returned only at
creation. The UI offers seven-, 30- and 90-day expiry choices.

## Authorization sources

- [Capability values, roles and live resolution](../../server/internal/domain/hostedaccounts/service.go)
  and [authority query](../../server/sqlc/queries/hosted_account_authority.sql).
- [RPC capability map](../../server/internal/handlers/middleware/rpc_permissions.go)
  and [authentication allowlists](../../server/internal/handlers/interceptors/authentication.go).
- [Account and administration handlers](../../server/internal/handlers/hostedaccounts/handler.go),
  [reporting and allocation handlers](../../server/internal/handlers/hostedaccounts/assets.go),
  [activity handlers](../../server/internal/handlers/hostedaccounts/activity.go) and
  [pool handlers](../../server/internal/handlers/hostedaccounts/pool.go).
- [Key authority and lifecycle](../../server/internal/domain/hostedaccounts/key.go),
  [credential authority query](../../server/sqlc/queries/hosted_account_key.sql) and
  [profile selection and password verification](../../server/internal/domain/hostedaccounts/pool.go).
- [Key endpoint integration coverage](../../server/internal/handlers/hostedaccounts/key_integration_test.go)
  and [pool endpoint integration coverage](../../server/internal/handlers/hostedaccounts/pool_integration_test.go).
