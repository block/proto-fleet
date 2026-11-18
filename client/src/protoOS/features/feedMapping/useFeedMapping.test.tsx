import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { readMappingAttempts, saveMappingAttempt } from "./attemptStorage";
import { useFeedMappingState } from "./feedMappingContext";
import { FeedMappingProvider } from "./FeedMappingProvider";
import type { FeedMapping, MappingAssessment, MappingRun } from "@/protoOS/api/adapters/feedMapping";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn() }));

const keyFor = (token: string) => `proto-os:feed-mapping:ct1:${token}`;
const key = keyFor("saved-token");
const assessment: MappingAssessment = {
  mode: "manual",
  hardMappingAvailable: true,
  boards: [],
  configurationReasons: [],
  changes: [],
};
const running: MappingRun = {
  id: "run-1",
  status: "running",
  phase: "ISOLATING",
  feeds: [{ id: "feed-1", boards: 0, complete: false, timedOut: false }],
  warnings: [],
};
const api = () => ({
  assess: vi.fn<FeedMapping["assess"]>().mockResolvedValue(assessment),
  active: vi.fn<FeedMapping["active"]>().mockResolvedValue(null),
  get: vi.fn<FeedMapping["get"]>().mockResolvedValue(running),
  plan: vi.fn<FeedMapping["plan"]>().mockResolvedValue({ ...running, status: "planned" }),
  start: vi.fn<FeedMapping["start"]>().mockResolvedValue(running),
  cancel: vi.fn<FeedMapping["cancel"]>().mockResolvedValue({ ...running, status: "canceled" }),
  verify: vi.fn<FeedMapping["verify"]>().mockResolvedValue(undefined),
});
const mount = (mapping: FeedMapping) =>
  renderHook(() => useFeedMappingState()!, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <FeedMappingProvider mapping={mapping} deviceId="ct1">
        {children}
      </FeedMappingProvider>
    ),
  });

beforeEach(() => {
  localStorage.clear();
  vi.mocked(pushToast).mockClear();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

test.each(["start", "recover", "preflight"])("supports HTTP without randomUUID during %s", async (path) => {
  vi.stubGlobal("crypto", { getRandomValues: crypto.getRandomValues.bind(crypto) });
  const mapping = api();
  if (path === "recover") mapping.active.mockResolvedValue(running);
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.ready).toBe(true));
  if (path !== "recover") {
    if (path === "preflight") mapping.active.mockResolvedValue(running);
    await act(async () => {
      await result.current.start({ ...running, status: "planned" });
    });
  }
  await waitFor(() => expect(result.current.run?.id).toBe(running.id));
  expect(readMappingAttempts("ct1")).toEqual([{ token: expect.any(String), runId: running.id }]);
  expect(mapping.start).toHaveBeenCalledTimes(path === "start" ? 1 : 0);
});

test.each([false, true])("recovers a missing saved run (previously observed: %s)", async (previouslyObserved) => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  if (!previouslyObserved) mapping.get.mockResolvedValue(null);
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.ready).toBe(true));
  const fresh = { ...assessment, configurationReasons: ["controller restarted"] };
  mapping.assess.mockResolvedValue(fresh);
  mapping.get.mockResolvedValue(null);
  act(() => result.current.refresh());
  await waitFor(() => expect(result.current.assessment).toEqual(fresh));
  expect(result.current.run).toBeUndefined();
  expect(result.current.unresolved).toBe(true);
  expect(result.current.canRemap).toBe(false);
  expect(localStorage.getItem(key)).not.toBeNull();
  act(() => result.current.clear());
  await waitFor(() => expect(result.current.canRemap).toBe(true));
  expect(localStorage.getItem(key)).toBeNull();
  expect(mapping.start).not.toHaveBeenCalled();
});

