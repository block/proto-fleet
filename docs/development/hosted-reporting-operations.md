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
3. Save configuration and changed register rows using the current account revision.
   Each accepted batch changes only its specified rows, increments the revision,
   and records the actor, time, source reference and changed sections without
   retaining previous contents. Imports commit bounded batches and show committed
   progress; a later failure leaves earlier accepted batches intact. Register
   pages carry that revision, so a concurrent change requires reloading before
   continuing. Later input changes invalidate an older draft's eligibility for
   publication; recalculate it against the current facts.
4. Generate the period draft and inspect metric causes, source windows and both
   alternative breakdowns. Container and order totals describe the same estate;
   they must each reconcile and must not be added together.
5. Publish the exact draft revision when ready. The creator or another operator
   with Create reports can publish. An incomplete issue requires an explicit exception explaining
   the missing evidence; the exception does not make its values authoritative.
6. Download the printable HTML and evidence ZIP. The ZIP includes CSV tables and
   a JSON manifest with exact values, units, provenance and file digests. The
   client evidence profile `hosted-report-evidence/v4` keeps report-period client
   tables separate from the host-only source JSON; older profiles remain subject
   to their publication safety checks. Stored artifact bytes are frozen within the publication transaction. Client meter
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

The authoritative current facts are indexed rows. There is no 10,000-fact or
8 MiB account-lifetime quota. One mutation request accepts at most 200 changes
and 1 MiB of canonical mutation JSON; configuration has a separate 1 MiB bound.
Each accepted batch validates its final relations and physical witnesses and
writes facts, scope, revision and audit together in a five-second transaction.
Register reads return at most 200 rows and 2 MiB of encoded page data, with
revision-bound cursors.

Preparation and publication select only the complete current/comparison/support
context needed for that report. Metadata preflight limits selected canonical
payloads to 32 MiB and estimates heap use against 256 MiB before decoding them;
selection has a 45-second deadline. One ordered metadata query returns the
preflight-approved keys, then payload reads remain page- and byte-bounded. This
is an operation budget, not a fact-count
limit. It does not establish peak resident memory or guarantee every continuous
fleet workload fits. Host source JSON is streamed fact by fact into stored chunks
and has a separate 64 MiB output bound. A report that exceeds these bounds fails
without changing its draft, artifacts or audit; small mutations and bounded
register reads remain available.

Control observations are captured in one SQL statement so an atomic restoration
cannot become partly visible between reads. The fixed-width capture buffer has a
separate 16 MiB estimated memory budget (512 bytes per window), with one extra row
detecting exhaustion; the existing result-byte and 45-second capture bounds still
apply. This bounds one report operation, not retained account facts.

Preparation admits at most 1,024 combined container and order breakdowns. Results
have an 8 MiB conservative JSON budget and written reports a 16 MiB HTML cap.
Preparation and publication each have a 60-second operation and transaction
deadline; cancellation or failure rolls back the operation.

