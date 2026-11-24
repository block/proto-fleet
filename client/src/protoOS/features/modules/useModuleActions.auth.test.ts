import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { buildModuleRows } from "./moduleListModel";
import { useModuleActions } from "./useModuleActions";
import { tanksFixture } from "@/protoOS/features/tanks/Tanks.fixtures";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({ command: vi.fn(), telemetry: vi.fn(), authErrors: vi.fn() }));
const adapter = { runHashboardAction: mocks.command, getCurrentTelemetry: mocks.telemetry };
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter, isFleetHosted: false }),
}));
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mocks.authErrors }),
}));

beforeEach(() => {
  vi.clearAllMocks();
  vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());

test.each(["timeout", "device change"])("%s during auth refresh prevents a late command retry", async (cancel) => {
  const fixture = tanksFixture(1, 1);
  const state = useMinerStore.getState();
  state.resetDeviceData();
  state.hardware.replaceHardware(fixture.hardware);
  state.telemetry.mergeTelemetry(fixture.telemetry);
  const rows = buildModuleRows(fixture.topology, fixture.telemetry);
  const { result } = renderHook(() => useModuleActions(rows, fixture.topology));
  mocks.command.mockRejectedValueOnce({ status: 401, error: { message: "Unauthorized" } });
  let finishRefresh!: () => void;
  mocks.authErrors.mockImplementation(
    ({ onSuccess }: { onSuccess: () => Promise<void> }) =>
      new Promise<void>((resolve) => {
        finishRefresh = () => resolve(onSuccess());
      }),
  );
  act(() => result.current.actionsFor([rows[0].id])[0].onClick());
  let command!: Promise<void>;
  await act(async () => {
    command = result.current.confirm();
  });
  expect(mocks.authErrors).toHaveBeenCalledTimes(1);
  if (cancel === "timeout") {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30_000);
    });
    expect(result.current.outcome?.error).toContain("outcome is unknown");
  } else {
    await act(async () => {
      useMinerStore.getState().resetDeviceData();
    });
    expect(result.current.request).toBeUndefined();
  }
  expect(result.current.pending).toBe(false);
  await command;
  await act(async () => {
    finishRefresh();
  });
  expect(mocks.command).toHaveBeenCalledTimes(1);
  expect(mocks.telemetry).not.toHaveBeenCalled();
  expect(result.current.outcome?.result).toBeUndefined();
  expect(vi.getTimerCount()).toBe(0);
});
