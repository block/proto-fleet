# Client

## Overview

This directory contains two React applications and a shared component library:

- **ProtoOS**: Mining dashboard UI served by the miner's embedded API server (single-miner view)
- **ProtoFleet**: Fleet management UI for managing multiple miners (fleet-wide view)
- **Shared**: Common UI components used by both applications

### Tech Stack

- **React 19** with TypeScript
- **Vite 8** for builds and dev server
- **Zustand** for state management with Immer middleware
- **React Router 7** for routing
- **Tailwind CSS 4** for styling
- **Vitest** and Testing Library for testing
- **Storybook 10** for component documentation
- **Recharts** for data visualization
- **Motion** (Framer Motion) for animations

## Directory Layout

```
client
├── .storybook                # Storybook configuration
├── dist                      # Compiled production output
│  ├── protoFleet             # ProtoFleet build output
│  └── protoOS                # ProtoOS build output
├── public                    # Favicon and static assets
├── scripts                   # Development scripts
├── src
│  ├── protoFleet             # Fleet management UI source
│  │  └── index.html          # ProtoFleet entry point
│  ├── protoOS                # Mining dashboard UI source
│  │  └── index.html          # ProtoOS entry point
│  └── shared                 # Shared components, hooks, and utilities
├── eslint.config.js          # Linting rules
├── package.json              # Dependencies and npm scripts
├── postcss.config.js         # PostCSS/Tailwind configuration
├── tsconfig.json             # TypeScript configuration
└── vite.config.ts            # Vite multi-app build configuration
```

## Getting Started

### Install dependencies

```bash
npm install
```

### Start dev server

```bash
# Start ProtoOS dev server
npm run dev:protoOS

# Start ProtoFleet dev server
npm run dev:protoFleet

# Access at http://localhost:5173
```

### Proxy Setup

Both apps require proxy configuration to route API requests to backend servers. Create a `.env` file in this directory:

**ProtoOS**:

```
PROXY_URL=http://127.0.0.1:8000
```