test.each(["poll", "storage event"])("honors dismissal from another provider through %s", async (delivery) => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  mapping.get.mockResolvedValue({ ...running, status: "canceled" });
  const first = mount(mapping);
  const second = mount(mapping);
  await waitFor(() => expect(first.result.current.run?.status).toBe("canceled"));
  await waitFor(() => expect(second.result.current.run?.status).toBe("canceled"));
  const calls = mapping.get.mock.calls.length;
  act(() => first.result.current.clear());
  act(() => {
    if (delivery === "poll") second.result.current.refresh();
    else window.dispatchEvent(new StorageEvent("storage", { key, newValue: null, storageArea: localStorage }));
  });
  await waitFor(() => expect(second.result.current.run).toBeUndefined());
  expect(second.result.current.unresolved).toBe(false);
  expect(localStorage.getItem(key)).toBeNull();
  expect(mapping.get).toHaveBeenCalledTimes(calls);
});

test("clears a token-only attempt already deleted by another provider", async () => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token" }));
  const first = mount(api());
  const second = mount(api());
  await waitFor(() => expect(first.result.current.ready && second.result.current.ready).toBe(true));
  act(() => first.result.current.clear());
  act(() => second.result.current.clear());
  await waitFor(() => expect(second.result.current.canRemap).toBe(true));
  expect(localStorage.getItem(key)).toBeNull();
});

test("does not restore a dismissed record when a saved-run read finishes late", async () => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  const finished = { ...running, status: "canceled" as const };
  mapping.get.mockResolvedValue(finished);
  const first = mount(mapping);
  const second = mount(mapping);
  await waitFor(() => expect(first.result.current.run?.status).toBe("canceled"));
  await waitFor(() => expect(second.result.current.run?.status).toBe("canceled"));
  let resolve!: (value: MappingRun) => void;
  mapping.get.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  act(() => second.result.current.refresh());
  await waitFor(() => expect(resolve).toBeDefined());
  act(() => first.result.current.clear());
  await act(async () => resolve(finished));
  await waitFor(() => expect(second.result.current.run).toBeUndefined());
  expect(localStorage.getItem(key)).toBeNull();
});

test("does not delete a newer attempt from another tab", async () => {
  localStorage.setItem(keyFor("old-token"), JSON.stringify({ token: "old-token" }));
  const { result } = mount(api());
  await waitFor(() => expect(result.current.ready).toBe(true));
  const newer = JSON.stringify({ token: "new-token" });
  localStorage.setItem(keyFor("new-token"), newer);
  act(() => result.current.clear());
  expect(localStorage.getItem(keyFor("new-token"))).toBe(newer);
  expect(localStorage.getItem(keyFor("old-token"))).toBeNull();
  expect(result.current.unresolved).toBe(true);
});

test("refuses Start when recovery storage cannot be written", async () => {
  const mapping = api();
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.canRemap).toBe(true));
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("Storage unavailable");
  });
  await act(async () => {
    await result.current.start({ ...running, status: "planned" });
  });
  expect(mapping.start).not.toHaveBeenCalled();
  expect(result.current.notice).toContain("Storage unavailable");
});

test("refuses Start if another tab saves an attempt during preflight", async () => {
  const mapping = api();
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.canRemap).toBe(true));
  let resolve!: (value: MappingAssessment) => void;
  mapping.assess.mockImplementationOnce(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  let start!: Promise<MappingRun | undefined>;
  act(() => {
    start = result.current.start({ ...running, status: "planned" });
  });
  await waitFor(() => expect(resolve).toBeDefined());
  const newer = JSON.stringify({ token: "another-tab" });
  localStorage.setItem(keyFor("another-tab"), newer);
  act(() =>
    window.dispatchEvent(
      new StorageEvent("storage", { key: keyFor("another-tab"), newValue: newer, storageArea: localStorage }),
    ),
  );
  await act(async () => {
    resolve(assessment);
    await start;
  });
  expect(mapping.start).not.toHaveBeenCalled();
  expect(localStorage.getItem(keyFor("another-tab"))).toBe(newer);
  expect(result.current.unresolved).toBe(true);
});

