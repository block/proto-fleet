import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { useErrors } from "./useErrors";
import { useMinerStatusSummary } from "@/protoOS/hooks/status";
import { asModuleId, type MinerError } from "@/protoOS/store/contract";
import useMinerStore from "@/protoOS/store/useMinerStore";

const mocks = vi.hoisted(() => ({
  adapter: null as { getErrors: ReturnType<typeof vi.fn> } | null,
  getErrors: vi.fn(),
  handleAuthErrors: vi.fn(),
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({ useMinerHosting: () => ({ adapter: mocks.adapter }) }));
vi.mock("@/protoOS/store/hooks/useAuth", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mocks.handleAuthErrors }),
}));
const failures: MinerError[] = [
  {
    id: "hashboard-fault",
    code: "hashboard-fault",
    message: "Hashboard fault",
    severity: "critical",
    owner: { kind: "module", id: asModuleId("module:1") },
    source: { kind: "hashboard", slot: 1 },
  },
  {
    id: "psu-fault",
    code: "psu-fault",
    message: "PSU fault",
    severity: "critical",
    owner: { kind: "module", id: asModuleId("module:1") },
    source: { kind: "psu", slot: 1 },
  },
];
beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  mocks.adapter = null;
  mocks.getErrors.mockResolvedValue(failures);
  useMinerStore.getState().resetDeviceData();
});
afterEach(() => vi.useRealTimers());

test("updates the header as soon as the adapter resolves, without opening its status dialog or waiting for a poll", async () => {
  const { result, rerender, unmount } = renderHook(() => {
    useErrors({ poll: true, pollIntervalMs: 15_000 });
    return useMinerStatusSummary();
  });
  expect(mocks.getErrors).not.toHaveBeenCalled();
  mocks.adapter = { getErrors: mocks.getErrors };
  rerender();
  await act(async () => {});
  expect(mocks.getErrors).toHaveBeenCalledOnce();
  expect(result.current).toBe("Multiple failures");
  // Subsequent refreshes still run once per interval and can clear failures.
  mocks.getErrors.mockResolvedValue([]);
  await act(() => vi.advanceTimersByTimeAsync(15_000));
  expect(mocks.getErrors).toHaveBeenCalledTimes(2);
  expect(result.current).toBe("Hashing");
  const signal = mocks.getErrors.mock.calls[1][0].signal as AbortSignal;
  unmount();
  expect(signal.aborted).toBe(true);
  await act(() => vi.advanceTimersByTimeAsync(15_000));
  expect(mocks.getErrors).toHaveBeenCalledTimes(2);
});

test("respects disabled reads when the adapter resolves and fetches once when enabled", async () => {
  const { rerender } = renderHook(({ enabled }) => useErrors({ enabled, poll: false }), {
    initialProps: { enabled: false },
  });
  mocks.adapter = { getErrors: mocks.getErrors };
  rerender({ enabled: false });
  await act(async () => {});
  expect(mocks.getErrors).not.toHaveBeenCalled();
  rerender({ enabled: true });
  await act(async () => {});
  expect(mocks.getErrors).toHaveBeenCalledOnce();
  await act(() => vi.advanceTimersByTimeAsync(30_000));
  expect(mocks.getErrors).toHaveBeenCalledOnce();
});
