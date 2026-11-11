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
   statement and signature (`POST /api/v1/frigate-packages/upload/chunked`).
2. `StartUpdate` records a job for one container. The `frigate-update-runner`,
   which runs only on the active Fleet replica, then:
   - stages the package on each controller with that controller's own
     Begin/Append/Commit calls (1 MiB chunks, resuming from the controller's
     acknowledged offset);
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

- **mdk_auth sites are unsupported in v1.** Fleet talks to Frigate over the CT1
  plugin's native gRPC connection (port 9095) and has no login or bearer-token
  step. A controller with `server.mdk_auth.enabled` refuses the calls.
- **FleetNode-owned controllers are unsupported in v1.** `StartUpdate` and
  `GetSupport` return `FailedPrecondition` for a container whose controller is
  reached through a Fleet Node.
- **The controllers must allow remote triggers** (`update.allow_remote_trigger`
  on Frigate) and report self-coordination support, the update helper, a
  Debian-package installation, and a healthy etcd quorum. `GetSupport` reports
  the first failing condition with a stable code, for example
  `UPDATE_REMOTE_TRIGGER_DISABLED`. Fleet's verdict is advisory; Frigate checks
  again when the update starts.
- **Controllers re-verify the signature.** Fleet checks the release statement
  against its own key directory only to reject a bad upload early. Each
  controller verifies the statement against the keys it trusts and refuses the
  package if they differ. Fleet's check is not a security boundary.
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

## Configuration

All settings are Fleet server flags with environment variables:

| Environment variable | Default | Meaning |
| --- | --- | --- |
| `FRIGATE_UPDATE_PACKAGE_DIR` | `firmware/frigate-packages` | Where uploaded packages are stored. The default is inside the persisted firmware volume (`artifacts/firmware` is mounted at `/app/firmware` by the standard compose files), so the packages survive container recreation. Any other directory must be on persistent storage and writable by the server user. |
| `FRIGATE_UPDATE_MAX_PACKAGE_SIZE` | `536870912` | Largest package Fleet accepts, in bytes. |
| `FRIGATE_UPDATE_RELEASE_KEYS_DIR` | empty | Directory of trusted release public keys. Empty disables package upload; the rest of the feature still works for packages already stored. |
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

With the standard compose deployment, mount the directory read-only into the
`fleet-api` container and set the variable there.

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
 "release_statement_b64": "...", "release_signature_b64": "..."}
-> {"upload_id": "...", "version": "1.4.0", "architecture": "arm64"}

PUT  /api/v1/frigate-packages/upload/chunked/{upload_id}      Content-Range: bytes 0-33554431/123456789
POST /api/v1/frigate-packages/upload/chunked/{upload_id}/complete
-> {"package": {...}, "reused": false}
```

The signature is checked before any bytes are accepted. At complete, Fleet
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

There is no automated end-to-end test against the fake pair: `fake-ct1` is a
separate Go module with a `main` package, and the update path also needs the CT1
plugin and a paired device. The domain tests (`server/internal/domain/frigateupdate`)
cover the runner against an in-memory controller pair instead.

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
