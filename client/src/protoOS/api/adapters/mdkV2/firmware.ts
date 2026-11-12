import { base64Encode } from "@bufbuild/protobuf/wire";
import {
  type HashboardFirmware,
  type HashboardFirmwareAttempt,
  type HashboardFirmwareBoard,
  HashboardFirmwareCancelError,
  type HashboardFirmwareOutcome,
  type HashboardFirmwareProgress,
  HashboardFirmwareStartRefusedError,
} from "../hashboardFirmware";
import type { AdapterRequestOptions } from "../types";
import type { MdkV2Client } from "./connectClient";
import { firmwareDigest } from "./firmwareDigest";
import { readFirmwareImage } from "./firmwareImage";
import {
  V2FirmwareArtifactSource,
  V2FirmwareUpdateStatus,
  type V2GetContainerMapResponse,
} from "./generated/generatedApi";

export function firmwareBoards(map: V2GetContainerMapResponse, serverId: string): HashboardFirmwareBoard[] {
  // Fleet's rule (docs/development/ct1-grpc.md): every tank and every module position of its grid must be reported.
  // The map carries no completeness signal, so a tank that maps fewer modules than its grid cannot be told apart
  // from one whose modules are not all reported yet, and updating it could silently skip boards.
  const incomplete = () =>
    new Error(
      "The container map is incomplete. A local update needs every tank, module position, and board slot to be reported.",
    );
  const tanks = map.containerMap?.tanks;
  const located = tanks?.filter((tank) => tank.id !== 0) ?? [];
  if (located.length !== 6 || new Set(located.map((tank) => tank.id)).size !== 6) throw incomplete();
  const boards: HashboardFirmwareBoard[] = [];
  for (const tank of tanks ?? []) {
    const unlocated = tank.id === 0;
    const expected = (tank.moduleLayout?.rows ?? 0) * (tank.moduleLayout?.columns ?? 0) || 16;
    if (
      !unlocated &&
      (!tank.id ||
        tank.id < 1 ||
        tank.id > 6 ||
        expected > 16 ||
        tank.modules?.length !== expected ||
        new Set(tank.modules.map((m) => m.id)).size !== expected)
    )
      throw incomplete();
    if (unlocated && (tank.modules?.length !== 1 || tank.modules[0].id !== 0)) throw incomplete();
    for (const module of tank.modules ?? []) {
      if (!unlocated && (!module.id || module.id < 1 || module.id > expected || module.slots?.length !== 3))
        throw incomplete();
      const positions = new Set<number>();
      for (const slot of module.slots ?? []) {
        const position = slot.hashboard?.location?.hashboardPosition || slot.configuredLocation?.hashboardPosition;
        if (!unlocated) {
          const n = Number(position);
          if (
            !Number.isInteger(n) ||
            n < 1 ||
            n > 3 ||
            positions.has(n) ||
            (slot.configuredLocation?.hashboardPosition && Number(slot.configuredLocation.hashboardPosition) !== n)
          )
            throw incomplete();
          positions.add(n);
        }
        if (!slot.occupied) continue;
        const mac = slot.hashboard?.macAddress?.trim().toUpperCase();
        const owner = slot.hashboard?.ownerServerId || serverId;
        if (!mac || !/^([0-9A-F]{2}:){5}[0-9A-F]{2}$/.test(mac) || boards.some((board) => board.mac === mac) || !owner)
          throw incomplete();
        boards.push({
          mac,
          owner,
          label: unlocated ? mac : `Tank ${tank.displayNumber || tank.id} · Module ${module.id} · Board ${position}`,
        });
      }
    }
  }
  if (!boards.length) throw new Error("The container has no occupied hashboards to update.");
  return boards;
}

