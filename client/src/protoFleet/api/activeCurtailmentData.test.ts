import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";

import {
  applyActiveCurtailmentEvent,
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

  it("keeps newer active curtailment data when an older refresh returns later", async () => {
    let resolveRefresh: (value: { event: CurtailmentEvent }) => void = () => {};
    mockGetActiveCurtailment.mockReturnValue(
      new Promise<{ event: CurtailmentEvent }>((resolve) => {
        resolveRefresh = resolve;
      }),
    );

    const refreshPromise = refreshActiveCurtailmentData();
    applyActiveCurtailmentEvent(curtailmentEvent("newer-event"));
    resolveRefresh({ event: curtailmentEvent("older-event") });

    await refreshPromise;

    expect(getActiveCurtailmentSnapshot().event?.eventUuid).toBe("newer-event");
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
