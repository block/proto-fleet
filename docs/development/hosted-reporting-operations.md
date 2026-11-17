# Hosted report operations

The Reports workspace prepares monthly, quarterly, annual and ad hoc reports
from validated saved facts. Publication makes an immutable version available to the
client; delivery confirmation is a separate recorded event. A provisional or
unavailable metric remains visibly qualified after publication.

Each client, official report type and period has one original. Continue its draft
or prepare a replacement of its current publication rather than creating another
original. A confirmed delivery remains confirmed when later delivery attempts fail.
Delivery timestamps cannot precede publication of the exact version being delivered.

## Access and setup

Open **Settings → Reports** in the host workspace. The **Work queue** lists
cross-client obligations; open a row to review its details and available actions.
**Client reports**, **Report requests**
and **Reporting settings** separate preparation, request handling and setup for
the selected client. Workflow, data quality and delivery are independent: a
published version can still have missing required information or unconfirmed delivery.
Host operators with organization `account:manage` permission can assign reporting
authority. The client Account administrator role does not grant this authority,
and host account management itself does not permit preparation or publication.
Grant **Create reports** explicitly per account and site. This one permission
allows saving validated inputs, creating drafts and publishing them.
The creator can publish their own draft; no separate reviewer or submission step
is required.

Clients read issued versions through their account's Reports tab using existing
View and site access. Drafts, host identities and delivery notes stay private.
Selected-account host preview cannot use preparation or publication actions.
API keys cannot call reporting endpoints. A report spanning several sites is
accessible only while the reader can access every included site; downloads
recheck that authority before each stored chunk.

The library uses the shared report-type filter and separate report-name and
report-type columns. Opening any cell in a row navigates to that report or saved
analysis; keyboard users can open the focused row with Enter or Space.

The client report library also contains **Generate report** analyses for miner
performance, energy usage and issues/repairs. These immutable account-shared
snapshots have CSV exports and do not enter the official report publication or cadence
workflow. Performance uses historical allocation-authorized readings; energy
uses saved whole meter intervals; repair activity uses client-visible tickets
and labels status as current at generation. Performance calls positive-hash
sample percentage **Observed hashing availability**, not contractual uptime;
intentional curtailment can lower it. Existing saved snapshots and CSVs retain
their original labels. Missing evidence remains explicit.
Generation requires direct account administration and the client's **Generate ad
hoc reports** permission, bounded by parent account permissions. Deletion requires
direct account administration even when generation is disabled. Viewers can read
and export, and host preview remains read-only. Saved
analyses retain live account/site access checks; repair snapshots also require
current ticket access and visibility of every included ticket.
Generation retries reuse a request identity and return the same saved snapshot.
After confirmation, deletion permanently removes the snapshot and CSV while
retaining a metadata-only audit and request receipt. Retrying the original
generation request cannot recreate it. Generation has bounded process-wide
concurrency; library pagination does not bound generation cost or saved storage.
Lists use bounded pages; clients can search across saved analyses and load more
results. Analysis energy rows show attributed consumption, without whole shared-meter
readings or allocation fractions. Browser downloads verify the frozen artifact digest.

Where a secure browser supports saving directly to a file, reporting downloads
write one chunk at a time and hash incrementally. The browser commits the file
only after the complete artifact matches its frozen digest. Cancellation, access
loss, interrupted streams and integrity failures abort the uncommitted file.
Canceling the file picker does not start an export or select another save method.

Other browsers use a 32 MiB retained-buffer policy. The browser rejects the next
chunk before retaining bytes beyond that budget, cancels the export and offers
no partial file. The error directs the reader to ask their host for the complete
file. Parts plus Blob construction can use roughly 64 MiB plus a producer chunk;
actual engine overhead varies. This is a browser memory policy, not an input,
artifact or API size limit: stored artifacts and API exports remain complete.
The native file path can receive larger artifacts without retaining the complete
file in JavaScript. Prior sampled workloads do not establish this browser policy
or large-download delivery acceptance; G4 commissioning must exercise the intended
browsers, artifact sizes, memory behavior and complete digest-verified delivery.

Record the agreement reference, IANA timezone, reporting-year boundary, holidays,
source identities and representative evidence. Schedule starts are explicit first
required monthly, quarterly and annual periods, with a reporting owner and
supporting calendar reference. An omitted cadence is not inferred from an
allocation date. Check each configuration revision against its source before saving.
Only account team managers can select or change authorized representatives.
Report creators can preserve those recipients while editing other settings.

## Curtailment observations

Newly prepared reports include automatically captured curtailment control
confirmations in **Events**. Open a row for its evidence basis. Conventional
miners use their original allocation; shared containers contribute only the
client's originally installed, allocated hashboards. Host reasons, actors,
controller identifiers and other clients' hardware are excluded.

