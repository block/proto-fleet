import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { useFeedMappingState } from "./feedMappingContext";
import { FeedMappingProvider } from "./FeedMappingProvider";
import type { FeedMapping, MappingAssessment, MappingRun } from "@/protoOS/api/adapters/feedMapping";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster", () => ({ pushToast: vi.fn() }));

const key = "proto-os:feed-mapping:ct1";
const assessment: MappingAssessment = {
  mode: "manual",
  hardMappingAvailable: true,
  boards: [],
  configurationReasons: [],
  changes: [],
  historyIncomplete: false,
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
  expect(JSON.parse(localStorage.getItem(key)!)).toMatchObject({ token: expect.any(String), runId: running.id });
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
  localStorage.setItem(key, JSON.stringify({ token: "old-token" }));
  const { result } = mount(api());
  await waitFor(() => expect(result.current.ready).toBe(true));
  const newer = JSON.stringify({ token: "new-token" });
  localStorage.setItem(key, newer);
  act(() => result.current.clear());
  expect(localStorage.getItem(key)).toBe(newer);
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
  localStorage.setItem(key, newer);
  act(() => window.dispatchEvent(new StorageEvent("storage", { key, newValue: newer, storageArea: localStorage })));
  await act(async () => {
    resolve(assessment);
    await start;
  });
  expect(mapping.start).not.toHaveBeenCalled();
  expect(localStorage.getItem(key)).toBe(newer);
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
