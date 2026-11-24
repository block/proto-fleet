import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, test, vi } from "vitest";
import { readPoolAttempts, removePoolAttempt, savePoolAttempt } from "./attemptStorage";
import { usePoolAssignmentActions } from "./usePoolAssignmentActions";
import { hasWorkerNameUpdate, startWorkerNameUpdate, useWorkerNameUpdateScope } from "./workerNameUpdates";
import type {
  PoolAssignmentAttempt,
  PoolAssignmentService,
  PoolAssignmentStatus,
  WorkerNameReview,
} from "@/protoOS/api/adapters/poolAssignment";

const hosting = vi.hoisted(() => ({
  generation: 1,
  service: { applyWorkerNames: vi.fn<PoolAssignmentService["applyWorkerNames"]>(), status: vi.fn() },
}));
vi.mock("@/protoOS/contexts/MinerHostingContext", () => ({
  useMinerHosting: () => ({ adapter: { poolAssignment: hosting.service }, isFleetHosted: false }),
}));
vi.mock("@/protoOS/store/useMinerStore", () => ({
  default: (select: (state: unknown) => unknown) =>
    select({ systemInfo: { data: { identity: { deviceId: "ct1" } } }, connectionGeneration: hosting.generation }),
}));

beforeEach(() => {
  Object.defineProperty(navigator, "locks", {
    configurable: true,
    value: {
      request: (_key: string, _options: unknown, callback: () => Promise<PoolAssignmentStatus>) => callback(),
    },
  });
});

const attempt: PoolAssignmentAttempt = {
  id: "attempt",
  macs: ["aa:bb:cc:00:00:01"],
  destinationRef: "proto-os:pools:test",
  pools: [],
  configurationConfirmed: false,
  operationIds: [],
  phase: "Saving pools",
};

const review: WorkerNameReview = {
  groups: [],
  unchangedCount: 0,
  capacity: { groupsUsed: 1, maxGroups: 4, maxBoards: 4, maxPools: 3 },
};

test("reads recovery records on storage changes instead of on telemetry renders", () => {
  const key = "proto-os:pool-assignment:ct1:attempt";
  localStorage.setItem("unrelated", "value");
  const scan = vi.spyOn(Storage.prototype, "key");
  try {
    const { result, rerender } = renderHook(() => usePoolAssignmentActions({ showRecovery: true }));
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

test("module actions do not show pool recovery controls for tracked worker name updates", () => {
  const { result } = renderHook(usePoolAssignmentActions);
  const workerUpdate = { ...attempt, unsubmittedWorkerNameBoards: 0 };
  try {
    act(() => savePoolAttempt("ct1", workerUpdate));
    expect(result.current.recoveryButton).toBeNull();
  } finally {
    removePoolAttempt("ct1", workerUpdate.id);
  }
});

test("a pending worker name update frees the page and blocks pool actions after the editor leaves", async () => {
  const first = renderHook(usePoolAssignmentActions);
  const second = renderHook(usePoolAssignmentActions);
  const target = { macs: attempt.macs };
  const [editPool] = second.result.current.action(target);
  const [, updateWorkerName] = first.result.current.action(target);
  act(() => updateWorkerName.onClick?.());
  expect(first.result.current.open).toBe(true);

  const dialog = first.result.current.dialog;
  expect(dialog).not.toBeNull();
  let finishSave!: (result: PoolAssignmentStatus) => void;
  const saving = new Promise<PoolAssignmentStatus>((resolve) => {
    finishSave = resolve;
  });
  const service = { applyWorkerNames: () => saving, status: vi.fn() };
  let update: ReturnType<typeof startWorkerNameUpdate>;
  act(() => {
    update = startWorkerNameUpdate(service, "ct1", dialog!.props.workerNameUpdateOwner, review);
  });
  expect(first.result.current.open).toBe(false);
  expect(first.result.current.dialog).not.toBeNull();
  expect(second.result.current.action(target).every((action) => action.disabled)).toBe(true);
  act(() => editPool.onClick?.());
  expect(second.result.current.open).toBe(false);

  first.unmount();
  expect(second.result.current.action(target).every((action) => action.disabled)).toBe(true);
  await act(async () => {
    finishSave({ state: "saved", message: "Saved", confirmed: 1, total: 1, blockers: [] });
    await update;
  });
  expect(second.result.current.action(target).every((action) => !action.disabled)).toBe(true);
});

test("changing the connection stops old requests and preserves the recovery record", async () => {
  const scope = renderHook(useWorkerNameUpdateScope);
  let signal: AbortSignal | null | undefined;
  hosting.service.applyWorkerNames.mockImplementation(async (_review, callbacks, options) => {
    signal = options?.signal;
    callbacks.track(attempt);
    return new Promise<PoolAssignmentStatus>((_, reject) => {
      signal?.addEventListener("abort", () => reject(signal?.reason), { once: true });
    });
  });
  try {
    const update = startWorkerNameUpdate(hosting.service, "ct1", "owner", review);
    expect(readPoolAttempts("ct1")).toMatchObject([{ id: attempt.id }]);
    expect(signal?.aborted).toBe(false);
    hosting.generation += 1;
    scope.rerender();
    await expect(update).resolves.toEqual({ state: "interrupted" });
    expect(signal?.aborted).toBe(true);
    expect(hasWorkerNameUpdate("ct1")).toBe(false);
    expect(readPoolAttempts("ct1")).toMatchObject([{ id: attempt.id }]);
  } finally {
    removePoolAttempt("ct1", attempt.id);
  }
});
