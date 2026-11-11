import { describe, expect, it, vi } from "vitest";
import { Buffer, File } from "node:buffer";
import { createHash } from "node:crypto";
import { type CT1FirmwareAttempt, CT1FirmwareStartRefusedError } from "../ct1Firmware";
import type { MdkV2Client } from "./connectClient";
import { createCT1Firmware, firmwareBoards } from "./firmware";
import { firmwareTestImage, firmwareTestMap } from "./firmwareTestFixtures";
import { V2FirmwareTargetStatus, V2FirmwareUpdateStatus } from "./generated/generatedApi";

const setup = () => {
  const map = firmwareTestMap();
  const mock = {
    hashboardServiceGetContainerMap: vi.fn().mockResolvedValue({ data: map }),
    systemServiceGetSystemStatus: vi.fn().mockResolvedValue({ data: { status: { serverId: "owner-a" } } }),
    firmwareServiceBeginFirmwareArtifactUpload: vi
      .fn()
      .mockResolvedValue({ data: { uploadId: "session", maxChunkBytes: 1024 } }),
    firmwareServiceWriteFirmwareArtifactUploadChunk: vi.fn().mockImplementation((_path, chunk) => ({
      data: { nextOffset: String(Number(chunk.offset) + atob(chunk.data).length) },
    })),
    firmwareServiceCommitFirmwareArtifactUpload: vi
      .fn()
      .mockResolvedValue({ data: { artifact: { uploadId: "artifact" } } }),
    firmwareServiceAbortFirmwareArtifactUpload: vi.fn().mockResolvedValue({ data: {} }),
    firmwareServiceStartFirmwareUpdate: vi.fn().mockResolvedValue({ data: {} }),
    firmwareServiceGetFirmwareUpdate: vi.fn(),
    firmwareServiceCancelFirmwareUpdate: vi.fn().mockImplementation(({ taskId, serverId }) => ({
      data: { update: { taskId, serverId, status: V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_CANCELED } },
    })),
  };
  const firmware = createCT1Firmware(mock as unknown as MdkV2Client, (params) => ({
    ...params,
    headers: { "X-Frigate-Actor": "proto-os" },
  }));
  const attempt: CT1FirmwareAttempt = {
    taskId: "client-task",
    boards: firmwareBoards(map, "owner-a"),
    version: "9.0.0",
    build: "prod",
  };
  const image = {
    file: new File([firmwareTestImage()], "f1-app-prod.bin") as unknown as globalThis.File,
    version: "9.0.0",
    build: "prod" as const,
  };
  return { mock, firmware, attempt, image, map };
};

