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

function curtailmentEvent(eventUuid: string, state = CurtailmentEventState.ACTIVE): CurtailmentEvent {
  return create(CurtailmentEventSchema, { eventUuid, state });
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

  it.each([
    ["restoring", CurtailmentEventState.RESTORING],
    ["restored", CurtailmentEventState.COMPLETED],
    ["incomplete restore", CurtailmentEventState.COMPLETED_WITH_FAILURES],
  ])("preserves a %s curtailment for one empty active response", async (eventUuid, state) => {
    applyActiveCurtailmentEvent(curtailmentEvent(eventUuid, state));
    mockGetActiveCurtailment.mockResolvedValue({ event: undefined });

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe(eventUuid);

    await refreshActiveCurtailmentData();
    expect(getActiveCurtailmentSnapshot().event).toBeUndefined();
  });
});
