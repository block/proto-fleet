import { renderHook, type RenderHookResult, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import {
  CurtailmentCandidateSchema,
  CurtailmentMode,
  FixedKwParamsSchema,
  type PreviewCurtailmentPlanResponse,
  PreviewCurtailmentPlanResponseSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import type { CurtailmentFormValues } from "@/protoFleet/features/energy/curtailmentTypes";
import {
  buildPreviewCurtailmentPlanRequest,
  estimateCurtailDuration,
  estimateRestoreDuration,
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
  minDurationSec: "",
  maxDurationSec: "",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak",
  includeMaintenance: true,
};

interface PreviewHookRenderProps {
  values?: CurtailmentFormValues;
  disabled?: boolean;
}

function previewResponse(): PreviewCurtailmentPlanResponse {
  return create(PreviewCurtailmentPlanResponseSchema, {
    candidates: [
      create(CurtailmentCandidateSchema, { deviceIdentifier: "miner-1" }),
      create(CurtailmentCandidateSchema, { deviceIdentifier: "miner-2" }),
      create(CurtailmentCandidateSchema, { deviceIdentifier: "miner-3" }),
    ],
    estimatedReductionKw: 45,
    mode: CurtailmentMode.FIXED_KW,
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, { targetKw: 40 }),
    },
  });
}

function renderPreviewHook(
  initialProps: PreviewHookRenderProps = {},
): RenderHookResult<ReturnType<typeof useCurtailmentPlanPreview>, PreviewHookRenderProps> {
  return renderHook(
    ({ values = baseValues, disabled }: PreviewHookRenderProps) =>
      useCurtailmentPlanPreview({
        open: true,
        values,
        disabled,
        debounceMs: 0,
      }),
    {
      initialProps,
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

  it("builds a supported fixed-kW whole-fleet preview request", () => {
    const request = buildPreviewCurtailmentPlanRequest(baseValues);

    expect(request?.scope.case).toBe("wholeOrg");
    expect(request?.mode).toBe(CurtailmentMode.FIXED_KW);
    expect(request?.modeParams.case).toBe("fixedKw");
    if (request?.modeParams.case !== "fixedKw") {
      throw new Error("Expected fixedKw mode params");
    }
    expect(request.modeParams.value.targetKw).toBe(40);
    expect(request?.includeMaintenance).toBe(true);
    expect(request?.forceIncludeMaintenance).toBe(true);
  });

  it("builds device-set and maintenance opt-out fields", () => {
    const request = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      scopeType: "deviceSet",
      scopeId: "groups",
      deviceSetIds: ["group-1", "group-2"],
      includeMaintenance: false,
    });

    expect(request?.scope.case).toBe("deviceSetIds");
    if (request?.scope.case !== "deviceSetIds") {
      throw new Error("Expected deviceSetIds scope");
    }
    expect(request.scope.value.deviceSetIds).toEqual(["group-1", "group-2"]);
    expect(request?.includeMaintenance).toBe(false);
    expect(request?.forceIncludeMaintenance).toBe(false);
  });

  it("does not build a request until the target is valid", () => {
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "" })).toBeUndefined();
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "0" })).toBeUndefined();
  });

  it("derives restore estimate from selected miners and restore controls", () => {
    expect(estimateRestoreDuration(baseValues, 18)).toBe("~2 minutes");
    expect(estimateRestoreDuration({ ...baseValues, restoreBatchSize: "", restoreIntervalSec: "" }, 18)).toBe(
      "Server default",
    );
    expect(estimateRestoreDuration({ ...baseValues, restoreBatchSize: "1.5" }, 18)).toBe("Server default");
  });

  it("derives curtail estimate from min and max duration controls", () => {
    expect(estimateCurtailDuration(baseValues)).toBe("Server default");
    expect(estimateCurtailDuration({ ...baseValues, minDurationSec: "300", maxDurationSec: "1800" })).toBe(
      "5 minutes - 30 minutes",
    );
    expect(estimateCurtailDuration({ ...baseValues, minDurationSec: "300" })).toBe("5 minutes - server default");
    expect(estimateCurtailDuration({ ...baseValues, maxDurationSec: "1800" })).toBe("Up to 30 minutes");
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
          curtailEstimate: "Server default",
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

  it("keeps the current preview visible while new values are loading", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result, rerender } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.preview).toEqual(
        expect.objectContaining({
          targetKw: 40,
          estimatedReductionKw: 45,
        }),
      );
    });

    const currentPreview = result.current.preview;
    mockPreviewCurtailmentPlan.mockReturnValueOnce(new Promise(() => {}));

    rerender({ values: { ...baseValues, targetKw: "50" } });

    expect(result.current.preview).toBe(currentPreview);

    await waitFor(() => {
      expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(2);
    });

    expect(result.current.preview).toBe(currentPreview);
    expect(result.current.isPreviewLoading).toBe(true);
  });

  it("keeps the current preview visible while values are temporarily invalid", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result, rerender } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.preview).toBeDefined();
    });

    const currentPreview = result.current.preview;

    rerender({ values: { ...baseValues, targetKw: "" } });

    expect(result.current.preview).toBe(currentPreview);
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

  it("clears stale preview state when previewing is disabled", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result, rerender } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.preview).toBeDefined();
    });

    rerender({ disabled: true });

    expect(result.current).toEqual({
      preview: undefined,
      previewError: undefined,
      isPreviewLoading: false,
    });
  });
});
