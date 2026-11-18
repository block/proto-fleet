import { BinaryReader, WireType } from "@bufbuild/protobuf/wire";
import { type Timestamp, timestampDate } from "@bufbuild/protobuf/wkt";
import { type Client, ConnectError } from "@connectrpc/connect";
import {
  decodeBytes,
  type FrigateController,
  type FrigatePackage,
  type FrigateRollout,
  type FrigateRolloutPhase,
  type FrigateServerUpdate,
  FrigateUpdateError,
  type FrigateUpdatePlan,
  type FrigateUpdateProblem,
  type FrigateUpdateRecord,
  FrigateUpdateRefusedError,
  type FrigateUpdateRelease,
  releaseOf,
} from "../frigateServerUpdate";
import { describeFrigateUpdateProblem, ETCD_UNHEALTHY, REMOTE_TRIGGER_DISABLED } from "../frigateUpdateMessages";
import type { AdapterRequestOptions } from "../types";
import { createSha256 } from "./firmwareDigest";
import {
  FrigateUpdateAgentPhase,
  type GetServerUpdateSupportResponse,
  ServerInstallationType,
  ServerUpdateCoordination,
  type ServerUpdateRollout,
  ServerUpdateRolloutPhase,
  type ServerUpdateService,
} from "./generated/proto/mdk/v2/server_update_pb";
import { CONTAINER_REFUSAL_CODES } from "./writeErrors";

export type ServerUpdateClient = Client<typeof ServerUpdateService>;

// Frigate's limits for a self-coordinated package (frigateupdate.Stager).
const CHUNK_BYTES = 1024 * 1024;
const MAX_PACKAGE_BYTES = 512 * 1024 * 1024;
const MAX_STATEMENT_BYTES = 64 * 1024;
const MAX_SIGNATURE_BYTES = 128;
const STATEMENT_SCHEMA = "frigate-release-statement/v1";
const STATEMENT_FIELDS = new Set([
  "schema",
  "package",
  "version",
  "architecture",
  "repository",
  "sha256",
  "size",
  "min_installed_version",
  "issued_at",
]);
// The controllers accept only this package from this repository; anything else fails at Start.
const RELEASE_PACKAGE = "frigate";
const RELEASE_REPOSITORY = "btc-mining/frigate";
const SERVER_ID_HEADER = "x-frigate-server-id";
const REFUSAL_DOMAIN = "mdk.v2.frigate";
// Append failures are retried by asking the controller where its copy stands, which Begin answers for a known stage.
const STAGE_RESUMES = 3;
/** Reads `google.rpc.ErrorInfo` (reason = 1, domain = 2) without bundling the google.rpc schema. */
function refusalReason(error: ConnectError): string {
  for (const detail of error.details) {
    if (!("type" in detail) || detail.type !== "google.rpc.ErrorInfo") continue;
    try {
      const reader = new BinaryReader(detail.value);
      let reason = "";
      let domain = "";
      while (reader.pos < reader.len) {
        const [field, wireType] = reader.tag();
        if (field === 1 && wireType === WireType.LengthDelimited) reason = reader.string();
        else if (field === 2 && wireType === WireType.LengthDelimited) domain = reader.string();
        else reader.skip(wireType, field);
      }
      if (domain === REFUSAL_DOMAIN && reason) return reason;
    } catch {
      // A malformed detail carries no reason.
    }
  }
  return "";
}

/**
 * Whether a write was refused is decided by the gRPC code alone, with the same codes as the other container writes.
 * Frigate also attaches a reason when it can't tell whether its write landed (a failed etcd commit is `Unavailable`
 * with `UPDATE_ETCD_UNAVAILABLE`), so a reason only words the failure.
 */
function updateError(error: unknown, signal?: AbortSignal | null): unknown {
  if (signal?.aborted) return signal.reason;
  const connectError = ConnectError.from(error);
  const reason = refusalReason(connectError);
  const failure = CONTAINER_REFUSAL_CODES.has(connectError.code)
    ? new FrigateUpdateRefusedError(reason, connectError.rawMessage)
    : reason
      ? new FrigateUpdateError(reason, connectError.rawMessage)
      : new Error(connectError.rawMessage || "The controller didn't answer.");
  return Object.assign(failure, { cause: error });
}

