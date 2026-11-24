import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, type Mock, test, vi } from "vitest";
import { useTelemetry } from "./useTelemetry";
import { moduleFixture } from "@/protoOS/api/adapters/mdkV2/moduleFixtures";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useAuthErrors } from "@/protoOS/store/hooks/useAuth";
import { useMinerPsuById } from "@/protoOS/store/hooks/useMiner";
import { beginStoreWrite } from "@/protoOS/store/storeWriteGuard";
import useMinerStore from "@/protoOS/store/useMinerStore";
import { createEmptyTelemetryState } from "@/protoOS/store/utils/createEmptyTelemetryState";
import { usePoll } from "@/shared/hooks/usePoll";

const mockGetCurrentTelemetry = vi.fn();
let pollFetch: (() => Promise<void>) | undefined;

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: vi.fn(),
}));

vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: vi.fn(),
}));

vi.mock("@/protoOS/store/storeWriteGuard", () => ({
  beginStoreWrite: vi.fn(() => () => true),
}));

vi.mock("@/shared/hooks/usePoll", () => ({
  usePoll: vi.fn(),
}));

describe("useTelemetry", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    pollFetch = undefined;
    mockGetCurrentTelemetry.mockResolvedValue(createEmptyTelemetryState());
    (useMinerHosting as Mock).mockReturnValue({
      adapter: { getCurrentTelemetry: mockGetCurrentTelemetry },
    });
    (useAuthErrors as Mock).mockReturnValue({ handleAuthErrors: vi.fn() });
    (usePoll as Mock).mockImplementation(({ fetchData }) => {
      pollFetch = fetchData;
    });
  });

  test("polling discovers a module PSU and a failed refresh preserves its last snapshot", async () => {
    const store = useMinerStore.getState();
    store.resetDeviceData();
    const initial = moduleFixture("noPsu");
    store.hardware.replaceHardware(initial.hardware);
    store.telemetry.mergeTelemetry(initial.telemetry);
    const ready = moduleFixture("healthy");
    const module = [...ready.hardware.topology!.modules.values()][0];
    const id = module.psuIds[0];
    mockGetCurrentTelemetry.mockResolvedValue(ready.telemetry);
    const { result } = renderHook(() => {
      useTelemetry({ level: ["hashboard"], poll: false });
      return useMinerPsuById(id);
    });
    expect(result.current).toBeNull();
    await act(async () => {
      await pollFetch?.();
    });
    expect(result.current?.inputPower?.latest?.value).toBe(4282);
    expect(useMinerStore.getState().hardware.topology?.modules.get(module.id)?.psuIds).toEqual([id]);
    const before = useMinerStore.getState();
    mockGetCurrentTelemetry.mockRejectedValue(new Error("Connection lost"));
    await act(async () => {
      await pollFetch?.();
    });
    expect(useMinerStore.getState().hardware).toBe(before.hardware);
    expect(useMinerStore.getState().telemetry).toBe(before.telemetry);
    expect(result.current?.inputPower?.latest?.value).toBe(4282);
  });

  test("uses a stable write guard key scoped to the requested telemetry levels", async () => {
    renderHook(() => useTelemetry({ level: ["psu", "miner"], poll: false }));
    await act(async () => {
      await pollFetch?.();
    });

    expect(beginStoreWrite).toHaveBeenCalledWith("telemetry:current:miner,psu");

    renderHook(() => useTelemetry({ level: ["asic"], poll: false }));
    await act(async () => {
      await pollFetch?.();
    });

    expect(beginStoreWrite).toHaveBeenLastCalledWith("telemetry:current:asic");
  });
});