test("keeps a saved attempt when storage cannot be read", async () => {
  const saved = JSON.stringify({ token: "saved-token" });
  localStorage.setItem(key, saved);
  const mapping = api();
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.ready).toBe(true));
  const read = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
    throw new Error("Storage blocked");
  });
  act(() => result.current.refresh());
  await waitFor(() => expect(result.current.error).toBe("Storage blocked"));
  expect(result.current.unresolved).toBe(true);
  expect(result.current.canRemap).toBe(false);
  read.mockRestore();
  expect(localStorage.getItem(key)).toBe(saved);
});

test("expires running confirmation when status reads time out and ignores late responses", async () => {
  const mapping = api();
  mapping.active.mockResolvedValue(running);
  const { result, unmount } = mount(mapping);
  await waitFor(() => expect(result.current.runningConfirmed).toBe(true));
  let resolve!: (value: MappingRun) => void;
  mapping.active.mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  vi.useFakeTimers();
  act(() => result.current.refresh());
  await act(async () => {
    await vi.advanceTimersByTimeAsync(15000);
  });
  expect(result.current.runningConfirmed).toBe(false);
  expect(result.current.error).toContain("timed out");
  await act(async () => resolve(running));
  expect(result.current.runningConfirmed).toBe(false);
  unmount();
});

test("clears a successful run only after a fresh healthy assessment and emits one success toast", async () => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  mapping.get.mockResolvedValue({ ...running, status: "completed" });
  let confirm!: (value: MappingAssessment) => void;
  const confirmation = new Promise<MappingAssessment>((resolve) => {
    confirm = resolve;
  });
  mapping.assess.mockResolvedValueOnce(assessment).mockReturnValue(confirmation);
  const view = mount(mapping);
  await waitFor(() => expect(mapping.assess.mock.calls.length).toBeGreaterThanOrEqual(2));
  expect(localStorage.getItem(key)).not.toBeNull();
  expect(pushToast).not.toHaveBeenCalled();
  await act(async () => confirm(assessment));
  await waitFor(() => expect(view.result.current.completedSuccessfully).toBe(true));
  expect(view.result.current.run?.status).toBe("completed");
  expect(localStorage.getItem(key)).toBeNull();
  expect(pushToast).toHaveBeenCalledExactlyOnceWith({ message: "Hashboard mapping complete", status: "success" });
  act(() => view.result.current.refresh());
  await act(async () => {});
  view.unmount();
  const reloaded = mount(mapping);
  await waitFor(() => expect(reloaded.result.current.ready).toBe(true));
  expect(reloaded.result.current.run).toBeUndefined();
  expect(pushToast).toHaveBeenCalledTimes(1);
});

test("shows a success that another tab confirmed while this tab still showed the run as running", async () => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  mapping.active.mockResolvedValue(running);
  const first = mount(mapping);
  const second = mount(mapping);
  await waitFor(() => expect(first.result.current.runningConfirmed).toBe(true));
  await waitFor(() => expect(second.result.current.runningConfirmed).toBe(true));
  const completed = { ...running, status: "completed" as const };
  mapping.active.mockResolvedValue(null);
  mapping.get.mockResolvedValue(completed);
  act(() => second.result.current.refresh());
  await waitFor(() => expect(second.result.current.completedSuccessfully).toBe(true));
  expect(localStorage.getItem(key)).toBeNull();
  expect(first.result.current.run?.status).toBe("running");

  act(() => window.dispatchEvent(new StorageEvent("storage", { key, newValue: null, storageArea: localStorage })));
  await waitFor(() => expect(first.result.current.completedSuccessfully).toBe(true));
  expect(first.result.current.run).toEqual(completed);
  expect(first.result.current.unresolved).toBe(false);
  expect(readMappingAttempts("ct1")).toEqual([]);
  expect(pushToast).toHaveBeenCalledOnce();
});