const rolloutPhases: Record<ServerUpdateRolloutPhase, FrigateRolloutPhase> = {
  [ServerUpdateRolloutPhase.UNSPECIFIED]: "unknown",
  [ServerUpdateRolloutPhase.VERIFYING]: "verifying",
  [ServerUpdateRolloutPhase.STOPPING]: "stopping",
  [ServerUpdateRolloutPhase.INSTALLING]: "installing",
  [ServerUpdateRolloutPhase.MIGRATING]: "migrating",
  [ServerUpdateRolloutPhase.STARTING]: "starting",
  [ServerUpdateRolloutPhase.SUCCEEDED]: "succeeded",
  [ServerUpdateRolloutPhase.FAILED]: "failed",
  [ServerUpdateRolloutPhase.CANCELLED]: "cancelled",
  [ServerUpdateRolloutPhase.VERIFIED]: "verified",
  [ServerUpdateRolloutPhase.ABANDONED]: "abandoned",
};
const endedPhases = new Set<FrigateRolloutPhase>(["succeeded", "failed", "cancelled", "verified", "abandoned"]);

const agentPhases: Partial<Record<FrigateUpdateAgentPhase, string>> = {
  [FrigateUpdateAgentPhase.CONNECTING]: "Connecting",
  [FrigateUpdateAgentPhase.STAGED]: "Staged",
  [FrigateUpdateAgentPhase.STOPPED]: "Software stopped",
  [FrigateUpdateAgentPhase.INSTALLED]: "Installed",
  [FrigateUpdateAgentPhase.MIGRATED]: "Migrated",
  [FrigateUpdateAgentPhase.STARTED]: "Software started",
  [FrigateUpdateAgentPhase.FAILED]: "Failed",
};

const date = (timestamp: Timestamp | undefined) => (timestamp ? timestampDate(timestamp) : undefined);
const problem = (code: string, message: string): FrigateUpdateProblem | undefined =>
  code || message ? { code, message } : undefined;

export function toFrigateRollout(rollout: ServerUpdateRollout): FrigateRollout {
  const phase = rolloutPhases[rollout.phase] ?? "unknown";
  return {
    jobId: rollout.jobId,
    targetVersion: rollout.targetVersion,
    fromVersion: rollout.fromVersion,
    phase,
    ended: endedPhases.has(phase),
    actor: rollout.actor,
    releaseKeyId: rollout.releaseKeyId,
    retryable: rollout.retryable,
    cancellable: rollout.cancellable,
    updatedAt: date(rollout.updatedAt)?.getTime(),
    problem: problem(rollout.errorCode, rollout.errorMessage),
    nextStep: rollout.nextStep || undefined,
    instances: rollout.instances
      .map((instance) => ({
        instanceId: instance.instanceId,
        phase: agentPhases[instance.phase] ?? "Not reported",
        runningVersion: instance.runningVersion || undefined,
        lastContact: date(instance.lastContact),
        contactStale: instance.contactStale,
        problem: problem(instance.errorCode, instance.errorMessage),
      }))
      .sort((a, b) => a.instanceId.localeCompare(b.instanceId)),
  };
}

const RELEASE_FILES =
  "Choose the three files of one controller software release: the .deb package, its .deb.statement.json, and its .deb.statement.sig.";

