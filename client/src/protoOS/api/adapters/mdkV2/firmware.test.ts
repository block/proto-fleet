import { describe, expect, it, vi } from "vitest";
import { Buffer, File } from "node:buffer";
import { createHash } from "node:crypto";
import {
  type HashboardFirmwareAttempt,
  HashboardFirmwareCancelError,
  HashboardFirmwareStartRefusedError,
} from "../hashboardFirmware";
import type { MdkV2Client } from "./connectClient";
import { createHashboardFirmware, firmwareBoards } from "./firmware";
import { firmwareTestImage, firmwareTestMap } from "./firmwareTestFixtures";
import {
  V2FirmwareTargetStatus,
  V2FirmwareUpdateStatus,
  type V2GetContainerMapResponse,
} from "./generated/generatedApi";

const tanksOf = (map: V2GetContainerMapResponse) => map.containerMap!.tanks!;
/** The fixture map with `count` modules in every tank, reported as a complete 1 x `count` grid. */
const mapWithModules = (count: number) => {
  const map = firmwareTestMap();
  tanksOf(map).forEach((tank, tankIndex) => {
    const [first] = tank.modules!;
    tank.moduleLayout = { rows: 1, columns: count };
    tank.modules = Array.from({ length: count }, (_, moduleIndex) => ({
      id: moduleIndex + 1,
      slots: first.slots!.map((slot, position) => ({
        ...slot,
        // Lower-case hex, as a controller may report it; targets are always sent upper-case.
        hashboard: {
          ...slot.hashboard,
          macAddress: `02:00:00:0${tankIndex}:${moduleIndex.toString(16).padStart(2, "0")}:0${position}`,
        },
      })),
    }));
  });
  return map;
};
const twoModuleMap = () => mapWithModules(2);

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
  const firmware = createHashboardFirmware(mock as unknown as MdkV2Client, (params) => ({
    ...params,
    headers: { "X-Frigate-Actor": "proto-os" },
  }));
  const attempt: HashboardFirmwareAttempt = {
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
    expect(result.outcome).toBe("completedWithErrors");
    expect(result.boards).toHaveLength(18);
    expect(result.boards.filter((board) => board.message === "flash failed")).toHaveLength(2);
    mock.firmwareServiceGetFirmwareUpdate.mockRejectedValueOnce({ status: 404 });
    const missing = await firmware.progress(attempt);
    expect(missing.outcome).toBeUndefined();
    expect(missing.errors[0]).toContain("Update not found");
    expect((await firmware.cancel(attempt)).pending).toBe(false);
    expect(mock.firmwareServiceCancelFirmwareUpdate).toHaveBeenCalledTimes(2);
    expect(mock.firmwareServiceCancelFirmwareUpdate).toHaveBeenCalledWith(
      { taskId: "client-task", serverId: "owner-b" },
      expect.anything(),
    );
  });
  it.each([
    [["COMPLETED", "COMPLETED"], "completed"],
    [["COMPLETED", "COMPLETED_WITH_ERRORS"], "completedWithErrors"],
    [["CANCELED", "COMPLETED"], "canceled"],
    [["CANCELED", "INTERRUPTED"], "interrupted"],
    [["FAILED", "COMPLETED_WITH_ERRORS"], "failed"],
    [["COMPLETED", "RUNNING"], undefined],
    [["CANCELED", "PENDING"], undefined],
  ] as const)("reports owners that ended as %j with outcome %s", async ([ownerA, ownerB], outcome) => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceGetFirmwareUpdate.mockImplementation(({ serverId, taskId }) => ({
      data: {
        update: {
          taskId,
          serverId,
          status: V2FirmwareUpdateStatus[`FIRMWARE_UPDATE_STATUS_${serverId === "owner-a" ? ownerA : ownerB}`],
          targets: attempt.boards
            .filter((board) => board.owner === serverId)
            .map((board) => ({
              macAddress: board.mac,
              status: V2FirmwareTargetStatus.FIRMWARE_TARGET_STATUS_FLASHING,
            })),
        },
      },
    }));
    expect((await firmware.progress(attempt)).outcome).toBe(outcome);
  });
  it.each([
    ["COMPLETED", "completedWithErrors"],
    ["FAILED", "failed"],
    ["RUNNING", undefined],
  ] as const)("reports a board missing from a job that is %s with outcome %s", async (status, outcome) => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceGetFirmwareUpdate.mockImplementation(({ serverId, taskId }) => ({
      data: {
        update: {
          taskId,
          serverId,
          status: V2FirmwareUpdateStatus[`FIRMWARE_UPDATE_STATUS_${serverId === "owner-a" ? status : "COMPLETED"}`],
          targets: attempt.boards
            .filter((board) => board.owner === serverId)
            .slice(serverId === "owner-a" ? 1 : 0)
            .map((board) => ({
              macAddress: board.mac,
              status: V2FirmwareTargetStatus.FIRMWARE_TARGET_STATUS_SUCCEEDED,
            })),
        },
      },
    }));
    const result = await firmware.progress(attempt);
    // An ended job never gains targets. The board is named, and the update ends so that it can be dismissed;
    // while the job is still running the board may yet be reported.
    expect(result.outcome).toBe(outcome);
    expect(result.errors).toEqual([`${attempt.boards[0].label}: missing from update status`]);
    expect(result.boards.find((board) => board.mac === attempt.boards[0].mac)?.status).toBe("Status unavailable");
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
  it.each<[string, (map: V2GetContainerMapResponse) => void]>([
    [
      "a tank that maps fewer modules than its grid",
      (map) => {
        tanksOf(map)[0].modules!.pop();
      },
    ],
    [
      "a tank with no reported grid and fewer than 16 modules",
      (map) => {
        delete tanksOf(map)[0].moduleLayout;
      },
    ],
    [
      "two modules of one tank sharing a number",
      (map) => {
        tanksOf(map)[0].modules![1].id = 1;
      },
    ],
    [
      "a module numbered outside its tank's grid",
      (map) => {
        tanksOf(map)[0].modules![1].id = 3;
      },
    ],
    [
      "two slots of one module reporting the same position",
      (map) => {
        const slot = tanksOf(map)[0].modules![0].slots![1];
        slot.configuredLocation = { hashboardPosition: 1 };
        slot.hashboard!.location = { hashboardPosition: 1 };
      },
    ],
    [
      "a board reported in a position other than its slot's",
      (map) => {
        tanksOf(map)[0].modules![0].slots![0].configuredLocation = { hashboardPosition: 2 };
      },
    ],
    [
      "two boards with the same MAC address",
      (map) => {
        const [first, second] = tanksOf(map)[0].modules![0].slots!;
        second.hashboard!.macAddress = first.hashboard!.macAddress;
      },
    ],
    [
      "a malformed MAC address",
      (map) => {
        tanksOf(map)[0].modules![0].slots![0].hashboard!.macAddress = "02:00:00:00:00";
      },
    ],
    [
      "an occupied slot that reports no board",
      (map) => {
        delete tanksOf(map)[0].modules![0].slots![0].hashboard;
      },
    ],
    [
      "unlocated inventory that is not reported as one module 0",
      (map) => {
        tanksOf(map).unshift({ id: 0, modules: [{ id: 1, slots: [] }] });
      },
    ],
  ])("refuses a board map with %s", (_case, change) => {
    const map = twoModuleMap();
    expect(firmwareBoards(map, "owner")).toHaveLength(36);
    change(map);
    expect(() => firmwareBoards(map, "owner")).toThrow("incomplete");
  });
  it("expects 16 module positions in a tank that reports no grid", () => {
    const map = mapWithModules(16);
    for (const tank of tanksOf(map)) delete tank.moduleLayout;
    const boards = firmwareBoards(map, "owner");
    expect(boards).toHaveLength(288);
    expect(boards.map((board) => board.mac)).toContain("02:00:00:05:0F:02");
    tanksOf(map)[5].modules!.pop();
    expect(() => firmwareBoards(map, "owner")).toThrow("incomplete");
    // A tank holds at most 16 modules, even when it reports and fills a larger grid.
    expect(() => firmwareBoards(mapWithModules(17), "owner")).toThrow("incomplete");
  });
  it("targets every occupied board of a complete map and labels it by position", () => {
    const map = twoModuleMap();
    expect(firmwareBoards(map, "owner").map((board) => board.label)).toContain("Tank 6 · Module 2 · Board 3");
    // An empty slot belongs to a complete map but not to the update.
    tanksOf(map)[0].modules![1].slots![2].occupied = false;
    expect(firmwareBoards(map, "owner")).toHaveLength(35);
    for (const tank of tanksOf(map))
      for (const module of tank.modules!) for (const slot of module.slots!) slot.occupied = false;
    expect(() => firmwareBoards(map, "owner")).toThrow("no occupied hashboards");
  });
  it("targets unlocated boards by MAC, owned by the serving controller when the map names no owner", () => {
    const map = firmwareTestMap();
    tanksOf(map).unshift({
      id: 0,
      modules: [{ id: 0, slots: [{ occupied: true, hashboard: { macAddress: "02:00:00:00:ff:0a" } }] }],
    });
    const boards = firmwareBoards(map, "serving");
    expect(boards).toHaveLength(19);
    expect(boards[0]).toEqual({ mac: "02:00:00:00:FF:0A", owner: "serving", label: "02:00:00:00:FF:0A" });
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
  it("reports each owner that could not answer a cancel on its own line, worded as its status error is", async () => {
    const { mock, firmware, attempt } = setup();
    mock.firmwareServiceCancelFirmwareUpdate.mockRejectedValue(new Error("offline"));
    mock.firmwareServiceGetFirmwareUpdate.mockRejectedValue(new Error("offline"));
    const failure = await firmware.cancel(attempt).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(HashboardFirmwareCancelError);
    const lines = ["owner-a: offline", "owner-b: offline"];
    expect([...(failure as HashboardFirmwareCancelError).failures].sort()).toEqual(lines);
    // The dialog shows a line once however many requests reported it, so both must use the same words.
    expect([...(await firmware.progress(attempt)).errors].sort()).toEqual(lines);
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
    // Two full 16 KiB chunks and a short last one.
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
      expect(refused).toBeInstanceOf(HashboardFirmwareStartRefusedError);
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
    expect(uncertain).not.toBeInstanceOf(HashboardFirmwareStartRefusedError);
    expect(mock.firmwareServiceStartFirmwareUpdate).toHaveBeenCalledOnce();
  });
});
