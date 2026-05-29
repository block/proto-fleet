import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  applyActiveCurtailmentEvent,
  dismissActiveCurtailmentEvent,
  fetchActiveCurtailmentData,
  getActiveCurtailmentSnapshot,
  refreshActiveCurtailmentData,
  resetActiveCurtailmentData,
} from "@/protoFleet/api/activeCurtailmentData";
import {
  type CurtailmentEvent,
  CurtailmentEventSchema,
  CurtailmentEventState,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";

const { mockGetActiveCurtailment } = vi.hoisted(() => ({
  mockGetActiveCurtailment: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    getActiveCurtailment: mockGetActiveCurtailment,
  },
}));

function curtailmentEvent(eventUuid: string, state = CurtailmentEventState.ACTIVE): CurtailmentEvent {
  return create(CurtailmentEventSchema, {
    eventUuid,
    state,
  });
}

describe("activeCurtailmentData", () => {
  beforeEach(() => {
    resetActiveCurtailmentData();
    vi.clearAllMocks();
  });

  it("keeps dismissed events suppressed when an older refresh is discarded", async () => {
    let resolveRefresh: (value: { event: CurtailmentEvent }) => void = () => {};
    mockGetActiveCurtailment
      .mockReturnValueOnce(
        new Promise<{ event: CurtailmentEvent }>((resolve) => {
          resolveRefresh = resolve;
        }),
      )
      .mockResolvedValueOnce({ event: curtailmentEvent("dismissed-event") });

    const staleRefreshPromise = refreshActiveCurtailmentData();
    dismissActiveCurtailmentEvent("dismissed-event");
    resolveRefresh({ event: curtailmentEvent("different-event") });

    await staleRefreshPromise;
    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it("shares one active curtailment request across concurrent refresh callers", async () => {
    let resolveRefresh: (value: { event: CurtailmentEvent }) => void = () => {};
    mockGetActiveCurtailment.mockReturnValue(
      new Promise<{ event: CurtailmentEvent }>((resolve) => {
        resolveRefresh = resolve;
      }),
    );

    const refreshPromise = refreshActiveCurtailmentData();
    const pendingRefreshPromise = fetchActiveCurtailmentData();

    expect(mockGetActiveCurtailment).toHaveBeenCalledTimes(1);

    resolveRefresh({ event: curtailmentEvent("shared-event") });
    const [snapshot, pendingRefresh] = await Promise.all([refreshPromise, pendingRefreshPromise]);

    expect(snapshot.event?.eventUuid).toBe("shared-event");
    expect(pendingRefresh.event?.eventUuid).toBe("shared-event");

    pendingRefresh.commit();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("shared-event");
    expect(mockGetActiveCurtailment).toHaveBeenCalledTimes(1);
  });

  it("starts a fresh request after all shared request subscribers abort", async () => {
    mockGetActiveCurtailment
      .mockImplementationOnce(
        (_request: unknown, options?: { signal?: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            options?.signal?.addEventListener(
              "abort",
              () => reject(new DOMException("The operation was aborted.", "AbortError")),
              { once: true },
            );
          }),
      )
      .mockResolvedValueOnce({ event: curtailmentEvent("fresh-event") });

    const abortController = new AbortController();
    const abortedRequest = fetchActiveCurtailmentData({ signal: abortController.signal }).catch((error) => error);

    abortController.abort();

    const freshRefresh = await fetchActiveCurtailmentData();

    expect(freshRefresh.event?.eventUuid).toBe("fresh-event");
    expect(mockGetActiveCurtailment).toHaveBeenCalledTimes(2);
    await expect(abortedRequest).resolves.toBeInstanceOf(DOMException);
  });

  it.each([
    ["restoring", CurtailmentEventState.RESTORING],
    ["restored", CurtailmentEventState.COMPLETED],
    ["incomplete restore", CurtailmentEventState.COMPLETED_WITH_FAILURES],
  ])("preserves a %s curtailment when active polling briefly returns empty", async (eventUuid, state) => {
    applyActiveCurtailmentEvent(curtailmentEvent(eventUuid, state));
    mockGetActiveCurtailment.mockResolvedValue({ event: undefined });

    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe(eventUuid);
  });
});