Artifacts are persisted in 64 KiB chunks. This bounds a chunk buffer, not the
total archive bytes, total storage, or the complete calculation's memory. Every
evidence hour selected by the report's evidence cutoff is exported, hourly or as
day rollups (see [Evidence export](hosted-reporting-evidence.md#evidence-export));
there is no evidence-row admission cap. A timeout can therefore be the effective
boundary. Do
not truncate required evidence or enlarge a limit to represent an unsupported
workload as complete.

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

Account discovery has a five-second query deadline. If discovery times out, the
worker retries on its next tick without cancelling the long-lived worker
context. Per-account transaction bounds and notification claims remain separate.

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

G1 includes obtaining the contractual formula/source decision and tracked
implementation in [issue 272](https://github.com/btc-mining/proto-fleet-internal/issues/272).
Signed definitions and worked examples must cover uptime, exclusions, remedy and
credit calculations, episode continuity and backlog conventions. A readiness
flag cannot supply a missing formula or source. Reviewed external
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
rules; those still require explicit reviewed obligations and contractual evidence.

[PR 344](https://github.com/btc-mining/proto-fleet-internal/pull/344) being merged
does not establish that qualified ticket capture is deployed, active or complete
for this account. Verify the deployed build, source semantics, account visibility,
coverage start, receipt/correction handling and the consuming report field before
calling that adapter an authoritative contractual source. Allocation transfer
alone does not establish hosting order coverage or contractual uptime. Reconcile
the source contracts and approved G1–G3 evidence before replacing reviewed imports. Runtime counter capture remains
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

## Reproduce the sampled lifecycle workload

`TestHostedReportingWorkloadLifecycle` exercises bounded mutations → Prepare → Publish →
all three stored artifact downloads through the real service and a disposable
Postgres/Timescale database. It creates a synthetic account with one CT1,
six tanks, 96 modules, 288 allocated boards, 96 explicit qualified historical
module mappings and one hosting order. Bulk SQL constructs the historical
fixture; actual board metrics and diagnostic writes use the existing
normalization/capture triggers. It does not benchmark controller ingestion or
establish a production client's estate.

| Profile | Aggregate fact dimensions | Total |
| --- | --- | ---: |
| September 2026, 30 days | 288 board coverage + 96 module mappings + 1 order + 8,640 daily board hashrate intervals + 30 container meter intervals + 1 fee + 4 incidents + 8 module obligations + 288 pool observations | 9,356 |
| Calendar 2025, compared with 2024 | Same coverage/mappings/order + 6,912 monthly board hashrate intervals over both years + 24 meter intervals + 24 fees + 4 incidents + 8 module obligations + 288 pool observations | 7,645 |

These facts are aggregate daily/monthly intervals, not five-minute or full-density
telemetry. The annual case supplies distinct prior/current hash and meter values,
asserts the calculated year-over-year changes, and checks that the host source
JSON retains both comparison years. Only current-report-period monitoring hours
are frozen; prior-year typed facts do not imply prior-year frozen monitoring.
The fixture leaves prerequisites unapproved and sources uncertified, publishes
with an explicit synthetic exception, and asserts the result remains incomplete.

From the repository root, use Hermit and a dedicated local test database:

```sh
eval "$(./bin/hermit env --activate)"
cd server
DB_ADDRESS=127.0.0.1:58769 DB_PASSWORD=fleet HOSTED_REPORT_WORKLOAD=1 \
  go test ./internal/domain/hostedaccounts \
  -run '^TestHostedReportingWorkloadLifecycle$' -count=1 -v
```

Use the database address for the intended disposable test instance. The harness
creates and drops a separate database per test. `HOSTED_REPORT_WORKLOAD_POLLS`
accepts 20 (default), 200 or 1000 sampled hours. Each sample writes one closed
evidence hour for each of the 288 board intervals, so twenty samples freeze 5,760
hours. One hour is corrected with a second revision before preparation and
frozen at that revision; a third revision written after preparation has a bucket
inside the period and is excluded by the draft's evidence cutoff. The monthly
profile exports hourly observations; the annual profile exceeds the hourly limit
and exports day rollups. These samples are sparse across the month/year; the
multiplier is not a cadence.

The test logs exact input/result bytes, mutation/Prepare/Publish duration, frozen
membership, and each download's bytes, chunk count, duration and SHA-256. It
streams service chunks to temporary files, verifies stable authority fingerprints
and outer digests, then verifies ZIP member byte counts/digests and CSV row counts.
The cancelled-publication attempt must leave no artifacts or publication audit.
`TestHostedReportingWorkloadInputBudgetsPreserveCurrentRecord` separately rejects
oversized mutation count/bytes while preserving current facts, scope, revision
and audit, and verifies bounded reads still work. The selected-context exhaustion
test rejects oversized report work before payload decoding or report changes,
then accepts a small update and a bounded read. Existing output-limit and
publication rollback tests cover excess breakdowns, oversized result/HTML and
transactional failures; there is no evidence-row cap.

On 2026-11-09, the integrated indexed-fact and publication implementation passed
both default 20-sample profiles against isolated local Docker Postgres/Timescale.
That run predates hourly evidence: it froze raw monitoring events, so the evidence
archive figures it recorded do not apply and are omitted here. For hourly
evidence export measurements, see
[Evidence export](hosted-reporting-evidence.md#evidence-export).

| Bounded-fact observation | Monthly | Annual with prior year |
| --- | ---: | ---: |
| Input fixture JSON bytes | 2,624,555 | 2,136,216 |
| Facts | 9,356 | 7,645 |
| All bounded mutation batches | 26.595 s | 22.232 s |
| Prepare / 60 s | 0.861 s | 0.784 s |
| Publish / 60 s | 2.461 s | 2.571 s |
| Result JSON bytes / 8,388,608 | 67,870 | 69,904 |
| Combined breakdowns / 1,024 | 2 | 2 |
| Written HTML bytes / 16,777,216 | 77,312 | 78,889 |
| Host source JSON bytes | 2,624,792 | 2,136,452 |
| HTML / source download | 15.3 / 147.9 ms | 17.2 / 105.8 ms |

Mutation duration is the sum of many accepted operations, each with its own
five-second transaction budget. It is not one atomic whole-file transaction.
These local sequential latencies exclude fixture construction, network delivery,
production concurrency, WAL and backups, and are not an SLA. Digests are checked
on every run rather than fixed as snapshot constants, because report identities
and capture times change. Browser memory and native/fallback file saving are
verified separately; this service fixture does not measure browser delivery.
The earlier whole-input implementation passed a 200-sample profile, but that
historical run does not validate the current implementation at that density.
No current 200- or 1000-sample run, full-cadence year, or
32/128/512-container/order sweep is established by these results.

`TestHostedReportingLargeAccountLifecycle` separately exercises two accounts
through legal 200-change mutations, every bounded register page, preparation,
inspection, publication and all three immutable downloads:

| Large-account observation | Irrelevant accumulated history | Relevant fee stream |
| --- | ---: | ---: |
| Current facts | 12,002 | 10,203, including 10,201 relevant fees |
| Canonical fact payload bytes | 14,268,372 | 12,129,361 |
| Complete register traversal pages | 240 | 206 |
| All bounded mutation batches | 19.620 s | 17.678 s |
| Prepare | 105.1 ms | 440.1 ms |
| Publish | 121.6 ms | 946.0 ms |
| Frozen host source JSON bytes | 1,004 | 12,140,193 |
| Frozen host source chunks | 1 | 186 |

The history case reproduces the small baseline exactly and omits irrelevant fees
from its selected source. The relevant case checks every original fee identity,
exact amount, full interval and source reference in a source artifact above
8 MiB. Both traverse every stored fact exactly once without a response exceeding
200 rows or 2 MiB. Every downloaded artifact matches its outer SHA-256; every ZIP
member has one manifest entry whose byte count, digest and CSV row count match.
Client source-artifact access remains denied. These are synthetic commercial
facts, not a claim that arbitrary telemetry density or estate size fits the
selected-report operation budget.

The pure-calculation output guard continues to reject repeated source lists
before constructing a result above the existing 8 MiB budget. The breakdown cap
alone does not establish useful throughput at its maximum cardinality.

## Evidence density and production capacity

Typed-fact admission and evidence volume are independent. Hourly evidence
storage, closing cost, export representation and the publication deadline are
measured in [Hosted reporting evidence](hosted-reporting-evidence.md#sizing).

G4 commissioning must record the real client/CT1/board estate, cadence,
correction rate, evidence volume, fact aggregation granularity, concurrent
report load, storage and WAL growth, backup duration, and restore acceptance.
Rehearse the complete month/year lifecycle and restore on representative retained
history; record the build, migration catalog, hardware and measured boundary.
Restore checks must include evidence hours and revisions, report evidence
cutoffs, allocation identities, artifact byte counts/digests and access after
restore. A benchmark pass or a merged PR is not storage/backup/restore acceptance.

If required evidence exceeds a transaction's measured capacity, record the
failing stage/count/bytes and keep the contractual limitation visible. A
followup must address freeze/export/retention representation without omitting
required evidence. Any future incremental fact store must specify backfill,
atomic revision/site/audit maintenance, full validation, consuming service/UI
wiring and cutover before being represented as complete. No new cap,
asynchronous job system or source certification is implied by these
measurements.

## Migration and recovery checkpoints

Reporting builds include these internal migrations. 1077 replaced the earlier
1055 raw-event hold with hourly evidence; see
[Hosted reporting evidence](hosted-reporting-evidence.md#upgrade-and-rollback). Startup applies pending migrations; upgrading a binary is not
a preparation-only action. Follow the supported catalog/checkpoint procedure in
[database migrations](migrations.md), rehearse with writers stopped and preserve a
coordinated backup. A version number alone does not prove an equivalent catalog.

| Migration | Maintained behavior |
| --- | --- |
| 1071`internal_hosted_reporting_inputs` | Explicit reporting authority, current configuration/revision and indexed current facts. |
| 1077`internal_hourly_reporting_evidence` | Append-only hourly monitoring evidence and gaps; drops the 1055 raw-event hold. |
| 1079`internal_hosted_reports` | Draft/issued reports, evidence cutoffs, delivery attempts and requests. |
| 1080`internal_hosted_report_artifacts` | Immutable artifact metadata and stored chunks. |
| 1086`internal_hosted_report_queue` | Durable queue generations, notification claims and events. |
| 1087`internal_hosted_report_request_fulfillment` | Recorded external request fulfillment. |
| 1088`internal_hosted_account_analyses` | Immutable saved account analyses and generation receipts. |
| 1089`internal_hosted_reporting_delivery` | Persisted notification destinations and retry/delivery state. |
| 1090`internal_account_analysis_deletion` | Metadata-only deletion receipts for saved analyses. |
| 1091`internal_hosted_report_generation_permission` | Account generation capability and permission propagation. |
| 1092`internal_hosted_curtailment_observations` | Prospective curtailment confirmations and original asset scope. |

Do not edit a deployed migration or force a mismatched checkpoint to bypass its
checks. Populated reporting down migrations protect retained history; inspect
the specific migration guard and prefer a forward correction. A coordinated
restore requires the matching application build and database backup, stopped
writers, explicit accounting for observations lost after the backup, and digest
verification before restarting reporting. Apply the evidence runbook's storage
alerts, named operations owner and pause/resume gap recording before production
capture. This document does not grant a release approval or attest a deployed
source, contract formula or delivery procedure.