function parseStatement(bytes: Uint8Array): Omit<FrigatePackage, "deb" | "statement" | "signature"> {
  const invalid = (detail: string) => new Error(`The release statement is not valid: ${detail}.`);
  let value: unknown;
  try {
    value = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  } catch {
    throw invalid("it is not JSON");
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid("it is not a JSON object");
  const fields = value as Record<string, unknown>;
  const unknown = Object.keys(fields).find((key) => !STATEMENT_FIELDS.has(key));
  if (unknown) throw invalid(`it has an unknown field "${unknown}"`);
  const text = (key: string, required = true) => {
    const field = fields[key];
    if (field === undefined && !required) return undefined;
    if (typeof field !== "string" || (required && !field)) throw invalid(`"${key}" is missing`);
    return field || undefined;
  };
  if (fields.schema !== STATEMENT_SCHEMA) throw invalid(`its schema is not ${STATEMENT_SCHEMA}`);
  const name = text("package");
  if (name !== RELEASE_PACKAGE)
    throw new Error(`This is not a controller software release: its statement is for "${name}".`);
  const repository = text("repository")!;
  if (repository !== RELEASE_REPOSITORY)
    throw new Error(`This release was not published from ${RELEASE_REPOSITORY}. The controllers will refuse it.`);
  const sha256 = text("sha256")!;
  if (!/^[0-9a-f]{64}$/.test(sha256)) throw invalid('"sha256" is not a lowercase SHA-256');
  const size = fields.size;
  if (typeof size !== "number" || !Number.isSafeInteger(size) || size <= 0) throw invalid('"size" is not a size');
  if (size > MAX_PACKAGE_BYTES) throw new Error("The package is larger than the controllers accept (512 MiB).");
  return {
    version: text("version")!,
    architecture: text("architecture")!,
    repository,
    sha256,
    sizeBytes: size,
    minInstalledVersion: text("min_installed_version", false),
    issuedAt: text("issued_at", false),
  };
}

function controllerOf(support: GetServerUpdateSupportResponse): FrigateController {
  return {
    instanceId: support.instanceId,
    runningVersion: support.runningVersion,
    architecture: support.architecture,
    spoolFreeBytes: Number(support.spoolFreeBytes),
    trustedKeyIds: support.trustedReleaseKeyIds,
  };
}

/** Plain-language reasons one controller can't take a self-coordinated update now. */
function supportBlockers(support: GetServerUpdateSupportResponse, pkg: FrigatePackage | undefined): string[] {
  const text = (code: string) => describeFrigateUpdateProblem(code, "").title;
  const blockers: string[] = [];
  if (!support.selfCoordinationSupported) blockers.push(text("UPDATE_SELF_COORDINATION_UNSUPPORTED"));
  if (support.installationType !== ServerInstallationType.DEBIAN_PACKAGE)
    blockers.push("A controller wasn't installed from a Debian package, so it can't be updated this way.");
  if (!support.helperAvailable) blockers.push(text("UPDATE_HELPER_UNAVAILABLE"));
  if (support.etcdMembersTotal > 0 && support.etcdMembersHealthy * 2 <= support.etcdMembersTotal)
    blockers.push(
      `${ETCD_UNHEALTHY} ${support.etcdMembersHealthy} of ${support.etcdMembersTotal} members are healthy.`,
    );
  if (!support.remoteTriggerAllowed) blockers.push(REMOTE_TRIGGER_DISABLED);
  if (!support.trustedReleaseKeyIds.length) blockers.push(text("UPDATE_NO_TRUSTED_KEYS"));
  if (support.activeJobId) blockers.push(text("UPDATE_ACTIVE_JOB"));
  if (pkg) {
    if (support.architecture && support.architecture !== pkg.architecture)
      blockers.push(`The package is built for ${pkg.architecture}, but a controller runs on ${support.architecture}.`);
    // Zero means the controller didn't report its space; Start decides then.
    const free = Number(support.spoolFreeBytes);
    if (free > 0 && free < pkg.sizeBytes) blockers.push(text("UPDATE_INSUFFICIENT_SPACE"));
  }
  return blockers;
}

/** The numeric core of a version ("v1.3.0-2+build" is [1, 3, 0]), or nothing when it isn't dotted numbers. */
function versionCore(version: string): number[] | undefined {
  const core = version.trim().replace(/^v/, "").split(/[-~+]/)[0];
  return /^\d+(\.\d+)*$/.test(core) ? core.split(".").map(Number) : undefined;
}

/** Whether `left` is clearly older than `right`. Versions that can't be compared are left for Start to judge. */
function olderThan(left: string, right: string): boolean {
  const a = versionCore(left);
  const b = versionCore(right);
  if (!a || !b) return false;
  for (let index = 0; index < Math.max(a.length, b.length); index++) {
    const difference = (a[index] ?? 0) - (b[index] ?? 0);
    if (difference) return difference < 0;
  }
  return false;
}

const FILE_CHANGED = "The package file changed. Choose the release again.";

/** One chunk of the package, read when it is needed so the whole file is never held in memory. */
async function readChunk(deb: File, offset: number): Promise<Uint8Array> {
  try {
    return new Uint8Array(await deb.slice(offset, offset + CHUNK_BYTES).arrayBuffer());
  } catch (error) {
    throw Object.assign(new Error(FILE_CHANGED), { cause: error });
  }
}

export function createFrigateServerUpdate(
  client: ServerUpdateClient,
  decorate: (params?: AdapterRequestOptions) => AdapterRequestOptions,
): FrigateServerUpdate {
  const call = async <T>(
    params: AdapterRequestOptions | undefined,
    instanceId: string | undefined,
    send: (options: { headers?: Record<string, string>; signal?: AbortSignal }) => Promise<T>,
  ): Promise<T> => {
    const options = decorate(params);
    const headers = instanceId ? { ...options.headers, [SERVER_ID_HEADER]: instanceId } : options.headers;
    try {
      return await send({ headers, signal: options.signal ?? undefined });
    } catch (error) {
      throw updateError(error, options.signal);
    }
  };
  /** Begins a controller's copy, or returns the copy this update already began there. */
  const beginStage = (
    record: FrigateUpdateRecord,
    instanceId: string,
    release: FrigateUpdateRelease,
    params: AdapterRequestOptions | undefined,
  ) =>
    call(params, instanceId, (options) =>
      client.beginServerUpdateStage(
        {
          manifest: {
            jobId: record.jobId,
            instanceId,
            expectedInstanceIds: record.instanceIds,
            targetVersion: record.targetVersion,
            sha256: release.sha256,
            sizeBytes: BigInt(release.sizeBytes),
            actor: "proto-os",
            coordination: ServerUpdateCoordination.SELF,
            releaseStatement: decodeBytes(release.statement),
            releaseSignature: decodeBytes(release.signature),
          },
        },
        options,
      ),
    );
  const rolloutOf = (rollout: ServerUpdateRollout | undefined) => {
    if (!rollout?.jobId) throw new Error("The controller didn't return the update.");
    return toFrigateRollout(rollout);
  };

  return {
    async readPackage(files, params) {
      const debs = files.filter((file) => file.name.endsWith(".deb"));
      const statements = files.filter((file) => file.name.endsWith(".json"));
      const signatures = files.filter((file) => file.name.endsWith(".sig"));
      if (files.length !== 3 || debs.length !== 1 || statements.length !== 1 || signatures.length !== 1)
        throw new Error(RELEASE_FILES);
      const [deb] = debs;
      // Sizes are checked before anything is read, so a wrong file is never loaded.
      if (!statements[0].size || statements[0].size > MAX_STATEMENT_BYTES)
        throw new Error("The release statement file is not a controller software release statement.");
      if (!signatures[0].size || signatures[0].size > MAX_SIGNATURE_BYTES)
        throw new Error("The release signature file is not a controller software release signature.");
      const statement = new Uint8Array(await statements[0].arrayBuffer());
      const signature = new Uint8Array(await signatures[0].arrayBuffer());
      const parsed = parseStatement(statement);
      if (deb.size !== parsed.sizeBytes)
        throw new Error("The package doesn't match its release statement: the file size differs.");
      const sha256 = createSha256();
      for (let offset = 0; offset < deb.size; offset += CHUNK_BYTES) {
        params?.signal?.throwIfAborted();
        sha256.update(await readChunk(deb, offset));
      }
      params?.signal?.throwIfAborted();
      if (sha256.digest() !== parsed.sha256)
        throw new Error("The package doesn't match its release statement: the SHA-256 differs.");
      return { deb, statement, signature, ...parsed };
    },

    async plan(pkg, params) {
      const local = await call(params, undefined, (options) => client.getServerUpdateSupport({}, options));
      const supports = [local];
      const blockers: string[] = [];
      if (local.peerInstanceId) {
        try {
          const peer = await call(params, local.peerInstanceId, (options) =>
            client.getServerUpdateSupport({}, options),
          );
          if (peer.instanceId !== local.peerInstanceId) throw new Error("A different controller answered.");
          supports.push(peer);
          if (peer.clusterId !== local.clusterId)
            blockers.push("The two controllers report different clusters, so they can't be updated together.");
        } catch (error) {
          if (params?.signal?.aborted) throw error;
          blockers.push(
            `Controller ${local.peerInstanceId} can't be reached: ${error instanceof Error ? error.message : "no answer"}.`,
          );
        }
      }
      for (const support of supports) blockers.push(...supportBlockers(support, pkg));
      if (pkg) {
        // Frigate installs only a strict upgrade on each controller, so one that already runs the release blocks the
        // update for both.
        const current = supports.filter((support) => support.runningVersion === pkg.version);
        if (current.length && current.length === (local.peerInstanceId ? 2 : 1))
          blockers.push(`The controllers already run version ${pkg.version}.`);
        else
          for (const { instanceId } of current)
            blockers.push(
              `Controller ${instanceId} already runs version ${pkg.version}. An update needs every controller on an older version, so install this release on the other controller by hand.`,
            );
        // Start refuses these too, but only after the whole package has been copied.
        for (const { instanceId, runningVersion } of supports) {
          if (olderThan(pkg.version, runningVersion))
            blockers.push(
              `Controller ${instanceId} runs version ${runningVersion}, which is newer than this release. Only a newer release can be installed.`,
            );
          else if (pkg.minInstalledVersion && olderThan(runningVersion, pkg.minInstalledVersion))
            blockers.push(
              `This release needs version ${pkg.minInstalledVersion} or newer, and controller ${instanceId} runs ${runningVersion}. Install an earlier release first.`,
            );
        }
      }
      return {
        controllers: supports.map(controllerOf),
        blockers: [...new Set(blockers)],
        activeJobId: supports.find((support) => support.activeJobId)?.activeJobId,
      } satisfies FrigateUpdatePlan;
    },

    async stage(pkg, record, onProgress, params) {
      const size = pkg.deb.size;
      if (size !== pkg.sizeBytes) throw new Error(FILE_CHANGED);
      const fingerprints: Record<string, string> = { ...record.fingerprints };
      const total = size * record.instanceIds.length;
      for (const [index, instanceId] of record.instanceIds.entries()) {
        const begin = () => beginStage(record, instanceId, releaseOf(pkg), params);
        let started = await begin();
        const manifestSha256 = started.manifestSha256;
        if (!manifestSha256) throw new Error(`Controller ${instanceId} didn't accept the package.`);
        // A copy that is resumed after a reload must continue the stage this update began, not another package's.
        const known = record.fingerprints[instanceId];
        if (known && known !== manifestSha256)
          throw new Error(`Controller ${instanceId} restarted the copy with a different package.`);
        fingerprints[instanceId] = manifestSha256;
        const report = (offset: number) =>
          onProgress(Math.floor(((index * size + offset) / total) * 100), { ...fingerprints });
        let offset = Number(started.nextOffset);
        report(offset);
        for (let resumes = 0; offset < size;) {
          params?.signal?.throwIfAborted();
          const chunk = await readChunk(pkg.deb, offset);
          try {
            const appended = await call(params, instanceId, (options) =>
              client.appendServerUpdateStage(
                { jobId: record.jobId, manifestSha256, offset: BigInt(offset), chunk },
                options,
              ),
            );
            const next = Number(appended.nextOffset);
            if (next !== offset + chunk.length)
              throw new Error(`Controller ${instanceId} reported an unexpected copy offset.`);
            offset = next;
            resumes = 0;
          } catch (error) {
            if (params?.signal?.aborted || ++resumes > STAGE_RESUMES) throw error;
            started = await begin();
            if (started.manifestSha256 !== manifestSha256)
              throw Object.assign(new Error(`Controller ${instanceId} restarted the copy with a different package.`), {
                cause: error,
              });
            offset = Number(started.nextOffset);
          }
          report(offset);
        }
        const committed = await call(params, instanceId, (options) =>
          client.commitServerUpdateStage({ jobId: record.jobId, manifestSha256 }, options),
        );
        if (committed.sha256 !== pkg.sha256 || Number(committed.bytesStaged) !== size)
          throw new Error(`Controller ${instanceId} received a package that doesn't match the file.`);
      }
      onProgress(100, { ...fingerprints });
      return fingerprints;
    },

    async discard(record, params) {
      const { release } = record;
      // A controller with no saved fingerprint may still hold a copy: it accepted the start and the answer was lost.
      // Asking again returns that copy's fingerprint. A refusal means it holds none.
      const fingerprintOf = async (instanceId: string) => {
        const known = record.fingerprints[instanceId];
        if (known || !release) return known;
        try {
          return (await beginStage(record, instanceId, release, params)).manifestSha256;
        } catch (failure) {
          if (failure instanceof FrigateUpdateRefusedError && failure.code !== "UPDATE_ACTIVE_JOB") return undefined;
          throw failure;
        }
      };
      const remove = async (instanceId: string) => {
        const manifestSha256 = await fingerprintOf(instanceId);
        if (manifestSha256)
          await call(params, instanceId, (options) =>
            client.abortServerUpdateStage({ jobId: record.jobId, manifestSha256 }, options),
          );
      };
      const failures = await Promise.all(
        record.instanceIds.map((instanceId) =>
          remove(instanceId).then(
            () => undefined,
            (failure: unknown) => ({ instanceId, failure }),
          ),
        ),
      );
      params?.signal?.throwIfAborted();
      // A refusal without a reason means the controller holds no stage of this update (a different one, or none), so
      // there is nothing to remove there.
      const kept = failures.filter(
        (result) => result && !(result.failure instanceof FrigateUpdateRefusedError && !result.failure.code),
      ) as { instanceId: string; failure: unknown }[];
      // The job started after all, so its package is in use: Frigate says to cancel the update instead.
      const active = kept.find(
        ({ failure }) => failure instanceof FrigateUpdateError && failure.code === "UPDATE_ACTIVE_JOB",
      );
      if (active) throw active.failure;
      if (kept.length) {
        const [{ instanceId, failure }] = kept;
        const detail = failure instanceof Error && failure.message ? failure.message : "no answer";
        throw Object.assign(
          new Error(`Controller ${instanceId} didn't confirm the package was removed (${detail}). Discard it again.`),
          { cause: failure },
        );
      }
    },

    async start(record, params) {
      const response = await call(params, undefined, (options) =>
        client.startServerUpdate(
          {
            jobId: record.jobId,
            targetVersion: record.targetVersion,
            expectedInstanceIds: record.instanceIds,
            idempotencyKey: record.idempotencyKey,
            expectedManifestSha256: record.fingerprints,
          },
          options,
        ),
      );
      return rolloutOf(response.rollout);
    },

    async status(jobId, params) {
      try {
        const response = await call(params, undefined, (options) =>
          client.getServerUpdateStatus({ jobId: jobId ?? "" }, options),
        );
        return { rollout: rolloutOf(response.rollout), servedByMaintenance: response.servedByMaintenance };
      } catch (error) {
        if (error instanceof FrigateUpdateError && error.code === "UPDATE_NOT_FOUND")
          return { servedByMaintenance: false };
        throw error;
      }
    },

    async retry(jobId, idempotencyKey, params) {
      const response = await call(params, undefined, (options) =>
        client.retryServerUpdate({ jobId, idempotencyKey }, options),
      );
      return rolloutOf(response.rollout);
    },

    async cancel(jobId, idempotencyKey, params) {
      const response = await call(params, undefined, (options) =>
        client.cancelServerUpdate({ jobId, idempotencyKey }, options),
      );
      return rolloutOf(response.rollout);
    },
  };
}
