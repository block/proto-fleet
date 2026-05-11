import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useHashboardStatus } from "./useHashboardStatus";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetHashboardStatus = vi.fn();

const mockGetHashboard = vi.fn();
const mockAddHashboard = vi.fn();
const mockGetAsic = vi.fn();
const mockLinkAsicToHashboard = vi.fn();
const mockBatchAddAsics = vi.fn();
const mockSetState = vi.fn();

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
        addHashboard: mockAddHashboard,
        getAsic: mockGetAsic,
        linkAsicToHashboard: mockLinkAsicToHashboard,
        batchAddAsics: mockBatchAddAsics,
      },
      telemetry: {
        asics: new Map(),
      },
    }),
    setState: mockSetState,
  },
  getAsicId: (serial: string, index: number) => `${serial}-${index}`,
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
      api: {
        getHashboardStatus: mockGetHashboardStatus,
      },
    });
    mockGetHashboardStatus.mockResolvedValue({ data: {} });
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

    expect(mockGetHashboardStatus).toHaveBeenCalledWith({ hbSn: "HB-1" }, expect.any(Object));
  });

  // When useTelemetry has already created an ASIC entry (with index but no
  // row/column), useHashboardStatus must still patch row and column.
  // Skipping that update left the ASIC grid unable to render rows because
  // `getAsicsRows` filters out ASICs missing position data.
  test("merges row/column onto ASICs that already exist in the hardware store", async () => {
    mockGetHashboard.mockReturnValue(undefined);
    mockGetAsic.mockReturnValue({
      id: "HB-1-0",
      hashboardSerial: "HB-1",
      index: 0,
      hashboardIndex: 1,
    });
    mockGetHashboardStatus.mockResolvedValue({
      data: {
        "hashboard-stats": {
          asics: [{ index: 0, row: 2, column: 3 }],
        },
      },
    });

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
        id: "HB-1-0",
        hashboardSerial: "HB-1",
        index: 0,
        hashboardIndex: 1,
        row: 2,
        column: 3,
      }),
    ]);
    // linkAsicToHashboard is a no-op when the entry pre-existed (the hashboard
    // is updated separately with the full asicIds list).
    expect(mockLinkAsicToHashboard).not.toHaveBeenCalled();
  });
});
