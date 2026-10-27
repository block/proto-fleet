# RFC 0003: Fleet Node updates

- **Status**: draft
- **Author(s)**: Rongxin (@rl-block)
- **Created**: 2026-10-21
- **Last updated**: 2026-10-23

## Summary

Let an operator update Fleet Nodes from Fleet. Each Node gets updates from the Fleet server it is paired with by default, and Fleet can change a Node's update source when an operator needs to move it to a mirror.

Fleet becomes the distribution point for signed Fleet Node releases. It retrieves public releases automatically when it can, accepts operator uploads for private release builds, verifies each bundle on ingest, stores it durably, and serves it to Nodes over the existing gateway connection. Nodes never need GitHub access.

Fleet sends a Node an operation ID, an exact target version, a revocation list, references to release artifacts, and, when requested, an update-source setting. It does not send scripts, shell commands, repository changes, or trust roots. The unprivileged daemon downloads the bundle, and a small root-owned executor verifies the release signature before running the existing transactional installer.

Fleet records every stored release, update batch, per-Node operation, transition, and final outcome. This gives operators progress, recovery information, and an audit trail.

This RFC replaces the statement in [RFC 0001](0001-agent-server-split.md#fleet-node-updates) that self-update over the gateway is out of scope. It also delivers RFC 0001's planned signed Fleet Node release artifacts.

## Goals

1. Give operators a repeatable way to update selected Fleet Nodes to an exact release.
2. Report progress, failures, and the version running after each update.
3. Document normal update, failure recovery, and key rotation workflows.
4. Keep a durable record of each update for audit and debugging.
5. Support deployments where Nodes cannot reach GitHub or cannot access private release builds.
6. Preserve the current Fleet server and Node trust boundary: Fleet coordinates updates, but Nodes verify the code they run.

## Non-goals

- Zero-downtime Node replacement. RFC 0001's warm-standby design is separate.
- Plugin updates.
- Windows or Docker development Nodes.
- Downgrades, nightly builds, or switching a Node between publishing repositories.
- Updates that start without an operator request.
- A final answer for every rollout policy. The open questions list the decisions that still need team review.

## Current system and constraints

Fleet Nodes are updated today by signing in to the host and running `install-fleet-node.sh` for an exact release. The installer downloads from GitHub by default, or installs files staged by an operator with `--release-dir DIR VERSION`. Staged installs keep the same checksum, manifest, version, repository, and rollback checks as downloaded installs.

Fleet already has several pieces this proposal can reuse:

- Nodes keep an authenticated outbound connection to their Fleet server over `ControlStream`.
- Fleet already serves command artifacts to Nodes, such as miner firmware.
- Node binaries embed their version and publishing repository, expose them through commands, and report the version on heartbeats.
- Fleet already shows Node versions and flags Nodes whose command protocol is behind the server.
- Fleet already downloads release metadata for its own update checks.
- The daemon already loads root-owned config from `/etc/fleetnode/config.yaml`.

The design also has to fit current deployment constraints:

- Fleet never opens connections to Nodes. Nodes reach Fleet directly or through the server's HTTPS endpoint, for example behind a load balancer.
- Some Fleet servers can reach GitHub over HTTPS. Others rely on upload.
- Front ends may cap request sizes, time out idle requests, or rate-limit bootstrap RPCs by source IP. Several Nodes at a site may share one source IP.
- HA deployments may have local file storage on each Fleet host, without file replication between hosts ([RFC 0002](0002-active-passive-fleet-ha.md)).
- Node clocks must be correct enough for existing authentication and for release certificate checks.
- Public capacity guidance plans for up to 1,000 Nodes per server, and up to 2,000 for sizing.

## Proposal

An update has five steps:

1. An operator selects Nodes and a target version in Fleet.
2. Fleet ensures the release bundle exists in its release store and has been verified.
3. Fleet records a batch and one operation per Node before dispatching anything.
4. Each Node downloads its bundle from its configured source and asks the local root executor to install it.
5. Fleet records acknowledgement, phase changes, reconnect, final version, and any failure.

The host installer remains the manual path. Existing Nodes need one manual update to the first signed release that contains the executor. After that, routine updates can be driven from Fleet.

### Decision summary

| Area | Direction |
| ---- | --------- |
| Trigger | Fleet API and Nodes page, gated by `fleetnode:manage`. |
| What Fleet sends | Operation ID, target version, revocation list, artifact references, and optional update-source changes. No scripts, repository changes, or trust roots. |
| Default source | The paired Fleet server, derived from enrollment state. Re-pairing changes the source. |
| Source management | Operators with `fleetnode:manage` can ask Fleet to set a Node's source to its paired server or an HTTPS mirror. Local-directory sources remain host-controlled unless a root-owned allowlist explicitly permits them. |
| Release store | Verified bundles in Fleet, filled by automatic retrieval or chunked upload. |
| HA rule | A batch starts only when every healthy Fleet host can serve the release, or the release is in shared storage. |
| Trust | Nodes verify release signatures locally. Fleet verification catches mistakes early but is not trusted by the Node. |
| Privilege split | The daemon downloads. A root, socket-activated executor verifies and installs without network access. |
| Version policy | Newer semantic versions only, from the same publishing repository. |
| Support signal | Explicit Node update capability in `ControlHello`, not a new command protocol version. |
| Records | Durable release, batch, operation, event, activity, metric, and local host records. |
| Pacing | Canary first, then bounded waves. Default wave size is 10 until reviewed. |

### Update source

By default, a Node's update source is its paired Fleet server. The daemon derives this from the `server_url` stored at enrollment. There is no hardcoded download location for Nodes. Running `fleetnode-enroll --force` against a different server changes the default source because it changes the enrollment server.

Operators can also change a Node's source from Fleet. The source-change API is gated by `fleetnode:manage`, records the actor, reason, previous source, and new source, and sends the change over the existing control stream. A Node accepts these remotely managed source values:

- `fleet`: return to the paired Fleet server.
- `https://<mirror>/<path>`: a static mirror using the release layout.

Local directory sources remain host-controlled by default because they can expose host filesystem paths and are harder for Fleet to validate. A deployment that wants Fleet to select local directories can add a root-owned allowlist on the host; the Node rejects remote local paths outside that allowlist. The daemon reports the effective source type and whether it is host-managed or Fleet-managed.

This keeps routing and trust separate. A source controls where files are available. It does not decide what code the Node may run. Every source change is reversible by re-pairing, returning to `fleet`, or changing the host allowlist.

### Fleet compromise and Node compromise

The current architecture makes Fleet compromise high impact, but not identical to Node host compromise.

Fleet already has operational authority over Nodes and miners. It can issue server-to-node control commands, trigger discovery and pairing, dispatch miner commands such as reboot, start or stop mining, curtailment, pool changes, password changes, firmware workflows, and artifact downloads, and persist state that drives future commands. In practice, a compromised Fleet server or database could disrupt mining, redirect miner behavior through existing command paths, hide or alter records, and make update operations unavailable.

The boundary that remains is code execution and host ownership. Fleet does not get shell access to the Node host, cannot read files directly from the host, cannot decrypt data that only the Node can decrypt, and cannot make the update executor install unsigned or wrong-repository Node code. RFC 0001's accepted target architecture also treats command identity, sequencing, and audit as part of the server-to-node command boundary. This RFC should therefore treat Fleet compromise as a severe operational compromise, but not as equivalent to arbitrary Node host compromise.

That distinction supports a practical relaxation: Fleet may change a Node's update source for operational convenience, because a malicious source still cannot make the executor install code the release pipeline did not sign. The added risk is availability and steering. A compromised Fleet can point Nodes at an unavailable mirror, a slow mirror, or a mirror it controls, and can suppress or alter the audit trail. The mitigation is to gate source changes on `fleetnode:manage`, record every change, report the effective source on heartbeats, preflight the source before rollout, and keep repository, signature, revocation, and root-trust checks local to the Node.

### Release store

The store holds the archive, checksum, and signature for each version and architecture. Bundles enter the store in two ways:

- Automatic retrieval from a configurable release location. The default is the GitHub releases for Fleet's publishing repository.
- Manual upload through the UI or API, one architecture at a time. Uploads use the existing chunked upload pattern so each request can stay below common front-end limits such as 64 MiB.

Automation can upload with a Fleet API key from inside the deployment. A release pipeline can use its own credentials to fetch a private release and then upload it to Fleet. Fleet servers and Nodes do not need GitHub credentials.

Fleet verifies signature, version, repository, architecture, digest, and size before accepting a bundle. It records source, supplier, digest, signing key, sizes, and removal. Upload, retrieval settings, and removal are gated by `fleetnode:manage`; Fleet records the operator or API key behind each change. Removing a release is an operator action and does not erase update history.

### Store backends

The database always stores release metadata. Bundle files use one of these backends:

- Replicated host files by default. Each Fleet host keeps a verified copy and runs a reconciler. Missing or corrupt files are fetched from a peer host, or from the retrieval location when allowed.
- Shared object storage for deployments that already have it. Hosts keep a local verified cache.

A batch can start only when every healthy host has a verified copy, or when the selected backend is shared. This prevents a failover from stranding a running batch on a host that cannot serve the release.

Database-backed bundle storage is deferred. It remains a fallback if replicated host files and shared object storage prove insufficient, but it should not be part of the first automated-update release because it adds migrations, backup growth, and replication traffic of about 45 MiB per release and architecture.

### Signing and trust

Each Fleet Node release gains a signature file. The signed statement binds the archive digest, version, publishing repository, and architecture. Binding these fields prevents a valid archive from being replayed as another version, another architecture, or another repository's release.

The proposed hierarchy is:

- Offline root keys, held by two custodians, issue repository-scoped signing certificates and revocation lists.
- KMS-held signing keys sign release statements from protected release workflows.
- Signing certificates last about 13 months and are scoped to the repositories they may sign for.
- Each Node binary embeds one or two root public keys.
- A root-signed revocation list names revoked signing keys and carries a monotonic version plus a valid-until timestamp. Fleet sends the current list with every update command. Nodes reject expired lists, lists older than their stored high-water mark, and releases that require a newer revocation list than Fleet supplied.

The executor verifies every release before installing it. It checks root trust, certificate validity, repository scope, revocation, signature, digest, version, repository, and architecture. Fleet runs the same verification at ingest, but the Node never relies on Fleet having done it.

Manual installs keep HTTPS, checksum, version, and repository checks. When the installed binary supports signed verification, the installer verifies the new release with the installed binary before replacing it.

The first release that installs the root executor uses a stricter bootstrap path than older manual updates. The installer must verify that release's signature against a root key pinned in the installer or supplied independently by the operator before it installs the executor. HTTPS and checksum verification alone are not enough for the transition that creates the new local trust boundary.

Root compromise is the worst case. Fleet and the daemon cannot replace a Node's trust root. Recovery requires a host-local installer option, `--trust-root FILE --trust-root-sha256 DIGEST`, where the operator supplies a new root key and independently published fingerprint. The executor never accepts a root key from Fleet.

### Node executor

The first signed release adds a socket-activated root service, `fleet-node-update`, and its socket. The normal daemon remains unprivileged.

The daemon downloads release files into its spool directory, then sends the executor a small request plus open file descriptors over the local socket. The socket is reachable only by root and the Fleet Node daemon service account. The executor also checks the caller identity and rejects requests that are not tied to an operation Fleet dispatched to the daemon. The request contains the operation ID, target version, and revocation list. It does not contain paths, URLs, repositories, commands, or scripts.

The executor accepts only one operation at a time. It checks version policy and repository consistency, copies the files into a private root-owned staging directory, verifies the release, checks free space, and runs the archive's own installer with `--release-dir`. The installer still performs the stop, replace, start, readiness wait, and rollback behavior.

The executor writes status and local history under `/var/lib/fleetnode-update`. The daemon can read status but cannot write it. Local history keeps the most recent 200 finished operations and gives host-level debugging information when Fleet is unreachable.

### Gateway, API, and UI

`ControlHello` carries the live Node identity and update capability: version, publishing repository, architecture, executor availability, source type, whether the source is host-managed or Fleet-managed, trusted root key IDs, revocation-list version, and last operation. New fields are optional on the wire. Older Nodes keep their command stream and show as needing a manual update.

Fleet dispatches an update only when the Node has an active stream, reports a supported architecture, has the executor, trusts the target release's root, reports enough disk space, and is running the same publishing repository as Fleet. When the Node's source is Fleet, the store must also hold the right architecture and satisfy the HA rule.

When the Node's source is a mirror or local directory, Fleet cannot inspect the source directly. Before adding that Node to a canary or wave, Fleet asks the daemon to preflight the target bundle from its configured source. A source that cannot serve the target is recorded as `rejected` before installation starts.

`UpdateFleetNodes` takes Node IDs and an exact version, then returns the batch ID and operation IDs. `SetFleetNodeUpdateSource` sets a Node back to its paired Fleet server or to an HTTPS mirror, and records the reason and actor. `ListFleetNodes` adds update availability, effective source, source management mode, free disk space, and latest operation state. The UI starts with the Nodes page and later adds release-store, source-management, and history views.

The update flow includes release preparation. Target selection shows store availability by architecture, routes missing bundles to retrieval or chunked upload, and returns the operator to batch creation only after the required bundles verify successfully.

### Pacing and outcomes

The batch runner runs on the active Fleet instance. It updates one canary Node first, then starts waves. The proposed default is 10 Nodes per wave, with a server-wide maximum still to decide. A default wave of 10 bounds one batch's active download load to roughly 450 MB for current releases and avoids a strictly sequential rollout that could take a day or more on a large server. Concurrent batches need a shared server-wide cap so Fleet's total active download load does not grow with each batch.

A batch stops starting new operations after the first failed or unconfirmed Node. Updates already in progress finish and are recorded. Disconnected Nodes are skipped when their turn arrives. Because batches and operations are stored before dispatch, a new active Fleet instance can resume after a restart or HA failover without starting duplicate operations.

Outcomes are:

- `succeeded`: the Node reconnects and reports the target version.
- `failed`: the executor or installer reports failure other than a completed rollback.
- `rolled_back`: the target failed to start and the previous version is running.
- `unconfirmed`: the Node does not reconnect within the timeout after activation.
- `rejected`: Fleet or the executor refuses the operation before installation starts.
- `skipped`: the Node is disconnected when its turn arrives.

### Records and observability

Fleet records releases, host copies, batches, operations, and operation events. The schema should preserve records when a Node is revoked or deleted and when a release is removed. Phase 1 keeps records indefinitely; pruning policy is an open question.

Fleet also writes activity-log events for releases added and removed, and for update batches requested and completed. Metrics cover outcomes, phase durations, Nodes behind Fleet's version, Node disk space, and release-store replication state. Structured logs include operation IDs.

Nodes report free space for the filesystems that hold the program, daemon state, and executor work directory. Fleet uses that for display, alerting, and admission checks. The executor repeats the check before staging so a Node that filled up after its last heartbeat fails before the service stops. Current releases need about 450 MB for download, verified copy, unpacked candidate, and rollback space.

## Pros and cons

### Pros

- Operators can update many Nodes without host logins.
- Nodes do not need GitHub access or GitHub credentials.
- Private release builds can be uploaded once and distributed through Fleet.
- Operators can move Nodes to a mirror from Fleet when network topology or repository access changes.
- Fleet records who requested an update, what ran, and how it ended.
- The Node, not Fleet, decides whether a release is trusted.
- Existing staged installs, heartbeats, artifact downloads, and rollback behavior are reused.
- HA deployments can resume batches after failover because release availability is checked before dispatch.

### Costs and risks

- Every Node gets a root component that must be secured and maintained.
- Root custody, KMS setup, signing ceremonies, re-signing, and rotation drills become operational work.
- Fleet serves release downloads, so waves must bound server load and shared-site bandwidth.
- Remote source changes increase the impact of a compromised Fleet server: it can steer Nodes to an unavailable or attacker-controlled mirror, even though signatures still protect code integrity.
- Replicated host files need a peer transfer path and reconciler. A future database backend would avoid that path but increase database size and backup cost.
- Certificate checks depend on correct Node clocks.
- Existing Nodes need one manual update before Fleet can update them.
- Each update briefly interrupts Node telemetry and commands for that Node's miners.
- The initial records are kept indefinitely unless a retention policy is added.

## Failure behavior

Most failures happen before the old daemon stops. Fleet rejects a Node if the release is missing, the Node is disconnected, the reported disk space is too low, the source is unsupported, or the Node lacks update capability. The executor rejects or fails before activation if the request is malformed, another operation is active, the repository pin does not match, free space is short, the download is corrupt, or signature verification fails.

Source changes fail before any update starts when the URL is invalid, the local path is not allowlisted, the Node cannot persist the change, or the source preflight cannot fetch and verify the target bundle. The Node keeps its previous source on failure, and Fleet records the rejected source-change attempt.

If the new version does not start, the existing installer rollback path restores the previous version and Fleet records the operation as rolled back. If the Node starts but never reconnects, Fleet marks the operation unconfirmed after the timeout and points the operator to host logs and local history. If Fleet restarts or fails over, the new active instance resumes stored batches and reconciles missed outcomes when Nodes reconnect.

If a release installs, starts, and reconnects but later proves bad, recovery is a forward update to a newer signed repair release. The UI and runbook should guide operators to identify affected Nodes, prepare the repair release in the store, and apply that newer target. Downgrade remains out of scope.

## Alternatives considered

- **Nodes download directly from GitHub.** This leaves update management scattered across hosts and does not work for private release builds without host credentials.
- **Configure an update URL on every Node.** This duplicates enrollment state and drifts when a Node is re-paired.
- **Let Fleet change any source without Node-side limits.** Operationally convenient, but too broad: a compromised Fleet could push arbitrary local paths or unsupported URL schemes to every Node. This RFC allows remote changes only to the paired server or an HTTPS mirror by default, with local paths behind a host allowlist.
- **Use SSH scripts around the installer.** Operators still need shell access to every host, and Fleet still lacks central progress and records.
- **Let the daemon replace itself.** The unprivileged service would need write access to `/opt/fleetnode` and service-control power.
- **Distribute unsigned bundles.** Fleet would choose code that runs as root on every Node.
- **Use one long-lived signing key.** Rotation and compromise recovery would both require coordinated Node updates.
- **Use public transparency-log signing.** Private repository identity and artifact digests would become public.
- **Use GitHub artifact attestations.** Nodes would need GitHub API access or a changing trust root.
- **Store bundles only in host-local files.** HA failover could move a batch to a host that lacks the uploaded release.
- **Require shared object storage everywhere.** Some deployments do not have it.
- **Run strictly sequential batches.** Large fleets would take too long to update.
- **Reuse the firmware rollout engine.** It carries miner-specific release-channel and model-group concepts that Fleet Nodes do not need.

## Open questions

1. Which KMS should hold signing keys, which hardware should hold root keys, and who are the root custodians?
2. Should upstream and private fork releases share one root with repository-scoped certificates, or should each repository have its own root?
3. Is a 13-month signing-certificate lifetime with yearly rotation right?
4. Is upload enough for private releases, or should Fleet support credentialed retrieval with a read-only token?
5. Should Fleet prefetch the release matching its own version after it upgrades?
6. How many releases should the store keep by default?
7. Should repository pin repair be an installer action or a documented manual fix?
8. Is 10 minutes the right unconfirmed timeout, and should a Node that never reconnects roll back automatically? The proposed default is no automatic rollback beyond the installer's.
9. Should Fleet allow a target newer than Fleet's own version? The proposed default is yes when the operator selects it and the store has it.
10. How long should update records and event timelines be retained?
11. Should `fleetcli` expose updates, uploads, and history in phase 1?
12. Which storage backend should each deployment type default to, and should firmware and command artifacts later use the same replication path?
13. What default wave size and server-wide maximum should batches use, and should waves also be limited by site or shared source IP? Options include allowing only one active batch at a time, sharing one server-wide active-download cap across all batches, and adding a per-site cap for Nodes behind the same source IP.
14. What operator actions should Fleet expose after a batch stops on a failed or unconfirmed Node? The proposed default is explicit operator control: retry the failed Node with a new operation ID, skip it and continue the remaining Nodes, or cancel the batch. Fleet should not auto-continue after a failure.

## Rollout

- **Phase 0, done:** safer manual updates and visible Node identity. Operators can stage releases without GitHub access, and Fleet shows Node versions.
- **Phase 1a, signing:** root ceremony, signing keys, signed release assets, `fleetnode release verify`, revocation lists, root recovery in the installer, a rotation runbook, and a CI rotation drill.
- **Phase 1b, executor:** root executor, socket and service units, packaging changes, local-directory testing, local history, and native qualification on supported architectures.
- **Phase 1c, distribution:** release store, retrieval, chunked upload, replicated host files, optional shared object storage, reconciler, Node capability fields, `update_node`, update-source management, release artifact purpose, mirror source, progress heartbeats, and disk-space reporting.
- **Phase 1d, records and UI:** migrations, API, batch runner, activity events, metrics, release-store UI, Nodes page actions, history views, documentation, and pin repair.
- **Phase 2, optional:** maintenance windows, multi-site scheduling, and prompts to update Nodes after Fleet itself upgrades.

## Validation

The implementation should prove these behaviors before phase 1 ships:

- Signing rejects tampered archives, wrong repository scope, wrong architecture, version mismatch, expired certificates, not-yet-valid certificates, revoked keys, unknown roots, and stale revocation lists.
- Rotation drills cover a second signing key, a second root, and recovery from a compromised root with an independently supplied fingerprint.
- Update source defaults to the enrolled server, follows re-pairing, accepts gated Fleet-managed changes to the paired server or HTTPS mirrors, rejects invalid or unauthorised local paths, reports the effective source and management mode, and preflights non-Fleet sources before rollout.
- Release-store tests cover retrieval, chunked upload, ingest rejection, retention, replicated host files, shared object storage, reconciliation, peer fetch authorization, HA availability, and failover after upload.
- Disk-space checks cover heartbeat reporting, Fleet admission, and executor failure before staging.
- Front-end tests cover chunked upload through request-size limits, release download through an idle-timeout path, and waves from Nodes sharing a source IP.
- Executor and installer tests cover request parsing, file descriptor handling, version policy, repository checks, status, local history, units, rollback, and root recovery.
- Compatibility tests prove old Nodes keep connecting to a new Fleet server and new Nodes can connect to an older server.
- Native qualification updates a physical arm64 Node and an amd64 VM from Fleet, from a mirror, and from a local directory.
- Scale tests update hundreds of simulated Nodes and measure duration, Fleet throughput, and database load.
