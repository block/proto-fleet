# Frigate server updates from Fleet

Fleet can stage a signed Frigate Debian package on both controllers of a CT1
container and trigger a coordinated HA upgrade. Fleet only transports and
tracks the update. The controllers verify, coordinate, stop, install, migrate
and restart themselves, and they keep reporting progress while Frigate is
stopped.

This page is for operators and developers. The wire contract is
`proto/frigateupdate/v1/frigateupdate.proto`; the controller side is the MDK
`ServerUpdateService` (`sdk/mdk/proto/mdk/v2/server_update.proto`).

## What Fleet does

1. An administrator uploads a release package together with its signed release
   statement and signature (`POST /api/v1/frigate-packages/upload/chunked`). In
   the Fleet UI, packages are listed, uploaded and deleted under **Settings →
   Firmware → Files**, in the **Proto Containers** section. That section is
   hidden while no Proto Container is paired where the user can list or update
   one. The Fleet UI, the text the Fleet server writes for it, and ProtoOS call
   Frigate "controller software" and do not use the name Frigate. The upload
   dialog has one file picker for all the files of a release. It identifies
   each file by its extension: `.deb` for the package, `.json` for the release
   statement, `.sig` for the release signature, and `.pem` for the optional
   release public key; see
   [Signature check at upload](#signature-check-at-upload).
2. `StartUpdate` records a job for one container. The `frigate-update-runner`,
   which runs only on the active Fleet replica, then:
   - stages the package on each controller with that controller's own
     Begin/Append/Commit calls (1 MiB chunks, resuming from the controller's
     acknowledged offset). For a container behind a Fleet Node, the node makes
     these calls; see
     [Containers behind a Fleet Node](#containers-behind-a-fleet-node);
   - calls Start once, with each controller's manifest fingerprint, so Frigate
     refuses to run a package other than the one Fleet staged;
   - polls rollout status from the controllers every 5 seconds until the
     rollout reaches a final state.

   The runner advances at most eight jobs at a time. Jobs are dispatched least
   recently dispatched first (never-dispatched first, then oldest), so jobs that
   stay runnable, such as retryable failures, cannot take every slot on every
   pass and starve the others. The rotation is in memory on the active replica.
3. The container page shows per-controller progress. `RetryUpdate` and
   `CancelUpdate` forward to Frigate with idempotency keys derived from the job.

Every job uses `SERVER_UPDATE_COORDINATION_SELF`: the controllers coordinate the
rollout between themselves. Fleet does not hold a maintenance token and does not
drive the install steps.

## Requirements and limits

- **Token-protected HA pairs are not supported yet.** Fleet sends a paired
  controller's stored token on its CT1 gRPC connection (port 9095), but it cannot
  yet pair an HA pair whose controllers have `server.mdk_auth.enabled`. See
  [the gRPC transport guide](ct1-grpc.md) for the token and its limits.
- **A container behind a Fleet Node needs a node with command protocol V20.**
  For an older node, `GetSupport` and every update call return
  `FailedPrecondition` with a message that tells the operator to upgrade the
  node. See [Containers behind a Fleet Node](#containers-behind-a-fleet-node)
  for the other limits of that path.
- **The controllers must allow remote triggers** (`update.allow_remote_trigger`
  on Frigate) and report self-coordination support, the update helper, a
  Debian-package installation, and a healthy etcd quorum. `GetSupport` reports
  the first failing condition with a stable code, for example
  `UPDATE_REMOTE_TRIGGER_DISABLED`. Fleet's verdict is advisory; Frigate checks
  again when the update starts.
- **Controllers re-verify the signature.** Fleet checks the release statement
  only to reject a bad upload early. Each controller verifies the statement
  against the keys it trusts and refuses the package if they differ. Fleet's
  check is not a security boundary, and a Fleet that has no release key
  installed accepts an upload it cannot verify; see
  [Signature check at upload](#signature-check-at-upload).
- **Control archives must be gzip or uncompressed.** Fleet reads the package's
  `control` file with the Go standard library to confirm it matches the signed
  statement, so a `.deb` whose `control.tar` is compressed with xz or zstd is
  rejected with a message asking for a gzip-compressed control archive. Data
  archives may use any compression. Rebuild with
  `dpkg-deb -Zgzip` (or any builder that gzips the control member) if needed.
- A job's package blob lives on the Fleet host that received the upload. If Fleet
  fails over to a replica that never received it, a job that is still staging
  fails before anything reaches a controller (`FLEET_PACKAGE_MISSING`); upload
  the package again and restart the update. A running rollout is unaffected,
  because Frigate already holds its own copy.
- Frigate refuses packages over 1 GiB. Fleet's own default limit is 512 MiB.
- `ListPackages` sets `stored_on_this_host` on each package. It is false when
  the package record is shared (the database is) but this replica holds no copy
  of the bytes, for example after a failover. Clients should disable "update"
  for such a package and ask for a new upload, rather than discovering
  `FLEET_PACKAGE_MISSING` after the job is created.
- If every controller is unreachable, a job in `starting` cannot be proven
  unknown to Frigate. It stays `starting`, records `unreachable_since`, and is
  neither failed by the start deadline nor cancelled locally until a controller
  answers; an operator cancel stays pending until then. After 24 hours with no
  answer, Fleet abandons the job (see below).

## Containers behind a Fleet Node

Fleet cannot open a connection to a controller that a Fleet Node manages. The
node makes the update calls, next to the controllers. The job, its states, the
permission, and the UI are the same as for a container that Fleet reaches
directly.

- **Calls.** Each update call goes to the node as one `MinerCommand` with a
  `frigate_update` action (`proto/fleetnodegateway/v1/fleetnodegateway.proto`).
  The action carries the bytes of the plugin's update request
  (`FrigateServerUpdateRequest`), and the acknowledgement carries the bytes of
  the response. The gateway has no update schema of its own. The node runs the
  call on the CT1 plugin. A refusal from a controller is not a failed command.
  The node returns it as an outcome with the controller's `UPDATE_*` reason,
  message, and status code. Fleet then handles it as it handles a refusal on a
  direct connection.
- **Package.** Package bytes do not go through the control stream. Fleet
  begins the copy on each controller and records its manifest fingerprint.
  Fleet then sends one `stage` command for all the controllers that are not
  staged. The node downloads the package from Fleet one time
  (`DownloadCommandArtifact`, purpose `CONTROLLER_SOFTWARE_PACKAGE`). Fleet
  serves the package only to the `stage` command that names it. The node checks
  the size and the SHA-256 of the download. It then copies the package to all
  the controllers at the same time, in 1 MiB chunks. Each copy starts from the
  offset that its controller acknowledges. The node then commits each copy.
  The node sends no bytes to a controller that reports a different fingerprint.
  The `stage` command returns one outcome for each controller. Fleet checks the
  size and the SHA-256 that each controller committed, as it does on a direct
  connection.
- **Write admission.** A support or a status call is a read. Each other call
  goes through the device write gate as a controller software update, as on a
  direct connection.

Limits of this path:

- The node must speak command protocol V20. Fleet does not send an update
  command to an older node. A new node works with an older Fleet, which sends
  no update commands. Upgrade Fleet and the node in any order.
- The package can be 500 MiB at most. This is the download limit of the node,
  which is less than Fleet's default upload limit of 512 MiB. The node refuses
  a larger package, and the job fails with `FLEET_STAGE_STALLED` after the
  stall timeout. The error message gives the limit.
- A node downloads one package or firmware file at a time. A `stage` command
  that arrives during another download gets a busy answer. Fleet sends the
  `stage` command again on each later pass until the stall timeout.
- One `stage` command has 10 minutes on the node for the download and for the
  copies. Fleet waits 30 seconds longer. A copy that is not complete in that
  time continues in the next `stage` command from the acknowledged offset. The
  download is not resumed: each `stage` command downloads the full package
  again.
- On the node, each controller call during a `stage` command has a fixed limit
  of 30 seconds. Each 1 MiB chunk has a fixed limit of 60 seconds.
  `FRIGATE_UPDATE_CALL_TIMEOUT` and `FRIGATE_UPDATE_CHUNK_TIMEOUT` apply only to
  the calls that Fleet makes.
- Fleet gets no position while the node copies. `staged_bytes` changes only
  when a `stage` command ends. After a copy that failed, Fleet reads the offset
  from the controller. The stall timeout (`FRIGATE_UPDATE_STAGE_STALL_TIMEOUT`)
  counts from the last time that an offset moved. Thus a slow copy that moves
  bytes in each `stage` command is not a stall.
- If Fleet cannot read the offset after a copy that failed, the time of that
  `stage` command does not count toward the stall timeout. Fleet leaves out at
  most one stall timeout between two confirmed offsets. Thus a job with no
  confirmed progress fails as stalled after two times the stall timeout.

## Permission

`controller:software_update` gates everything in this feature. It is not part of
the seeded ADMIN role. Grant it explicitly through a role to the people who may
update controller software. The other permissions an administrator already
has are not enough.

| Operation | Needs the permission |
| --- | --- |
| `GetSupport`, `StartUpdate`, `GetUpdate`, `ListUpdates`, `RetryUpdate`, `CancelUpdate` | at the container's site |
| `ListPackages`, package upload | at any site |
| `DeletePackage` | organization-wide, because packages are shared by every site |
| `SetReleaseChannelControllerSoftware` (rollout service) | at organization scope, together with `miner:firmware_update` |

A release channel can name the controller software version its containers
should run. That target starts no update here; it holds the channel's firmware
updates from containers that are behind. See
[CT1 controller software version](firmware-rollout-api.md#ct1-controller-software-version).

`ListPackages` also returns `has_updatable_container`: whether a container is
paired at a site where the caller holds the permission. Listing containers
needs read access at their site, which this permission does not imply, so the
Fleet UI uses this answer, not an empty container list, before it hides
controller software from someone who may update it.

## Configuration

All settings are Fleet server flags with environment variables:

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `FRIGATE_UPDATE_PACKAGE_DIR` | `firmware/frigate-packages` | Where uploaded packages are stored. The default is inside the persisted firmware volume (`artifacts/firmware` is mounted at `/app/firmware` by the standard compose files), so the packages survive container recreation. Any other directory must be on persistent storage and writable by the server user. |
| `FRIGATE_UPDATE_MAX_PACKAGE_SIZE` | `536870912` | Largest package Fleet accepts, in bytes. |
| `FRIGATE_UPDATE_RELEASE_KEYS_DIR` | empty | Directory of trusted release public keys. When it is empty, or holds no key, Fleet has no installed release key and checks an upload as [Signature check at upload](#signature-check-at-upload) describes. With the standard compose deployment, set it to `/app/frigate-release-keys` (see [Release key directory](#release-key-directory)). |
| `FRIGATE_UPDATE_POLL_INTERVAL` | `5s` | Runner tick and rollout status poll interval. |
| `FRIGATE_UPDATE_STAGE_STALL_TIMEOUT` | `10m` | Staging that makes no progress for this long fails the job. |
| `FRIGATE_UPDATE_START_TIMEOUT` | `10m` | A job that has been `starting` this long, with a fresh status read showing Frigate never accepted the job, is failed with `FLEET_START_TIMEOUT`. |
| `FRIGATE_UPDATE_CALL_TIMEOUT` | `30s` | Timeout for a controller call that carries no package bytes. |
| `FRIGATE_UPDATE_CHUNK_TIMEOUT` | `60s` | Timeout for staging one 1 MiB chunk. |

Abandoned upload sessions are removed after `CHUNKED_UPLOAD_SESSION_TTL` (the
same setting the firmware uploader uses, default 1 hour). At most 8 package
uploads can be in progress at once, and a single user may have at most 2 of
those. A user over the cap, or a Fleet that is full, gets `429`; a store or database that cannot serve the request gets `503`.

The same sweep removes orphan package directories: a blob directory with no live
package row (an import that failed between storing the file and recording it, or
a package whose delete could not remove the file) that is older than one hour.
The grace period keeps it from racing an import that is still recording its row.

### Signature check at upload

What Fleet does with the release signature depends on whether it has a release
key installed in its [release key directory](#release-key-directory).

| Fleet has an installed key | Public key in the upload | Result |
| --- | --- | --- |
| Yes | Not used | The signature must verify against an installed key. If it does not, Fleet refuses the upload. |
| No | The key that signed the release | Fleet verifies the signature with that key and records the key ID. |
| No | A key that did not sign the release | Fleet stores the package without a verified signature and returns a warning. |
| No | None | Fleet stores the package without a verified signature and returns a warning. |

A key from the uploader never replaces an installed key. A key directory that
Fleet cannot read, or a key file that does not parse, is an error: Fleet does
not treat it as "no installed key".

The public key is PEM `PUBLIC KEY` data for an ECDSA P-256 key, at most 4096
bytes. Fleet refuses a public key file that it cannot parse, because that is a
mistake in the upload and not a signature that Fleet could not confirm.

These checks do not need a key, so Fleet applies them to every upload and
refuses a package that fails one:

- The statement is in the canonical form, and the signature is 1 to 128 bytes.
- The package size and SHA-256 match the statement.
- The `.deb` control package, version and architecture match the statement.

A package that Fleet stored without a verified signature has an empty
`release_key_id`. The Fleet UI shows "Not verified" as its signer key.
For such a package Fleet cannot compare the signing key with the keys the
controllers trust before an update starts, so that readiness check
(`FLEET_RELEASE_KEY_UNTRUSTED`) is skipped. The controllers still verify the
signature before they stop, and a package with a bad signature ends the rollout
in the verifying phase after it was staged.

When Fleet verifies the signature with a key from the upload, it records the
key ID as usual. Before an update starts, Fleet then requires every controller
to trust that key ID, so a package that was signed with a different key does not
reach staging.

The activity log entry for an upload records the outcome as `signature_check`:
`installed_key`, `uploaded_key` or `unverified`.

### Release key directory

The key directory holds the public halves of the keys that sign Frigate
releases: PEM `PUBLIC KEY` blocks for ECDSA P-256 keys in files ending `.pem`.
Fleet reads the directory on every verification, so adding or removing a key
takes effect without a restart. A file that does not parse is an error rather
than being skipped, so a bad rotation cannot silently shrink the trusted set.

```bash
mkdir -p /etc/fleet/frigate-release-keys
cp release.pub /etc/fleet/frigate-release-keys/release-2026.pem
chmod 0644 /etc/fleet/frigate-release-keys/release-2026.pem
export FRIGATE_UPDATE_RELEASE_KEYS_DIR=/etc/fleet/frigate-release-keys
```

The standard compose deployment (`deployment-files/docker-compose.yaml`) already
mounts `artifacts/frigate-release-keys` read-only at `/app/frigate-release-keys`
in the `fleet-api` container. The `artifacts` directory sits beside `deployment`
and survives installer and updater tree swaps. To turn on package upload, copy
the key in and point the variable at the mount in the deployment `.env`:

```bash
sudo mkdir -p <install>/artifacts/frigate-release-keys
sudo install -m 0644 frigate-release.pem <install>/artifacts/frigate-release-keys/
echo 'FRIGATE_UPDATE_RELEASE_KEYS_DIR=/app/frigate-release-keys' | sudo tee -a <install>/deployment/.env
```

Then rerun `run-fleet.sh` so `fleet-api` is recreated with the variable, the
same way as any other `.env` change. It restarts the stack using the install's
project name, profile and enabled overlays, which a plain `docker compose up -d`
would miss:

```bash
cd <install>/deployment
./run-fleet.sh
```

Later key changes need no restart. HA deployments do not mount this directory
yet, so an HA Fleet has no installed release key.

A key file may be a symlink (Kubernetes secret mounts and `ln -s` rotations are
followed; a dangling link is an error). A `.pem` file that holds no key, or any
PEM block that is not `PUBLIC KEY`, is an error rather than being ignored.

A signature is ECDSA P-256 ASN.1 DER, at most 128 bytes (the limit Frigate's
verifier uses), over the SHA-256 of the exact statement bytes. The key ID Fleet and the controllers report is `sha256:` plus the
lowercase hex SHA-256 of the public key's SPKI DER encoding.

### Release statement format

Fleet accepts only the canonical form Frigate's `releasesig.ParseStatement`
accepts: compact JSON with the fields in the fixed order
`schema`, `package`, `version`, `architecture`, `repository`, `sha256`, `size`,
`min_installed_version`, `issued_at`, with `min_installed_version` always
present (possibly empty), `issued_at` as `2006-01-02T15:04:05Z`, and no whitespace, unknown
fields or trailing data. Fleet parses the statement and re-marshals it; any
difference from the input bytes is refused, because a statement Frigate would
refuse after Fleet had staged the package wastes the whole staging step. The
`.deb` version is matched against the statement by splitting at the last hyphen
(Debian revisions), so `1.4.0-1` matches statement version `1.4.0`.

### Upload request

```text
POST /api/v1/frigate-packages/upload/chunked
{"filename": "frigate_1.4.0_arm64.deb", "size": 123456789,
 "release_statement_b64": "...", "release_signature_b64": "...",
 "release_public_key_b64": "..."}
-> {"upload_id": "...", "version": "1.4.0", "architecture": "arm64"}

PUT  /api/v1/frigate-packages/upload/chunked/{upload_id}      Content-Range: bytes 0-33554431/123456789
POST /api/v1/frigate-packages/upload/chunked/{upload_id}/complete
-> {"package": {...}, "reused": false}
```

`release_public_key_b64` is optional. Both responses carry a `warning` string
when Fleet accepts the package without a verified signature; see
[Signature check at upload](#signature-check-at-upload).

The release is checked before any bytes are accepted. At complete, Fleet
re-hashes the file, compares size and SHA-256 with the statement, and compares
the `.deb` control package, version and architecture with the statement. The
`control.tar.gz` is decompressed with a 1 MiB cap and the `ar` member sizes are
bounded, so a decompression bomb is refused.

Uploading the same package bytes again returns the existing record
(`reused: true`). When the statement bytes or the signing key differ from the
stored ones (for example the release was re-signed by a rotated key, or a
`min_installed_version` floor was added), the stored statement, signature and
key ID are replaced with the freshly verified ones, provided no job that still
blocks the container (`staging` through `running`, or `failed` and retryable)
uses the package. Otherwise the upload fails with `FailedPrecondition`, because
changing the statement under a running job would make the stored record disagree
with what the controllers were given. A re-upload that differs only in the
signature bytes is not a change: ECDSA signatures are randomized.

An upload that Fleet did not verify never replaces a release that Fleet did
verify:

- With the same statement, the stored record and its verified signature stay,
  and the response has no warning.
- With a different statement, the upload fails with `FailedPrecondition`. Delete
  the stored package first, or upload with the release public key.

When the stored package is not verified either, the new statement and signature
replace the stored ones, with the same rule for a job that still blocks the
container. Uploading an unverified package again with the key that signed it
makes it a verified package.

### nginx body limits

Chunks must fit the reverse proxy's request body limit. The bundled nginx
configurations set `client_max_body_size 64m` for `/api-proxy/`, and chunked uploads
use Fleet's configured chunk size (32 MiB by default; the server accepts chunks up
to 64 MiB), so the default limits are enough. If you put another proxy in front of Fleet, allow request
bodies of at least the chunk size.

## Job states

`staging` (uploading to the controllers), `staged`, `starting` (Start sent, not yet
confirmed), `running` (Frigate accepted; see the rollout phase), then one of
`succeeded`, `failed`, `cancelled`, `verified` (stage-only update completed) or
`abandoned`. A `failed` job with `retryable` set still blocks a new update for
that container; use Retry or Cancel.

If Fleet cannot tell whether Frigate accepted a Start request (a timeout, for
example) it does not send a second one. Every later pass reads the rollout back
first and sends Start again only when Frigate reports it does not know the job,
using the same idempotency key.

A `starting` job does not wait forever. Once it has been `starting` for
`FRIGATE_UPDATE_START_TIMEOUT`, and a fresh status read shows Frigate does not
know the job, Fleet sends Frigate a Cancel (key `fleet-start-timeout-<job id>`,
best effort, to close the window where a lost Start is accepted late) and then
fails the job with `FLEET_START_TIMEOUT`, not retryable, with cleanup pending. If
Frigate answers the Cancel as knowing the job, Fleet takes Frigate's answer
instead of failing it. Fleet never fails a job on an unreadable status.

An operator Cancel on a `starting` job asks Frigate first. When a fresh status
read shows Frigate does not know the job and no Start can be in flight, the
cancel completes in Fleet and the job becomes `cancelled` with cleanup pending.
The runner holds a start lease (`start_lease_until`, the call timeout plus 5
seconds) while a Start is in flight. The Start call's deadline is the persisted
lease minus those 5 seconds, so a slow commit or scheduling delay shortens the
call rather than letting it outlive the lease; when no time is left the runner
sends nothing and retries on the next pass. If the lease is live, the cancel is not
refused: Fleet records a durable cancel intent (`cancel_requested_at`, plus the
operator's key and actor) and returns the job still `starting` with
`cancellable` false and `UpdateJob.cancel_requested_at` set, which the UI shows
as "cancel requested". From then on the runner takes no new start lease and sends
no Start, and the lease re-check under the row lock also honours the intent, so a
failing Start cannot keep renewing the lease and block the cancel. After the
existing lease expires the runner reads a fresh status:

- Frigate has no record: the cancel completes in Fleet exactly as above.
- Frigate has the rollout: Fleet sends Frigate a Cancel with key
  `fleet-cancel-pending-<job id>-<action counter>` and applies the answer. If
  Frigate refuses because the rollout is past cancellation, the job follows the
  rollout Fleet read, the intent is cleared, and a `frigate_update_cancel_refused`
  activity entry records the refusal. A Start that was in flight and is accepted
  after the cancel was recorded is cancelled the same way.
- The status is unreadable: the job stays pending and nothing is sent.

Repeating the operator's cancel key while the cancel is pending, or sending a
different key, returns the pending job without another Frigate call. Every
recorded key answers from its record once the job is final. If Frigate does know
the job when the operator cancels, the cancel is refused and the answer Fleet
just read is applied.

Retry and Cancel keys are recorded per job (`frigate_update_action`, at most 64
per job). Any key Fleet has seen replays its recorded outcome, including a delayed
duplicate of an earlier action, and does not reach Frigate again with a new key.

Cleanup of staged copies is a separate sweep with its own runtime job
(`frigate-package-cleanup`, next to `frigate-update-runner`), so a controller
that never answers an abort cannot hold up polling or starting. Each pass is
bounded to two minutes. A job whose abort failed is stamped `cleanup_attempted_at`
and moves behind untried jobs, so one dead controller cannot starve the rest. An
abort is by manifest fingerprint; a refusal from Frigate counts as done, and Fleet
gives up 24 hours after the job ended. `CANCELLED` and `VERIFIED` rollouts need no
Fleet cleanup because Frigate reclaims its staged copy itself when the rollout
exits (as it does on success); a failure after the point of no return is the
exception, where Frigate keeps its copy for the retry.

While Frigate is stopped for the install, the controllers' maintenance service
answers status, retry and cancel on the same port, and the UI shows the
update as served by maintenance. If no controller answers, the job stays in its
state and records when contact was lost (`unreachable_since`). Any answer clears
it. Once no controller has answered for 24 hours, including when the container
is no longer paired, Fleet abandons the job: it becomes `abandoned` with
`FLEET_CONTROLLERS_LOST`, not retryable or cancellable, with cleanup pending.
The controllers may still be partway through installing, so the job's
`next_step` asks the operator to check them on site.

## Running against the fake HA pair

`server/fake-ct1` can simulate both controllers of one CT1 cluster and the full
`ServerUpdateService`. See its README, section "HA pair and Frigate server
updates", for every fault switch.

```bash
cd server/fake-ct1
openssl ecparam -genkey -name prime256v1 -noout -out release.key
openssl ec -in release.key -pubout -out release.pem
CT1_HA_PAIR=1 CT1_UPDATE_TRUSTED_KEY_FILES=$PWD/release.pem go run .
```

Then run Fleet with the same public key in its key directory:

```bash
mkdir -p /tmp/frigate-release-keys && cp server/fake-ct1/release.pem /tmp/frigate-release-keys/
export FRIGATE_UPDATE_RELEASE_KEYS_DIR=/tmp/frigate-release-keys
export FRIGATE_UPDATE_PACKAGE_DIR=/tmp/frigate-packages
```

Pair the fake container as a CT1 device, grant yourself
`controller:software_update`, build a package whose control file matches the
statement (the fake does not unpack it), sign the statement with `release.key`,
and upload it from the container page or with the REST calls above. The fake
steps through the rollout phases every `CT1_UPDATE_STEP` (default 2s) and stops
answering ordinary services while it is "installing", which exercises the
maintenance-served status path.

The domain tests (`server/internal/domain/frigateupdate`) cover the runner
against an in-memory controller pair. Three tests use the fake pair.
`TestFleetNodeReleaseChannelFirmware` in `server/e2e` runs a full update
through the public API, the Fleet runner, and a real Fleet Node. Start it with
`just fleetnode-channel-e2e`. See the [E2E testing guide](../../server/e2e/README.md).
The CT1 plugin tests
(`plugin/proto/internal/ct1/frigate_update_simulator_test.go`) run the update
calls against it. `TestCT1SimulatorControllerSoftwareUpdateThroughNode` in
`server/cmd/fleetnode` runs a full update through the Fleet Node path. It uses
Fleet's handle for a container behind a node, the node's command handler, the
Proto plugin process, and the fake pair. That test replaces only the control
stream between Fleet and the node.

## Operating notes

- Alerts can fire while a controller is stopped for the install. Acknowledge or
  silence them for the maintenance window; Fleet does not manage alert windows.
- Failed rollouts that stopped the controllers are never rolled back
  automatically. Follow the `next_step` text on the job.
- Package deletion is refused while a job that uses the package is still active.
- Removing a container from the fleet is refused while it has a blocking job:
  `staging`, `staged`, `starting`, `running`, or `failed` and retryable. The
  rollout keeps running on the controllers after Start or Retry returns, and only
  its job can report or recover it, so the controllers must not be re-paired as
  another device first. Let the update finish, retry it, or cancel it. A job
  whose controllers never answer again is abandoned after 24 hours, which
  releases the container. Cleanup of a removed container's staged copies ends
  with the removal; Frigate expires partial copies itself.
- Controller update calls are not treated as pool changes. An unfinished pool
  removal on the container does not refuse staging, Start, Retry, Cancel or
  abort, because a removal that began while the controllers were stopped cannot
  finish until those calls bring them back.
  Finished jobs keep their package record, so history stays readable after the
  package file is deleted.
