import { act, renderHook } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import { removePoolAttempt, savePoolAttempt } from "./attemptStorage";
import { usePoolAssignmentActions } from "./usePoolAssignmentActions";
import type { PoolAssignmentAttempt } from "@/protoOS/api/adapters/poolAssignment";

vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: { poolAssignment: {} }, isFleetHosted: false }),
}));
vi.mock("@/protoOS/store/useMinerStore", () => ({
  default: (select: (state: unknown) => unknown) =>
    select({ systemInfo: { data: { identity: { deviceId: "ct1" } } }, connectionGeneration: 1 }),
}));

const attempt: PoolAssignmentAttempt = {
  id: "attempt",
  macs: ["aa:bb:cc:00:00:01"],
  destinationRef: "proto-os:pools:test",
  pools: [],
  configurationConfirmed: false,
  operationIds: [],
  phase: "Saving pools",
};

test("reads recovery records on storage changes instead of on telemetry renders", () => {
  const key = "proto-os:pool-assignment:ct1:attempt";
  localStorage.setItem("unrelated", "value");
  const scan = vi.spyOn(Storage.prototype, "key");
  try {
    const { result, rerender } = renderHook(usePoolAssignmentActions);
    expect(result.current.recoveryButton).toBeNull();
    scan.mockClear();
    rerender();
    expect(scan).not.toHaveBeenCalled();

    act(() => savePoolAttempt("ct1", attempt));
    expect(result.current.recoveryButton).not.toBeNull();
    act(() => removePoolAttempt("ct1", attempt.id));
    expect(result.current.recoveryButton).toBeNull();

    act(() => {
      const record = JSON.stringify(attempt);
      localStorage.setItem(key, record);
      window.dispatchEvent(new StorageEvent("storage", { key, newValue: record, storageArea: localStorage }));
    });
    expect(result.current.recoveryButton).not.toBeNull();
  } finally {
    scan.mockRestore();
    localStorage.removeItem("unrelated");
    localStorage.removeItem(key);
  }
});
