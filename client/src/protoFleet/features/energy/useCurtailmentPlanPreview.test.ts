import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
  createCurtailmentPlanPreview,
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
  siteId: "",
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
  curtailBatchSize: "2",
  curtailBatchIntervalSec: "30",
  restoreBatchSize: "10",
  restoreIntervalSec: "120",
  reason: "Grid peak",
  includeMaintenance: true,
  forceIncludeAllPairedMiners: false,
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

function fullFleetPreviewResponse(candidateCount = 3) {
  return create(PreviewCurtailmentPlanResponseSchema, {
    candidates: Array.from({ length: candidateCount }, (_, index) =>
      create(CurtailmentCandidateSchema, { deviceIdentifier: `miner-${index + 1}` }),
    ),
    estimatedReductionKw: 45,
    mode: CurtailmentMode.FULL_FLEET,
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
    mockPreviewCurtailmentPlan.mockReset();
    mockHandleAuthErrors.mockReset();
    mockHandleAuthErrors.mockImplementation(
      ({ error, onError }: { error: unknown; onError: (error: unknown) => void }) => onError(error),
    );
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("builds supported fixed-kW preview requests", () => {
    const wholeFleetRequest = buildPreviewCurtailmentPlanRequest(baseValues);
    expect(wholeFleetRequest?.scopes[0]?.scope.case).toBe("wholeOrg");
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

    expect(minerRequest?.scopes[0]?.scope.case).toBe("deviceIdentifiers");
    if (minerRequest?.scopes[0]?.scope.case !== "deviceIdentifiers") {
      throw new Error("Expected deviceIdentifiers scope");
    }
    expect(minerRequest.scopes[0].scope.value.deviceIdentifiers).toEqual(["miner-1", "miner-2"]);
    expect(minerRequest.includeMaintenance).toBe(false);
    expect(minerRequest.forceIncludeMaintenance).toBe(false);

    const allMinerRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      scopeType: "wholeOrg",
      siteSelection: "site",
      siteId: "42",
      minerSelectionMode: "all",
      deviceIdentifiers: ["miner-1", "miner-2"],
    });

    expect(allMinerRequest?.scopes[0]?.scope.case).toBe("wholeOrg");

    const siteRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      scopeType: "site",
      scopeId: "site-42",
      siteId: " 42 ",
    });

    expect(siteRequest?.scopes[0]?.scope.case).toBe("site");
    if (siteRequest?.scopes[0]?.scope.case !== "site") {
      throw new Error("Expected site scope");
    }
    expect(siteRequest.scopes[0].scope.value.siteId).toBe(42n);

    const multiSiteRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      scopeType: "explicitMiners",
      siteSelection: "site",
      scopeId: "2 sites",
      siteId: "42",
      siteIds: ["42", "43"],
      deviceIdentifiers: ["miner-1"],
    });

    expect(multiSiteRequest?.scopes.map((scope) => scope.scope.case)).toEqual(["site", "site", "deviceIdentifiers"]);

    const allSitesRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      scopeType: "site",
      siteSelection: "allSites",
      scopeId: "All sites",
      siteId: "42",
      siteIds: ["42", "43"],
    });

    expect(allSitesRequest?.scopes.map((scope) => scope.scope.case)).toEqual(["site", "site"]);
  });

  it("builds full-fleet preview requests without requiring fixed-kW params", () => {
    const request = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      curtailmentMode: "fullFleet",
      targetKw: "",
      toleranceKw: "",
    });

    expect(request?.scopes[0]?.scope.case).toBe("wholeOrg");
    expect(request?.mode).toBe(CurtailmentMode.FULL_FLEET);
    expect(request?.modeParams.case).toBeUndefined();
    // baseValues carries a stale includeMaintenance: true (as hydrated from a
    // pre-change profile/event); the maintenance pair derives solely from the
    // all-paired flag, so it must not leak into the preview request.
    expect(request?.includeMaintenance).toBe(false);
    expect(request?.forceIncludeMaintenance).toBe(false);
    expect(request?.forceIncludeAllPairedMiners).toBe(false);
  });

  it("sends all-paired targeting only for full-fleet preview requests", () => {
    const fixedKwRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      forceIncludeAllPairedMiners: true,
    });
    expect(fixedKwRequest?.forceIncludeAllPairedMiners).toBe(false);

    const fullFleetRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      curtailmentMode: "fullFleet",
      targetKw: "",
      forceIncludeAllPairedMiners: true,
    });
    expect(fullFleetRequest?.forceIncludeAllPairedMiners).toBe(true);
  });

  it("mirrors the start builder's all-paired scope gate and maintenance opt-in", () => {
    const allPairedRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      curtailmentMode: "fullFleet",
      targetKw: "",
      includeMaintenance: false,
      forceIncludeAllPairedMiners: true,
    });
    expect(allPairedRequest?.forceIncludeAllPairedMiners).toBe(true);
    expect(allPairedRequest?.includeMaintenance).toBe(true);
    expect(allPairedRequest?.forceIncludeMaintenance).toBe(true);

    const minerScopedRequest = buildPreviewCurtailmentPlanRequest({
      ...baseValues,
      curtailmentMode: "fullFleet",
      targetKw: "",
      scopeType: "explicitMiners",
      deviceIdentifiers: ["miner-1"],
      includeMaintenance: false,
      forceIncludeAllPairedMiners: true,
    });
    expect(minerScopedRequest?.forceIncludeAllPairedMiners).toBe(false);
    expect(minerScopedRequest?.includeMaintenance).toBe(false);
    expect(minerScopedRequest?.forceIncludeMaintenance).toBe(false);
  });

  it("does not build a request until target and scope are valid", () => {
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "" })).toBeUndefined();
    expect(buildPreviewCurtailmentPlanRequest({ ...baseValues, targetKw: "0" })).toBeUndefined();
    expect(
      buildPreviewCurtailmentPlanRequest({ ...baseValues, scopeType: "deviceSet", deviceSetIds: [] }),
    ).toBeUndefined();
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
        scopeType: "site",
        scopeId: "site-bad",
        siteId: "site-42",
      }),
    ).toBeUndefined();
    expect(
      buildPreviewCurtailmentPlanRequest({
        ...baseValues,
        scopeType: "site",
        scopeId: "site-zero",
        siteId: "0",
      }),
    ).toBeUndefined();
  });

  it("surfaces unsupported device-set previews without calling the API", () => {
    const { result } = renderPreviewHook({
      ...baseValues,
      scopeType: "deviceSet",
      scopeId: "racks",
      deviceSetIds: ["rack-1"],
    });

    expect(result.current).toEqual({
      preview: undefined,
      previewError:
        "Rack and group curtailment previews are not supported yet. Select specific miners or the whole fleet to preview and start this curtailment.",
      isPreviewLoading: false,
    });
    expect(mockPreviewCurtailmentPlan).not.toHaveBeenCalled();
  });

  it("skips site-scoped previews with invalid site ids", () => {
    const { result } = renderPreviewHook({
      ...baseValues,
      scopeType: "site",
      scopeId: "site-bad",
      siteId: "site-42",
    });

    expect(result.current).toEqual({
      preview: undefined,
      previewError: undefined,
      isPreviewLoading: false,
    });
    expect(mockPreviewCurtailmentPlan).not.toHaveBeenCalled();
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
          curtailEstimate: "~30 seconds",
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

  it("fetches site-scoped previews with the selected site id", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result } = renderPreviewHook({
      ...baseValues,
      scopeType: "site",
      siteSelection: "site",
      scopeId: "Austin, TX",
      siteId: "42",
    });

    await waitFor(() => {
      expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(1);
    });

    const request = mockPreviewCurtailmentPlan.mock.calls[0][0];
    expect(request.scopes[0]?.scope.case).toBe("site");
    if (request.scopes[0]?.scope.case !== "site") {
      throw new Error("Expected site scope");
    }
    expect(request.scopes[0].scope.value.siteId).toBe(42n);
    expect(result.current.preview).toEqual(expect.objectContaining({ scopeLabel: "from Austin, TX" }));
  });

  it("uses the site label in mixed site and miner preview labels", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result } = renderPreviewHook({
      ...baseValues,
      scopeType: "explicitMiners",
      siteSelection: "site",
      scopeId: "Austin, TX",
      siteId: "42",
      deviceIdentifiers: ["miner-1", "miner-2"],
    });

    await waitFor(() => {
      expect(result.current.preview?.scopeLabel).toBe("from Austin, TX and selected miners");
    });
  });

  it("labels all-sites previews without treating them as whole fleet", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result } = renderPreviewHook({
      ...baseValues,
      scopeType: "site",
      siteSelection: "allSites",
      scopeId: "All sites",
      siteId: "42",
      siteIds: ["42", "43"],
    });

    await waitFor(() => {
      expect(result.current.preview?.scopeLabel).toBe("from all sites");
    });
  });

  it("uses selected-miner preview labels without repeating the selected count", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse(1));

    const { result } = renderPreviewHook({
      ...baseValues,
      scopeType: "explicitMiners",
      scopeId: undefined,
      deviceIdentifiers: ["miner-1"],
    });

    await waitFor(() => {
      expect(result.current.preview?.scopeLabel).toBe("from the selected miner");
    });
  });

  it("maps full-fleet previews against the estimated fleet reduction", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(fullFleetPreviewResponse());

    const { result } = renderPreviewHook({
      ...baseValues,
      curtailmentMode: "fullFleet",
      targetKw: "",
      toleranceKw: "",
    });

    await waitFor(() => {
      expect(result.current.preview).toEqual(
        expect.objectContaining({
          selectedMinerCount: 3,
          targetKw: 45,
          estimatedReductionKw: 45,
          curtailEstimate: "~30 seconds",
          restoreEstimate: "Immediately",
          scopeLabel: "across the fleet",
        }),
      );
    });

    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: CurtailmentMode.FULL_FLEET,
        modeParams: expect.objectContaining({ case: undefined }),
      }),
      expect.objectContaining({
        signal: expect.any(AbortSignal),
      }),
    );
  });

  it("uses policy target counts when unavailable all-paired targets exist", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(
      create(PreviewCurtailmentPlanResponseSchema, {
        candidates: [],
        estimatedReductionKw: 45,
        mode: CurtailmentMode.FULL_FLEET,
        policyTargetCount: 4,
        unavailableTargetCount: 3,
      }),
    );

    const { result } = renderPreviewHook({
      ...baseValues,
      curtailmentMode: "fullFleet",
      targetKw: "",
      toleranceKw: "",
      forceIncludeAllPairedMiners: true,
    });

    await waitFor(() => {
      expect(result.current.preview).toEqual(
        expect.objectContaining({
          selectedMinerCount: 4,
          unavailableMinerCount: 3,
          targetKw: 45,
          estimatedReductionKw: 45,
        }),
      );
    });
    expect(result.current.previewError).toBeUndefined();
  });

  it("treats blank restore fields as immediate in the local preview", () => {
    const preview = createCurtailmentPlanPreview(
      {
        ...baseValues,
        restoreBatchSize: "",
        restoreIntervalSec: "",
      },
      {
        selectedMinerCount: 25,
        targetKw: 40,
        estimatedReductionKw: 45,
      },
    );

    expect(preview.restoreEstimate).toBe("Immediately");
  });

  it("adds selected facility fans to local preview metadata", () => {
    const preview = createCurtailmentPlanPreview(
      {
        ...baseValues,
        facilityFanDeviceIds: ["31", "32"],
      },
      {
        selectedMinerCount: 25,
        targetKw: 40,
        estimatedReductionKw: 45,
      },
    );

    expect(preview.facilityFanDeviceCount).toBe(2);
  });

  it("adds facility fan delays to curtail and restore estimates when infrastructure is selected", () => {
    const preview = createCurtailmentPlanPreview(
      {
        ...baseValues,
        curtailBatchSize: "1",
        curtailBatchIntervalSec: "10",
        restoreBatchSize: "1",
        restoreIntervalSec: "10",
        facilityFanDeviceIds: ["31"],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "20",
      },
      {
        selectedMinerCount: 2,
        targetKw: 40,
        estimatedReductionKw: 45,
      },
    );

    expect(preview.curtailEstimate).toBe("~55 seconds");
    expect(preview.restoreEstimate).toBe("~30 seconds");
  });

  it("ignores facility fan delays when no infrastructure is selected", () => {
    const preview = createCurtailmentPlanPreview(
      {
        ...baseValues,
        curtailBatchSize: "1",
        curtailBatchIntervalSec: "10",
        restoreBatchSize: "1",
        restoreIntervalSec: "10",
        facilityFanDeviceIds: [],
        fanOffDelaySec: "45",
        fanRestoreDelaySec: "20",
      },
      {
        selectedMinerCount: 2,
        targetKw: 40,
        estimatedReductionKw: 45,
      },
    );

    expect(preview.curtailEstimate).toBe("~10 seconds");
    expect(preview.restoreEstimate).toBe("~10 seconds");
  });

  it("shows infrastructure delay alongside server-default miner curtail timing", () => {
    const preview = createCurtailmentPlanPreview(
      {
        ...baseValues,
        curtailBatchSize: "",
        curtailBatchIntervalSec: "",
        facilityFanDeviceIds: ["31"],
        fanOffDelaySec: "45",
      },
      {
        selectedMinerCount: 2,
        targetKw: 40,
        estimatedReductionKw: 45,
      },
    );

    expect(preview.curtailEstimate).toBe("Server default + 45 seconds");
  });

  it("estimates zero-sized restore waves with the safety limit when an interval is set", () => {
    const preview = createCurtailmentPlanPreview(
      {
        ...baseValues,
        restoreBatchSize: "0",
        restoreIntervalSec: "3600",
      },
      {
        selectedMinerCount: 20_001,
        targetKw: 40,
        estimatedReductionKw: 45,
      },
    );

    expect(preview.restoreEstimate).toBe("~2 hours");
  });

  it("surfaces empty preview candidates as a preview error", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse(0));

    const { result } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current).toEqual({
        preview: undefined,
        previewError: "No miners match this curtailment.",
        isPreviewLoading: false,
      });
    });
    expect(mockHandleAuthErrors).not.toHaveBeenCalled();
  });

  it("refreshes the preview while request values stay unchanged", async () => {
    let resolveRefresh: (response: ReturnType<typeof previewResponse>) => void = () => {};
    const refreshPromise = new Promise<ReturnType<typeof previewResponse>>((resolve) => {
      resolveRefresh = resolve;
    });
    mockPreviewCurtailmentPlan.mockReturnValue(refreshPromise);
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse(0));

    const { result } = renderHook(
      ({ values }) =>
        useCurtailmentPlanPreview({
          open: true,
          values,
          debounceMs: 0,
          refreshIntervalMs: 5,
        }),
      {
        initialProps: { values: baseValues },
      },
    );

    await waitFor(() => {
      expect(result.current.previewError).toBe("No miners match this curtailment.");
    });
    await waitFor(() => {
      expect(mockPreviewCurtailmentPlan.mock.calls.length).toBeGreaterThanOrEqual(2);
    });

    await act(async () => {
      resolveRefresh(previewResponse(2));
      await refreshPromise;
    });

    await waitFor(() => {
      expect(result.current.preview).toEqual(
        expect.objectContaining({
          selectedMinerCount: 2,
          estimatedReductionKw: 45,
        }),
      );
    });
    expect(result.current.previewError).toBeUndefined();
    expect(mockPreviewCurtailmentPlan.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it("ignores an older initial preview after a newer refresh completes", async () => {
    vi.useFakeTimers();
    let resolveInitial: (response: ReturnType<typeof previewResponse>) => void = () => {};
    let resolveRefresh: (response: ReturnType<typeof previewResponse>) => void = () => {};
    const initialPromise = new Promise<ReturnType<typeof previewResponse>>((resolve) => {
      resolveInitial = resolve;
    });
    const refreshPromise = new Promise<ReturnType<typeof previewResponse>>((resolve) => {
      resolveRefresh = resolve;
    });
    mockPreviewCurtailmentPlan.mockReturnValueOnce(initialPromise).mockReturnValueOnce(refreshPromise);

    const { result, rerender } = renderHook(
      ({ refreshIntervalMs }) =>
        useCurtailmentPlanPreview({
          open: true,
          values: baseValues,
          debounceMs: 0,
          refreshIntervalMs,
        }),
      {
        initialProps: { refreshIntervalMs: 5 },
      },
    );

    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5);
    });
    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(2);

    await act(async () => {
      resolveRefresh(previewResponse(0));
      await refreshPromise;
    });
    expect(result.current.previewError).toBe("No miners match this curtailment.");
    rerender({ refreshIntervalMs: 0 });

    await act(async () => {
      resolveInitial(previewResponse(3));
      await initialPromise;
    });

    expect(result.current.preview).toBeUndefined();
    expect(result.current.previewError).toBe("No miners match this curtailment.");
  });

  it("updates local preview labels without refetching for batch edits", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse());

    const { result, rerender } = renderPreviewHook();

    await waitFor(() => {
      expect(result.current.preview?.curtailEstimate).toBe("~30 seconds");
    });

    rerender({
      values: {
        ...baseValues,
        minDurationSec: "60",
        maxDurationSec: "120",
        curtailBatchSize: "1",
        curtailBatchIntervalSec: "30",
        restoreBatchSize: "1",
        restoreIntervalSec: "0",
        reason: "Updated reason",
      },
    });

    expect(result.current.preview).toEqual(
      expect.objectContaining({
        curtailEstimate: "~1 minute",
        restoreEstimate: "Immediately",
      }),
    );
    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(1);
  });

  it("hides the previous preview while a valid refresh is debounced", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse()).mockReturnValueOnce(new Promise(() => {}));

    const { result, rerender } = renderHook(
      ({ values, debounceMs }) =>
        useCurtailmentPlanPreview({
          open: true,
          values,
          debounceMs,
        }),
      {
        initialProps: { values: baseValues, debounceMs: 0 },
      },
    );

    await waitFor(() => {
      expect(result.current.preview).toBeDefined();
    });

    rerender({
      values: {
        ...baseValues,
        scopeType: "explicitMiners",
        scopeId: undefined,
        deviceIdentifiers: ["miner-99"],
        targetKw: "50",
      },
      debounceMs: 1,
    });

    await waitFor(() => {
      expect(result.current.isPreviewLoading).toBe(true);
    });

    expect(result.current.preview).toBeUndefined();
    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(2);
  });

  it("hides fixed-kW previews immediately after switching to full-fleet mode", async () => {
    mockPreviewCurtailmentPlan.mockResolvedValueOnce(previewResponse()).mockReturnValueOnce(new Promise(() => {}));

    const { result, rerender } = renderHook(
      ({ values, debounceMs }) =>
        useCurtailmentPlanPreview({
          open: true,
          values,
          debounceMs,
        }),
      {
        initialProps: { values: baseValues, debounceMs: 0 },
      },
    );

    await waitFor(() => {
      expect(result.current.preview).toEqual(expect.objectContaining({ targetKw: 40 }));
    });

    rerender({
      values: {
        ...baseValues,
        curtailmentMode: "fullFleet",
        targetKw: "",
        toleranceKw: "",
      },
      debounceMs: 1,
    });

    expect(result.current).toEqual({
      preview: undefined,
      previewError: undefined,
      isPreviewLoading: true,
    });
    expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(1);

    await waitFor(() => {
      expect(mockPreviewCurtailmentPlan).toHaveBeenCalledTimes(2);
    });
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
