# Hosted account permissions

Hosted accounts separate reporting allocation, private account access and physical
fleet operations. Capability names in the client describe the existing server
bits; changing a label does not change authorization.

## Capabilities and roles

| Bit | Server capability | Client label               | Meaning                                                                                                                                                                               |
| --- | ----------------- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `View`            | View account data          | Read the selected account's permitted data. Authentication method also limits which endpoints can be called.                                                                          |
| 2   | `ManagePools`     | Manage pool configurations | Create, revise and archive saved pool configurations.                                                                                                                                 |
| 4   | `ChangePools`     | Change pools               | Apply shared configurations to currently owned miners or installed module boards using a Fleet session and current login password.                                                                       |
| 8   | `ManageMembers`   | Manage memberships         | Manage the account's team, invitations and explicit ancestor grants; also rename the account and manage site policy within the permitted scope.                                       |
| 16  | `Delegate`        | Allocate and recall assets | Manage permitted child accounts and reporting allocations. Customer administrators allocate directly held miners or hashboards (including module selections) to subclients and recall those allocations to the parent. |
| 32  | `GenerateReports` | Generate ad hoc reports    | Generate saved operational analyses. This also requires direct account-administrator membership; host preview cannot generate.                                                        |

Viewer supplies bit 1, Pool operator supplies bits 1, 2 and 4, and Account
administrator supplies all six bits. Effective capabilities combine direct
membership and explicit host/parent grants, then intersect them with every account
ceiling on the path. Suspension anywhere on that path denies account access.
Live organization affiliation and user/organization status also apply.

## Maintenance tickets

Ticketing is a separate account feature, enabled by default when creating a
client. Existing client accounts require explicit opt-in when upgrading; the
host root remains enabled so hosts can enable individual clients. The host can
change ticketing in client permissions. Disabling ticketing on an
account or ancestor removes client access to existing tickets, submission,
comments and status actions; host maintenance records remain.

All direct teammates can use ticketing when it is enabled for the account.
There is no per-teammate ticket permission. Existing pool, allocation and
device-control permissions still apply to their respective operations. Explicit
ancestor grants do not provide direct-team access. Hosts use the maintenance
queue under their existing organization and site permissions.

Client ticket RPCs require a Fleet session, the selected account header, live
account View authority and direct membership. Account API keys cannot use them.
The account response's `ticket_access` field reports this derived eligibility;
it is not an independently configurable permission.

Ticket creation requires a problem description. Clients can submit it without an
asset or diagnosis. Component defaults to Unknown until the failed part is
identified; Other identifies a known component absent from the named choices.
Hosts can correct the component and record their internal diagnosis during triage.

Host work categories are Miner, Infrastructure and Other. Miner groups standalone
miners, modules and tracked hashboards while retaining their distinct target kinds.
New equipment tickets concern one parent miner or module, optionally with that
module's affected boards, at one site. The server verifies stable identities,
current allocation and board parentage before saving and after placement locks.
Existing multi-machine tickets retain their original targets and history.

Host-created Other tickets require a site and description, without an equipment
target. Sharing requires explicitly selected affected clients, as for infrastructure
audiences. Client submissions without equipment retain the existing account-scoped
general ticket flow and do not require a site.

Asset audiences are recorded when the ticket opens. Transfers and later board
triage preserve that audience and the original installation; a new owner does not
inherit previous tickets. Miner targets retain their identifier and opening
building/rack, so queue filters match the opening placement after equipment moves.

Clients see only their own target snapshots, component text and conversation.
Current site policy and host physical-read authority redact target labels, models,
locations and installation details outside that scope. The historical target
identity, conversation and permitted close/reopen actions remain available; site
policy changes do not erase the opening audience.
Conversation responses contain up to 50 comments, oldest first, with a cursor
for loading earlier visible comments. A hidden or deleted page anchor requires
refreshing the conversation; it is not treated as the end of history. Host notes default
to internal; shared host notes inherit the ticket's audience. Client replies stay
in their account conversation. Internal notes and other clients' replies are excluded. Host operational notes and target repair evidence are excluded from
client projections. Comment author names remain visible on shared replies.

Any teammate with ticket access can close an open ticket created by their
account while it has no active parts reservations or RMA details. Once the host
has started work, the host manages closure.
Clients cannot close host-created tickets, but can reopen any visible closed
ticket. The host can close without client approval. Every closure requires an
explicit client-visible resolution note supplied in the closing request and
stored separately from operational notes;
a client's note remains private to its account while all affected accounts see
the shared status. Closing and reopening preserve host operational notes. Client
ticket creation, comments, closure and reopening record actor/account/ticket
audits in the same transaction, without conversation contents. Reopening preserves
physical repair history and consumed stock; completion forms select only active
part reservations.

Hosts can record actual module model evidence on module and board tickets and
link distinct replacement hardware. Module replacement requires a known matching
module model; the container controller model is never substituted. Historical
board selection uses the installation at ticket opening, including removed or
moved boards, without requiring another parent ticket.

Changing a live module model also requires authority at its current site, with
placement locked while the change is applied. Saving an unchanged historical
model does not overwrite newer live inventory evidence or prevent an operator
from adding repair notes to a ticket at its original site.

Ticket actions do not change device-reported maintenance status. Contractual
offline classification, SLA clocks, S3 approval and remedy calculations are not
implemented in this workflow.