describe("CT1 firmware", () => {
  it("uploads once, retries a lost chunk response, persists before Start, and targets every occupied board", async () => {
    const { mock, firmware, attempt, image } = setup();
    mock.firmwareServiceWriteFirmwareArtifactUploadChunk.mockRejectedValueOnce({ status: 503 });
    const persist = vi.fn(() => expect(mock.firmwareServiceStartFirmwareUpdate).not.toHaveBeenCalled());
    const progress = vi.fn();
    await firmware.install(image, attempt, progress, {}, persist);
    expect(mock.firmwareServiceBeginFirmwareArtifactUpload).toHaveBeenCalledOnce();
    expect(mock.firmwareServiceBeginFirmwareArtifactUpload.mock.calls[0][0].metadata.source).toMatchObject({
      releaseTag: "9.0.0",
      assetName: "f1-app-prod.bin",
    });
    expect(mock.firmwareServiceWriteFirmwareArtifactUploadChunk).toHaveBeenCalledTimes(3);
    expect(mock.firmwareServiceWriteFirmwareArtifactUploadChunk.mock.calls[0]).toEqual(
      mock.firmwareServiceWriteFirmwareArtifactUploadChunk.mock.calls[1],
    );
    expect(persist).toHaveBeenCalledOnce();
    expect(mock.firmwareServiceStartFirmwareUpdate).toHaveBeenCalledWith(
      expect.objectContaining({
        taskId: "client-task",
        options: { build: "prod", force: true },
        scope: { identifiers: attempt.boards.map((board) => board.mac) },
      }),
      expect.objectContaining({ headers: { "X-Frigate-Actor": "proto-os" } }),
    );
    expect(progress).toHaveBeenLastCalledWith(100);
    expect(mock.firmwareServiceAbortFirmwareArtifactUpload).not.toHaveBeenCalled();
  });
  it("aborts a rejected upload and never starts or retries final errors", async () => {
    const { mock, firmware, attempt, image } = setup();
    mock.firmwareServiceWriteFirmwareArtifactUploadChunk.mockRejectedValue({ status: 400 });
    await expect(firmware.install(image, attempt, vi.fn(), {}, vi.fn())).rejects.toEqual({ status: 400 });
    expect(mock.firmwareServiceWriteFirmwareArtifactUploadChunk).toHaveBeenCalledOnce();
    expect(mock.firmwareServiceAbortFirmwareArtifactUpload).toHaveBeenCalledOnce();
    expect(mock.firmwareServiceStartFirmwareUpdate).not.toHaveBeenCalled();
  });
  it("does not retry an uncertain Start and can recover both owners independently", async () => {
    const { mock, firmware, attempt, image } = setup();
    mock.firmwareServiceStartFirmwareUpdate.mockRejectedValue({ status: 503 });
    await expect(firmware.install(image, attempt, vi.fn(), {}, vi.fn())).rejects.toEqual({ status: 503 });
    expect(mock.firmwareServiceStartFirmwareUpdate).toHaveBeenCalledOnce();
    mock.firmwareServiceGetFirmwareUpdate.mockImplementation(({ serverId, taskId }) => ({
      data: {
        update: {
          taskId,
          serverId,
          status: V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS,
          targets: attempt.boards
            .filter((board) => board.owner === serverId)
            .map((board, i) => ({
              macAddress: board.mac,
              status: i
                ? V2FirmwareTargetStatus.FIRMWARE_TARGET_STATUS_SUCCEEDED
                : V2FirmwareTargetStatus.FIRMWARE_TARGET_STATUS_FAILED,
              message: i ? "" : "flash failed",
            })),
        },
      },
    }));
    const result = await firmware.progress(attempt);
    expect(result.complete).toBe(true);
    expect(result.boards).toHaveLength(18);
    expect(result.boards.filter((board) => board.message === "flash failed")).toHaveLength(2);
    mock.firmwareServiceGetFirmwareUpdate.mockRejectedValueOnce({ status: 404 });
    const missing = await firmware.progress(attempt);
    expect(missing.complete).toBe(false);
    expect(missing.errors[0]).toContain("Update not found");
    expect((await firmware.cancel(attempt)).pending).toBe(false);
    expect(mock.firmwareServiceCancelFirmwareUpdate).toHaveBeenCalledTimes(2);
    expect(mock.firmwareServiceCancelFirmwareUpdate).toHaveBeenCalledWith(
      { taskId: "client-task", serverId: "owner-b" },
      expect.anything(),
    );
  });
  it("refuses incomplete, duplicate or changed board maps before uploading", async () => {
    const { mock, firmware, attempt, image, map } = setup();
    const tank = map.containerMap!.tanks![0];
    tank.modules![0].slots![0].hashboard!.macAddress = "02:00:00:00:ff:ff";
    await expect(firmware.install(image, attempt, vi.fn(), {}, vi.fn())).rejects.toThrow("board map changed");
    expect(mock.firmwareServiceBeginFirmwareArtifactUpload).not.toHaveBeenCalled();
    tank.modules![0].slots!.pop();
    expect(() => firmwareBoards(map, "owner")).toThrow("incomplete");
    const missing = firmwareTestMap();
    missing.containerMap!.tanks!.pop();
    expect(() => firmwareBoards(missing, "owner")).toThrow("incomplete");
    const duplicate = firmwareTestMap();
    duplicate.containerMap!.tanks![1] = duplicate.containerMap!.tanks![0];
    expect(() => firmwareBoards(duplicate, "owner")).toThrow("incomplete");
  });
  it("does not treat an accepted cancellation as finished until all owners stop", async () => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceCancelFirmwareUpdate.mockImplementation(({ taskId, serverId }) => ({
      data: { update: { taskId, serverId, status: V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_RUNNING } },
    }));
    expect(await firmware.cancel(attempt)).toEqual({ pending: true, finished: false, absent: [] });
  });
  it("keeps cancel failures visible and still attempts every owner", async () => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceCancelFirmwareUpdate.mockRejectedValueOnce(new Error("offline"));
    await expect(firmware.cancel(attempt)).rejects.toThrow("offline");
    expect(mock.firmwareServiceCancelFirmwareUpdate).toHaveBeenCalledTimes(2);
  });
  it("keeps owners that do not know the task unresolved when canceling", async () => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceCancelFirmwareUpdate.mockImplementation(({ taskId, serverId }) =>
      serverId === "owner-a"
        ? Promise.reject({ status: 404 })
        : { data: { update: { taskId, serverId, status: V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_RUNNING } } },
    );
    expect(await firmware.cancel(attempt)).toEqual({ pending: true, finished: false, absent: ["owner-a"] });
    mock.firmwareServiceCancelFirmwareUpdate.mockRejectedValue({ status: 404 });
    expect(await firmware.cancel(attempt)).toEqual({
      pending: false,
      finished: false,
      absent: ["owner-a", "owner-b"],
    });
  });
  it("reports a job that ended before the cancel arrived as finished, not canceled", async () => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceCancelFirmwareUpdate.mockImplementation(({ taskId, serverId }) => ({
      data: {
        update: {
          taskId,
          serverId,
          status:
            serverId === "owner-a"
              ? V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_COMPLETED_WITH_ERRORS
              : V2FirmwareUpdateStatus.FIRMWARE_UPDATE_STATUS_CANCELED,
        },
      },
    }));
    expect(await firmware.cancel(attempt)).toEqual({ pending: false, finished: true, absent: [] });
  });
  it("sends every byte of a multi-chunk image at the offsets the controller reassembles", async () => {
    const { mock, firmware, attempt } = setup();
    const bytes = firmwareTestImage("prod", "f1", 40_000);
    const image = {
      file: new File([bytes], "f1-app-prod.bin") as unknown as globalThis.File,
      version: "9.0.0",
      build: "prod" as const,
    };
    // Each 16 KiB chunk spans two of the 8,192-byte slices the base64 encoder works in.
    mock.firmwareServiceBeginFirmwareArtifactUpload.mockResolvedValue({
      data: { uploadId: "session", maxChunkBytes: 16384 },
    });
    const received = new Uint8Array(bytes.length);
    mock.firmwareServiceWriteFirmwareArtifactUploadChunk.mockImplementation((_path, chunk) => {
      const data = Uint8Array.from(atob(chunk.data), (character) => character.charCodeAt(0));
      received.set(data, Number(chunk.offset));
      return { data: { nextOffset: String(Number(chunk.offset) + data.length) } };
    });
    await firmware.install(image, attempt, vi.fn(), {}, vi.fn());
    expect(mock.firmwareServiceWriteFirmwareArtifactUploadChunk.mock.calls.map(([, chunk]) => chunk.offset)).toEqual([
      "0",
      "16384",
      "32768",
    ]);
    expect(Buffer.from(received).equals(Buffer.from(bytes))).toBe(true);
    expect(mock.firmwareServiceBeginFirmwareArtifactUpload.mock.calls[0][0].metadata.images).toEqual([
      { name: "f1-app-prod.bin", sizeBytes: "40000", sha256: createHash("sha256").update(bytes).digest("hex") },
    ]);
  });
  it("stops a canceled upload between chunks, releases its session, and never starts", async () => {
    const { mock, firmware, attempt, image } = setup();
    const controller = new AbortController();
    const write = mock.firmwareServiceWriteFirmwareArtifactUploadChunk;
    const acknowledge = write.getMockImplementation()!;
    write.mockImplementation((path, chunk) => {
      controller.abort();
      return acknowledge(path, chunk);
    });
    const persist = vi.fn();
    await expect(firmware.install(image, attempt, vi.fn(), { signal: controller.signal }, persist)).rejects.toThrow();
    expect(write).toHaveBeenCalledOnce();
    expect(mock.firmwareServiceAbortFirmwareArtifactUpload).toHaveBeenCalledOnce();
    // The session is released on a fresh signal: reusing the aborted one would silently leak it.
    expect(mock.firmwareServiceAbortFirmwareArtifactUpload.mock.calls[0][1].signal.aborted).toBe(false);
    expect(persist).not.toHaveBeenCalled();
    expect(mock.firmwareServiceStartFirmwareUpdate).not.toHaveBeenCalled();
  });
  it("does not save or start an upload canceled while it was being committed", async () => {
    const { mock, firmware, attempt, image } = setup();
    const controller = new AbortController();
    mock.firmwareServiceCommitFirmwareArtifactUpload.mockImplementation(() => {
      controller.abort();
      return { data: { artifact: { uploadId: "artifact" } } };
    });
    const persist = vi.fn();
    await expect(firmware.install(image, attempt, vi.fn(), { signal: controller.signal }, persist)).rejects.toThrow();
    expect(persist).not.toHaveBeenCalled();
    expect(mock.firmwareServiceStartFirmwareUpdate).not.toHaveBeenCalled();
  });
  it.each([400, 401, 403, 404, 429, 501])(
    "reports a Start the controller refused with %i as refused",
    async (status) => {
      const { mock, firmware, attempt, image } = setup();
      mock.firmwareServiceStartFirmwareUpdate.mockRejectedValue(
        Object.assign(new Error("hashboard update already running"), { status }),
      );
      const refused = await firmware.install(image, attempt, vi.fn(), {}, vi.fn()).catch((error: unknown) => error);
      expect(refused).toBeInstanceOf(CT1FirmwareStartRefusedError);
      expect((refused as Error).message).toBe("hashboard update already running");
      expect(mock.firmwareServiceStartFirmwareUpdate).toHaveBeenCalledOnce();
    },
  );
  it.each([409, 499, 500, 503, 504, undefined])("keeps a Start that failed with %s uncertain", async (status) => {
    const { mock, firmware, attempt, image } = setup();
    const failure = Object.assign(new Error("lost"), { status });
    mock.firmwareServiceStartFirmwareUpdate.mockRejectedValue(failure);
    const uncertain = await firmware.install(image, attempt, vi.fn(), {}, vi.fn()).catch((error: unknown) => error);
    expect(uncertain).toBe(failure);
    expect(uncertain).not.toBeInstanceOf(CT1FirmwareStartRefusedError);
    expect(mock.firmwareServiceStartFirmwareUpdate).toHaveBeenCalledOnce();
  });
});