**Restoration unconfirmed** means no matching positive restoration is available
within that row's reporting cutoff. Release, failure, event completion and
installation removal do not establish physical restoration. A cutoff bounds the
reporting window; it does not mean the equipment was continuously observed or
curtailed through that instant. Later confirmations supersede earlier windows.
These control endpoints do not change reviewed curtailment duration, contractual
uptime/exclusions, hosting fees or credits. Those still require reviewed inputs
and approved contract calculations. Absence of observations is not proof of zero activity.

Capture starts with the upgraded application after migration 1092; older mutable
target summaries are not backfilled. Both confirmation and capture must precede
the as-of cutoff. Preparation freezes these rows and their site scope alongside
other report results; later restoration never rewrites a published report.
The capture tables have no automatic expiry or application update/delete API.
Include their storage in the existing reporting evidence backup and capacity
budget. The down migration refuses to discard any captured confirmations.

## Prepare and issue

1. Record effective orders and commercial coverage separately from physical
   allocation. Include source references and historical allocation interval IDs.
   Unsupported past ownership is unavailable, even if the asset belongs to the
   client today.
2. Import supported fixed-format meter, fee, incident, hashrate, pool, obligation
   or externally calculated facts using the supplied templates. Keep credential
   material, internal ticket notes and infrastructure errors out of source
   references and narratives. Validate register completeness explicitly, including
   genuinely empty registers; absence of rows is not proof of zero activity.
3. Save the validated inputs. Each account has one editable current input set;
   saves replace it using a revision counter to reject concurrent edits. The
   audit records who changed it, when, the source reference and changed sections,
   without retaining previous contents. Later input changes invalidate an older
   draft's eligibility for publication; recalculate it against the current inputs.
4. Generate the period draft and inspect metric causes, source windows and both
   alternative breakdowns. Container and order totals describe the same estate;
   they must each reconcile and must not be added together.
5. Publish the exact draft revision when ready. The creator or another operator
   with Create reports can publish. An incomplete issue requires an explicit exception explaining
   the missing evidence; the exception does not make its values authoritative.
6. Download the printable HTML and evidence ZIP. The ZIP includes CSV tables and
   a JSON manifest with exact values, units, provenance and file digests. Stored
   artifact bytes are frozen within the publication transaction. Client meter
   tables contain attributed consumption only; original readings and allocation
   fractions remain host-only. **Download source inputs** in the host report
   actions menu provides the relevant original inputs frozen at publication,
   including comparison and episode context. This JSON file uses the existing
   artifact storage; later input edits do not change it. Reports retain their
   input revision counter and existing telemetry evidence links, without a
   separate input-history table or input digest. Older evidence ZIPs without this
   boundary remain host-only until a correction supplies safe client evidence.
7. Record the agreed delivery channel and actual send/receipt evidence. Portal
   publication alone does not confirm delivery. Keep failed attempts and their
   follow-up visible until a supported confirmation is recorded.

Each preparation accepts at most 10,000 facts, including orders, and 1,024 combined
container and order breakdowns. Results are limited to 8 MiB of JSON and written
reports to 16 MiB of HTML. Preparation, publication and queue calculation honor
cancellation and operation deadlines. Evidence ZIPs stream into stored chunks.
Exceeding a preparation or publication limit rolls back the operation.

Reports open as a workbook with one grid at a time. Results use one row per
container, hosting order or account, with measures as columns. The alternative
breakdowns are selected separately. Columns can be chosen and reordered; long
grids are paged. Each value retains its data-quality status. Selecting a value
opens the filtered Evidence sheet. Events and Evidence use compact, clickable
rows; opening a row shows its full details in the shared modal. Qualifications,
methods and sources remain in Evidence details, while value and quality stay
visible in the table. Repairs and reconciliation each have their own sheet.

Drafts can be recalculated before publication. Published reports and downloads
cannot be edited. Use **Replace report** in the report actions
menu to prepare a replacement for the same period, with a client-visible reason.
It uses the same Draft → Published workflow. Operators share one
active replacement draft. The library shows the current publication, and host
views also show pending drafts. Earlier publications are available in the
secondary **Publication history** section when a replacement exists. The stored
correction relationship and publication numbers remain unchanged internally.

Ad hoc requests retain their receipt time, requester, scope, owner, target,
limitations and communication/delivery references. Requested drafts retain the
canonical request identity and original receipt. A confirmed delivery of a linked
issued report fulfills that request. For packages delivered outside Fleet,
record external fulfillment with the actual delivery time, package reference and
confirmation evidence; this appends an idempotent record without rewriting the
receipt. An original reference alone never confirms fulfillment. Account-wide
request access requires unrestricted effective account and site authority.
Acknowledging queue work does
not acknowledge or satisfy the contractual report request itself.

