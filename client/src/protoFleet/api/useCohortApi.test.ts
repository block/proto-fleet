import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { durationMs, timestampMs } from "@bufbuild/protobuf/wkt";

import {
  CohortFirmwareValidationWindow,
  CohortSummarySchema,
  CohortTelemetryComparisonWindow,
  GetCohortFirmwareValidationResponseSchema,
  GetCohortFirmwareVersionHistoryResponseSchema,
  GetCohortTelemetryComparisonResponseSchema,
  ListCohortsResponseSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { useCohortApi } from "@/protoFleet/api/useCohortApi";

const mocks = vi.hoisted(() => ({
  getCohortFirmwareVersionHistory: vi.fn(),
  getCohortFirmwareValidation: vi.fn(),
  getCohortTelemetryComparison: vi.fn(),
  listCohorts: vi.fn(),
  handleAuthErrors: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  cohortClient: {
    getCohortFirmwareVersionHistory: mocks.getCohortFirmwareVersionHistory,
    getCohortFirmwareValidation: mocks.getCohortFirmwareValidation,
    getCohortTelemetryComparison: mocks.getCohortTelemetryComparison,
    listCohorts: mocks.listCohorts,
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

  it("serializes the model-specific firmware validation request", async () => {
    const response = create(GetCohortFirmwareValidationResponseSchema, { targetedCount: 5 });
    mocks.getCohortFirmwareValidation.mockResolvedValue(response);
    const { result } = renderHook(() => useCohortApi());

    await act(async () => {
      await result.current.getFirmwareValidation({
        cohortId: 42n,
        manufacturer: "Proto",
        model: "Rig",
        comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS,
      });
    });

    expect(mocks.getCohortFirmwareValidation).toHaveBeenCalledWith(
      expect.objectContaining({
        cohortId: 42n,
        manufacturer: "Proto",
        model: "Rig",
        comparisonWindow: CohortFirmwareValidationWindow.SIX_HOURS,
      }),
    );
  });

  it("loads every cohort page for overview comparisons", async () => {
    mocks.listCohorts
      .mockResolvedValueOnce(
        create(ListCohortsResponseSchema, {
          cohorts: [create(CohortSummarySchema, { id: 1n, label: "Default", isDefault: true })],
          nextPageToken: "page-2",
        }),
      )
      .mockResolvedValueOnce(
        create(ListCohortsResponseSchema, {
          cohorts: [create(CohortSummarySchema, { id: 2n, label: "Rollout A" })],
        }),
      );
    const { result } = renderHook(() => useCohortApi());

    let cohorts: Awaited<ReturnType<typeof result.current.listAllCohorts>> = [];
    await act(async () => {
      cohorts = await result.current.listAllCohorts();
    });

    expect(cohorts.map((cohort) => cohort.label)).toEqual(["Default", "Rollout A"]);
    expect(mocks.listCohorts).toHaveBeenNthCalledWith(1, expect.objectContaining({ pageSize: 500, pageToken: "" }));
    expect(mocks.listCohorts).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ pageSize: 500, pageToken: "page-2" }),
    );
  });

  it("serializes the selected cohorts and operating-outcome window", async () => {
    const response = create(GetCohortTelemetryComparisonResponseSchema, {
      comparisonWindow: CohortTelemetryComparisonWindow.TWENTY_FOUR_HOURS,
    });
    mocks.getCohortTelemetryComparison.mockResolvedValue(response);
    const { result } = renderHook(() => useCohortApi());

    await act(async () => {
      await result.current.getTelemetryComparison({
        cohortIds: [1n, 42n],
        comparisonWindow: CohortTelemetryComparisonWindow.TWENTY_FOUR_HOURS,
      });
    });

    expect(mocks.getCohortTelemetryComparison).toHaveBeenCalledWith(
      expect.objectContaining({
        cohortIds: [1n, 42n],
        comparisonWindow: CohortTelemetryComparisonWindow.TWENTY_FOUR_HOURS,
      }),
    );
  });
});