const errorMessage = (error: unknown) => (error instanceof Error ? error.message : "Controller unavailable");
const statusCode = (error: unknown) => (error as { status?: number } | undefined)?.status;
// Status and cancel word an owner's failure identically, so the dialog can show it once.
const ownerLine = (owner: string, message: string) => `${owner}: ${message}`;
const owners = (attempt: HashboardFirmwareAttempt) => [...new Set(attempt.boards.map((board) => board.owner))];
// A job has ended once it reports one of these statuses. They are listed from best to worst: when owners end
// differently, the update as a whole is reported by the worst of them.
const endedOutcomes = new Map<V2FirmwareUpdateStatus, HashboardFirmwareOutcome>([
  [V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_COMPLETED, "completed"],
  [V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS, "completedWithErrors"],
  [V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_CANCELED, "canceled"],
  [V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_INTERRUPTED, "interrupted"],
  [V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_FAILED, "failed"],
]);
const outcomeRank = [...endedOutcomes.values()];
const isTerminal = (status?: V2FirmwareUpdateStatus) => status !== undefined && endedOutcomes.has(status);
// The same split as Fleet's firmwareStartRefused (plugin/proto/internal/ct1/firmware.go): with these answers the
// controller created no job. Every other outcome, including no status at all, is uncertain and stays tracked.
const startRefusedStatuses = new Set([400, 401, 403, 404, 429, 501]);
// CT1 hashboards are F1 hardware, so these containers accept only F1 images.
const hardwareRevision = "f1";

