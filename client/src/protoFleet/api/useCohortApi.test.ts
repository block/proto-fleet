import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { durationMs, timestampMs } from "@bufbuild/protobuf/wkt";

import { GetCohortFirmwareVersionHistoryResponseSchema } from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";

const mocks = vi.hoisted(() => ({
  getCohortFirmwareVersionHistory: vi.fn(),
  handleAuthErrors: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  cohortClient: {
    getCohortFirmwareVersionHistory: mocks.getCohortFirmwareVersionHistory,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({ handleAuthErrors: mocks.handleAuthErrors }),
}));

describe("useCohortApi", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("serializes the cohort firmware history range and granularity", async () => {
    const response = create(GetCohortFirmwareVersionHistoryResponseSchema, { memberCount: 2 });
    mocks.getCohortFirmwareVersionHistory.mockResolvedValue(response);
    const { result } = renderHook(() => useCohortApi());
    const startTime = new Date("2026-07-13T12:00:00Z");
    const endTime = new Date("2026-07-14T12:00:00Z");

    await act(async () => {
      await result.current.getFirmwareVersionHistory({
        cohortId: 42n,
        startTime,
        endTime,
        granularitySeconds: 90,
      });
    });

    expect(mocks.getCohortFirmwareVersionHistory).toHaveBeenCalledTimes(1);
    const request = mocks.getCohortFirmwareVersionHistory.mock.calls[0]?.[0];
    expect(request.cohortId).toBe(42n);
    expect(timestampMs(request.startTime)).toBe(startTime.getTime());
    expect(timestampMs(request.endTime)).toBe(endTime.getTime());
    expect(durationMs(request.granularity)).toBe(90_000);
  });
});