test("retries the hardware refresh for a released run after the poll aborts it", async () => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  mapping.active.mockResolvedValue(running);
  const settled = vi
    .fn<(signal: AbortSignal) => Promise<void>>()
    .mockImplementationOnce(
      (signal) =>
        new Promise((_resolve, reject) =>
          signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true }),
        ),
    )
    .mockResolvedValue(undefined);
  const first = renderHook(() => useFeedMappingState()!, {
    wrapper: ({ children }: { children: ReactNode }) => (
      <FeedMappingProvider mapping={mapping} deviceId="ct1" onSettled={settled}>
        {children}
      </FeedMappingProvider>
    ),
  });
  await waitFor(() => expect(first.result.current.runningConfirmed).toBe(true));
  mapping.active.mockResolvedValue(null);
  mapping.get.mockResolvedValue({ ...running, status: "completed" });
  localStorage.removeItem(key);

  // The run leaves the 3-second poll interval when it completes, which aborts the first refresh.
  act(() => window.dispatchEvent(new StorageEvent("storage", { key, newValue: null, storageArea: localStorage })));
  await waitFor(() => expect(settled).toHaveBeenCalledTimes(2));
  expect(settled.mock.calls[0][0].aborted).toBe(true);
  expect(first.result.current.completedSuccessfully).toBe(true);
  expect(mapping.get).toHaveBeenLastCalledWith(running.id, expect.anything());
});

test.each([
  { name: "failed run", run: { ...running, status: "failed" as const }, assessment },
  { name: "canceled run", run: { ...running, status: "canceled" as const }, assessment },
  {
    name: "run warning",
    run: { ...running, status: "completed" as const, warnings: ["Check feed power"] },
    assessment,
  },
  {
    name: "timeout",
    run: { ...running, status: "completed" as const, feeds: [{ ...running.feeds[0], timedOut: true }] },
    assessment,
  },
  { name: "configuration issue", assessment: { ...assessment, configurationReasons: ["configuration mismatch"] } },
  { name: "unavailable assessment", assessment: { ...assessment, unavailable: "Report unavailable" } },
  { name: "deferred verification", assessment: { ...assessment, deferredReason: "switch unavailable" } },
  {
    name: "unlocated board",
    assessment: {
      ...assessment,
      boards: [{ mac: "board", unlocated: true, conflict: false, status: "unverified", reasons: [], coverage: [] }],
    },
  },
  {
    name: "conflicting board",
    assessment: {
      ...assessment,
      boards: [{ mac: "board", unlocated: false, conflict: true, status: "mismatch", reasons: [], coverage: [] }],
    },
  },
])("retains the result for $name", async ({ run, assessment: finalAssessment }) => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  mapping.get.mockResolvedValue(run ?? { ...running, status: "completed" });
  // An earlier healthy read must not clear a problem in the post-completion assessment.
  mapping.assess.mockResolvedValueOnce(assessment).mockResolvedValue(finalAssessment);
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.run?.id).toBe(running.id));
  await act(async () => {});
  expect(result.current.completedSuccessfully).toBe(false);
  expect(localStorage.getItem(key)).not.toBeNull();
  expect(pushToast).not.toHaveBeenCalled();
});

test("retains the result if the post-completion assessment fails", async () => {
  localStorage.setItem(key, JSON.stringify({ token: "saved-token", runId: running.id }));
  const mapping = api();
  mapping.get.mockResolvedValue({ ...running, status: "completed" });
  mapping.assess.mockResolvedValueOnce(assessment).mockRejectedValue(new Error("Assessment unavailable"));
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.error).toBe("Assessment unavailable"));
  expect(localStorage.getItem(key)).not.toBeNull();
  expect(pushToast).not.toHaveBeenCalled();
});

