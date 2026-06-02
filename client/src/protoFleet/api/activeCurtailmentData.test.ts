import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

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
vi.mock("@/protoFleet/api/clients", () => ({ curtailmentClient: { getActiveCurtailment: mockGetActiveCurtailment } }));

function curtailmentEvent(
  eventUuid: string,
  state = CurtailmentEventState.ACTIVE,
  overrides: { reason?: string } = {},
): CurtailmentEvent {
  return create(CurtailmentEventSchema, { eventUuid, state, ...overrides });
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

  it("keeps a newer applied event when a later subscriber commits a stale shared refresh", async () => {
    let resolveRefresh: (value: { event?: CurtailmentEvent }) => void = () => undefined;
    mockGetActiveCurtailment.mockReturnValueOnce(
      new Promise<{ event?: CurtailmentEvent }>((resolve) => {
        resolveRefresh = resolve;
      }),
    );

    const preMutationRefresh = fetchActiveCurtailmentData();
    applyActiveCurtailmentEvent(curtailmentEvent("started-event"));
    const postMutationRefresh = fetchActiveCurtailmentData();

    resolveRefresh({ event: undefined });
    const [preMutationSnapshot, postMutationSnapshot] = await Promise.all([preMutationRefresh, postMutationRefresh]);
    preMutationSnapshot.commit();
    postMutationSnapshot.commit();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("started-event");
  });

  it("preserves a mutation-backed event through one stale shared active refresh", async () => {
    applyActiveCurtailmentEvent(curtailmentEvent("started-event"), { preserveAgainstStaleRefresh: true });
    mockGetActiveCurtailment.mockResolvedValueOnce({ event: undefined }).mockResolvedValueOnce({ event: undefined });

    const firstRefresh = fetchActiveCurtailmentData();
    const secondRefresh = fetchActiveCurtailmentData();
    const [firstSnapshot, secondSnapshot] = await Promise.all([firstRefresh, secondRefresh]);
    firstSnapshot.commit();
    secondSnapshot.commit();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("started-event");

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it("preserves mutation-backed fields through one stale same-event active refresh", async () => {
    applyActiveCurtailmentEvent(
      curtailmentEvent("updated-event", CurtailmentEventState.ACTIVE, { reason: "Updated" }),
      {
        preserveAgainstStaleRefresh: true,
      },
    );
    mockGetActiveCurtailment.mockResolvedValueOnce({
      event: curtailmentEvent("updated-event", CurtailmentEventState.ACTIVE, { reason: "Previous" }),
    });

    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event?.reason).toBe("Updated");
  });

  it("rejects a reset-aborted shared request as an AbortError", async () => {
    mockGetActiveCurtailment.mockImplementationOnce(
      (_request: unknown, options?: { signal?: AbortSignal }) =>
        new Promise((_resolve, reject) => {
          options?.signal?.addEventListener("abort", () => reject(new ConnectError("canceled", Code.Canceled)), {
            once: true,
          });
        }),
    );

    const pendingRefresh = refreshActiveCurtailmentData();
    resetActiveCurtailmentData();

    await expect(pendingRefresh).rejects.toBeInstanceOf(DOMException);
  });

  it("clears a restoring curtailment after an empty active response", async () => {
    applyActiveCurtailmentEvent(curtailmentEvent("restoring", CurtailmentEventState.RESTORING));
    mockGetActiveCurtailment.mockResolvedValue({ event: undefined });

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it("does not let stale empty refreshes clear a newer restoring event", async () => {
    let resolveStaleRefresh: (value: { event?: CurtailmentEvent }) => void = () => undefined;
    mockGetActiveCurtailment
      .mockReturnValueOnce(
        new Promise<{ event?: CurtailmentEvent }>((resolve) => {
          resolveStaleRefresh = resolve;
        }),
      )
      .mockResolvedValue({ event: undefined });

    const staleRefresh = fetchActiveCurtailmentData();
    applyActiveCurtailmentEvent(curtailmentEvent("restoring", CurtailmentEventState.RESTORING));
    resolveStaleRefresh({ event: undefined });

    const staleSnapshot = await staleRefresh;
    staleSnapshot.commit();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("restoring");

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });

  it.each([
    ["restored", CurtailmentEventState.COMPLETED],
    ["incomplete restore", CurtailmentEventState.COMPLETED_WITH_FAILURES],
  ])("preserves a %s curtailment through empty active responses until dismissal", async (eventUuid, state) => {
    applyActiveCurtailmentEvent(curtailmentEvent(eventUuid, state));
    mockGetActiveCurtailment.mockResolvedValue({ event: undefined });

    await refreshActiveCurtailmentData();
    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe(eventUuid);

    dismissActiveCurtailmentEvent(eventUuid);
    await refreshActiveCurtailmentData();

    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });
});
