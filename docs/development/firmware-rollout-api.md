# Firmware rollout REST API

External tools can upload firmware, manage release channels, start updates, and
control or observe their progress over HTTP. A channel's `DELEGATED` behavior
lets a controller choose which queued miners to update and when; Fleet retains
ownership of membership, compatibility checks, offline capacity, and operator
pause/cancel controls.

The resource endpoints below use the same authenticated handlers and lifecycle
as the Fleet UI. The existing JSON RPC endpoints at
`POST /rollout.v1.RolloutService/<Method>` remain available. The
[rollout contract](../../proto/rollout/v1/rollout.proto) defines all fields,
enums, validation limits, and lifecycle rules.

## Authentication and request format

Create an API key in **Settings → Integrations**. The key's owner needs
`miner:firmware_update` for all channel and rollout operations, uploads,
metadata changes, and deletion. File listing, upload configuration, and checksum
lookup accept either `fleet:read` or `miner:firmware_update`. Keys inherit their
owner's current permissions; revoking the key or removing those permissions
also affects subsequent requests and upload chunks.

Send `Authorization: Bearer <key>`. Session cookies also work, but do not send
both credentials. Channel and rollout data is scoped to the authenticated
organization. Firmware files retain the server's shared catalog and existing
permission checks.

For channel and rollout endpoints:

- Send `Content-Type: application/json` with every POST, PUT, and DELETE,
  even for an action without parameters (send `{}` or an empty body). Other
  content types are rejected. GET filters go in the query string.
- Responses use protobuf JSON: camelCase fields, string-valued 64-bit IDs and
  revisions, enum names such as `ROLLOUT_METHOD_DELEGATED`, and RFC 3339 times.
  Default values may be omitted. Snake_case protobuf field names are accepted
  in requests too.
- The path supplies `channelId` or `rolloutId`; omit it from the body. A
  conflicting ID, unknown field, or repeated query parameter is rejected.
- Successful operations return HTTP 200 and the corresponding RPC response
  envelope (`channel`, `rollout`, `startedRollouts`, etc.). Errors use the
  Connect JSON shape: `code`, `message`, and optional `details`.
- Request bodies are bounded to 16 MiB. Field and page limits still apply.

Firmware file endpoints keep their existing snake_case JSON, multipart upload
format, and `{ "error": "..." }` error shape.

## CT1 hashboard archives

For `target_manufacturer=Proto` and `target_model=CT1`, upload the release
`hashboard-firmware-<version>.zip` with `hashboard_build` set to `prod`, `dev`,
or `mfgtest`. This additional snake_case field is accepted by multipart uploads
and upload metadata, and returned in firmware file information. Other targets
must omit it. The existing Fleet upload dialogs expose the build selection, and
`fleetcli firmware` upload, check, and metadata commands accept
`--hashboard-build`.

Fleet reads embedded image metadata to select that build, excludes unsigned
and evaluation-board images, and requires `firmware_version` to match the
embedded version (an optional leading `v` is accepted on upload). It stores a
deterministic ZIP containing only the selected images. The stored checksum is
therefore the checksum of the selected ZIP, not of the original release archive;
each build has a distinct artifact identity. The existing checksum check may
miss an original multi-build archive, but upload completion deduplicates the
normalized artifact. Stored filenames include version and build.

Metadata edits cannot change a CT1 artifact's embedded build or version; upload
a new artifact instead. Frigate remains responsible for validating signatures
and board compatibility. Fleet enforces native upload limits of 64 images,
64 MiB per image, and 256 MiB total uncompressed image data, and rejects archives
with more than 1,024 entries or 1 MiB of central directory before reading them.
The directory check walks the records Go's ZIP reader will parse, so forged
end-record offsets or wrapped entry counts cannot hide entries. Each process
holds at most two decoded archives in memory at once.

Assign the resulting file ID to a CT1 release-channel target using the existing
API. CT1 convergence uses fresh, uniform hashboard firmware observations rather
than the controller software version; release-channel and rollout
`firmware_version` fields report that version, or empty when boards are mixed
or not observed; `firmware_version_state` on channel miners and rollout devices
tells those apart. Mixed boards are mismatched, so enforcement updates the
container. An unknown version is neither on target nor mismatched, so it
cannot start, verify, or reopen a rollout. Updates use one-off Frigate jobs; Fleet
retains channel enforcement, retry, pause, and cancellation scheduling. Canceling
a Fleet rollout does not cancel an already-dispatched Frigate job. Per-board
progress and owner/failure evidence appear in container module details.

## CT1 PSU capsules