Routes `/api/v1` requests to the miner API server. The proxy URL can point to a locally running miner-api-server, a test node IP, or a mock data API server like [Stoplight](https://stoplight.io/mocks/proto-mining/mdk-api/656299768).

**ProtoFleet**:

```
FLEET_PROXY_URL=http://127.0.0.1:4000
```

Routes `/api-proxy` requests to the fleet server. If you are implementing a new API endpoint, you may need to add the path to `vite.config.ts`.

## Building

```bash
# Build both applications
npm run build

# Build individual applications
npm run build:protoOS
npm run build:protoFleet

# Preview production builds
npm run preview:protoOS
npm run preview:protoFleet
```

### Multi-App Build System

Vite is configured with mode-based builds. Each app has its own `index.html` entry point in `src/{app}/` and builds to `dist/{app}/`. Always specify the mode when building: `vite build --mode protoOS`.

### R2 local firmware updates

On a direct R2 connection, **Settings → General → Firmware → Upload firmware**
accepts a local, non-empty `.swu` file. The confirmation uploads the selected file
through the authenticated MDK v1 multipart endpoint, which starts installation
automatically. Keep the page open during upload; the existing firmware status
and reboot controls follow installation. The rig validates the image and enforces
its upload limits. If upload completion is uncertain, check the device status
before retrying. This control is unavailable in Fleet-hosted views;
the R2 catalog update flow remains available. R2 images must bundle a ProtoOS release
containing this feature before operators can use it on-device.

### CT1 local pool configuration

On a direct CT1 connection, **Settings → Pools** reads mining-group mode before
offering container-wide edits. With groups enabled, the page uses Fleet's shared
configuration library table and details view. The table shows named configurations,
the primary pool, backup count, and assigned equipment count. It shows 25
configurations per page. Select a row to view its assigned hashboards, pool
settings, and recent controller update history. If the controller reports hashboards with no group
membership, **No reported group assignment** shows the count. Select **View
hashboards** to see their locations and MAC addresses. Select one or more
hashboards and choose **Edit pool** to assign them. **Select all** selects all
hashboard rows that match the search across all pages and can be assigned.
The list shows what to check if a board cannot be assigned. If the controller
reports missing, invalid, or duplicate board MAC addresses, the list is read-only.
Select **Reload pools** to check the inventory again. If no saved
configuration is available, select **New pool configuration** in the picker. **Create
configuration** saves an unused local pool set. **Edit configuration** uses the
same primary/backup form as Fleet. If hashboards use the configuration, review
the changes and affected boards before saving. Existing passwords can be preserved only when the
pool ID, URL, and complete username still match. **Delete configuration** requires
an unused configuration that is neither Default nor managed elsewhere. It also requires
the current revision. Fleet-owned and older configurations
can be edited without changing their external reference, power or tuning policy,
disabled pools, or pool weights. Configuration and membership changes invalidate
the save; group writes also require the current controller revision.

Default uses the same editor and the controller's legacy PoolService. Its name
is fixed and it cannot be deleted. This API writes one pool at a time, so the
client checks current settings before each write and stops on a conflict. A
partial save or missing acknowledgement is reported as unconfirmed and is not
retried automatically. Existing passwords can be kept or replaced, but this API
cannot clear a saved password. Fleet's pairing-default action has no CT1
equivalent, so ProtoOS does not show Set as default.

Library catalogue reads are bounded and do not fetch the board inventory. The
initial mode read keeps the existing configuration snapshot. Moving boards requires
a current inventory read. Configuration deletion checks group membership.
Controller-reported settings do not
prove that every board has applied them. Frigate stores the complete pool username,
which ProtoOS shows in the pool library.

Container-wide edits remain available when the controller reports the supported
group protocol with groups disabled, no suspension, and no pending group change.
Every write checks that state again. Reorders perform one full preflight, then
check the mode revision before each priority write and stop if it changes.
Between reorder steps, a transient capability-read failure is retried once;
priority writes are never retried.
After any successful priority write, a later failure reports that some priorities
were saved and the final order is unconfirmed. Authentication errors keep their
status and show sign-in guidance; neither writes nor reorders are replayed.
Pool `FailedPrecondition` responses remain uncertain because Frigate can return
them after saving a change when its live reload fails.
This client preflight is not an authorization boundary or an
atomic lock against another controller client. Failed reads keep editing blocked
until a successful reload. Disabled mode reads only capability and operation
state; leftover group records do not gate legacy editing. In group mode,
membership stability uses group IDs and revisions, so live transfer progress
does not invalidate the view. Operation details appear in both modes. Terminal
`COMPLETE` and `FAILED` receipts do not block legacy editing; unfinished work
still blocks it even when it reports an error.
If the controller does not support the group protocol, saved container pools
remain visible in a read-only list. Other capability-read failures keep the page
unavailable. Pool reads time out after ten seconds so **Reload pools** stays usable.
Use **Reload pools** to refresh the configuration.
In **Modules**, choose **Edit
pool** from a module's actions or the selected-module action bar. Module details
also show the action bar when you select hashboards. Use **Select all** to select
the installed boards in the module. Open **Actions** to edit pools or update
worker names. Each board also has its own actions menu.

ProtoOS reuses Fleet's configuration picker, primary/backup form, and
modal presentation. It opens **Edit pool** directly without Fleet's password
step. Choose a named configuration or **New pool configuration**. Saving a new
configuration creates an unused local pool set and returns to the picker.
**Review assignment** shows the exact hashboards, pool URLs, complete usernames,
and available places in the destination group. Select **Assign** to confirm.
The picker identifies selections with mixed configurations. It closes and shows
a notification when the controller confirms the saved assignment.

Select a saved configuration and choose **Edit usernames** to make a copy for
the selected hashboards. The copy keeps the source pool order, URLs, enabled
states, and weights. Enter each full username, including any periods, as the
pool expects it. Enter any required passwords again. Saving the copy moves only
the reviewed hashboards. Use a module selection to apply one configuration to
its current boards. Use board selection to assign different usernames to
siblings. A module selection creates no rule for future boards in those slots.
ProtoOS asks for a fresh review when a reviewed board moves or is replaced
before submission. A saved username belongs to its controller's configuration
and group membership.

Use **Update worker name** from a board's actions to start with its current
pool configuration. The same action is available for modules and selected
boards. ProtoOS uses Fleet's full-screen worker name editor. Choose and order
properties such as MAC address, tank, or a custom string or counter. Select a
separator and check the preview. **Apply** uses Fleet's confirmations and opens
**Review worker names**. **Save and update equipment** updates the pool usernames.
The editor suggests replacing the suffix after the last period and keeps
backup usernames with no suffix. Generated worker names use one segment.
Review the complete pool usernames before saving. Use **Edit pool → Edit
usernames** to enter exact values for worker names with periods or another
account format.

Apply one worker name to every hashboard in a mining group to update its pools
in place. For a subset or different names within one group, ProtoOS creates a
group for each new name and moves only its selected hashboards. Frigate copies
the current pool settings and passwords directly on the controller. This
requires pool-copy support and enough free group slots. A Default worker-name
update also requires a controller that can check the reviewed pool revision
when it saves. After an interrupted
update, open **Settings → Pools** and check **Pool assignment status** before retrying. ProtoOS checks
submitted writes without repeating them.

The picker shows the controller's limits for groups, hashboards, and pools.
If all group places are in use, choose a saved configuration with space or
delete an unused configuration in **Settings → Pools**. Reusing a saved
configuration uses its existing credentials. A new group can open separate pool
connections. The pool provider can enforce its own connection limits.
Pool passwords remain in memory until the write. Recovery records contain no
passwords.

Local writes are available only on direct CT1 Connect connections. The picker
can assign the controller's Default configuration, configurations created in
ProtoOS, and configurations without an external owner reference. A destination
needs at least one configured pool. Assigned boards keep their mining intent.
Boards selected from **No reported group assignment** take the destination's
mining intent. Assigning to a running group requires confirmation. Fleet-owned
assignments can be moved in Fleet. You can edit their pool
configurations in ProtoOS after checking all affected hashboards. ProtoOS
blocks moves involving custom power or tuning settings. It also blocks moves
for isolated boards or selections that mix running and stopped boards. Each
write respects the reported limits and uses revision checks. If groups are
disabled, the review explains that all container mining must stop for activation.
Start mining from the controller's mining controls if you want it to resume.
Review the assignment again before moving boards after activation.

A nonsecret recovery record is stored per controller and attempt in the browser.
**Pool assignment status** in **Settings → Pools** restores it after reload. Recovery only
reads status, membership, and pool records; it never replays writes. A missing
credential-write acknowledgement remains uncertain even when public pool fields
match. Long-running transfers keep their status record; partial or failed changes
require a fresh review for any remaining boards. Saved configuration and membership
do not prove that every board has applied the settings.
After an interrupted activation, **Check mining** reads the original controller's
mining state. If mining is stopped, use the controller's mining controls to resume it.
When you select **Edit pool**, ProtoOS checks any stored attempt before opening
the picker. In the unassigned list, select **Done** on the recovery result to
reload membership after a confirmed move.

Fleet-hosted ProtoOS keeps its existing read-only pool view; hosted assignments
are managed in Fleet.

### CT1 configuration statistics

With mining groups enabled on a direct CT1 connection, **Pools** lists the named
configurations currently assigned to hashboards. It shows each configuration's
active pool, assigned count, and share totals for the selected period. Open a row
to use the existing Primary/Backup statistics view for that configuration.
Saved configurations without equipment stay in **Settings → Pools**.

Only the current page (25 configurations) or selected configuration loads pool
readings and history. History joins by controller-assigned global pool IDs, never
by endpoint, username, or the Primary/Backup label. Acceptance rate is computed
from summed share counts. Missing pool history leaves the aggregate unavailable;
partial coverage stays labeled. Group connection status uses scoped diagnostics,
with reporting-board coverage shown when incomplete. Missing scoped diagnostics
remain unknown and never fall back to Default's readings. CT1 labels complete
pool credentials **Username**. R2 and Fleet-hosted pool statistics keep their
existing flow. CT1 with groups disabled also keeps the legacy statistics layout.

### CT1 local hashboard mapping

On a direct CT1 connection, Home shows a callout when hashboards have no physical
location or report mapping conflicts. Tank 0 / module 0 is unlocated inventory.
The mapping dialog shows CU1 identities, expected locations, observed feeds,
verification evidence, and recent mapping changes. Failed reads retain the last
result and block new mapping starts.

In `manual` mode, **Refresh mapping** opens a confirmation dialog. Confirming
starts a new run with a spinner and status updates. The dialog explains the power
and hashrate effects. The run turns all controllable feeds off, maps each
selected feed, then requests restoration of all feeds to on. This includes feeds
that were off before the run. Failures can leave restoration incomplete.
The dialog shows progress and cancellation. The lost-pool-connection callout is
hidden during a confirmed running mapping, including restoration and cancellation.
The no-pools-configured warning remains visible. Tracking continues across pages.
The dialog recovers active runs after reload
and retains unconfirmed starts until the operator checks the controller.

In `switch_ports` mode, correct cable connections, CU1 conflicts, or the configured
schedule, then select **Verify hashboards**. Verification can briefly stop and
restart boards. It cannot rewrite the configured physical locations. `observe`
mode permits inspection only. Fleet-hosted ProtoOS views do not expose these
commissioning controls.

The browser tests in `e2eTests/protoOS/standalone/feedMapping.spec.ts` use the
isolated CT1 simulators with `CT1_UNLOCATED_BOARDS=all` and real mapping RPCs. They test
confirmation, progress, reload recovery, and completed locations. They do not validate
physical feed switching or deployed controller configuration.

### CT1 local hashboard firmware

On a direct CT1 connection, **Settings → General → Firmware → Upload hashboard
firmware** accepts one `f1-app-prod.bin`, `f1-app-dev.bin`, or
`f1-app-mfgtest.bin` from a hashboard release. It reads the embedded F1 metadata
and displays the version, build, and occupied-board count before confirmation.
ZIP archives, other hardware, and mismatched filenames are refused. This control
is absent in Fleet-hosted views, which keep Fleet's firmware flow.

The browser checks the complete six-tank map: every module position of each
tank's grid and every board slot must be reported. A tank that maps fewer
modules than its grid is refused, as in Fleet, because the map carries no signal
that separates an unmapped position from one not reported yet, and updating it
could silently skip boards. It then uploads the image once to the connected
controller using the chunked Connect API. It supplies the image version and filename as release
attribution, computes SHA-256 even on plain LAN HTTP, and retries only uncertain
chunk writes at the same offset. Frigate validates the artifact and coordinates
its distribution to the owning controllers. The update explicitly targets every
occupied board, including unlocated inventory, with the selected build and
`force=true`: boards report version but not installed build, so same-version
build changes must still flash. Mining stop/restore uses Frigate's defaults.

Keep the page open during upload. Canceling an upload aborts its session when
possible; sessions lost on disconnect expire after ten idle minutes or a
controller restart. Before starting, ProtoOS saves its task ID and board-owner
map in local browser storage, under a key of its own. A tab only writes or
removes the record of its own task, so tabs cannot overwrite or delete each
other's record even when they start at the same moment; the controller decides
which Start wins. One update is shown at a time: a second tab on the same origin
adopts the saved record instead of starting another update, and any further
saved record is shown once the current one is released. A Start that the
controller definitively refuses creates no job and is not tracked. After a lost response or reload on the same origin,
**Hashboard update status** queries each owner without resubmitting Start.

Tracking is cleared only when every owner confirms the outcome: **Done** after
the update ends, or a cancel that every owner answers with a canceled job.
Missing or unreachable owners remain visibly unresolved, and a job that finished
before the cancel arrived keeps its per-board result until **Done**. When a
cancel attempt leaves an owner unreachable or reporting the task as not found,
**Stop tracking** releases the record after confirmation; an update still
running on that controller continues. The confirmation opens over the status
dialog with focus on **Keep tracking**, so a repeated key press cannot release
the record. The state of a cancel is saved with the
record, so both a request that is still stopping boards and one that was not
confirmed read the same after a reload.

Once every owner's job has ended, the status states how: finished, finished with
errors, canceled, interrupted, or failed. It comes from the owners' job
statuses, so an update that another client cancels or a controller interrupts
is not reported as finished; when owners end differently, the worst outcome is
shown. A board that its owner's ended job does not list is named as missing and
counts as an error, as in Fleet, so the update reads at least as finished with
errors and can still be dismissed with **Done**. While that job is running the
board may yet be reported, so no outcome is shown. Progress includes each
board's phase and failure details. Browser storage does not transfer across
origins, browsers, or controllers, and an unfinished upload must be selected
again after a reload.

The simulator covers upload integrity and job lifecycle, not real signing or
flashing. Frigate must update its pinned ProtoOS release before this feature
appears on physical containers. PSU firmware, controller software, and release
archive upload are separate flows.

### CT1 local controller software update

On a direct CT1 connection, **Settings → General → Update controller software**
updates Frigate on both HA controllers through Frigate's self-coordinated
`ServerUpdateService`. Fleet-hosted views don't have it; they use Fleet's
remote updates.

- **Choose.** Pick the three files of one release: the `.deb` (at most
  512 MiB), its `.deb.statement.json` and its `.deb.statement.sig`. One picker
  takes them together or one at a time, and shows each file under its type.
  Fleet's upload dialog uses the same picker
  (`src/shared/features/frigateUpdate`). When all three are chosen, ProtoOS
  checks that they agree with each other and with what each controller
  reports, and shows anything that blocks the update. The controllers verify
  the signature themselves when the update starts, and the controller's
  `update.allow_remote_trigger` setting applies.
- **Copy.** The package is hashed and copied to each controller in 1 MiB
  pieces, resuming an interrupted copy. A recovery record is saved in browser
  storage first, and is shared between tabs of one browser.
- **Start.** Start is sent once to the HA primary and never re-sent
  automatically. A failed Start is settled by Status: an existing rollout is
  followed; otherwise the copied package is offered again, at once after a
  refusal (`InvalidArgument`, `FailedPrecondition`, `Unimplemented`) and after
  Start's 60 s timeout plus a margin when the outcome is unknown.
- **Follow.** The dialog polls Status. While Frigate is stopped its
  maintenance service answers Status, Retry and Cancel, and ProtoOS shows its
  booting screen; an update being followed stays on that screen with its
  dialog open. With no record, ProtoOS looks for a rollout to follow, including
  one started elsewhere.
- **Retry and Cancel** appear when the rollout allows them. Each keeps its
  idempotency key until the action is no longer offered or the rollout's
  `updated_at` has changed, so a lost answer replays the same request.
- **Discard** removes the copy from both controllers and keeps the record
  until each confirms. **Stop tracking** is offered when the update can't be
  followed; it changes nothing on the controllers.

Frigate's `UPDATE_*` codes are worded for both apps in
`src/shared/features/frigateUpdate/codeText.ts`. The simulator
(`CT1_HA_PAIR=1`) serves `ServerUpdateService` over Connect; see
`server/fake-ct1/README.md` for its trusted-key and refusal faults.

### CT1 on-device standalone

CT1 uses the same `dist/protoOS` build as the standalone rig dashboard. The browser opens the MDK v2 API listener at `http://<controller>:8080/` and probes `/api/v2/system/info` on that origin. The CT1 adapter then makes same-origin Connect calls (`/mdk.v2.<Service>/<Method>`, binary protobuf) with the required `X-Frigate-Actor: proto-os` wire header, without a v1 bearer token or login/onboarding gate. No Vite server, minerproxy, CORS setup, or `PROXY_URL` is needed on the device. Locally, the ProtoOS Vite server proxies `/api/v1`, `/api/v2`, and `/mdk.v2.*` to `PROXY_URL`. Fleet-hosted CT1 views keep using the `/api/v2` REST routes because Fleet's miner proxy allowlists and authorizes those routes.

The MDK v2 host packaging flow consumes the existing protoOS artifact:

- Host packaging pins the release tag, full commit, tarball asset and SHA-256. Release/nightly publication here does **not** automatically advance that pin.
- The host verifies the release identity, checksum and archive, and owns any installation-time integrity metadata. Controller images consume the packaged bundle; this repository does not install it.
- The MDK v2 host serves the bundle on the same listener as `/api/v2/`. Follow the host's installation and service-restart procedures when updating the dashboard.

The existing artifact workflow supplies `proto-os-${VERSION}.tar.gz` with archive root **`dist/protoOS`**. Preserve `index.html`, `config.js`, `version.txt`, all `assets/` lazy chunks, and optional favicon/fonts. `version.txt` must identify the release tag and full source commit. Do not flatten the archive or include links, unrelated files, or host-generated integrity metadata. The tarball never contains the Frigate manifest that is described below. `nfpm-proto-os.yaml` and the `.ipk` remain **R2/OpenWRT-only**, not the MDK v2 CT1 deployment path.

Each `v*` release tag also publishes a separate CT1 Debian package, `proto-os_${VERSION}_all.deb`, and its `.sha256` file. Nightly builds do not publish this package, because a nightly version is not a valid Debian version. Frigate installs the package with its own root update helper. That helper accepts only a static package, so the package contract is exact:

- The package name is `proto-os`, the architecture is `all`, and the maintainer is `Block Inc.`. The Debian version comes from the tag: remove the `v` and change the first `-` to `~`. For example, `v0.6.2-internal.14` becomes `0.6.2~internal.14`, and `v0.2.5-fw-auth-compat-2b0fcc4e7` becomes `0.2.5~fw-auth-compat-2b0fcc4e7`. The release tag must follow SemVer 2.0 without build metadata (`release.yml` checks this before any build).
- `Pre-Depends` is `frigate-protoos-host (>= 1)`. The package has no other relationship fields.
- The package has no maintainer scripts, triggers, or conffiles. Both archives in the package use gzip.
- The data holds only `/var/www/` and `/etc/web_dashboard_version`. The version file holds the tag. All files have mode `0644`, all directories have mode `0755`, and root owns all of them. The package has no links or special files.

`/var/www/` holds the `dist/protoOS` files and one generated file, `/var/www/.frigate-protoos-manifest.json`. The script `deployment-files/scripts/create-protoos-frigate-bundle.py` makes this file. The script copies `dist/protoOS` to `dist/protoOS-frigate`, checks and hashes the copy, and writes the manifest into the copy. `dist/protoOS` stays unchanged, so the tarball and the `.ipk` stay unchanged. The manifest records the repository, tag, commit, tarball name and tarball SHA-256, and the size and SHA-256 of each public file. Frigate verifies the generation hash in the manifest and checks the size and SHA-256 of each file against the manifest. Frigate rejects a bundle that differs.

The script fails the build when the tree has something that Frigate does not allow:

- The tree can hold only `index.html`, `config.js`, `version.txt`, `favicon.png`, `assets/`, and `fonts/`. It must have at least one file in `assets/`, and it cannot have symbolic links or special files.
- A path must be valid UTF-8. It cannot have a control character (U+0000 to U+001F, or U+007F) or a backslash. It cannot have an empty segment or a segment that starts with a dot.
- A path can have 32 segments at most. Each segment can have 255 bytes at most. The path can have 1016 bytes at most, so that the path with the `var/www/` prefix stays in the 1024 bytes that Frigate's installer allows.
- The tree can have 4094 files and directories at most, so that the package stays in the 4096 entries that Frigate's installer allows. A file can have 32 MiB at most, and all files together can have 128 MiB at most. The tarball can have 32 MiB at most.
- Other characters are allowed, for example `[`, `]`, `&`, `<`, a space, and non-ASCII letters. The manifest uses the same JSON escapes as Frigate.

`nfpm-proto-os-deb.yaml` builds the package from `dist/protoOS-frigate`. The workflow sets `SOURCE_DATE_EPOCH` to the commit time, and the script sets the same time on the staged files. Thus the same staged tree and tarball give the same `.deb` bytes. A new run of the same tag gives different bytes. The build date in `version.txt` and in the built JavaScript is the time of the run, and the manifest records the tarball SHA-256. Use the nfpm version that Hermit pins in `bin/`. That version writes `./` before the paths in `md5sums`, and Frigate accepts one optional `./`.

After the build, `create-protoos-frigate-bundle.py --verify-deb` applies the checks of Frigate's installer: the control data, the archive framing, the paths, the file modes, the size limits, `md5sums`, and the manifest in the package. The publish job of `release.yml` checks that the `.sha256` file matches the `.deb`, and it runs `--verify-deb` again on the downloaded package before it publishes. The test `deployment-files/tests/test_protoos_frigate_bundle.py` compares the manifest with the manifest that Frigate's own Go stager writes for the same fixture, and it builds a real package. The test file explains how to refresh that fixture.

Frigate installs a `proto-os` package only when a signed release statement describes it. The statement is a short JSON document in Frigate's `frigate-release-statement/v1` format: the package name, Debian version, architecture `all`, the repository, the package SHA-256 and size, an empty `min_installed_version`, and the issue time. `create-protoos-frigate-bundle.py --verify-deb ... --write-statement PATH` writes it after the package passes verification, with the same bytes that Frigate's Go code writes; the test compares it with a statement from Frigate's `frigate-release-statement` tool. The `sign-protoos-release` job of `release.yml` calls the workflow `.github/workflows/sign-protoos-release.yml`. That workflow runs `deployment-files/scripts/sign-protoos-release.sh` on the package that the same run built (the `proto-os-artifact` artifact), not on bytes read back from the draft release. The script checks the `.sha256` file, writes the statement, signs it with an AWS KMS ECDSA P-256 key (`ECDSA_SHA_256` over the raw statement), checks the signature with OpenSSL, and refuses a key whose ID is not `PROTOOS_RELEASE_PUBLIC_KEY_ID`. The workflow then checks that the draft still holds the same package, uploads `proto-os_${VERSION}_all.deb.statement.json` and `.statement.sig`, and only then does the `publish-release` job publish the release.

`release.yml` takes the signing workflow from `main`, not from the tag. A tag runs its own copy of `release.yml`, and a prerelease tag can point to a commit on a branch. Thus the role trust policy must accept only the jobs of `sign-protoos-release.yml` on `main`, and a tag cannot change the code that uses the key. A change to the signing workflow has an effect only after it merges to `main`. The `uses` line in `release.yml` names this repository, so a fork or a moved repository must change that line and the role trust policy.

The workflow needs the repository variables `PROTOOS_RELEASE_SIGNING_ROLE_ARN`, `PROTOOS_RELEASE_KMS_KEY_ID`, `PROTOOS_RELEASE_KMS_REGION` and `PROTOOS_RELEASE_PUBLIC_KEY_ID`, and the `protoos-release-signing` environment with required reviewers and with self-review prevented (see the comment in `sign-protoos-release.yml` for the role trust policy). When only some of the variables are set, the workflow fails. The workflow signs only tags whose commit is on `main`, and only a run that signs waits for the environment approval. Approve the signing job in 7 days or less. GitHub keeps the `proto-os-artifact` artifact for 7 days. After that time the signing job cannot download the package, and the release stays a draft. Then re-run all jobs to build and sign the release again. A prerelease tag cut from a branch publishes without a statement, so Flotilla does not offer it to CT1 controllers. Use a KMS key of its own, not Frigate's release key: Frigate trusts ProtoOS keys only from `/usr/share/frigate/protoos-release-keys`, and a key there cannot sign a `frigate` package. Until the variables are set, the workflow warns and the release publishes without a statement. Flotilla never offers an unsigned release, and Frigate's helper refuses one before `dpkg` runs, so CT1 controllers keep serving the ProtoOS copy bundled in `frigate.deb`. Releases published before signing stay unsigned; to ship that code to CT1, cut a new tag once the key is provisioned and its public key ships in Frigate.

To verify a fresh production build against an isolated same-origin CT1 simulator, activate Hermit from the repository root, then run:

```bash
cd client
npx playwright install chromium # once per browser version
npm run test:e2e:ct1
```

This builds protoOS, starts its own `fake-ct1` on port 18081 serving `dist/protoOS`, and checks desktop/mobile boot, tank/module deep-link reloads, no login gate, and scoped tank stop/start with actor-only decoration and untouched neighboring tank state. It refuses to reuse an existing server and restores the touched simulator scope. See [the E2E guide](e2eTests/protoOS/README.md) for focused commands. The simulator preview does not replace the MDK v2 host's artifact verification or an on-hardware deployment check.

For rollout, select a published validated tarball, advance the MDK v2 host's release pin in a separate change, and run its package/image verification. On an authorized controller, check root and tank/module refreshes, reads, and a narrowly scoped stop/start round trip on the management network. This repository's browser smoke does not install a package, update the host's pin, or validate a physical controller.

## Testing

```bash
# Run all tests
npm test

# Run tests matching a pattern
npx vitest run <test-file-name>

# Watch mode for a specific file
npx vitest watch <test-file-name>

# Run tests in a specific directory
npx vitest run src/protoOS/features/kpis
```

## Code Quality

```bash
# Lint code
npm run lint

# Format code with Prettier
npm run format

# Check formatting without writing
npm run format:check

# Run Storybook for visual component testing
npm run storybook
```

## Architecture

### State Management

**ProtoOS** uses Zustand with a slice-based architecture (`useMinerStore`):

- Hardware, Telemetry, UI, Auth, Miner Status, Mining Target, Network Info, System Info slices
- Key data types: `Measurement`, `MetricTelemetry`, `MetricTimeSeries`
- See `src/protoOS/store/README.md` for comprehensive documentation

**ProtoFleet** uses Zustand with a slice-based architecture (`useFleetStore`):

- Fleet, UI, Auth, Onboarding slices
- Fleet slice handles miner collection, device status counts, filtering, and streaming telemetry

### ProtoOS container Home

The container Home overview includes a live cooling schematic using its existing
telemetry poll. The diagram toolbar's System controls launcher opens a shared
modal for pump power/speed, dry-cooler power and auto/manual targets, both air
conditioners' power and target temperatures, and tank lights. It loads the
controller's supported controls when opened. Each power switch stays in its own
row; equipment groups sit side by side on larger screens and stack on phones.
Power and mode save immediately, while numeric values save on Enter or blur.
Each write is followed by settings readback before a success toast; errors retain
the draft. Requests have deadlines, and connection changes cancel pending work.
Temperature inputs follow the display preference and write Celsius to MDKv2.
Flow and pressures remain readings. Fluid levels are reported per bank in mm;
the schematic does not infer individual tank fill percentages.

The Power page shows facility total, mining power, each reported PDU, and combined
fan history as separate lines over the selected period. These measurements overlap
and are not stacked. PDU readings appear in the chart legend rather than the top
summary. The top summary remains live; Fans High/Low/Average describe
the selected period. Missing history remains unavailable rather than zero.
Pump, air-conditioning, and auxiliary power histories are not exposed by the
current API, so their series and statistics cards are omitted. Power has no
System/Hashboards selector because percentage-band history is unsupported.

### Native container overview

Fleet's Buildings cards and rows open `/containers/:identifier` inside Fleet's
shell, including FleetNode-owned and unreachable controllers. The page reads
collected data; it does not connect to the controller or require an embedded
ProtoOS proxy. `ContainerService.GetContainer` authorizes the current site and
returns the stable Fleet device ID used to load tanks and modules through the
typed inventory API. All cursor pages are fetched before publishing a refresh.

The page polls metadata and inventory every 15 seconds. Transient failures keep
the topology visible with unavailable current readings and a retry; access
denials or removal clear retained data. Navigation cancels outstanding loads.
Tank cards open `/containers/:identifier/tanks/:tankId` using the stable Fleet
tank ID. Module tiles open the native Fleet module detail page. Container performance uses Fleet's telemetry
API and existing performance charts, scoped to the controller and its site.
Container-reported power remains distinct from summed module equipment power.

In the Miners list, CT1 module rows expand into three read-only hashboard slots.
Expanded modules load current board data from Fleet and retain expansion across
polling refreshes. Boards have durable hardware IDs and separate installation
history, so moving or replacing a board does not merge its telemetry with another
board. See [CT1 hashboard inventory and telemetry](../docs/development/ct1-hashboards.md).

`protoFleet/features/containers` owns the page and container presentation;
Fleet's dashboard owns `ChartWidget`. App-neutral primitives and telemetry
formatters remain in `shared`. ProtoOS owns its on-device Systems view, adapters,
and controls. Fan readings and equipment-control states are not collected by
Fleet yet, so this page identifies those gaps and offers no device commands.

### Native tank detail

Container tank cards and tank rows in Racks open the native tank page. It checks
both typed tank identity and container ancestry, inherits the container's read
access, and uses collected Fleet data for directly managed and FleetNode-owned
controllers. The page provides module equipment totals (separate from plant
power), numbered module states, site/container/Buildings breadcrumbs, and a
**View modules** link to the tank-filtered inventory. Partial totals exclude
missing module readings; stale or entirely missing measurements remain unavailable.
Partial metric values include their reporting-module coverage in inventory, tank
KPIs, and container tank cards, independently of module health.

The tank drawing uses outlined module cards with numbering from the bottom left
and module status markers shared with the legend. Isolated modules retain their
state and count under Needs Attention with the same warning marker.
Unknown modules use an outlined marker and, only when present, a small count
annotation below the status legend, including when the controller remains fresh.
The three hashboard outlines remain neutral until hashboard-level Fleet data lands in #163; board states and
drill-downs are not wired yet.

Performance requests a single server-aggregated tank history response using the
shared Fleet duration setting: 1h, 24h, 7d, 30d, 90d, or 1y. SQL averages each
current, non-retired module within a Unix-epoch-aligned bucket, then sums those
module averages. Staggered timestamps and different sample rates cannot dilute
tank totals. Repeated source observations use the latest collection. Responses
contain at most 1000 buckets. Hashrate/power bounds sum per-module minima/maxima;
efficiency bounds use paired power/hashrate extrema and omit undefined bounds. Missing/non-finite module readings are excluded per
metric; only buckets with no readings remain gaps. Efficiency uses paired
hashrate/power readings from the same modules and is weighted by hashrate. Authorization checks both the tank's
owning container and its current site, including during the aggregation query.
The browser polls one bounded response and does no module aggregation. Tank
charts use explicit TH/s, kW, J/TH, and Celsius units without legacy unit guessing.

History availability follows Fleet's retained raw samples and hourly/daily
summaries. Longer windows can contain gaps where collection or retained summaries
are unavailable.

Module clicks from Miners, container tiles, and tank tiles open the native Fleet
page at `/containers/:identifier/tanks/:tankId/modules/:moduleId`. The page checks
the stable module ID and both ancestors; removed, retired, replaced, and
inaccessible modules are never substituted by position. It shows collected module
KPIs, three hashboard slots, and existing scoped mining actions. The header
status pill shares its presentation with ProtoOS and opens a modal with current
board/PSU issue causes. Hashboard cards show an issue icon only when issues are
reported; the info button opens board status details. Fleet and ProtoOS share the
hashboard card layout. Fleet omits the ASIC preview and shows identity in the
info modal. ASIC average uses the normalized temperature reading; ASIC high uses
the optional `maximumAsicTemperatureC` already preserved in collected MDK health.
Missing, non-finite, or stale temperatures render as a dash. No embedded ProtoOS view or live ASIC requests are used.

Board cards distinguish empty, unknown, isolated, and stale evidence. Source
freshness is checked independently for board state and measurements. Module
refresh failures hide current readings and controls; access loss clears the page.
Stored history remains available for stale controllers once Fleet access is
resolved.

The Other hardware section uses a shared PSU card in Fleet and ProtoOS, with input/output
voltage and power plus average/high PSU temperature. Fleet reads existing collected
`psuElectrical`, `psuThermal`, and `psuIdentity` payloads; no additional device polling is
needed. The three boards report one module-owned PSU: Fleet picks the freshest electrical
and temperature snapshots independently, accounting for source time and embedded sample
age, and never sums duplicate PSU readings. Missing or expired metrics show a dash.
The info button opens PSU-only collected issues and available manufacturer metadata;
the issue indicator is non-interactive. In Fleet those details also show the module's
latest PSU firmware update from the collected `psuFirmwareUpdate` payload: its state and
progress, the controller that ran it, and the failure reason. A PSU that an update left
needing recovery is named there, and its card shows the issue indicator. PSU firmware is
sent with the container's **Update firmware** action; uploading a `.psucap` file stores
it as CT1 PSU firmware. A release channel assigns PSU firmware on a **PSU firmware** row
under the container model, after **Hashboard firmware**. The row appears once a capsule
for the model is uploaded or assigned. It has its own firmware choice, update status,
and container list, which reports each container's PSU firmware version.

Performance shows module history using the existing paginated Fleet history API. The page fetches all pages for a fixed time window, refreshes once per
minute, and clears failed results. Requests are cancelled on navigation and
bounded to 20 pages; an exceeded limit asks for a shorter range rather than
showing partial history. Charts show 90-second sample averages for 1h/24h, hourly
averages for 7d, and daily averages for longer windows, with explicit gaps and
SI-to-display conversions. Interactive collected, stale, partial, and replaced
states are under **Proto Fleet / Containers / Native module page** in Storybook.

Tank-scoped cooling/equipment
readings are not available in the current Fleet API; the page omits the empty
cooling section. Tank rows, cards, and the native tank page expose scoped mining
start/stop through the tank actions menu. List selection keeps tank IDs separate
from rack IDs; bulk tank actions apply only to the selected tanks.

Module rows in the Miners list and the native module page expose Start mining
and Stop mining. Module selections use stable Fleet module IDs and a separate
Module actions menu; mixed miner/module selections keep their command paths
separate. Confirmation names the selected modules. The server groups the exact
selection by controller, rechecks current org/site permissions and module
identity, and carries the expected identity through direct and Fleet Node
dispatch. Fleet Node module commands require command protocol V5.

The CT1 plugin checks live controller/module identity and sends only the verified
board identifiers to the native StartMiningScoped/StopMiningScoped RPCs. Replacement, retired, stale,
missing, or ambiguous modules fail without broadening the scope. Module and tank
commands share a per-controller pending-command guard. Completion refreshes
inventory; a partially applied or unknown controller result remains unconfirmed,
while successful controllers retain their confirmed counts.

Fleet resolves stable tank IDs to the current controller/site and queues one
command per controller. Admission requires current topology, driver tank-mining
support, and the requested mining permission at every selected tank’s site. A
pending tank command blocks another command for the same controller. Execution
rechecks tank presence and placement, then reads the controller’s live map and
sends one explicit module scope to `/api/v2/mining/scoped/start` or `stop`. Empty
modules are omitted; empty or unavailable tanks fail without a write. Container
mining intent is never changed by this path.

Tank mining uses the same count-only confirmation dialog as miner bulk actions
and closes when confirmed. The action bar returns after command admission,
while a progress toast follows each controller's existing command batch.
Success reports completed tank counts only after all batches finish. Failed
controller batches can contain partial or unknown writes, so they do not count
as confirmed tank failures or offer a retry. Their completion error preserves
counts from successful controllers and asks the operator to check the remaining
tanks. Missing results or interrupted polling also show an unconfirmed completion
error without a retry. As with miner bulk Sleep/Wake, command completion drives
the success toast; inventory readings update separately through normal polling.
Direct controllers use the plugin SDK; FleetNode controllers require command protocol
V4 and updated SDK/plugin binaries. Older nodes reject this action before
sending it, and failed or ambiguous device writes are terminal rather than
automatically retried. Shared equipment readings remain on the
container page.
Storybook's **Proto Fleet / Containers / Native tank page** exercises the actual
page and collected-data hooks with isolated API fixtures.

### CT1 inventory filters

The Miners tab includes containers in **Buildings** and tanks in **Racks**;
the Racks tab also includes containers in **Buildings**. CT1 options are labeled
with their entity type so identically named or numbered entities stay distinct.
Tanks use stable Fleet tank IDs under repeated
`tank` URL keys. Tank labels include the owning container and site, and options
come from all readable tanks across `ListTankOptions` cursor pages. This label-only
catalog avoids module health and telemetry aggregation.
Selections within Buildings (buildings/containers) or Racks (racks/tanks) combine
with OR; the two categories, site scope, and other filters combine with AND.
Unassigned options retain their conventional placement scope and combine with
selected CT1 entities using the same OR rule within each typed category.
When a container or tank is selected, both placement categories independently
restrict rows. A building plus Unassigned Racks then excludes fully unplaced
miners unless Unassigned Buildings is also selected.
Conventional-only miner selections use the native miner filter so the list,
CSV export, authentication counts, and commands share placement semantics.
For example, a building plus Unassigned Racks also includes fully unplaced miners.
Rack and tank IDs stay independent even when their numeric values match.

Containers use controller device IDs under repeated `container` URL keys, independently of
building IDs. Multiple selected containers combine with OR. Site scope, selected
sites, the Racks filter, telemetry, and other filter categories combine with AND;
contradictory scopes return no rows. Existing tank deep links use stable tank IDs
under `tank` and survive unrelated filter changes. The Racks view accepts only
facility ancestors; tank ancestors apply only to mining assets.

Selections survive refresh and saved views, and clearing the Buildings chip
clears both building and container selections and
leaves other filters intact. Container options come from the readable container
catalog with site labels. Filtering and pagination run on the unified inventory
API; selecting a new scope hides previous-scope rows until its response arrives.
Other miner-only filters keep their existing applicability and can therefore
produce no module matches even when a container or tank is selected.
Container-filtered module lists do not expose conventional miner CSV export.
When a container or tank filter is active, miner actions use explicitly selected
rows from the displayed page. Full-result Select all is unavailable because
native miner command filters cannot express those typed placement scopes.

### API Integration

CT1 health uses `ProtoOSStatusModal` with an optional `scope` (`tank` or `module`)
and `componentAddress` (canonical component reference). The scope determines the
aggregate list and the destination for Back, Escape, and Done from component
details; closing the aggregate returns to its opener. Component selection is
in-app state, not a shareable URL.

`scopeHealth.ts` derives membership from topology and combines current board/PSU
health with normalized active errors. Tank indicators use the same summary, so a
PSU-only fault remains visible. Reported health without an error message produces
a navigable observation, not a fabricated active error. The modal polls current
errors and telemetry; resolved errors disappear on refresh. R2 retains its
existing modal flow. Severity presentation and plant alarm grouping are separate
work. Storybook examples live under **Proto OS / Containers / Health drill-down**.

**ProtoOS** — Each miner API has an adapter in `src/protoOS/api/adapters/` with a private generated TypeScript client: MDK v1 from `proto-rig-api/openapi/MDK-API.json` and MDK v2 from `proto-rig-api/openapi/MDK-v2-API.json`. On a CT1, the MDK v2 adapter also uses a Connect client with protobuf-es bindings generated from `sdk/mdk/proto/mdk/v2`. Application code uses hooks in `src/protoOS/api/hooks/` which handle error handling, polling, and automatic store updates. Regenerate the REST clients with `npm run generate-api-types`, and the bindings with `just gen-mdk-protos` from the repository root. [`proto-rig-api`](../proto-rig-api/README.md) records the source of each contract.

**ProtoFleet** — gRPC-Web with Connect-RPC. Generated TypeScript code in `src/protoFleet/api/generated/` from Protobuf definitions. Supports server-to-client streaming for real-time telemetry. Custom hooks in `src/protoFleet/api/`.

Container entries in the building list and grid share an actions menu. It
exposes sleep or wake, plus manage power. Opening the menu
reads the controller's current mining status: stopped containers offer wake, active
containers offer sleep, and unknown or transitioning states offer neither. Commands require confirmation and
the corresponding Fleet permission. Fleet reads status and power settings through
typed `ContainerService` RPCs and queues each write as one command; see
[container menu commands](../docs/development/ct1-grpc.md#container-menu-commands).
Sleep/wake changes container mining intent and leaves
isolated boards under individual control. Power limits come from the controller,
with a minimum of 1 W because zero can remove the controller's power limit.
The menu uses the miner menu’s icons and navigation, operations, and settings groups.
View container, the card, and the list row open Fleet's native container details,
including for FleetNode-owned and unreachable controllers. When the status read
fails, the menu disables sleep, wake, and power actions;
navigation remains available. Power controls wait for a successful status read,
even for operators who only have power-management permission.
Unpair requires `miner:delete` and confirmation, then removes the controller through
Fleet's `DeleteMiners` API. It remains available when the controller is
unreachable, including FleetNode-owned controllers. After success, the row or card
is removed immediately and inventory refreshes in the background. Failed requests
keep the container visible for retry. Single-container unpairing records
`unpair_container` activity with “Unpaired container” and a container scope;
miner and bulk deletion retain their existing activity labels.

The menu also has these entries, each shown only with its Fleet permission:

- **Controller logs** opens the controller's logs and audit trail.
- **Enable mining groups** or **Disable mining groups** appears when the
  controller supports mining groups.
- **Edit pool**, or **Container pool assignment** when mining groups are enabled,
  stays disabled until Fleet has a current, settled mining-group observation.
- **Update firmware** sends a hashboard archive or a PSU capsule.
- **Update worker name** is visible but disabled.
- **Update controller software** requires `controller:software_update`.
- **Change site** appears when the caller can manage placement at the
  container's current site.

Host reboot, cooling, lighting, alarm controls, module/feed operations, and
advanced plant procedures
are not part of this menu. This surface does not change miner bulk selection,
scheduled commands, or curtailment dispatch. Interactive examples are under
**Proto Fleet / Containers / Actions** in Storybook.

### Observability

Client observability lives in `src/shared/observability/` as a vendor-neutral provider
registry. Datadog RUM is the first provider; a provider is a complete no-op unless its
required config is present. When enabled, Datadog RUM captures page/session data, forwards
React render errors (via the shared `ErrorBoundary`), and injects distributed-tracing
headers on same-origin `/api-proxy` calls. This is future-ready client plumbing for
client→server trace correlation; the bundled backend telemetry path does not currently export
correlated server spans to Datadog.

**Configuration** resolves runtime-first, then build-time:

1. `window.__RUNTIME_CONFIG__[KEY]` — rendered into `config.js` by the deployment nginx
   image at container start from `DD_*` environment variables. Operators enable Datadog on
   a prebuilt image by setting these in their `.env` — no rebuild required.
2. `import.meta.env.VITE_<KEY>` — build-time value for local dev and CI (set in `.env`).

Datadog keys (all read via both mechanisms; runtime `DD_*` / build-time `VITE_DD_*`):

| Key                             | Required                                     | Default                                |
| ------------------------------- | -------------------------------------------- | -------------------------------------- |
| `DD_APPLICATION_ID`             | yes                                          | —                                      |
| `DD_CLIENT_TOKEN`               | yes (public RUM token, not a secret API key) | —                                      |
| `DD_SITE`                       | no                                           | `datadoghq.com`                        |
| `DD_SERVICE`                    | no                                           | `proto-fleet-client`                   |
| `DD_ENV`                        | no                                           | build env (`production`/`development`) |
| `DD_RUM_SAMPLE_RATE`            | no                                           | `100`                                  |
| `DD_SESSION_REPLAY_SAMPLE_RATE` | no                                           | `0`                                    |
| `DD_TRACE_SAMPLE_RATE`          | no                                           | `100`                                  |

Both `DD_APPLICATION_ID` and `DD_CLIENT_TOKEN` must be set to enable; otherwise the client
runs unchanged with no SDK side effects.

**Data minimization:** RUM sends to the operator's own Datadog org. Session Replay is off by default
(`DD_SESSION_REPLAY_SAMPLE_RATE=0`) and masks all text/inputs when enabled
(`defaultPrivacyLevel: "mask"`). A `beforeSend` scrubber removes query strings that RUM does
not need for route-level analysis; extend it in `providers/datadog.ts` if the UI later surfaces
sensitive strings in action names or error metadata.

**Adding a provider** (Sentry, PostHog): implement `ObservabilityProvider` under
`src/shared/observability/providers/`, then add one `registerProvider(...)` call in
`src/shared/observability/providers.ts`. The entry point, transport, and error boundary are
provider-agnostic and do not change. End-to-end distributed traces remain follow-up work: the
backend must deliberately associate the incoming context and export server spans to the same
observability backend.

### Import Rules

Use the `@/` path alias for all absolute imports:

```typescript
// Good
import { Button } from "@/shared/components/Button";
import { useMinerStore } from "@/protoOS/store";

// Bad
import { Button } from "../../../shared/components/Button";
```

Strict import boundaries:

- `src/shared/` must never import from `src/protoOS` or `src/protoFleet`
- `src/protoOS` must never import from `src/protoFleet`, and vice versa

### Component Organization

Components follow a feature-based structure:

```
features/
└── kpis/
    ├── components/       # Feature-specific components
    ├── utils/            # Feature utilities
    ├── types.ts          # Feature types
    └── index.ts          # Public exports
```

- Components used within a single feature live in that feature's `components/` directory
- Components shared across features within one app live in `src/{app}/components/`
- Components shared across both apps live in `src/shared/components/`
- Shared components should be pure — consistent output given the same props

### Shared Components

Reusable components in `src/shared/components/` include:

- **Layout**: Card, ContentHeader, Divider, BackgroundImage
- **Interactive**: Button, ButtonGroup, Dialog, Modal, DurationSelector, Toggle
- **Data Display**: Chart, DataNullState, Callout, Chip, StatusBadge
- **Forms**: Checkbox, Input, Select, TextArea
- **Feedback**: Spinner, ErrorBoundary, Toast

All shared components have Storybook stories, support light/dark themes, and include TypeScript prop types.

### Storybook organization

Use these exact top-level names in story metadata `title` values. The sidebar
shows them in this order, with groups sorted alphabetically within each root:

| Root          | Content                                                         |
| ------------- | --------------------------------------------------------------- |
| `Foundation`  | Design tokens, typography, icons, and design guidance           |
| `Shared`      | App-neutral components and interactions from `src/shared/`      |
| `Proto Fleet` | Fleet-owned components and feature flows from `src/protoFleet/` |
| `Proto OS`    | Device-owned components and feature flows from `src/protoOS/`   |

Group app stories by the relevant existing feature, such as
`Proto Fleet/Containers/Overview` or `Proto OS/Containers/Tanks`. A feature using
shared primitives still belongs to its owning app. Avoid new catch-all roots
such as `Components`, separate product roots for containers, or alternative app
spellings. Keep stories beside their implementations; cross-app design guidance
lives in `src/shared/stories/`. Story title changes also change generated story
URLs, so update any links or automation that reference the old IDs.

`Foundation/Design Drift` lives in
[design-drift.stories.tsx](src/shared/stories/design-drift.stories.tsx). It collects
common pitfalls and proposed improvements using shared primitives and tokens;
its Fleet examples illustrate guidance useful to both apps. When adding or
updating a specimen, show the pitfall and the preferred pattern, explain when
the recommendation applies, and verify referenced source locations and component
APIs. Identify historical examples as such once their source is fixed; do not
present them as outstanding defects. Keep only examples that teach a distinct,
recurring lesson, and check both themes when updating their presentation.

## Testing on Hardware

1. Compile the UI: `npm run build`
2. Build the Linux image via GitHub Actions
3. Transfer the image to the control board's SD card
4. Connect the board via ethernet and access the UI at the board's IP address

## Learn More

- [React](https://react.dev/learn)
- [Vite](https://vitejs.dev/guide/)
- [Tailwind CSS](https://tailwindcss.com/docs/utility-first)
- [Recharts](https://release--63da8268a0da9970db6992aa.chromatic.com/?path=/docs/welcome--docs)

### Hosted account workspaces

Hosts manage clients through Settings → Admin → Clients. Fleet navigation has
no client account switcher. **View as client** opens the client's navigation at
`/accounts/:accountId/:section` inside a full-screen modal with a persistent
**Viewing as [client]** header and **Read-only** label. Close or Escape returns to
the host’s previous page with its URL, filters, and scroll position preserved.
The preview uses the same Overview, inventory, details and tools as a client administrator, subject to live account and site authority.
Mutation controls remain visible but disabled; the preview client also rejects
mutation RPCs. The authenticated host identity and audit attribution stay intact.
Client users with one accessible account see its
name as plain text; users with multiple accounts retain an account picker for their
workspaces at `/accounts/:accountId/:section`. Customers and subclients land on
Home, with Fleet for their allocated mining assets and Settings for administration.
Settings groups Pools under Fleet, Team, Access and Subclients under Admin,
and Preferences under Account, using the API’s effective capabilities. Desktop shows a Settings sidebar; phones
show the same sections in the expandable navigation drawer. Access includes
account details, renaming, the current user’s permissions, Host and parent access, and API
keys; account status appears only when suspended. Settings opens the first available account
section. Preferences reuses personal theme and temperature-unit settings, persisted
in the browser. All client roles can change them; host previews change only the
operator’s display preferences. Current readings, history charts and inventory
respect the selected temperature unit. `/accounts` lists accessible workspaces.
Host management retains Fleet navigation under Settings → Admin. Clicking a client
name or row opens `/accounts/:accountId/manage/assets`, with Mining assets, Team,
Client settings and Permissions tabs. Permission checkboxes save immediately, roll back failed updates, and lock while
a request is pending. Suspension and resumption are separate confirmed actions. Creating a client opens Mining assets without creating an invitation. **Allocate assets** opens
the in-place **Select assets** modal with the destination client fixed. Hosts select
eligible miners or CT1 modules from the current page and review the allocation before
confirming. Closing the picker preserves the underlying list; successful allocation
refreshes it in place and restores focus to **Allocate assets**.
The Team tab contains **Invite team member**; the first invitation defaults
to Account administrator when the team is empty. Inviting before allocation is also supported. Mining
assets and permission limits use host authority; private team and account settings
continue to require the client’s grants. **View as client** is an explicit preview
action on the management page and in the list menu. The owner-facing `/accounts` page directly lists managed clients
and offers **Create client**, without a Use an invitation code CTA. It uses host authority,
not private membership in each client. Rows summarize assigned miners and modules
(including subclient allocations), distinct containers represented by those
modules, and live direct client logins. Containers are placement groups, not
additional assigned assets. Login counts exclude invitations, revoked memberships,
deleted users, removed organization affiliations, ancestor grants, and subclient-only
memberships. Membership-only users retain their account
directory and invitation entry point. The root workspace opens Clients. Hosts allocate reporting capacity from client management or Fleet;
the root account has no separate Mining assets tab.

| Persona                    | Primary experience                                                  |
| -------------------------- | ------------------------------------------------------------------- |
| Host operator              | Existing physical fleet operations in Fleet                         |
| Host account administrator | Clients and reporting assignments in Settings → Admin → Clients     |
| Account viewer             | Whole-account metrics and read-only mining assets                   |
| Pool operator              | Account metrics, assets and existing pool configuration tools       |
| Account administrator      | Direct/subclient comparison, team and permitted allocation controls |
| Subclient                  | Its own assigned estate, without another delegation level           |

Overview counts conventional miners and container modules as mining assets.
Controllers are not additional miners. An account total includes resources
allocated to its subclients; administrators with membership-management or
delegation capability can compare the direct remainder and each subclient.
This reporting privilege does not grant access to a child’s private pools,
team, keys or settings. Private child workspace links still require explicit
account authority. Viewers receive no child ownership labels or owner filters.

Hashrate and mining equipment power show current available readings and separate
reporting counts. Efficiency requires complete valid power and hashrate coverage
for every selected asset, including complete module measurement coverage.
Partial/unknown module coverage, unavailable assets and assets outside the current
site policy are labeled. Allocation counts retain unavailable and outside-policy
assets, with their physical details and readings withheld; suspended child
allocations remain in the parent’s inventory totals. Dashboard counts and site
summaries cover the permitted allocated estate and exclude whole-site capacity
and plant power.

Overview reuses Dashboard health tiles and performance panels, including
segmented temperature and uptime distributions. The shared Fleet ranges are
1h (default), 24h, 7d, 30d, 90d and 1y. Allocation/site filters are server-scoped;
charts refresh automatically and on window focus. Status panels label percentages
against reporting assets and omit host-only drilldowns. Temperature bands use each
asset's latest valid authorized observation;
miner uptime uses recorded state snapshots, while module uptime uses authorized
diagnostic state observations (hashrate alone never marks a module hashing).
Entire estate, Directly managed and named subclient filters retain the
parent's navigation and permissions. Historical attribution uses the allocation
and delegation in effect when each sample was recorded. Migration 1031 begins
prospective interval tracking; earlier ownership is unknown and is never inferred
from current assignments. Recent windows up to 24 hours use authorized raw
samples; older or longer windows use allocation-specific hourly/daily history.
For larger estates, shorten the range or narrow allocation/site filters.
Partial power is a measured subtotal with reporting coverage; incomplete efficiency
remains unavailable. Missing buckets remain gaps rather than interpolated values.

Recall preserves retained readings from permitted allocation periods in charts,
but current miner/module details require a current allocation. Current site
permission gates history: removing access hides it, and restoring access restores
retained samples. History does not create a separate historical detail page.

Mining assets shows only the selected account’s allocation, including its
subclients. Its Name, Model, Status, Hashrate, Efficiency, Power, and Temp columns
use Fleet's labels, widths, metric cells, and module coverage presentation.
The account Fleet Add Filter menu supports Model, Site, Container, and Allocated to.
Choices cover the full authorized inventory across pages; server-side filtering
combines criteria before pagination and excludes redacted physical metadata.
Allocated to remains limited to viewers with assignment-management visibility.
Column visibility and order are saved per login and parent account through the
same Manage columns dialog as Fleet. Sorting applies across the inventory with
missing readings last; cursors are bound to the account, user, filters, and sort.
Both tables use 50-row pages and the same selection bar. Account selection stays
limited to the visible page and exposes only assign/recall actions. Model names
belong to miners; a module never inherits its controller's model or network details.
Fresh temperature and reported efficiency follow the existing mining-asset view's
freshness and site-policy redaction. It separates commercial reporting allocation from physical placement.
Hosts select miners/modules in Fleet and choose **Allocate to client** from More
(Actions on smaller screens). Fleet’s configurable **Allocated to** column opens
the same client management page as Clients, under host authority. Allocations
to subclients resolve to their direct client in the host view. Clients and Subclients also open these focused inventories without
requiring access to a child’s private workspace. A parent can recall from the
receiving account’s inventory, including a suspended child’s retained allocations.
An account’s own inventory is read-only; administrators can filter by subclient
and open **Manage subclient assets** for parent-authorized recall. An account
cannot surrender its own allocation using its own delegation grant.
Customer administrators open **Allocate assets** from Subclients and choose only
resources held directly by their account. Host assignment and customer delegation
use separate pickers from the inventory’s recall controls. Recall returns the selected
allocation to its parent. Whole-customer recall also clears its subclient
allocation. Fleet assignment resolves exact typed leaves with current allocation revisions in
a bounded lookup; missing or ineligible leaves block the entire selection. It does
not expand a global Fleet selection or silently assign a partial batch.
Selection and confirmation are limited to the displayed page;
filtering a container selects its displayed current modules, never its controller
or future/replacement hardware. Stale assignments require a fresh selection and
review. Retired/outside-policy child allocations can be recalled to clear them.
Assignments do not apply pools, change payout destinations, activate customer
firmware control or move equipment. Host operations continue under existing
organization permissions.

These routes reuse the Fleet layout and shared components without mounting host
fleet providers for account-only users. Page-owned RPC clients capture
`X-Fleet-Account-ID` and abort when account or login changes. Preview and filter
state remain local to each browser tab. Permission changes replace the scoped
connection and clear report, selection and private form state; selected-account reporting and assignment RPCs
currently require a Fleet session rather than an account API key.

Host-created clients record host administration as an ancestor-access grant,
separate from the client's team. Creation does not add the host operator as a
member or count host staff as client logins. Host operators with organization
`account:manage` can open the client workspace and invite its first administrator;
live role changes and client ceilings still constrain that access. An explicit
initial administrator is an actual team member. Customer-created subclients keep
the creating customer's initial administrator membership.

Client account and host team invitations show an eight-character code once for
manual sharing, formatted as `K7MP-4X9R`. Code entry shows one box per character
in the `XXXX-XXXX` layout: typing advances automatically,
hyphens are implied, and pasting into a box fills the rest, ignoring case, spaces
and hyphens. Characters outside the code alphabet are flagged. Recipients select
**Use an invitation code** on the login screen or account list, paste the code,
then use an existing Fleet login or register a new one. The preview names the
client account or host team and the offered role. Codes stay out of the URL and
are not retained across reloads. A signed-in recipient can accept with the
current login or choose **Use a different login** to sign out first. Optional
links use
`/account-invitation#token=…` at the sender’s current Fleet address; recipients
must be able to reach that address. **Copy invitation code** is the primary sharing
action; **Copy invitation link** is secondary, with separate copied states. The page removes the fragment before
previewing the invitation. New API-key secrets are also shown once for manual copying. Saved pool passwords are
never read back; a revision can retain them only for unchanged slot destinations
and usernames. Incoming profiles pin an immutable revision and require the
current login password when changed.

Host administrators with `user:manage` invite team members from Settings → Team →
**Invite team member**, choosing an assignable organization role and a one- or
24-hour expiry. Recipients choose their own username and password; nobody shares
a temporary password. A signed-in client login can accept a team invitation
without a password reset and receives the role's permissions immediately. Logins
that already hold a team role cannot accept another; change their role on the
Members tab instead. The **Invitations** tab lists pending, unavailable, revoked
and expired invitations, newest first. Pending and unavailable ones can be
revoked; revoked and expired ones can be deleted. Accepted invitations leave the
list, and the activity log records issuance, revocation, deletion and
acceptance. An invitation becomes permanently unavailable once its inviter's
roles or the invited role's permissions change, even if the change is later
reverted. Password resets remain on the Members tab.

Ancestor grants can be created for the direct parent and updated for existing
ancestors. Creating a subclient’s first grant to the hosting root needs ancestor
metadata that the current API does not expose.

Hashboard-level allocation, automatic allocation of new hardware, customer firmware
commands, pool application and downloadable reconciliation exports remain unsupported.

Account permissions distinguish **Manage pool configurations** from **Select stored pool
profile**; profile selection does not apply pools to equipment. New account API keys
offer View and optional saved-pool management, while reporting and account
administration require a session. See [Hosted account permissions](../docs/development/hosted-account-permissions.md)
for the operation and authentication matrix.
