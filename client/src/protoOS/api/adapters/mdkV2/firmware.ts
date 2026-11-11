import {
  type CT1Firmware,
  type CT1FirmwareAttempt,
  type CT1FirmwareBoard,
  type CT1FirmwareProgress,
  CT1FirmwareStartRefusedError,
} from "../ct1Firmware";
import type { AdapterRequestOptions } from "../types";
import type { MdkV2Client } from "./connectClient";
import { firmwareDigest } from "./firmwareDigest";
import { readFirmwareImage } from "./firmwareImage";
import {
  V2FirmwareArtifactSource,
  V2FirmwareUpdateStatus,
  type V2GetContainerMapResponse,
} from "./generated/generatedApi";

export function firmwareBoards(map: V2GetContainerMapResponse, serverId: string): CT1FirmwareBoard[] {
  const incomplete = () =>
    new Error("The container map is incomplete. Wait for all tanks, modules, and board slots before updating.");
  const tanks = map.containerMap?.tanks;
  const located = tanks?.filter((tank) => tank.id !== 0) ?? [];
  if (located.length !== 6 || new Set(located.map((tank) => tank.id)).size !== 6) throw incomplete();
  const boards: CT1FirmwareBoard[] = [];
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
const owners = (attempt: CT1FirmwareAttempt) => [...new Set(attempt.boards.map((board) => board.owner))];
const terminalStatuses = new Set<V2FirmwareUpdateStatus>([
  V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_COMPLETED,
  V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS,
  V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_FAILED,
  V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_CANCELED,
  V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_INTERRUPTED,
]);
const isTerminal = (status?: V2FirmwareUpdateStatus) => status !== undefined && terminalStatuses.has(status);
// The same split as Fleet's firmwareStartRefused (plugin/proto/internal/ct1/firmware.go): with these answers the
// controller created no job. Every other outcome, including no status at all, is uncertain and stays tracked.
const startRefusedStatuses = new Set([400, 401, 403, 404, 429, 501]);

export function createCT1Firmware(
  client: MdkV2Client,
  decorate: (params?: AdapterRequestOptions) => AdapterRequestOptions,
): CT1Firmware {
  return {
    async prepare(params) {
      const [map, system] = await Promise.all([
        client.hashboardServiceGetContainerMap(decorate(params)),
        client.systemServiceGetSystemStatus(decorate(params)),
      ]);
      return firmwareBoards(map.data, system.data.status?.serverId ?? "");
    },
    async install(image, attempt, onProgress, params, onStarting) {
      const options = decorate(params);
      const checked = await readFirmwareImage(image.file);
      if (checked.build !== attempt.build || checked.version !== attempt.version)
        throw new Error("Firmware selection changed. Choose the image again.");
      const current = await this.prepare(params);
      const identities = (boards: CT1FirmwareBoard[]) =>
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
          let binary = "";
          for (let i = 0; i < chunk.length; i += 8192) binary += String.fromCharCode(...chunk.subarray(i, i + 8192));
          const request = { imageIndex: 0, offset: String(offset), data: btoa(binary) };
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
            throw new CT1FirmwareStartRefusedError(
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
      const result: CT1FirmwareProgress = { boards: [], complete: true, errors: [] };
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
            if (!isTerminal(job.status)) result.complete = false;
            if (job.error) result.errors.push(`${owner}: ${job.error}`);
            for (const board of expected) {
              const target = job.targets?.find((target) => target.macAddress?.toUpperCase() === board.mac);
              if (!target) {
                result.complete = false;
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
            result.complete = false;
            result.errors.push(
              `${owner}: ${statusCode(error) === 404 ? "Update not found. Check again, or cancel and stop tracking before starting another update." : errorMessage(error)}`,
            );
            result.boards.push(...expected.map((board) => ({ ...board, status: "Status unavailable" })));
          }
        }),
      );
      result.boards.sort((a, b) => a.label.localeCompare(b.label, undefined, { numeric: true }));
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
            else failures.push(`${owner}: ${errorMessage(error)}`);
          }
        }),
      );
      if (failures.length) throw new Error(failures.join("; "));
      return { pending, finished, absent: absent.sort() };
    },
  };
}