export function createHashboardFirmware(
  client: MdkV2Client,
  decorate: (params?: AdapterRequestOptions) => AdapterRequestOptions,
): HashboardFirmware {
  return {
    readImage: (file) => readFirmwareImage(file, hardwareRevision),
    async prepare(params) {
      const [map, system] = await Promise.all([
        client.hashboardServiceGetContainerMap(decorate(params)),
        client.systemServiceGetSystemStatus(decorate(params)),
      ]);
      return firmwareBoards(map.data, system.data.status?.serverId ?? "");
    },
    async install(image, attempt, onProgress, params, onStarting) {
      const options = decorate(params);
      const checked = await this.readImage(image.file);
      if (checked.build !== attempt.build || checked.version !== attempt.version)
        throw new Error("Firmware selection changed. Choose the image again.");
      const current = await this.prepare(params);
      const identities = (boards: HashboardFirmwareBoard[]) =>
        boards
          .map((board) => `${board.mac}/${board.owner}`)
          .sort()
          .join(",");
      if (identities(current) !== identities(attempt.boards))
        throw new Error("The board map changed. Review the selection again.");
      const bytes = new Uint8Array(await image.file.arrayBuffer());
      const sha256 = await firmwareDigest(bytes, params?.signal);
      const begin = await client.firmwareServiceBeginFirmwareArtifactUpload(
        {
          metadata: {
            source: {
              source: V2FirmwareArtifactSource.FIRMWARE_ARTIFACT_SOURCE_RELEASE,
              releaseTag: image.version,
              assetName: image.file.name,
            },
            images: [{ name: image.file.name, sizeBytes: String(bytes.length), sha256 }],
          },
        },
        options,
      );
      const uploadId = begin.data.uploadId;
      if (!uploadId) throw new Error("Controller did not return an upload session.");
      let committed = false;
      try {
        const chunkSize = Math.min(begin.data.maxChunkBytes || 1024 * 1024, 1024 * 1024);
        for (let offset = 0; offset < bytes.length;) {
          params?.signal?.throwIfAborted();
          const chunk = bytes.subarray(offset, offset + chunkSize);
          const request = { imageIndex: 0, offset: String(offset), data: base64Encode(chunk) };
          let response;
          for (let retry = 0; ; retry++) {
            try {
              response = await client.firmwareServiceWriteFirmwareArtifactUploadChunk({ uploadId }, request, options);
              break;
            } catch (error) {
              if (params?.signal?.aborted || retry === 2 || (statusCode(error) && statusCode(error)! < 500))
                throw error;
            }
          }
          const next = Number(response.data.nextOffset);
          if (!Number.isSafeInteger(next) || next !== offset + chunk.length)
            throw new Error("Controller reported an unexpected upload offset. Choose the file again.");
          offset = next;
          onProgress(Math.round((offset / bytes.length) * 100));
        }
        const result = await client.firmwareServiceCommitFirmwareArtifactUpload({ uploadId }, options);
        committed = true;
        if (!result.data.artifact?.uploadId) throw new Error("Controller did not confirm the firmware artifact.");
        params?.signal?.throwIfAborted();
        onStarting();
        // Never retry Start. Recover this client-selected task on every owner instead.
        try {
          await client.firmwareServiceStartFirmwareUpdate(
            {
              artifact: result.data.artifact,
              taskId: attempt.taskId,
              scope: { identifiers: attempt.boards.map((board) => board.mac) },
              options: { build: image.build, force: true },
            },
            options,
          );
        } catch (error) {
          if (startRefusedStatuses.has(statusCode(error) ?? 0))
            throw new HashboardFirmwareStartRefusedError(
              error instanceof Error && error.message ? error.message : "The controller refused the update.",
            );
          throw error;
        }
      } finally {
        if (!committed)
          await client
            .firmwareServiceAbortFirmwareArtifactUpload({ uploadId }, decorate({ signal: AbortSignal.timeout(5000) }))
            .catch(() => undefined);
      }
    },
    async progress(attempt, params) {
      const result: HashboardFirmwareProgress = { boards: [], errors: [] };
      const ended: HashboardFirmwareOutcome[] = [];
      let unresolved = false;
      await Promise.all(
        owners(attempt).map(async (owner) => {
          const expected = attempt.boards.filter((board) => board.owner === owner);
          try {
            const { data } = await client.firmwareServiceGetFirmwareUpdate(
              { taskId: attempt.taskId, serverId: owner },
              decorate(params),
            );
            const job = data.update;
            if (!job || job.taskId !== attempt.taskId || job.serverId !== owner)
              throw new Error("Controller returned a different firmware task.");
            const outcome = job.status && endedOutcomes.get(job.status);
            if (outcome) ended.push(outcome);
            else unresolved = true;
            if (job.error) result.errors.push(ownerLine(owner, job.error));
            for (const board of expected) {
              const target = job.targets?.find((target) => target.macAddress?.toUpperCase() === board.mac);
              if (!target) {
                // An ended job never gains targets, so waiting for this board would leave an update that can never be
                // dismissed. Like Fleet (docs/development/ct1-grpc.md), count it as an error of the ended update.
                if (outcome) ended.push("completedWithErrors");
                result.errors.push(`${board.label}: missing from update status`);
              }
              result.boards.push({
                ...board,
                status:
                  target?.status?.replace("FIRMWARE_TARGET_STATUS_", "").toLowerCase().replaceAll("_", " ") ||
                  "Status unavailable",
                percent: target?.progressPercent,
                message: [target?.phase, target?.message || target?.reason].filter(Boolean).join(" · "),
              });
            }
          } catch (error) {
            unresolved = true;
            result.errors.push(
              ownerLine(
                owner,
                statusCode(error) === 404
                  ? "Update not found. Check again, or cancel and stop tracking before starting another update."
                  : errorMessage(error),
              ),
            );
            result.boards.push(...expected.map((board) => ({ ...board, status: "Status unavailable" })));
          }
        }),
      );
      result.boards.sort((a, b) => a.label.localeCompare(b.label, undefined, { numeric: true }));
      if (!unresolved) result.outcome = outcomeRank[Math.max(...ended.map((outcome) => outcomeRank.indexOf(outcome)))];
      return result;
    },
    async cancel(attempt, params) {
      const failures: string[] = [];
      const absent: string[] = [];
      let pending = false;
      let finished = false;
      await Promise.all(
        owners(attempt).map(async (owner) => {
          try {
            const { data } = await client.firmwareServiceCancelFirmwareUpdate(
              { taskId: attempt.taskId, serverId: owner },
              decorate(params),
            );
            if (!data.update || data.update.taskId !== attempt.taskId || data.update.serverId !== owner)
              throw new Error("Controller did not confirm cancellation for this task.");
            const { status } = data.update;
            if (!isTerminal(status)) pending = true;
            // The job ended before the cancel arrived: it was not canceled, and its result must stay visible.
            else if (status !== V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_CANCELED) finished = true;
          } catch (error) {
            if (statusCode(error) === 404) absent.push(owner);
            else failures.push(ownerLine(owner, errorMessage(error)));
          }
        }),
      );
      if (failures.length) throw new HashboardFirmwareCancelError(failures);
      return { pending, finished, absent: absent.sort() };
    },
  };
}