test.each(["accepted first", "refused first"])("concurrent starts preserve the accepted run (%s)", async (order) => {
  const acceptedApi = api();
  const refusedApi = api();
  let accept!: (run: MappingRun) => void;
  let refuse!: (error: Error) => void;
  acceptedApi.start.mockImplementation(
    () =>
      new Promise((resolve) => {
        accept = resolve;
      }),
  );
  refusedApi.start.mockImplementation(
    () =>
      new Promise((_resolve, reject) => {
        refuse = reject;
      }),
  );
  const accepted = mount(acceptedApi);
  const refused = mount(refusedApi);
  await waitFor(() => expect(accepted.result.current.canRemap && refused.result.current.canRemap).toBe(true));

  // Force both tabs' last advisory reads to see the same empty snapshot. Their writes remain real and shared.
  const getItem = Storage.prototype.getItem;
  const staleReads = vi.spyOn(Storage.prototype, "getItem").mockImplementation(function (this: Storage, storageKey) {
    return storageKey.startsWith("proto-os:feed-mapping:") ? null : getItem.call(this, storageKey);
  });
  let acceptedStart!: Promise<MappingRun | undefined>;
  let refusedStart!: Promise<MappingRun | undefined>;
  act(() => {
    acceptedStart = accepted.result.current.start({ ...running, status: "planned" });
    refusedStart = refused.result.current.start({ ...running, status: "planned" });
  });
  await waitFor(() => {
    expect(acceptedApi.start).toHaveBeenCalledTimes(1);
    expect(refusedApi.start).toHaveBeenCalledTimes(1);
  });
  staleReads.mockRestore();
  const saved = () =>
    Object.keys(localStorage)
      .filter((storageKey) => storageKey.startsWith("proto-os:feed-mapping:"))
      .map((storageKey) => JSON.parse(localStorage.getItem(storageKey)!));
  expect(saved()).toHaveLength(2);

  // Close both tabs before any recovery poll can hide data loss. Start responses must still persist their result.
  accepted.unmount();
  refused.unmount();
  const acceptRun = async () => {
    accept(running);
    await acceptedStart;
  };
  const refuseRun = async () => {
    refuse(Object.assign(new Error("Run already active"), { writeOutcome: "refused" }));
    await refusedStart;
  };
  await act(async () => {
    if (order === "accepted first") {
      await acceptRun();
      await refuseRun();
    } else {
      await refuseRun();
      await acceptRun();
    }
  });
  expect(saved()).toEqual([{ token: expect.any(String), runId: running.id }]);

  // Once the run has finished, active() cannot recover it. Only the saved ID can retrieve restoration warnings.
  const reopenedApi = api();
  reopenedApi.get.mockResolvedValue({ ...running, status: "completed", warnings: ["Check feed restoration"] });
  const reopened = mount(reopenedApi);
  await waitFor(() => expect(reopened.result.current.run?.warnings).toEqual(["Check feed restoration"]));
  expect(reopenedApi.get).toHaveBeenCalledWith(running.id, expect.anything());
});

test("recovers multiple saved runs one at a time without dropping the next result", async () => {
  saveMappingAttempt("ct1", { token: "first", runId: "run-1" });
  saveMappingAttempt("ct1", { token: "second", runId: "run-2" });
  const mapping = api();
  mapping.get.mockImplementation(async (id) => ({ ...running, id, status: "canceled", warnings: [id] }));
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.run?.id).toBe("run-1"));
  act(() => result.current.clear());
  await waitFor(() => expect(result.current.run?.id).toBe("run-2"));
  expect(result.current.run?.warnings).toEqual(["run-2"]);
  expect(readMappingAttempts("ct1")).toEqual([{ token: "second", runId: "run-2" }]);
});

test("observing a different active run does not overwrite a saved run's recovery data", async () => {
  saveMappingAttempt("ct1", { token: "older", runId: "older-run" });
  const mapping = api();
  mapping.active.mockResolvedValue(running);
  const { result } = mount(mapping);
  await waitFor(() => expect(result.current.run?.id).toBe(running.id));
  expect(readMappingAttempts("ct1")).toEqual(
    expect.arrayContaining([
      { token: "older", runId: "older-run" },
      { token: expect.any(String), runId: running.id },
    ]),
  );
  expect(readMappingAttempts("ct1")).toHaveLength(2);
});
