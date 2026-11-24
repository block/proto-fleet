# RFC 0003: Fleet Node updates

- **Status**: draft
- **Author(s)**: Rongxin (@rl-block)
- **Created**: 2026-10-21
- **Last updated**: 2026-11-05

## Summary

Let an operator update Fleet Nodes from Fleet. Each Node gets updates from the Fleet server it is paired with, unless the host's own config points it at a mirror or local directory.

Fleet becomes the distribution point for signed Fleet Node releases. Operators upload release bundles to Fleet, Fleet verifies each bundle on ingest, stores it locally, and serves it to Nodes over the existing gateway connection. Nodes never need GitHub access.

Fleet sends a Node an operation ID, an exact target version, and references to release artifacts. It does not send update sources, scripts, shell commands, repository changes, or signing keys. The unprivileged daemon downloads the bundle, and a small root-owned executor verifies the release signature before running the existing transactional installer.

Fleet records every stored release, update run, per-Node operation, transition, and final outcome. This gives operators progress, recovery information, and an audit trail.

This RFC replaces the statement in [RFC 0001](0001-agent-server-split.md#fleet-node-updates) that self-update over the gateway is out of scope. It also delivers RFC 0001's planned signed Fleet Node release artifacts.

## Goals

1. Give operators a repeatable way to update selected Fleet Nodes to an exact release.
2. Report progress, failures, and the version running after each update.
3. Document normal update, failure recovery, and release-signing key replacement workflows.
4. Keep a durable record of each update for audit and debugging.
5. Support deployments where Nodes cannot reach GitHub directly.
6. Preserve the current Fleet server and Node trust boundary: Fleet coordinates updates, but Nodes verify the code they run.

## Non-goals

- Zero-downtime Node replacement. RFC 0001's warm-standby design is separate, though this RFC never updates both halves of a Fleet Node HA pair at the same time and updates the standby first, so the existing HA failover can carry the pair's devices through the active Node's update.
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
- Fleet servers do not require GitHub access. Release bundles are uploaded by an operator or automation that already has access to the release location. When the active host can reach the publishing repository's public GitHub releases, Fleet also re-downloads a bundle it has lost.
- Front ends may cap request sizes, time out idle requests, or rate-limit bootstrap RPCs by source IP. Several Nodes at a site may share one source IP.
- Fleet server HA deployments ([RFC 0002](0002-active-passive-fleet-ha.md)) store files on each Fleet host without replicating them between hosts, and only the active host accepts Node connections.
- Node clocks must be correct enough for existing authentication.

## Proposal

An update has five steps:

1. An operator selects Nodes and a target version in Fleet.
2. Fleet ensures the release bundle is in its local release store and has been verified.
3. Fleet records an update run and one operation per Node before dispatching anything.
4. Each Node downloads its bundle from its configured source and asks the local root executor to install it.
5. Fleet records acknowledgement, phase changes, reconnect, final version, and any failure.

The host installer remains the manual path. Existing Nodes need one manual update to the first signed release that reports update capability: the executor plus the Node-side update handling and capability fields. After that, routine updates can be driven from Fleet.

Fleet surfaces Nodes that are not yet update-capable as needing a manual bootstrap update. The UI links to the signed bootstrap runbook and the installer inputs, and phase 1 validates on staging that an existing deployment can be converted to update-capable Nodes before routine updates are considered ready.

### Decision summary

| Area | Direction |
| ---- | --------- |
| Trigger | Fleet API and Nodes page, gated by `fleetnode:manage`. |
| What Fleet sends | Operation ID, target version, and artifact references. No update sources, scripts, repository changes, or signing keys. |
| Default source | The paired Fleet server, derived from enrollment state. Re-pairing changes the source. |
| Source management | Root-owned host config can override the source with an HTTPS mirror or a local directory. Fleet-managed source changes are deferred to phase 2. |
| Release store | Verified bundles uploaded to Fleet. Phase 1 uses local host storage. A Fleet server failover fails the in-flight update, and Fleet re-downloads a lost bundle from the public release before the next attempt instead of replicating it between hosts. |
| HA rule | Fleet never updates both halves of a Fleet Node HA pair at the same time, and updates the standby first. If one half fails, rolls back, or is unconfirmed, Fleet rejects its sibling in that run and later runs until the failed half updates successfully. |
| Trust | Nodes verify release signatures locally. Fleet verification catches mistakes early but is not trusted by the Node. |
| Privilege split | The daemon downloads. A root, socket-activated executor verifies and installs without network access. |
| Version policy | Newer semantic versions only, from the same publishing repository. |
| Support signal | Explicit Node update capability in `ControlHello`, not a new command protocol version. |
| Records | Durable release, update-run, operation, event, activity, metric, and local host records. |
| Pacing | Phase 1 runs updates sequentially. Waves and larger scheduling policies are deferred until Fleet Node counts justify them. |

### Update source

By default, a Node's update source is its paired Fleet server. The daemon derives this from the `server_url` stored at enrollment. There is no hardcoded download location for Nodes. Running `fleetnode-enroll --force` against a different server changes the default source because it changes the enrollment server.

A host can override the source in its root-owned config with either of these values:

- `https://<mirror>/<path>`: a static mirror using the release layout.
- A local directory, for sites that stage releases on the host.

Phase 1 has no Fleet-managed source changes. Fleet never sends a source to a Node, and the daemon reports the effective source type and whether host config overrides it. Letting operators change sources from Fleet is deferred to phase 2.

This keeps routing and trust separate. A source controls where files are available. It does not decide what code the Node may run. Every override is reversible by changing host config or re-pairing.

### Fleet compromise and Node compromise

The current architecture makes Fleet compromise high impact, but not identical to Node host compromise.

Fleet already has operational authority over Nodes and miners. It can issue server-to-node control commands, trigger discovery and pairing, dispatch miner commands such as reboot, start or stop mining, curtailment, pool changes, password changes, firmware workflows, and artifact downloads, and persist state that drives future commands. In practice, a compromised Fleet server or database could disrupt mining, redirect miner behavior through existing command paths, hide or alter records, and make update operations unavailable.

The boundary that remains is code execution and host ownership. Fleet does not get shell access to the Node host, cannot read files directly from the host, cannot decrypt data that only the Node can decrypt, and cannot make the update executor install unsigned or wrong-repository Node code. RFC 0001's accepted target architecture also treats command identity, sequencing, and audit as part of the server-to-node command boundary. This RFC should therefore treat Fleet compromise as a severe operational compromise, but not as equivalent to arbitrary Node host compromise.

Phase 1 does not let Fleet change a Node's update source, so a compromised Fleet cannot steer Nodes to another download location. It can still make updates unavailable and suppress or alter the audit trail.

### Release store

The store holds the archive, checksum, and signature for each version and architecture. New releases enter the store by upload in phase 1:

- Manual upload through the UI or API, one architecture at a time. Uploads use the existing chunked upload pattern so each request can stay below common front-end limits such as 64 MiB.

Automation can upload with a Fleet API key from inside the deployment. A release pipeline can fetch the release and upload it to Fleet. Fleet servers and Nodes do not need GitHub credentials.

Fleet verifies signature, version, repository, architecture, digest, and size before accepting a bundle. It records source, supplier, digest, signing key, sizes, and removal. Upload and removal are gated by `fleetnode:manage`; Fleet records the operator or API key behind each change. Removing a release is an operator action and does not erase update history.

### Store backends

The database stores release metadata. Bundle files start with local host storage:

- Local host files by default. The active Fleet host stores uploaded bundles in its file storage and serves them while it remains active.
- Shared object storage later, for deployments that want release bundles available to every host without re-uploading or re-downloading after failover.

If a Fleet server failover moves an update run to a host that lacks the bundle, Fleet marks the in-flight operation failed and the run stops, as it does for any other failure. Fleet does not copy bundles between hosts. Instead, when a run needs a release whose verified metadata is recorded but whose file is missing on the active host, Fleet re-downloads the archive, checksum, and signature anonymously from the publishing repository's public GitHub release, verifies them against the recorded digest and signature as it would an upload, and stores them locally before dispatching. The operator then retries the failed Node. Fleet re-downloads only on demand; it does not prefetch releases after it upgrades. If the active host cannot reach the release, the operator or automation uploads the bundle again. Fleet server failovers during Node updates should be rare, so phase 1 treats this as a recovery case rather than building a peer replication system.

Database-backed bundle storage is deferred. It remains a fallback if local host files and shared object storage prove insufficient, but it should not be part of the first automated-update release because it adds migrations, backup growth, and replication traffic of about 45 MiB per release and architecture.

### Signing and trust

Each Fleet Node release gains a signature file. The signed statement binds the archive digest, version, publishing repository, and architecture. Binding these fields prevents a valid archive from being replayed as another version, another architecture, or another repository's release.

The phase 1 signing model is intentionally simple:

- A single release-signing key signs Fleet Node release statements.
- Phase 1 supports one publishing repository, `block/proto-fleet`, and only its release workflow can use the key. Releases from `btc-mining/proto-fleet-internal`, including its `-internal.N` builds, are not signed. Nodes running them show as needing a manual update, and moving such a Node to `block/proto-fleet` uses the documented manual pin-repair fix.
- The private key lives in the production AWS KMS and is used only by protected release workflows. Only tags that pass the release workflow's on-main gate (final releases and `-rc.N` candidates) are signed; branch test builds are not.
- Each Node binary pins the public key.
- Planned key replacement uses a transition release signed with the outgoing key. Its Node binaries pin the replacement key, and its Fleet build accepts both keys at ingest. Operators upgrade Fleet, roll the transition release out to Nodes, and sign later releases with the replacement key. A later Fleet release drops the outgoing key once no Node reports trusting only it. Emergency replacement uses the manual bootstrap path below.

The executor verifies every release before installing it. It checks the pinned signing key, signature, digest, version, repository, and architecture. Fleet runs the same verification at ingest, but the Node never relies on Fleet having done it. Fleet's ingest check trusts the keys pinned in its own build, which is why the transition Fleet build accepts both keys until every Node has moved to the replacement.

Manual installs keep HTTPS, checksum, version, and repository checks. When the installed binary supports signed verification, the installer verifies the new release with the installed binary before replacing it.

The one-time bootstrap to the first update-capable release trusts the release download, as today's manual installs do. The installer comes from the same release page and runs as root, so no check inside it can be stronger than that page. The installer verifies the release signature against the key pinned in the installer; operators can instead supply the key and its fingerprint with `--trust-release-key`. After bootstrap, the key pinned in the installed binary is the Node's trust root.

Signing-key compromise is the worst case for automated updates. Fleet and the daemon cannot replace a Node's signing key on their own. Recovery requires a host-local installer option, `--trust-release-key FILE --trust-release-key-sha256 DIGEST`, where the operator supplies a new public key and independently published fingerprint. The executor never accepts a signing key from Fleet.

### Node executor

The first signed release adds a socket-activated root service, `fleet-node-update`, and its socket. The normal daemon remains unprivileged.

The daemon downloads release files into its spool directory, then sends the executor a small request plus open file descriptors over the local socket. The socket is reachable only by root and the Fleet Node daemon service account. The executor also checks the caller's peer credentials and records the daemon-supplied operation ID to correlate status and history. It cannot independently confirm that Fleet dispatched the operation, so the signature, repository, architecture, and newer-version checks are what limit a compromised daemon. The request contains the operation ID and target version. It does not contain paths, URLs, repositories, commands, or scripts.

The executor accepts only one operation at a time. It checks version policy and repository consistency, copies the files into a private root-owned staging directory, verifies the release, checks free space, and runs the archive's own installer with `--release-dir`. The installer still performs the stop, replace, start, readiness wait, and rollback behavior.

The executor writes status and local history under `/var/lib/fleetnode-update`. The daemon can read status but cannot write it. Local history keeps the most recent 200 finished operations and gives host-level debugging information when Fleet is unreachable.

### Gateway, API, and UI

`ControlHello` carries the live Node identity and update capability: version, publishing repository, architecture, executor availability, source type, whether host config overrides the source, trusted release-key ID, and last operation. New fields are optional on the wire. Older Nodes keep their command stream and show as needing a manual update.

Fleet records one operation for every selected Node. It marks ineligible Nodes as `rejected` before dispatch, including Nodes that are disconnected, lack the executor, report an unsupported architecture, do not trust the target release's signing key, lack disk space, are not running the same publishing repository as Fleet, or have a locked HA sibling. Rejected operations do not stop the run; Fleet continues with eligible Nodes. When the Node's source is Fleet, the active host's store must also hold the right architecture.

When the Node's source is a mirror or local directory, Fleet cannot inspect the source directly. If the daemon cannot fetch the target bundle from that source, the operation is recorded as `rejected` before installation starts and the run continues.

`UpdateFleetNodes` takes Node IDs and an exact version, then returns the update run ID and operation IDs. `ListFleetNodes` adds update availability, effective source, free disk space, and latest operation state. Phase 1 UI includes the Nodes page update action, release upload and preparation, and update history; implementation can sequence those surfaces, but the operator workflow needs all three.

The update flow includes release preparation. Target selection shows store availability by architecture, routes missing bundles to upload, and returns the operator to run creation only after the required bundles verify successfully.

### Pacing, HA guards, and outcomes

The phase 1 runner stays simple: it updates one Node at a time. Operators can select more than one Node, but Fleet queues them sequentially instead of starting waves. Fleet Node counts are expected to be in the dozens, not hundreds, so a sequential runner is easier to reason about and should still finish in an acceptable window. Phase 1 supports a configurable maximum selected-Node count, defaulting to 50; Fleet rejects larger runs until a later scheduler supports waves.

Fleet also applies an HA sibling guard to Fleet Node HA pairs: RFC 0001's warm-standby pairs, recorded in `fleet_node_ha_pair` as an active and a standby Node that share the same devices. Fleet never updates both halves of a pair at the same time. Phase 1 allows one active update run per organization, and a stopped run still counts as active, so the one-at-a-time runner covers every pair; Fleet rejects a new run until the current one completes or is cancelled. When a run includes both halves, Fleet updates the standby first and the active Node only after the standby succeeds.

Updating the active Node stops its control stream. If the stream stays down longer than the Fleet Node HA failover delay, currently 30 seconds, Fleet's existing HA service moves the pair's devices to the standby and swaps the roles, with no automatic failback. Updating the standby first means that failover lands on a Node already running the target. As with any Fleet Node HA failover, password updates and curtailment configs still queued for those devices fail and must be run again.

If one half of a pair ends `failed`, `rolled_back`, or `unconfirmed`, Fleet locks its sibling. A locked sibling is `rejected` before dispatch in the current run, including after the operator skips the failed Node, and in every later run. The lock clears when the failed Node later completes an update or reconnects running the target version, for example after a manual repair, or when the pair is dissolved.

An update run stops starting new operations after the first failed, rolled-back, or unconfirmed Node. The operator can then retry that Node with a new operation ID, skip it and continue the remaining eligible Nodes, or cancel the run. A run is `running`, `stopped` while it waits for an operator, `cancelled`, or `completed`, and the update-history view holds a stopped run's retry, skip, and cancel actions. Disconnected Nodes are rejected before dispatch or skipped when their turn arrives, depending on when the disconnect is observed. Because runs and operations are stored before dispatch, a new active Fleet instance can resume after a restart. After a Fleet server failover, the in-flight operation fails if the new active host does not have the bundle; Fleet re-downloads it, or the operator uploads it again, and the operator retries that Node.

Outcomes are:

- `succeeded`: the Node reconnects and reports the target version.
- `failed`: the executor refuses the operation, the executor or installer reports a failure other than a completed rollback, or the daemon cannot download the bundle from Fleet after dispatch, for example after a Fleet server failover to a host that lacks it.
- `rolled_back`: the target failed to start and the previous version is running.
- `unconfirmed`: the Node does not reconnect within the unconfirmed timeout after activation. The timeout is a Fleet setting that defaults to 10 minutes, and Fleet does not roll an unconfirmed Node back automatically.
- `rejected`: Fleet refuses the operation before dispatch, or the daemon cannot fetch the target bundle from a mirror or local-directory source.
- `skipped`: the Node is disconnected when its turn arrives, or the run is cancelled before its turn.

### Records and observability

Fleet records releases, host copies, update runs, operations, and operation events, including rejected attempts. The schema should preserve records when a Node is revoked or deleted and when a release is removed. Phase 1 keeps records indefinitely; pruning policy is an open question.

Fleet also writes activity-log events for releases added and removed, and for update runs requested, stopped, and completed. Metrics cover outcomes, phase durations, Nodes behind Fleet's version, Node disk space, and release-store availability. Structured logs include operation IDs.

Nodes report free space for the filesystems that hold the program, daemon state, and executor work directory. Fleet uses that for display, alerting, and admission checks. The executor repeats the check before staging so a Node that filled up after its last heartbeat fails before the service stops. Current releases need about 450 MB for download, verified copy, unpacked candidate, and rollback space.

## Pros and cons

### Pros

- Operators can update many Nodes without host logins.
- Nodes do not need GitHub access or GitHub credentials.
- Release bundles can be uploaded once and distributed through Fleet.
- Hosts can point a Node at a mirror or local directory in host config when network topology requires it.
- Fleet records who requested an update, what ran, and how it ended.
- The Node, not Fleet, decides whether a release is trusted.
- Existing staged installs, heartbeats, artifact downloads, and rollback behavior are reused.
- HA sibling guards avoid updating both halves of a pair at the same time, and lock the second half out if the first fails.

### Costs and risks

- Every Node gets a root component that must be secured and maintained.
- Signing-key custody, KMS setup, signing ceremonies, re-signing, and rotation drills become operational work.
- Fleet serves release downloads, so the sequential runner should still avoid saturating shared-site bandwidth.
- Local release storage means a Fleet server failover fails an in-flight update. Each HA Fleet upgrade also moves the active role to the other host, so the new active host lacks earlier bundles until Fleet re-downloads them. Deployments whose Fleet hosts cannot reach the public release must upload bundles again after a failover or upgrade.
- Existing Node authentication still depends on correct clocks.
- Existing Nodes need one manual update before Fleet can update them.
- Each update briefly interrupts Node telemetry and commands for that Node's miners. Updating the active Node of an HA pair can trigger the existing HA failover, which moves its devices to the standby and fails queued password updates and curtailment configs.
- The initial records are kept indefinitely unless a retention policy is added.

## Failure behavior

Most failures happen before the old daemon stops. Fleet rejects a Node if the release is missing, the Node is disconnected, the reported disk space is too low, the source is unsupported, or the Node lacks update capability. The executor fails the operation before activation if the request is malformed, another operation is active, the repository pin does not match, free space is short, the download is corrupt, or signature verification fails.

If the new version does not start, the existing installer rollback path restores the previous version and Fleet records the operation as rolled back. If the Node starts but never reconnects, Fleet marks the operation unconfirmed after the unconfirmed timeout and points the operator to host logs and local history. If Fleet restarts, the new active instance resumes stored update runs and reconciles missed outcomes when Nodes reconnect.

If a release installs, starts, and reconnects but later proves bad, recovery is a forward update to a newer signed repair release. The UI and runbook should guide operators to identify affected Nodes, prepare the repair release in the store, and apply that newer target. Downgrade remains out of scope.

## Alternatives considered

- **Nodes download directly from GitHub.** This leaves update management scattered across hosts and requires every Node to have GitHub access.
- **Configure an update URL on every Node.** This duplicates enrollment state and drifts when a Node is re-paired.
- **Let Fleet change Node update sources in phase 1.** Convenient for moving Nodes to a mirror, but it adds an API, a UI surface, and qualification work, and lets a compromised Fleet steer Nodes to an unavailable or attacker-controlled mirror. No phase 1 goal needs it, because Nodes already download from Fleet. It is deferred to phase 2.
- **Use SSH scripts around the installer.** Operators still need shell access to every host, and Fleet still lacks central progress and records.
- **Let the daemon replace itself.** The unprivileged service would need write access to `/opt/fleetnode` and service-control power.
- **Distribute unsigned bundles.** Fleet would choose code that runs as root on every Node.
- **Use a root key with rotating signing certificates.** Signing keys could rotate without a Node update, but root custody, certificate issuance, and revocation add ceremony and code. Phase 1 pins one KMS-backed key and accepts that replacing it needs a newer signed release or the manual `--trust-release-key` path.
- **Use public transparency-log signing.** Private repository identity and artifact digests would become public.
- **Use GitHub artifact attestations.** Nodes would need GitHub API access or a changing trust root.
- **Replicate bundles between Fleet hosts in phase 1.** This handles Fleet server failover more smoothly, but adds peer transfer and reconciliation work for a rare event. Phase 1 uses local storage, fails the in-flight update after failover, and re-downloads the lost bundle instead.
- **Require shared object storage everywhere.** Some deployments do not have it. It remains a future backend.
- **Run bounded waves in phase 1.** Faster for very large fleets, but Fleet Node counts are expected to be much smaller than miner counts. Sequential updates are simpler for v1.
- **Reuse the firmware rollout engine.** It carries miner-specific release-channel and model-group concepts that Fleet Nodes do not need.

## Open questions

1. Which production KMS key should sign Fleet Node releases, who can trigger that workflow, what approval path gates it, and who owns emergency disable or rotation? For emergency signing-key replacement, who approves the replacement, which channel publishes the new public-key fingerprint independently of Fleet and release assets, and what operator verification record is required before using `--trust-release-key`?
2. How long should update records and event timelines be retained?
3. Should Fleet allow a target newer than Fleet's own version? The proposed default is yes when the operator selects it and the store has it.
4. What rough success criteria should justify phase 1: expected Node/site count, manual update time saved, update frequency, or incidents this should prevent?
5. Should release-upload automation use a dedicated upload-only permission instead of `fleetnode:manage` for internal phase 1, or is the broader permission acceptable until automation leaves the controlled deployment environment?

## Rollout

- **Phase 0, done:** safer manual updates and visible Node identity. Operators can stage releases without GitHub access, and Fleet shows Node versions.
- **Phase 1a, signing:** production KMS signing key, signed release assets, `fleetnode release verify`, signing-key replacement through a manual bootstrap path, and a CI signing drill.
- **Phase 1b, executor:** root executor, socket and service units, packaging changes, local-directory testing, local history, and native qualification on supported architectures.
- **Before phase 1c, staging:** staging deploys signed `block/proto-fleet` `-rc.N` or final releases, and its Nodes take their one manual bootstrap to that repository's first update-capable release. Staging is the deployment that proves bootstrap conversion before production.
- **Phase 1c, distribution:** upload-backed release store, local host files, on-demand re-download of lost bundles, Node capability fields, `update_node`, release artifact purpose, host-configured mirror source, progress heartbeats, and disk-space reporting.
- **Phase 1d, records and UI:** migrations, API, sequential update runner, HA sibling guard, activity events, metrics, release-store UI, Nodes page actions, history views, documentation, and manual pin-repair guidance.
- **Phase 2, optional:** Fleet-managed update-source changes, maintenance windows, multi-site scheduling, and prompts to update Nodes after Fleet itself upgrades.

## Validation

The implementation should prove these behaviors before phase 1 ships:

- Signing rejects tampered archives, wrong repository scope, wrong architecture, version mismatch, and unknown signing keys.
- Signing drills cover the production KMS signer and recovery to a replacement key with an independently supplied fingerprint.
- Update source defaults to the enrolled server, follows re-pairing, honors host-config overrides to an HTTPS mirror or a local directory, and reports the effective source.
- Release-store tests cover chunked upload, ingest rejection, retention, local host files, missing bundles after a Fleet server failover, re-download of a recorded bundle, and re-upload when the public release is unreachable.
- HA tests cover standby-first ordering, rejection of a second active run, the sibling lock after a skip and in a later run, and an active Node update that triggers Fleet Node HA failover.
- Disk-space checks cover heartbeat reporting, Fleet admission, and executor failure before staging.
- Front-end tests cover chunked upload through request-size limits, release download through an idle-timeout path, sequential multi-Node runs, rejected ineligible Nodes, stopped-run recovery actions, bootstrap guidance, and the HA sibling guard.
- Executor and installer tests cover request parsing, file descriptor handling, version policy, repository checks, status, local history, units, rollback, and signing-key replacement.
- Compatibility tests prove old Nodes keep connecting to a new Fleet server and new Nodes can connect to an older server.
- Native qualification updates a physical arm64 Node and an amd64 VM from Fleet, from a mirror, and from a local directory.
- Scale tests update the default 50-Node phase 1 envelope sequentially and measure duration, Fleet throughput, and database load.

## Deferred / Open Questions

### From 2026-11-05 review

- **The on-main check does not control who can get a release signed** — Signing and trust, single-key signing model (P1, security-lens, confidence 75)

  Anyone who can push a branch and a version tag could get a production-signed Fleet Node release that every update-capable Node installs as root. GitHub runs a tag-triggered workflow from the tagged commit, so a branch can remove the release workflow's on-main check before tagging. Signing permission has to be enforced outside that workflow file, for example by GitHub environment protection plus an AWS OIDC trust policy bound to that environment. Deferred to follow-up hardening.