## Queue and notification handling

The runtime reporting job materializes elapsed scheduled periods and existing
reports and ad hoc requests as durable queue work. It records missing inputs,
draft/publication/delivery state and deadline qualifications. Current source health stays in
reporting settings. Published reports with confirmed delivery leave the work queue;
their data-quality qualifications and work history remain available. With no
approved contractual calendar, displayed internal targets remain provisional;
there is no invented contractual overdue determination.

Notification work uses persisted claims, generations and retry history alongside
the existing alert delivery infrastructure. Configure and verify the normal alert
routes before relying on notification delivery. A successful internal notification
is not evidence of client delivery. Acknowledge the exact queue generation with a
reason; changed work creates a new generation. Retry failures from the queue and
inspect the recorded attempts. Notifications contain an actionable queue link,
not report contents or financial figures.

Automatic notification delivery makes at most five attempts per generation, with
delays of one minute, five minutes, fifteen minutes and one hour. Accepted
destinations are retained so partial-failure retries skip them. No accepted route,
revoked authority or exhausted attempts requires operator review and an explicit
retry. Delivery remains at least once across process loss between destination
acceptance and recording that acceptance. Missing-period calculations reuse the
same current inputs until their revision counter changes.
Transient destination and cold routing lookups use the same automatic retry policy;
intentional routing suppression still requires operator review.

## Commissioning and interim reporting

Operations and commercial owners must supply and review these inputs before
calling the reporting process contractually ready:

| Gate | Required evidence                                                                                                                   |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------- |
| G1   | Signed offline, episode, uptime, exclusion, remedy and backlog definitions, with approved worked examples and implemented formulas. |
| G2   | Qualified deployed meter/pool/host sources, source timestamps, estate coverage and actual polling/capture performance.              |
| G3   | Effective orders, commercial coverage, commissioning, attribution/allocation method, warranty and finance-approved fee evidence.    |
| G4   | Measured evidence/artifact growth, retention budget, backup controls and a successful restore rehearsal on the intended estate.     |
| G5   | Agreed timezone/calendar, first required periods, representatives, operational owner and client delivery procedure.                 |

A readiness flag cannot supply a missing formula or source. Reviewed external
calculation results remain explicitly provisional, with their supporting
reference. Missing contractual definitions produce unavailable values. Power is
not metered energy, positive hashrate is not contractual uptime or pool reporting,
and runtime-job health is not dashboard availability. Dashboard availability
remains unavailable without independent client-visible probe evidence.

Fleet provides hashboard allocations and automatic/manual replacement transfer.
Reviewed coverage, hashrate and pool facts support conventional miners, current
hashboard allocations and retained legacy module periods. Board facts must match
their retained account, site, controller and allocation window; current placement
cannot rewrite historical attribution. Contractual repair obligations remain
module-based. Import reviewed `moduleCoverage` separately from performance coverage,
referencing this account's board coverage IDs and a finite historical window.
Each reference must match the order, site and controller throughout that window;
retained installations must prove unambiguous module and slot membership.
`wholeModule=true` requires all three distinct slots and permits module counts.
Partial mappings support reviewed module ticket/episode facts, without a
whole-module count. Split mappings at installation or ownership changes; a board
removal or transfer is not evidence of restoration. The calculations union
module windows so boards and repeated episodes do not multiply module counts.
Unmapped/shared windows qualify supported counts; the unknown module population
remains unavailable. Ticket counts retain their independent register-completeness
and observation qualifications. A mapping never supplies outage, SLA or credit
rules; those still require explicit reviewed obligations and contractual evidence. Qualified contractual
ticket adapters remain integration work; allocation transfer alone does not
establish hosting order coverage or contractual uptime. Reconcile the source contracts and approved
G1–G3 evidence before replacing reviewed imports. Runtime counter capture remains
deferred until an approved report field consumes it.

Before the first contractual delivery, name the interim reporting/on-call owner,
record the first monthly/quarterly/annual due dates from the agreement, and set
owner target dates for obtaining G1 and G5. For periods closing earlier, prepare
and publish the agreed manual package, label pre-history and missing
sources, preserve its evidence, and reconcile any later Fleet correction with
that original. Applying migrations or merging the stack does not commission the
process or establish that the agreement has been fulfilled.

The prospective observation hold retains its separate epoch, capacity and restore
limits described in [Hosted reporting evidence](hosted-reporting-evidence.md).
Issued input/evidence links and artifact chunks are preserved without an automatic
purge. Populated-history down migrations refuse destructive rollback; plan
forward corrections and verify storage/backup acceptance before deployment.
