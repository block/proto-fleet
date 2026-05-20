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

function renderPreviewHook(values: CurtailmentFormValues = baseValues) {
  return renderHook(() =>
    useCurtailmentPlanPreview({
      open: true,
      values,
      debounceMs: 0,
    }),
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

    const deviceSetRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      scopeType: "deviceSet",
      scopeId: "groups",
      deviceSetIds: ["group-1", "group-2"],
      includeMaintenance: false,
    });

    expect(deviceSetRequest?.scope.case).toBe("deviceSetIds");
    if (deviceSetRequest?.scope.case !== "deviceSetIds") {
      throw new Error("Expected deviceSetIds scope");
    }
    expect(deviceSetRequest.scope.value.deviceSetIds).toEqual(["group-1", "group-2"]);
    expect(deviceSetRequest.includeMaintenance).toBe(false);
    expect(deviceSetRequest.forceIncludeMaintenance).toBe(false);
  });

  it("does not build a request until target and scope are valid", () => {
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "" })).toBeUndefined();
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "0" })).toBeUndefined();
    expect(
      buildPreviewCurtailmentPlanRequest({ ...baseValues, scopeType: "deviceSet", deviceSetIds: [] }),
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
    );
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
