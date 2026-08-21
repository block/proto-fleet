import { act, renderHook, waitFor } from "@testing-library/react";
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

  test("keeps the target loading while an update request is in flight", async () => {
    mockGetMiningTarget.mockResolvedValue({
      moduleId: ROOT_MODULE_ID,
      valueWatts: 4000,
      resource: { status: "ready" },
    });
    // Never resolves: the update stays in flight so we can observe the interim status.
    const mockUpdateMiningTarget = vi.fn().mockReturnValue(new Promise(() => undefined));
    (useMinerHosting as Mock).mockReturnValue({
      adapter: { getMiningTarget: mockGetMiningTarget, updateMiningTarget: mockUpdateMiningTarget },
    });

    const { result } = renderHook(() => useMiningTarget());
    await waitFor(() => expect(mockGetMiningTarget).toHaveBeenCalled());

    act(() => {
      result.current.updateMiningTarget({ valueWatts: 5000 });
    });

    // setError(null) resolves the resource to "ready", so it must not run after
    // setPending(true) — otherwise the spinner-driving "loading" status is wiped
    // while the (multi-second) request is still pending.
    await waitFor(() => expect(mockUpdateMiningTarget).toHaveBeenCalled());
    expect(useMinerStore.getState().miningTarget.byModule.get(ROOT_MODULE_ID)?.resource.status).toBe("loading");
  });
});