For `target_manufacturer=Proto` and `target_model=CT1`, upload a signed PSUCAP
v1 capsule (`.psucap`, at most 512 KiB) with `component` set to `psu` and no
`hashboard_build`. This snake_case field is accepted by multipart uploads and
upload metadata, and returned in firmware file information. Other firmware
omits it. The Fleet upload dialogs set it for a `.psucap` file, and
`fleetcli firmware` upload, check, and metadata commands accept
`--component psu`.

Fleet checks the capsule's structure, digests, and outer signature against its
embedded certificate, then stores it byte for byte, so the stored checksum is
the capsule ID Frigate uses. Frigate and the hashboards remain responsible for
the certificate chain and for matching a capsule to a PSU. A capsule uploaded
without the component is refused, as is any other file uploaded with it. Stored
filenames name the PSU vendor, the PSU revision the capsule applies to, and the
start of the capsule ID.

A capsule does not name the firmware version it installs. `firmware_version`
is the uploader's label. A direct update does not compare it with the version
PSUs report (BOCO PSUs report `app=<revision>;bl=<revision>`). A release
channel does compare it, as described below. A metadata edit can change that
label but not the component.

Fleet keeps a channel's assignments, rollouts, and deployment records per
firmware component of a model. A CT1 container has two: its main firmware,
which is its hashboard firmware, and its PSU firmware. The two are assigned,
rolled out, and verified independently.

Assign a capsule to a channel by naming the component in the assignment:

```json
{"assignments":[{"manufacturer":"Proto","model":"CT1","component":"psu","firmwareFileId":"<capsule-file-id>"}]}
```

An assignment without `component` is the model's main firmware. The file must
be firmware for the assignment's component. A capsule assigned as main firmware
fails with `failed_precondition` (`ARTIFACT_MISMATCH`), and so does an archive
assigned as PSU firmware. Clearing names the component too, with an empty
`firmwareFileId`. One request can hold one assignment for each component of a
pair, so it can change hashboard firmware and PSU firmware together. Model
groups, plans, rollouts, and miner firmware history carry `component`. A pair
has a group for its main firmware and one more for each other component
assigned to it. A channel's `modelGroupCount` counts each of these groups. Pass
`component=psu` to `GET /release-channels/{channelId}/miners` to read each
container's PSU firmware version and deployment record instead of its
hashboard ones.

Fleet verifies PSU firmware as it verifies hashboard firmware, against what the
container reports. Every located board must report the same firmware version
for its module's PSU. That version must equal the capsule's
`firmware_version`. A capsule names no version, so enter the version PSUs
report once it is applied (BOCO PSUs report `app=<revision>;bl=<revision>`).
With any other label the update runs but its containers never verify and end
as failed. To correct a label, edit the file's version, clear the PSU
assignment, and assign the capsule again. Applying an assignment whose capsule
is unchanged does nothing. Differing PSU versions are drift, so enforcement
sends the capsule again, and the update leaves modules that already run it
alone. A version that is not observed neither verifies nor reopens a rollout.

A container takes one firmware command at a time. While an update of one
component is pending, a rollout of the other waits to dispatch. A container
cannot verify an update while a command of the other component runs. When that
command ends, the container gets the full resend interval to report before the
rollout sends its firmware again or fails the attempt.

