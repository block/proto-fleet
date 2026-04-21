import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useHashboardStatus } from "./useHashboardStatus";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetHashboardStatus = vi.fn();

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
        getHashboard: vi.fn(),
        addHashboard: vi.fn(),
        getAsic: vi.fn(),
        linkAsicToHashboard: vi.fn(),
        batchAddAsics: vi.fn(),
      },
      telemetry: {
        asics: new Map(),
      },
    }),
    setState: vi.fn(),
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
});
