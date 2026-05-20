import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  CurtailmentCandidateSchema,
  CurtailmentMode,
  FixedKwParamsSchema,
  PreviewCurtailmentPlanResponseSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { CurtailmentFormValues } from "@/protoFleet/features/energy/CurtailmentStartModal";
import {
  buildPreviewCurtailmentPlanRequest,
  useCurtailmentPlanPreview,
} from "@/protoFleet/features/energy/useCurtailmentPlanPreview";

const { mockHandleAuthErrors, mockPreviewCurtailmentPlan } = vi.hoisted(() => ({
  mockHandleAuthErrors: vi.fn(),
  mockPreviewCurtailmentPlan: vi.fn(),
}));

vi.mock("@/protoFleet/api/clients", () => ({
  curtailmentClient: {
    previewCurtailmentPlan: mockPreviewCurtailmentPlan,
  },
}));

vi.mock("@/protoFleet/store", () => ({
  useAuthErrors: () => ({
    handleAuthErrors: mockHandleAuthErrors,
  }),
}));

const baseValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  deviceSetIds: [],
  deviceIdentifiers: [],
  responseProfileId: "customPlan",
  curtailmentMode: "fixedKwReduction",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "40",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "300",
  maxDurationSec: "1800",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak",
  includeMaintenance: true,
};

function previewResponse(candidateCount = 3) {
  return create(PreviewCurtailmentPlanResponseSchema, {
    candidates: Array.from({ length: candidateCount }, (_, index) =>
      create(CurtailmentCandidateSchema, { deviceIdentifier: `miner-${index + 1}` }),
    ),
    estimatedReductionKw: 45,
    mode: CurtailmentMode.FIXED_KW,
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 40 }),
    },
  });
}

function renderPreviewHook(initialValues: CurtailmentFormValues = baseValues) {
  return renderHook(
    ({ values }) =>
      useCurtailmentPlanPreview({
        open: true,
        values,
        debounceMs: 0,
      }),
    {
      initialProps: { values: initialValues },
    },
  );
}

describe("useCurtailmentPlanPreview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockHandleAuthErrors.mockImplementation(
      ({ error, onError }: { error: unknown; onError: (error: unknown) => void }) => onError(error),
    );
  });

  it("builds supported fixed-kW preview requests", () => {
    const wholeFleetRequest = buildPreviewCurtailmentPlanRequest(baseValues);
    expect(wholeFleetRequest?.scope.case).toBe("wholeOrg");
    expect(wholeFleetRequest?.mode).toBe(CurtailmentMode.FIXED_KW);
    expect(wholeFleetRequest?.modeParams.case).toBe("fixedKw");
    if (wholeFleetRequest?.modeParams.case !== "fixedKw") {
      throw new Error("Expected fixedKw mode params");
    }
    expect(wholeFleetRequest.modeParams.value.targetKw).toBe(40);
    expect(wholeFleetRequest?.includeMaintenance).toBe(true);
    expect(wholeFleetRequest?.forceIncludeMaintenance).toBe(true);

    const minerRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      scopeType: "explicitMiners",
      scopeId: undefined,
      deviceIdentifiers: ["miner-1", "miner-2"],
      includeMaintenance: false,
    });

    expect(minerRequest?.scope.case).toBe("deviceIdentifiers");
    if (minerRequest?.scope.case !== "deviceIdentifiers") {
      throw new Error("Expected deviceIdentifiers scope");
    }
    expect(minerRequest.scope.value.deviceIdentifiers).toEqual(["miner-1", "miner-2"]);
    expect(minerRequest.includeMaintenance).toBe(false);
    expect(minerRequest.forceIncludeMaintenance).toBe(false);
  });

  it("does not build a request until target and scope are valid", () => {
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "" })).toBeUndefined();
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "0" })).toBeUndefined();
    expect(
      buildPreviewCurtailmentPlanRequest({ ...baseValues, scopeType: "deviceSet", deviceSetIds: [] }),
    ).toBeUndefined();
  });

  it("does not build unsupported device-set preview requests", () => {
    expect(
      buildPreviewCurtailmentPlanRequest({
        ...baseValues,
        scopeType: "deviceSet",
        scopeId: "racks",
        deviceSetIds: ["rack-1"],
      }),
    ).toBeUndefined();
    expect(
      buildPreviewCurtailmentPlanRequest({
        ...baseValues,
        scopeType: "deviceSet",
        scopeId: "groups",
        deviceSetIds: ["group-1"],
      }),
    ).toBeUndefined();
  });

  it("fetches and maps a preview response", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.preview).toEqual(
        expect.objectContaining({
          selectedMinerCount: 3,
          targetKw: 40,
          estimatedReductionKw: 45,
          curtailEstimate: "5 minutes - 30 minutes",
          restoreEstimate: "Immediately",
          scopeLabel: "across the fleet",
        }),
      );
    });

    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledWith(
      expect.objectContaining({
        includeMaintenance: true,
        forceIncludeMaintenance: true,
      }),
      expect.objectContaining({
        signal: expect.any(AbortSignal),
      }),
    );
  });

  it("updates local preview labels without refetching for non-request edits", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result, rerender } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.preview?.curtailEstimate).toBe("5 minutes - 30 minutes");
    });

    rerender({
      values: {
        ...baseValues,
        minDurationSec: "60",
        maxDurationSec: "120",
        restoreBatchSize: "1",
        restoreIntervalSec: "30",
        reason: "Updated reason",
      },
    });

    expect(result.current.preview).toEqual(
      expect.objectContaining({
        curtailEstimate: "1 minute - 2 minutes",
        restoreEstimate: "~1 minute",
      }),
    );
    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(1);
  });

  it("aborts in-flight previews when the request changes", async () => {
    mockPreviewCurtailmentPlan.mockReturnValue(new Promise(() => {}));

    const { rerender } = renderPreviewHook();

    await waitFor(() => {
      expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(1);
    });

    const firstOptions = mockPreviewCurtailmentPlan.mock.calls[0][1] as { signal: AbortSignal };
    expect(firstOptions.signal.aborted).toBe(false);

    rerender({ values: { ...baseValues, targetKw: "50" } });

    await waitFor(() => {
      expect(firstOptions.signal.aborted).toBe(true);
      expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(2);
    });

    const secondOptions = mockPreviewCurtailmentPlan.mock.calls[1][1] as { signal: AbortSignal };
    expect(secondOptions.signal.aborted).toBe(false);
  });

  it("hides stale previews when values no longer build a request", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result, rerender } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.preview).toBeDefined();
    });

    rerender({ values: { ...baseValues, targetKw: "" } });

    expect(result.current).toEqual({
      preview: undefined,
      previewError: undefined,
      isPreviewLoading: false,
    });
    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(1);
  });

  it("surfaces API errors through previewError", async () => {
    mockPreviewCurtailmentPlan.mockRejectedValueOnce(
      new ConnectError("insufficient curtailable load", Code.InvalidArgument),
    );

    const { result } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.previewError).toBe("insufficient curtailable load");
    });
    expect(mockHandleAuthErrors).toHaveBeenCalledTimes(1);
  });
});
