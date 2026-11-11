import { describe, expect, it, vi } from "vitest";
import { File } from "node:buffer";
import type { CT1FirmwareAttempt } from "../ct1Firmware";
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
    expect(await firmware.cancel(attempt)).toEqual({ pending: true, boards: attempt.boards });
  });
  it("keeps cancel failures visible and still attempts every owner", async () => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceCancelFirmwareUpdate.mockRejectedValueOnce(new Error("offline"));
    await expect(firmware.cancel(attempt)).rejects.toThrow("offline");
    expect(mock.firmwareServiceCancelFirmwareUpdate).toHaveBeenCalledTimes(2);
  });
});
