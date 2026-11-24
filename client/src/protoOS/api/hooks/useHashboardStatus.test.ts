import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useHashboardStatus } from "./useHashboardStatus";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { asAsicId } from "@/protoOS/store/contract";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetHashboardStatus = vi.fn();

const { mockGetHashboard, mockBatchAddHashboards, mockGetAsic, mockBatchAddAsics, mockSetState } = vi.hoisted(() => ({
  mockGetHashboard: vi.fn(),
  mockBatchAddHashboards: vi.fn(),
  mockGetAsic: vi.fn(),
  mockBatchAddAsics: vi.fn(),
  mockSetState: vi.fn(),
}));

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

vi.mock("@/protoOS/store", () => ({
  useMinerStore: {
    getState: () => ({
      hardware: {
        getHashboard: mockGetHashboard,
        batchAddHashboards: mockBatchAddHashboards,
        getAsic: mockGetAsic,
        batchAddAsics: mockBatchAddAsics,
      },
      telemetry: {
        asics: new Map(),
      },
    }),
    setState: mockSetState,
  },
}));

vi.mock("@/protoOS/store/hooks/useAuthRetry", () => ({
  useAuthRetry: () => {
    return ({ request, onSuccess, onError }: any) => {
      return request({ secure: false, headers: { Authorization: "Bearer " } })
        .then((result: any) => onSuccess?.(result))
        .catch((err: any) => onError?.(err));
    };
  },
}));

describe("useHashboardStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    (useMinerHosting as Mock).mockReturnValue({
      adapter: {
        getHashboardStatus: mockGetHashboardStatus,
      },
    });
    mockGetHashboardStatus.mockResolvedValue([]);
    (usePoll as Mock).mockImplementation(() => {});
  });

  test("fetches hashboard status for each supplied serial", async () => {
    renderHook(() =>
      useHashboardStatus({
        hashboardSerialNumbers: ["HB-1"],
        poll: false,
      }),
    );

    const pollArgs = (usePoll as Mock).mock.calls[0][0];

    await act(async () => {
      await pollArgs.fetchData();
    });

    expect(mockGetHashboardStatus).toHaveBeenCalledWith(["HB-1"], expect.any(Object));
  });

  // When useTelemetry has already created an ASIC entry (with index but no
  // row/column), useHashboardStatus must still patch row and column.
  // Skipping that update left the ASIC grid unable to render rows because
  // `getAsicsRows` filters out ASICs missing position data.
  test("merges row/column onto ASICs that already exist in the hardware store", async () => {
    mockGetHashboard.mockReturnValue(undefined);
    mockGetAsic.mockReturnValue({
      id: "serial:HB-1:asic:0",
      hashboardSerial: "HB-1",
      index: 0,
      hashboardIndex: 1,
    });
    mockGetHashboardStatus.mockResolvedValue([
      {
        serial: "HB-1",
        asicIds: ["serial:HB-1:asic:0"],
        asics: [{ id: "serial:HB-1:asic:0", hashboardSerial: "HB-1", index: 0, row: 2, column: 3 }],
        asicTelemetry: [{ asicId: "serial:HB-1:asic:0" }],
      },
    ]);

    renderHook(() =>
      useHashboardStatus({
        hashboardSerialNumbers: ["HB-1"],
        poll: false,
      }),
    );

    const pollArgs = (usePoll as Mock).mock.calls[0][0];

    await act(async () => {
      await pollArgs.fetchData();
    });

    expect(mockBatchAddAsics).toHaveBeenCalledWith([
      expect.objectContaining({
        id: "serial:HB-1:asic:0",
        hashboardSerial: "HB-1",
        index: 0,
        hashboardIndex: 1,
        row: 2,
        column: 3,
      }),
    ]);
    expect(mockBatchAddHashboards).toHaveBeenCalledWith([
      expect.objectContaining({ serial: "HB-1", asicIds: ["serial:HB-1:asic:0"] }),
    ]);
  });

  test("preserves the device-local index when creating ASIC hardware", async () => {
    mockGetHashboard.mockReturnValue(undefined);
    mockGetAsic.mockReturnValue(undefined);
    mockGetHashboardStatus.mockResolvedValue([
      {
        serial: "HB-1",
        asicIds: ["serial:HB-1:asic:0"],
        asics: [{ id: "serial:HB-1:asic:0", hashboardSerial: "HB-1", index: 0, row: 0, column: 0 }],
        asicTelemetry: [{ asicId: "serial:HB-1:asic:0" }],
      },
    ]);

    renderHook(() =>
      useHashboardStatus({
        hashboardSerialNumbers: ["HB-1"],
        poll: false,
      }),
    );

    const pollArgs = (usePoll as Mock).mock.calls[0][0];

    await act(async () => {
      await pollArgs.fetchData();
    });

    expect(mockBatchAddAsics).toHaveBeenCalledWith([
      expect.objectContaining({
        id: "serial:HB-1:asic:0",
        hashboardSerial: "HB-1",
        index: 0,
        row: 0,
        column: 0,
      }),
    ]);
  });

  test("writes canonical voltage and frequency samples into the telemetry store", async () => {
    // The real store runs its setState updater against Immer draft state; drive
    // the updater against a plain state object whose telemetry.asics is a real Map.
    const asics = new Map();
    mockSetState.mockImplementation((updater: (state: any) => void) => updater({ telemetry: { asics } }));
    mockGetHashboard.mockReturnValue(undefined);
    mockGetAsic.mockReturnValue(undefined);
    mockGetHashboardStatus.mockResolvedValue([
      {
        serial: "HB-1",
        asicIds: ["serial:HB-1:asic:0"],
        asics: [{ id: "serial:HB-1:asic:0", hashboardSerial: "HB-1", index: 0, row: 0, column: 0 }],
        asicTelemetry: [
          {
            asicId: "serial:HB-1:asic:0",
            voltage: { value: 0.3, units: "V" },
            frequency: { value: 650, units: "MHz" },
          },
        ],
      },
    ]);

    renderHook(() =>
      useHashboardStatus({
        hashboardSerialNumbers: ["HB-1"],
        poll: false,
      }),
    );

    const pollArgs = (usePoll as Mock).mock.calls[0][0];

    await act(async () => {
      await pollArgs.fetchData();
    });

    const asic = asics.get(asAsicId("serial:HB-1:asic:0"));
    expect(asic?.voltage?.latest).toEqual({ value: 0.3, units: "V" });
    expect(asic?.frequency?.latest).toEqual({ value: 650, units: "MHz" });
    expect(mockSetState).toHaveBeenCalledTimes(1);
  });
});