A channel owns a miner's firmware per component. A channel that assigns only
CT1 hashboard firmware does not own a container's PSU firmware. Fleet then
admits a direct PSU update sent with the container's **Update firmware**
action; see [PSU firmware updates](ct1-grpc.md#psu-firmware-updates). After the
channel assigns PSU firmware too, Fleet does not send that update. Finishing a
firmware command clears only the deployment record of the component it
updated.

In the Fleet UI, a channel lists PSU firmware as a **PSU firmware** row under
its container model, after **Hashboard firmware**. The row appears once a
capsule for that model is uploaded or assigned. Activity log entries for a PSU
rollout name the model without the component.

## CT1 controller software version

A container's controllers install the firmware that a channel assigns to it. A
channel can therefore name the controller software version its containers
should run before they take firmware. Set it per container manufacturer/model
pair:

```sh
curl -X PUT "$FLEET/api/v1/release-channels/$CHANNEL/controller-software" \
  -H "Authorization: Bearer $FLEET_API_KEY" -H "Content-Type: application/json" \
  -d '{"manufacturer":"Proto","model":"CT1","version":"1.6.0"}'
```

- The version must be that of an uploaded controller software package; an
  unknown version is rejected with `failed_precondition`. An empty `version`
  clears the target. A successful write returns an empty response.
- Setting or clearing the target needs both `miner:firmware_update` and
  `controller:software_update`, each held at organization scope because a
  channel can span sites. Reading it needs only `miner:firmware_update`.
- The target starts no update. Controller software is still updated one
  container at a time, as described in
  [Frigate server updates](frigate-server-updates.md). Deleting the package
  later does not clear the target.
- `GET /release-channels/{channelId}/controller-software` returns each
  container model in the channel with its target, how many containers are on
  it, and the total container count. It does not return container details.
- `GET /release-channels/{channelId}/containers` returns a page of containers
  with their reported controller software version and `onTarget`. Set
  `manufacturer` and `model` to the exact observed pair from the summary.
  Empty identities are exact filters, not wildcards. Set `pageSize` up to
  1000 and pass each response's `cursor` until it is empty.
  Versions compare after trimming whitespace and one leading `v`. A container
  that reports no version is not on the target.
- While a target is set, a rollout sends firmware only to containers on it.
  This applies to every firmware update the channel sends to a container,
  hashboard and PSU firmware alike. A container that is behind stays `QUEUED`:
  it uses no attempt and does not fail. When every container due an update is
  waiting, the rollout's
  `evidence.holdReason` names the version and how many containers need it. The
  rollout stays active until those containers are updated, skipped, or
  canceled, or the target is cleared.
- `AdvanceRollout` rejects a selection that includes a container behind the
  target with `DEVICE_NOT_DISPATCHABLE`, lists those containers in
  `deviceIdentifiers`, and queues nothing. Skip them or advance the rest.
- A firmware update started from a container's own menu is not held.

Each container reports one controller software version, so a pair with only
one controller updated can read as on the target.

## Resource endpoints

All paths below start with `/api/v1` on the Fleet server. The shipped web
frontend proxies these through `/api-proxy`, so a public frontend URL uses
`https://fleet.example.com/api-proxy/api/v1/...`. A direct development server
uses `http://localhost:4000/api/v1/...`.

| Operation | Method and path | RPC contract |
| --- | --- | --- |
| List or create channels | `GET /release-channels`, `POST /release-channels` | ListReleaseChannels, CreateReleaseChannel |
| Read, replace settings, or delete a channel | `GET`, `PUT`, `DELETE /release-channels/{channelId}` | GetReleaseChannel, UpdateReleaseChannel, DeleteReleaseChannel |
| Preview membership | `POST /release-channels/preview-scope` | PreviewReleaseChannelScope |
| Inspect membership conflicts | `GET /release-channels/membership-conflicts` | ListReleaseChannelMembershipConflicts |
| List model groups or miners | `GET /release-channels/{channelId}/model-groups`, `GET /release-channels/{channelId}/miners` | ListReleaseChannelModelGroups, ListReleaseChannelMiners |
| List container controller software | `GET /release-channels/{channelId}/containers` | ListReleaseChannelContainers |
| Read or set the controller software version | `GET`, `PUT /release-channels/{channelId}/controller-software` | ListReleaseChannelControllerSoftware, SetReleaseChannelControllerSoftware |
| Preview or apply assignments | `POST /release-channels/{channelId}/firmware/preview`, `POST /release-channels/{channelId}/firmware/apply` | PreviewReleaseChannelFirmware, ApplyReleaseChannelFirmware |
| List or read rollouts | `GET /rollouts`, `GET /rollouts/{rolloutId}` | ListRollouts, GetRollout |
| Inspect per-miner progress | `GET /rollouts/{rolloutId}/devices` | ListRolloutDevices |
| Poll events | `GET /rollouts/events`, `GET /rollouts/{rolloutId}/events` | ListRolloutEvents |
| Dispatch selected queued miners | `POST /rollouts/{rolloutId}/advance` | AdvanceRollout |
| Skip queued miners | `POST /rollouts/{rolloutId}/skip` | SkipRolloutDevices |
| Finish a delegated update | `POST /rollouts/{rolloutId}/complete` | CompleteRollout |
| Pause or resume | `POST /rollouts/{rolloutId}/pause`, `POST /rollouts/{rolloutId}/resume` | PauseRollout, ResumeRollout |
| Cancel remaining work | `POST /rollouts/{rolloutId}/cancel` | CancelRollout |
| Release a manual review gate | `POST /rollouts/{rolloutId}/continue` | ContinueRollout |
| Retry failed, skipped, or canceled work | `POST /rollouts/{rolloutId}/retry` | RetryFailedRolloutDevices |
| Restore the previous assignment | `POST /rollouts/{rolloutId}/rollback` | RollbackReleaseChannelFirmware |

`PUT` replaces channel settings, rather than patching individual fields. Read
the channel first and preserve settings you intend to keep. Assignment requests
are different: omitted manufacturer/model pairs retain their assignment; an
empty `firmwareFileId` explicitly clears one. An assignment is for one firmware
component of its pair, the main firmware unless `component` says otherwise; see
[CT1 PSU capsules](#ct1-psu-capsules).

| Firmware operation | Method and path |
| --- | --- |
| Allowed formats and size limits | `GET /firmware/config` |
| Check for an existing SHA-256 | `POST /firmware/check` with `sha256`, `target_manufacturer`, `target_model`, and `firmware_version` |
| Upload | `POST /firmware/upload` with multipart `file`, `target_manufacturer`, `target_model`, `firmware_version` |
| List files and metadata | `GET /firmware/files` |
| Replace target metadata | `PATCH /firmware/files/{fileId}` |
| Delete one file or all eligible files | `DELETE /firmware/files/{fileId}`, `DELETE /firmware/files` |
| Start chunked upload | `POST /firmware/upload/chunked` with `filename`, `file_size`, and target metadata |
| Append chunk | `PUT /firmware/upload/chunked/{uploadId}` with raw bytes and `Content-Range: bytes START-END/TOTAL` |
| Finish upload | `POST /firmware/upload/chunked/{uploadId}/complete` |

Read `chunk_size_bytes` from `/firmware/config` and send contiguous chunks in
order. Use the same API key for the entire upload. Upload sessions are temporary
and process-local; restart the upload if they expire or the server changes.
Assignments refer to immutable artifact checksums resolved from uploaded file
IDs. Existing file-in-use protections also apply to external callers.

## Example: controller-driven rollout

These commands illustrate a single-model channel. Substitute your server URL,
miner identifiers, target metadata, and response IDs. They perform real changes;
use a test fleet when evaluating an integration.

```bash
FLEET_URL=https://fleet.example.com/api-proxy
# Set FLEET_API_KEY from your secret store.

curl --fail-with-body "$FLEET_URL/api/v1/firmware/upload" \
  -H "Authorization: Bearer $FLEET_API_KEY" \
  -F file=@firmware.swu \
  -F target_manufacturer=Proto \
  -F target_model=Rig \
  -F firmware_version=1.4.3
```

Retain the returned `firmware_file_id`. Create a channel with an explicit scope
and a channel-wide capacity limit. An empty scope creates an empty channel.

```bash
curl --fail-with-body "$FLEET_URL/api/v1/release-channels" \
  -H "Authorization: Bearer $FLEET_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "External canary",
    "scope": {"deviceIdentifiers": ["miner-a", "miner-b"]},
    "behavior": {
      "method": "ROLLOUT_METHOD_DELEGATED",
      "order": "ROLLOUT_ORDER_LEAST_EFFICIENT_FIRST",
      "maxConcurrentOffline": 1,
      "controllerTimeoutSeconds": 600
    }
  }'
```

Use the returned `channel.id` as `CHANNEL_ID`. Preview the assignment before
applying it. The same JSON body is accepted at both endpoints.

```bash
curl --fail-with-body "$FLEET_URL/api/v1/release-channels/$CHANNEL_ID/firmware/preview" \
  -H "Authorization: Bearer $FLEET_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"assignments":[{"manufacturer":"Proto","model":"Rig","firmwareFileId":"<uploaded-file-id>"}]}'

curl --fail-with-body "$FLEET_URL/api/v1/release-channels/$CHANNEL_ID/firmware/apply" \
  -H "Authorization: Bearer $FLEET_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"assignments":[{"manufacturer":"Proto","model":"Rig","firmwareFileId":"<uploaded-file-id>"}]}'
```

Apply returns `startedRollouts`: one per changed model with mismatched miners,
possibly empty. Reapplying an unchanged assignment is not a restart. Each
delegated rollout snapshots its targets and sends no update commands until
advanced. Late joiners do not expand an active delegated snapshot. After it
finishes, reconciliation can create another delegated rollout for unsuppressed
miners that still need the assigned firmware; that rollout also waits for the
controller. Poll the channel's rollouts to discover these successors.

You can instead supply a `behaviorOverride` on preview/apply for a
one-off delegated update; its `maxConcurrentOffline` must be zero because the
saved channel budget always governs dispatch. Later reconciliation rollouts
use the channel's saved behavior again.

Read a rollout, then use its current `revision` for the next action:

```bash
curl --fail-with-body "$FLEET_URL/api/v1/rollouts/$ROLLOUT_ID" \
  -H "Authorization: Bearer $FLEET_API_KEY"

curl --fail-with-body "$FLEET_URL/api/v1/rollouts/$ROLLOUT_ID/advance" \
  -H "Authorization: Bearer $FLEET_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"count":1,"expectedRevision":"<current-revision>","note":"Start canary"}'
```

Use either `count` or `devices: { "deviceIdentifiers": ["miner-a"] }`, never
both. Count follows the rollout's saved order and can select fewer miners when
fewer remain. The returned `deviceIdentifiers` identify dispatched targets.
Dispatch admission rejects
stale revisions, paused/finished rollouts, nonqueued named targets, and a
selection exceeding the channel's available offline capacity without starting
any of that selection. A queued target that cannot be dispatched now also
rejects the whole selection with `DEVICE_NOT_DISPATCHABLE` and its identifier:
a command preflight filter such as curtailment rejected it, another firmware
command is pending for it, or it no longer reports the rollout's manufacturer
and model. Count selection picks the same target again, so skip it or name
other targets. An accepted update is asynchronous, not proof that the
miner has installed the firmware.

Poll `/devices` for phases and `/events` for lifecycle changes. Wait for your
health criteria before advancing more targets. To leave a queued miner alone:

```json
{"devices":{"deviceIdentifiers":["miner-b"]},"expectedRevision":"<current-revision>","note":"Excluded by controller health policy"}
```

POST that body to `/skip`. POST `{"expectedRevision":"<current-revision>"}`
to `/complete` to finish and mark any remaining queued targets skipped. Complete
rejects in-flight updates. Fleet also finishes automatically when all targets
are done, failed, skipped, or excluded; completion does not imply every miner
was updated successfully.

## Controller recovery and operator control

- Always send the last observed `expectedRevision` for rollout mutations.
  After a lost response or `STALE_REVISION`, read the rollout and devices before
  deciding what to do next. Do not blindly replay a count-based advance with a
  newly fetched revision. Omitting the revision disables this concurrency check.
- `WAITING_FOR_CONTROLLER` means the active rollout has no update in flight.
  A nonzero `controllerTimeoutSeconds` pauses it after that idle interval and
  emits `CONTROLLER_TIMED_OUT`. Reads do not reset the timer. Resume explicitly
  after investigating; it does not dispatch queued delegated work.
- Fleet never automatically resends a delegated attempt. An unverified attempt
  fails 10 minutes (the existing update verification interval) after its
  update command finishes, so the miner has time to reboot and report. The
  controller must explicitly request recovery.
- Retry on an **active** delegated rollout requeues recoverable targets, which
  still require Advance. Retry on a **finished** rollout follows the existing
  recovery contract: it may start an **all-at-once** recovery rollout for the
  current assignment, including failed/skipped/canceled work from earlier
  rollouts. Inspect the response instead of assuming the original rollout is
  still active or externally paced.
- Pause prevents further dispatch. Cancel stops remaining work and suppresses
  it for that assignment generation. Already-sent commands can finish. Skip
  and Complete do not undo an installed update. Rollback changes the current
  assignment and may start an all-at-once update to its previous version.
- Existing channels may be managed in the UI while an external controller is
  connected. An assignment change can supersede the controller's rollout;
  channel settings apply according to the existing snapshot/live-budget rules.
  Treat pause, cancellation, stale-generation, and missing-channel responses as
  authoritative, rather than trying to recreate or resume work automatically.

## Polling and errors

Most lists return a `cursor` only when another page exists. Keep filters fixed
while paging. `ListRollouts` additionally returns `pollCursor`: drain every page
with `cursor`, then use `pollCursor` for the next cycle. Merge results by rollout
ID and revision; replay is possible. Leave `status` unset to observe terminal
transitions. `updatedAfter` is a date filter, not a lossless polling mechanism.

The event feed is ordered oldest first. Its `cursor` is **always nonempty**,
including on an empty page. Persist the returned cursor after processing each
page and poll again with the same organization/channel/rollout filters. IDs
can have gaps. Cursor ordering accounts for concurrent commits within an
organization. Events begin when this feature is deployed; existing history is
not backfilled. Deleting a channel deletes its rollout/event history too, so
archive events externally if you need them after deletion.

Use `code` and the typed `rollout.v1.RolloutErrorInfo` detail rather than parsing
human-readable messages. For example, `failed_precondition` maps to HTTP 400 and
may include `STALE_REVISION` with `currentRevision`, `PAUSED`,
`OFFLINE_BUDGET_FULL`, `DEVICE_NOT_QUEUED`, `DEVICE_NOT_DISPATCHABLE`, or
`UPDATES_IN_FLIGHT`. Authentication
and authorization failures use HTTP 401/403; missing resources use 404. A
temporary server/leadership error is not proof that a previous mutation failed
to commit: reconcile state before retrying it.
