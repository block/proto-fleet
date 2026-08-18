import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useMiningTarget } from "./useMiningTarget";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useMinerStore } from "@/protoOS/store";
import { ROOT_MODULE_ID } from "@/protoOS/store/contract";

const mockGetMiningTarget = vi.fn();

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

describe("useMiningTarget", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useMinerStore.getState().resetDeviceData();
    (useMinerHosting as Mock).mockReturnValue({
      adapter: {
        getMiningTarget: mockGetMiningTarget,
      },
    });
  });

  test("does not automatically retry a failed initial request", async () => {
    mockGetMiningTarget.mockRejectedValueOnce(new Error("offline")).mockReturnValue(new Promise(() => undefined));

    renderHook(() => useMiningTarget());

    await waitFor(() => {
      expect(mockGetMiningTarget).toHaveBeenCalled();
      expect(useMinerStore.getState().miningTarget.byModule.get(ROOT_MODULE_ID)?.resource.status).toBe("error");
    });

    expect(mockGetMiningTarget).toHaveBeenCalledTimes(1);
  });
});
