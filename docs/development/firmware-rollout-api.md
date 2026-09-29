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

- Use `Content-Type: application/json` for request bodies. GET filters go in
  the query string; mutations take JSON, including DELETE when needed.
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
empty `firmwareFileId` explicitly clears one.

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
any of that selection. A command-level preflight rejection also rolls back the
selection. An accepted update is asynchronous, not proof that the
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
- Fleet never automatically resends a delegated attempt. Once no command is
  pending, an unverified attempt fails after the existing 10-minute update
  verification interval. The controller must explicitly request recovery.
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
`OFFLINE_BUDGET_FULL`, `DEVICE_NOT_QUEUED`, or `UPDATES_IN_FLIGHT`. Authentication
and authorization failures use HTTP 401/403; missing resources use 404. A
temporary server/leadership error is not proof that a previous mutation failed
to commit: reconcile state before retrying it.