A ticket that names a client's hashboard joins the open repair obligation for
that hashboard and client. If there is no open obligation, the ticket opens
one. A hashboard ticket names its hashboard when it opens. A module ticket
names no hashboard, and has no obligation, until an operator selects the
affected hashboard. A subsequent transfer starts a different obligation for
the new client. The obligation record holds only the hashboard, the client and
the linked tickets. Its start is the opening time of the linked ticket that
opened first, not the time of a later hashboard selection. Its subclient and
placement are those that this ticket recorded for the hashboard. When a ticket
closes, the obligation stays open. Evidenced restoration is not implemented,
thus no obligation has a restoration time at this time. Reports do not read
these records.

The account ticket endpoints are `ListAccountTickets`, `GetAccountTicket`,
`CreateAccountTicket`, `CommentAccountTicket`, `ChangeAccountTicketStatus`,
`ListAccountTicketAssets` on `HostedAccountService`.

Managing configurations and applying them are separate permissions. Creating an
unassigned configuration does not change equipment. Editing an assigned
configuration requires review and Change pools authority for every affected
target. Allocation alone does not grant Change pools or apply any configuration;
the initiating user's team permissions and current equipment ownership are
checked at confirmation and before each device write.

The shared `PoolConfigurationService` supplies the current library, revision,
assignment and result APIs for operators and accounts. The endpoint matrix below
documents retained `HostedAccountService` APIs. Legacy account pool library and
stored-profile methods have been removed; use the shared configuration service.

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

Repair-ticket selectors and opening ownership snapshots use the same board
allocations and derived module ownership. Pool reviews capture each board's
identity, owner, and revision. Ownership changes and controller moves retire
prior pool-setting intent, including changes made by automatic replacement.

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
moves. Replacements already pending before migration 1051 require the manual
fallback because their decision order cannot be reconstructed safely.

If a replacement is replaced again before its allocation transfer is confirmed,
the original board keeps its allocation. The latest board remains unallocated.
Later polls do not infer a chain through the unallocated intermediate board.
A host operator must use the manual fallback to select the intended replacement.

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

### Host ticket and note visibility

Host-created tickets default to internal, including tickets for client-assigned
miners, modules, and hashboards. Opening-time equipment ownership records the
potential recipients; it does not grant client access until the host explicitly
shares the ticket. Client-created tickets are shared with their opening audience.
Sharing preserves those recipients across subsequent equipment transfers.

Host comments default to internal notes at both the RPC and UI boundaries.
Hosts can post a client-visible reply or use the note's “Internal only” switch.
Turning the switch off requires confirmation and shares with the ticket audience;
turning it on hides the note from subsequent client reads. Visibility changes and
comment deletion advance the ticket version so client refreshes discard retained
older pages; responses from invalidated history loads cannot restore them. Account-scoped client
replies cannot be republished or hidden by this switch. Visibility changes check
the reviewed ticket version and advance it, so stale sharing requests cannot
republish a note after it has been made internal. Sharing a ticket leaves its
internal notes private. Internal tickets and notes produce no client ticket
list entries or direct reads. Host operational notes and target
repair evidence remain excluded from the client projection.

Migration 1048 introduces ticket audiences and explicit sharing. Legacy host
comments and newly created host notes default to internal. Opening targets and
audiences are written together in the ticket transaction; client responses use
a typed Go field allowlist with account and live site-policy filtering.

### Maintenance API request requirements

Ticket creation requires `description`; `diagnosis` is optional internal evidence.
Closing requires an explicit `resolution_note` in the request, including bulk
closure; saved operational `notes` do not satisfy it. Host creation accepts miner,
infrastructure, module and hashboard categories; account-level tickets use the
account service. Deploy callers with these updated request contracts together.

Replacement IDs are optional patches: omission preserves the recorded link. Use
`clear_replacement_module` or `clear_replacement_hashboard` to remove the link for
that target kind; a clear flag cannot accompany the corresponding replacement ID
or replacement model. Clearing a replacement field in the host editor sends the
matching clear flag and preserves the original target and model evidence.

### Contractual reporting authority

Client-generated operational analyses require a session, explicit selected-account
scope and View. Creation additionally requires the effective GenerateReports
capability and direct account-administrator membership; host preview cannot generate.
Hosts control generation through the client's capability ceiling, including all
parent ceilings. Revocation blocks new generation and retries, while existing
analyses remain readable/exportable with View and deletable by direct admins.
Migration 1064 preserves existing admin generation by enabling the new bit on
account ceilings and expanding complete administrator source grants; partial
ancestor grants remain exact. Rollback refuses an explicit client generation denial
that the older admin-only policy cannot represent.
Saved snapshots are shared with authorized account members and
remain subject to live account/site policy on list, read and each export chunk.
Repair analyses also require enabled ticket access, direct membership and current
visibility of all included tickets. Account API keys cannot generate or read these
analyses. Analysis access grants no official report preparation or publication rights.

The **Create reports** permission uses explicit account/user/site grants, separate
from account administration and ordinary maintenance permissions. Host operators
with organization `account:manage` permission can manage those grants; the client Account administrator role does not grant that authority.
Host account managers do not receive this permission automatically. A granted
operator can save validated inputs, create drafts and publish their own reports
without a separate approval step. Published snapshots and artifacts remain immutable.
Current inputs are editable; metadata-only audits retain the actor, time, source
reference and changed sections. Relevant original inputs are frozen once at
publication in a host-only source artifact. Clients receive attributed meter
consumption rather than whole shared-meter readings or allocation fractions.
Selected-account host preview is read-only, and reporting endpoints require Fleet
sessions rather than API keys.

Clients with View can inspect issued reports and frozen exports within their live
account/site policy. A multi-site report requires access to every included site;
access is rechecked before each downloaded chunk. Host drafts, setup, actor IDs
and delivery evidence are excluded from client responses. Existing general ticket
submission is unchanged; representative records do not independently enable
contractual ticket or S3 workflows.
