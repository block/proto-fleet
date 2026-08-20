import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useCoolingStatus } from "./useCoolingStatus";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthRetry, useMinerStore } from "@/protoOS/store";
import { ROOT_MODULE_KEY } from "@/protoOS/store/contract";
import { getFanId } from "@/protoOS/store/utils/entityIds";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetCoolingStatus = vi.fn();
const mockSetCoolingMode = vi.fn();
let pollFetch: (() => Promise<void>) | undefined;

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/protoOS/store")>()),
  useAuthRetry: vi.fn(),
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

describe("useCoolingStatus", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    pollFetch = undefined;
    useMinerStore.getState().resetDeviceData();
    useMinerStore.getState().telemetry.updateCoolingMode("automatic");

    (useMinerHosting as Mock).mockReturnValue({
      adapter: {
        getCoolingStatus: mockGetCoolingStatus,
        setCoolingMode: mockSetCoolingMode,
      },
    });
    (useAuthRetry as Mock).mockReturnValue(
      async ({
        request,
        onSuccess,
        onError,
      }: {
        request: (params: object) => Promise<void>;
        onSuccess: () => void;
        onError: (error: unknown) => void;
      }) => {
        try {
          await request({});
          onSuccess();
        } catch (error) {
          onError(error);
        }
      },
    );
    (usePoll as Mock).mockImplementation(({ fetchData }) => {
      pollFetch = fetchData;
    });
  });

  test("keeps a successful command authoritative over a stale concurrent poll", async () => {
    let resolveCommand: (() => void) | undefined;
    let commandResolved = false;
    mockSetCoolingMode.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveCommand = () => {
          commandResolved = true;
          resolve();
        };
      }),
    );
    mockGetCoolingStatus.mockImplementation(async () => ({
      mode: commandResolved ? "manual" : "automatic",
      fans: [],
    }));

    const { result } = renderHook(() => useCoolingStatus({ poll: true }));
    let command: Promise<void> | undefined;
    act(() => {
      command = result.current.setCooling({ mode: "manual" });
    });

    await waitFor(() => expect(mockSetCoolingMode).toHaveBeenCalledWith("manual", {}));
    await act(async () => {
      await pollFetch?.();
    });
    expect(useMinerStore.getState().telemetry.coolingModeByOwner.get(ROOT_MODULE_KEY)).toBe("automatic");

    await act(async () => {
      resolveCommand?.();
      await command;
    });

    await waitFor(() => {
      expect(useMinerStore.getState().telemetry.coolingModeByOwner.get(ROOT_MODULE_KEY)).toBe("manual");
    });
  });

  test("clears stale live fan readings when a cooling snapshot omits a fan", async () => {
    useMinerStore.getState().telemetry.updateFanTelemetry(1, {
      slot: 1,
      rpm: { latest: { value: 4_800, units: "RPM" } },
      percentage: { latest: { value: 75, units: "%" } },
      minRpm: { latest: { value: 1_200, units: "RPM" } },
    });
    mockGetCoolingStatus.mockResolvedValue({ mode: "automatic", fans: [] });

    renderHook(() => useCoolingStatus({ poll: true }));
    await act(async () => {
      await pollFetch?.();
    });

    const fan = useMinerStore.getState().telemetry.fans.get(getFanId(1));
    expect(fan?.rpm?.latest).toBeUndefined();
    expect(fan?.speedPercent?.latest).toBeUndefined();
    expect(fan?.minimumRpm?.latest?.value).toBe(1_200);
  });
});
