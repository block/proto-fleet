import { useEffect, useMemo, useRef, useState } from "react";
import { create, toJsonString } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CurtailmentMode,
  CurtailmentPriority,
  FixedKwParamsSchema,
  type PreviewCurtailmentPlanRequest,
  PreviewCurtailmentPlanRequestSchema,
  type PreviewCurtailmentPlanResponse,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { curtailmentNumericFieldLimits } from "@/protoFleet/features/energy/curtailmentNumericFields";
import {
  buildCurtailmentScopes,
  buildForceInclusionFields,
} from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import type { CurtailmentFormValues, CurtailmentPlanPreview } from "@/protoFleet/features/energy/CurtailmentStartModal";
import { useAuthErrors } from "@/protoFleet/store";

interface UseCurtailmentPlanPreviewOptions {
  open: boolean;
  values: CurtailmentFormValues;
  disabled?: boolean;
  debounceMs?: number;
  refreshIntervalMs?: number;
}

type CurtailmentPlanPreviewRequestValues = Pick<
  CurtailmentFormValues,
  | "scopeType"
  | "scopeId"
  | "siteSelection"
  | "siteId"
  | "siteIds"
  | "deviceSetIds"
  | "deviceIdentifiers"
  | "minerSelectionMode"
  | "curtailmentMode"
  | "targetKw"
  | "toleranceKw"
  | "priority"
  | "includeMaintenance"
  | "forceIncludeAllPairedMiners"
>;

interface CurtailmentPlanPreviewResult {
  preview?: CurtailmentPlanPreview;
  previewError?: string;
  isPreviewLoading: boolean;
}

interface CurtailmentPlanPreviewState {
  response?: PreviewCurtailmentPlanResponse;
  responseRequestKey?: string;
  responseRequestValues?: CurtailmentPlanPreviewRequestValues;
  previewError?: string;
  isPreviewLoading: boolean;
  requestKey?: string;
}

const emptyPreviewResult: CurtailmentPlanPreviewResult = {
  preview: undefined,
  previewError: undefined,
  isPreviewLoading: false,
};

const emptyPreviewState: CurtailmentPlanPreviewState = {
  response: undefined,
  responseRequestKey: undefined,
  responseRequestValues: undefined,
  previewError: undefined,
  isPreviewLoading: false,
  requestKey: undefined,
};

interface CurtailmentPlanPreviewSource {
  selectedMinerCount: number;
  facilityFanDeviceCount?: number;
  unavailableMinerCount?: number;
  targetKw?: number;
  estimatedReductionKw: number;
}

const emptyCandidatesPreviewError = "No miners match this curtailment.";

function parseNumber(value: string | undefined, isValid: (value: number) => boolean): number | undefined {
  const trimmed = value?.trim() ?? "";
  if (trimmed === "") {
    return undefined;
  }

  const parsed = Number(trimmed);
  return Number.isFinite(parsed) && isValid(parsed) ? parsed : undefined;
}

function parsePositiveNumber(value: string): number | undefined {
  return parseNumber(value, (parsed) => parsed > 0);
}

function parseNonNegativeNumber(value: string): number | undefined {
  return parseNumber(value, (parsed) => parsed >= 0);
}

function parsePositiveInteger(value: string): number | undefined {
  return parseNumber(value, (parsed) => parsed > 0 && Number.isInteger(parsed));
}

function parseNonNegativeInteger(value: string): number | undefined {
  return parseNumber(value, (parsed) => parsed >= 0 && Number.isInteger(parsed));
}

function toApiPriority(priority: CurtailmentFormValues["priority"]): CurtailmentPriority {
  return priority === "emergency" ? CurtailmentPriority.EMERGENCY : CurtailmentPriority.NORMAL;
}

function cloneRequestValues(values: CurtailmentPlanPreviewRequestValues): CurtailmentPlanPreviewRequestValues {
  return {
    ...values,
    siteIds: values.siteIds ? [...values.siteIds] : undefined,
    deviceSetIds: [...values.deviceSetIds],
    deviceIdentifiers: [...values.deviceIdentifiers],
  };
}

function hasPreviewTargets(response: PreviewCurtailmentPlanResponse): boolean {
  return response.candidates.length > 0 || response.policyTargetCount > 0;
}

export function buildPreviewCurtailmentPlanRequest(
  values: CurtailmentPlanPreviewRequestValues,
): PreviewCurtailmentPlanRequest | undefined {
  const scopes = buildCurtailmentScopes(values);

  if (scopes === undefined) {
    return undefined;
  }
  if (values.curtailmentMode === "fullFleet") {
    // Mirror the Start request builder so preview counts match what a
    // subsequent Start will actually target.
    return create(PreviewCurtailmentPlanRequestSchema, {
      scopes,
      mode: CurtailmentMode.FULL_FLEET,
      priority: toApiPriority(values.priority),
      ...buildForceInclusionFields(values),
    });
  }

  const targetKw = parsePositiveNumber(values.targetKw);

  if (targetKw === undefined) {
    return undefined;
  }

  return create(PreviewCurtailmentPlanRequestSchema, {
    scopes,
    mode: CurtailmentMode.FIXED_KW,
    priority: toApiPriority(values.priority),
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, {
        targetKw,
        toleranceKw: parseNonNegativeNumber(values.toleranceKw),
      }),
    },
    includeMaintenance: values.includeMaintenance,
    forceIncludeMaintenance: values.includeMaintenance,
    forceIncludeAllPairedMiners: false,
  });
}

export function getUnsupportedDeviceSetPreviewError(values: CurtailmentFormValues): string | undefined {
  if (values.scopeType !== "deviceSet" || values.deviceSetIds.length === 0) {
    return undefined;
  }

  return "Rack and group curtailment previews are not supported yet. Select specific miners or the whole fleet to preview and start this curtailment.";
}

function pluralize(value: number, singular: string): string {
  return `${value} ${singular}${value === 1 ? "" : "s"}`;
}

function formatSelectedScopeLabel(count: number, singular: string): string {
  return count === 1 ? `from 1 selected ${singular}` : `from ${count} selected ${singular}s`;
}

function formatSelectedMinerScopeLabel(count: number): string {
  return count === 1 ? "from the selected miner" : "from selected miners";
}

function formatScopeLabel(values: CurtailmentFormValues): string {
  if (values.minerSelectionMode === "all") {
    return "across the fleet";
  }

  const selectedMinerCount = values.deviceIdentifiers?.length ?? 0;
  const selectedSiteIds =
    values.siteSelection === "site" || values.siteSelection === "allSites"
      ? values.siteIds?.length
        ? values.siteIds
        : values.siteId
          ? [values.siteId]
          : []
      : [];
  const selectedSiteLabel =
    values.siteSelection === "allSites"
      ? "all sites"
      : selectedSiteIds.length === 1
        ? values.scopeId?.trim() || `site ${selectedSiteIds[0]}`
        : `${selectedSiteIds.length} selected sites`;
  const siteLabel = `from ${selectedSiteLabel}`;
  if (selectedSiteIds.length > 0 && selectedMinerCount > 0) {
    return `${siteLabel} and selected miners`;
  }
  if (selectedSiteIds.length > 0) {
    return siteLabel;
  }
  if (selectedMinerCount > 0) {
    return formatSelectedMinerScopeLabel(selectedMinerCount);
  }

  switch (values.scopeType) {
    case "deviceSet":
      if (values.scopeId === "racks") {
        return formatSelectedScopeLabel(values.deviceSetIds?.length ?? 0, "rack");
      }

      if (values.scopeId === "groups") {
        return formatSelectedScopeLabel(values.deviceSetIds?.length ?? 0, "group");
      }
      return formatSelectedScopeLabel(values.deviceSetIds?.length ?? 0, "set");
    case "site":
    case "explicitMiners":
    case "wholeOrg":
    default:
      return "across the fleet";
  }
}

function formatDurationEstimate(seconds: number, approximate = true): string {
  if (seconds <= 0) {
    return "Immediately";
  }

  const prefix = approximate ? "~" : "";

  if (seconds < 60) {
    return `${prefix}${pluralize(seconds, "second")}`;
  }

  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) {
    return `${prefix}${pluralize(minutes, "minute")}`;
  }

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;

  if (remainingMinutes === 0) {
    return `${prefix}${pluralize(hours, "hour")}`;
  }

  return `${prefix}${pluralize(hours, "hour")} ${pluralize(remainingMinutes, "minute")}`;
}

function formatDurationWithInfrastructureDelay(
  minerDurationSec: number | undefined,
  infrastructureDelaySec: number,
): string {
  if (minerDurationSec !== undefined) {
    return formatDurationEstimate(minerDurationSec + infrastructureDelaySec);
  }

  if (infrastructureDelaySec === 0) {
    return "Server default";
  }

  return `Server default + ${formatDurationEstimate(infrastructureDelaySec, false)}`;
}

function facilityFanDelaySeconds(
  facilityFanDeviceIds: CurtailmentFormValues["facilityFanDeviceIds"],
  delaySecValue: string | undefined,
): number {
  if ((facilityFanDeviceIds?.length ?? 0) === 0) {
    return 0;
  }

  return parseNonNegativeInteger(delaySecValue ?? "") ?? 0;
}

function estimateConfiguredBatchDurationSeconds(
  batchSizeValue: string,
  intervalSecValue: string,
  selectedMinerCount: number,
): number | undefined {
  const batchSize = parsePositiveInteger(batchSizeValue);
  const intervalSec = parseNonNegativeInteger(intervalSecValue);

  if (batchSize === undefined || intervalSec === undefined) {
    return undefined;
  }

  return estimateBatchDurationSeconds(batchSize, intervalSec, selectedMinerCount);
}

function estimateBatchDurationSeconds(batchSize: number, intervalSec: number, selectedMinerCount: number): number {
  const batchCount = Math.ceil(selectedMinerCount / batchSize);
  return Math.max(batchCount - 1, 0) * intervalSec;
}

function estimateCurtailDuration(values: CurtailmentFormValues, selectedMinerCount: number): string {
  const minerDurationSec = estimateConfiguredBatchDurationSeconds(
    values.curtailBatchSize,
    values.curtailBatchIntervalSec,
    selectedMinerCount,
  );
  const infrastructureDelaySec = facilityFanDelaySeconds(values.facilityFanDeviceIds, values.fanOffDelaySec);

  return formatDurationWithInfrastructureDelay(minerDurationSec, infrastructureDelaySec);
}

function estimateRestoreDuration(values: CurtailmentFormValues, selectedMinerCount: number): string {
  const batchSize = parseNonNegativeInteger(values.restoreBatchSize) ?? 0;
  const intervalSec = parseNonNegativeInteger(values.restoreIntervalSec) ?? 0;
  const infrastructureDelaySec = facilityFanDelaySeconds(values.facilityFanDeviceIds, values.fanRestoreDelaySec);
  if (intervalSec === 0) {
    return formatDurationEstimate(infrastructureDelaySec);
  }
  const effectiveBatchSize = batchSize === 0 ? curtailmentNumericFieldLimits.restoreBatchSize : batchSize;
  const minerDurationSec = estimateBatchDurationSeconds(effectiveBatchSize, intervalSec, selectedMinerCount);
  return formatDurationEstimate(minerDurationSec + infrastructureDelaySec);
}

export function createCurtailmentPlanPreview(
  values: CurtailmentFormValues,
  source: CurtailmentPlanPreviewSource,
): CurtailmentPlanPreview {
  const selectedMinerCount = Number.isFinite(source.selectedMinerCount) ? source.selectedMinerCount : 0;
  const facilityFanDeviceCount =
    source.facilityFanDeviceCount !== undefined && Number.isFinite(source.facilityFanDeviceCount)
      ? source.facilityFanDeviceCount
      : (values.facilityFanDeviceIds?.length ?? 0);
  const fallbackTargetKw =
    values.curtailmentMode === "fullFleet" && Number.isFinite(source.estimatedReductionKw)
      ? source.estimatedReductionKw
      : (parsePositiveNumber(values.targetKw) ?? 0);
  const targetKw =
    source.targetKw !== undefined && Number.isFinite(source.targetKw) ? source.targetKw : fallbackTargetKw;
  const estimatedReductionKw = Number.isFinite(source.estimatedReductionKw) ? source.estimatedReductionKw : targetKw;

  return {
    selectedMinerCount,
    facilityFanDeviceCount,
    unavailableMinerCount:
      source.unavailableMinerCount !== undefined && Number.isFinite(source.unavailableMinerCount)
        ? source.unavailableMinerCount
        : undefined,
    targetKw,
    estimatedReductionKw,
    curtailEstimate: estimateCurtailDuration(values, selectedMinerCount),
    restoreEstimate: estimateRestoreDuration(values, selectedMinerCount),
    scopeLabel: formatScopeLabel(values),
  };
}

function toCurtailmentPlanPreview(
  response: PreviewCurtailmentPlanResponse,
  values: CurtailmentFormValues,
): CurtailmentPlanPreview {
  const fixedKw = response.modeParams.case === "fixedKw" ? response.modeParams.value : undefined;
  const targetKw =
    fixedKw?.targetKw ??
    (values.curtailmentMode === "fullFleet" && Number.isFinite(response.estimatedReductionKw)
      ? response.estimatedReductionKw
      : undefined);

  return createCurtailmentPlanPreview(values, {
    selectedMinerCount: response.policyTargetCount > 0 ? response.policyTargetCount : response.candidates.length,
    unavailableMinerCount: response.unavailableTargetCount,
    targetKw,
    estimatedReductionKw: response.estimatedReductionKw,
  });
}

export function useCurtailmentPlanPreview({
  open,
  values,
  disabled = false,
  debounceMs = 300,
  refreshIntervalMs = 10_000,
}: UseCurtailmentPlanPreviewOptions): CurtailmentPlanPreviewResult {
  const { handleAuthErrors } = useAuthErrors();
  const [state, setState] = useState<CurtailmentPlanPreviewState>(emptyPreviewState);
  const requestGenerationRef = useRef(0);
  const requestValues = useMemo<CurtailmentPlanPreviewRequestValues>(
    () => ({
      scopeType: values.scopeType,
      scopeId: values.scopeId,
      siteSelection: values.siteSelection,
      siteId: values.siteId,
      siteIds: values.siteIds,
      deviceSetIds: values.deviceSetIds,
      deviceIdentifiers: values.deviceIdentifiers,
      minerSelectionMode: values.minerSelectionMode,
      curtailmentMode: values.curtailmentMode,
      targetKw: values.targetKw,
      toleranceKw: values.toleranceKw,
      priority: values.priority,
      includeMaintenance: values.includeMaintenance,
      forceIncludeAllPairedMiners: values.forceIncludeAllPairedMiners,
    }),
    [
      values.deviceSetIds,
      values.deviceIdentifiers,
      values.minerSelectionMode,
      values.curtailmentMode,
      values.includeMaintenance,
      values.forceIncludeAllPairedMiners,
      values.priority,
      values.scopeId,
      values.siteSelection,
      values.siteId,
      values.siteIds,
      values.scopeType,
      values.targetKw,
      values.toleranceKw,
    ],
  );
  const requestState = useMemo(() => {
    const request = buildPreviewCurtailmentPlanRequest(requestValues);

    return request === undefined
      ? undefined
      : {
          request,
          requestKey: toJsonString(PreviewCurtailmentPlanRequestSchema, request),
        };
  }, [requestValues]);
  const unsupportedDeviceSetPreviewError = getUnsupportedDeviceSetPreviewError(values);

  useEffect(() => {
    if (!open || disabled) {
      return;
    }

    if (requestState === undefined) {
      return;
    }

    let isActive = true;
    const abortController = new AbortController();
    const timeoutId = setTimeout(() => {
      const requestGeneration = requestGenerationRef.current + 1;
      requestGenerationRef.current = requestGeneration;
      setState((current) => ({
        ...current,
        previewError: undefined,
        isPreviewLoading: true,
        requestKey: requestState.requestKey,
      }));

      void curtailmentClient
        .previewCurtailmentPlan(requestState.request, { signal: abortController.signal })
        .then((response) => {
          if (!isActive || requestGeneration !== requestGenerationRef.current) {
            return;
          }

          if (!hasPreviewTargets(response)) {
            setState({
              response: undefined,
              responseRequestKey: undefined,
              responseRequestValues: undefined,
              previewError: emptyCandidatesPreviewError,
              isPreviewLoading: false,
              requestKey: requestState.requestKey,
            });
            return;
          }

          setState({
            response,
            responseRequestKey: requestState.requestKey,
            responseRequestValues: cloneRequestValues(requestValues),
            previewError: undefined,
            isPreviewLoading: false,
            requestKey: requestState.requestKey,
          });
        })
        .catch((error) => {
          if (!isActive || requestGeneration !== requestGenerationRef.current) {
            return;
          }

          handleAuthErrors({
            error,
            onError: (err) => {
              if (!isActive || requestGeneration !== requestGenerationRef.current) {
                return;
              }

              setState({
                response: undefined,
                responseRequestKey: undefined,
                responseRequestValues: undefined,
                previewError: getErrorMessage(err, "Preview is unavailable."),
                isPreviewLoading: false,
                requestKey: requestState.requestKey,
              });
            },
          });
        });
    }, debounceMs);

    return () => {
      isActive = false;
      clearTimeout(timeoutId);
      abortController.abort();
    };
  }, [debounceMs, disabled, handleAuthErrors, open, requestState, requestValues]);

  useEffect(() => {
    if (!open || disabled || refreshIntervalMs <= 0 || requestState === undefined) {
      return;
    }

    let isActive = true;
    let refreshInFlight = false;
    const abortControllers = new Set<AbortController>();
    const intervalId = setInterval(() => {
      if (refreshInFlight) {
        return;
      }

      refreshInFlight = true;
      const requestGeneration = requestGenerationRef.current + 1;
      requestGenerationRef.current = requestGeneration;
      const abortController = new AbortController();
      abortControllers.add(abortController);

      void curtailmentClient
        .previewCurtailmentPlan(requestState.request, { signal: abortController.signal })
        .then((response) => {
          if (!isActive || requestGeneration !== requestGenerationRef.current) {
            return;
          }

          if (!hasPreviewTargets(response)) {
            setState({
              response: undefined,
              responseRequestKey: undefined,
              responseRequestValues: undefined,
              previewError: emptyCandidatesPreviewError,
              isPreviewLoading: false,
              requestKey: requestState.requestKey,
            });
            return;
          }

          setState({
            response,
            responseRequestKey: requestState.requestKey,
            responseRequestValues: cloneRequestValues(requestValues),
            previewError: undefined,
            isPreviewLoading: false,
            requestKey: requestState.requestKey,
          });
        })
        .catch((error) => {
          if (!isActive || requestGeneration !== requestGenerationRef.current) {
            return;
          }

          handleAuthErrors({
            error,
            onError: (err) => {
              if (!isActive || requestGeneration !== requestGenerationRef.current) {
                return;
              }

              setState({
                response: undefined,
                responseRequestKey: undefined,
                responseRequestValues: undefined,
                previewError: getErrorMessage(err, "Preview is unavailable."),
                isPreviewLoading: false,
                requestKey: requestState.requestKey,
              });
            },
          });
        })
        .finally(() => {
          refreshInFlight = false;
          abortControllers.delete(abortController);
        });
    }, refreshIntervalMs);

    return () => {
      isActive = false;
      clearInterval(intervalId);
      abortControllers.forEach((abortController) => abortController.abort());
      abortControllers.clear();
    };
  }, [disabled, handleAuthErrors, open, refreshIntervalMs, requestState, requestValues]);

  if (!open || disabled) {
    return emptyPreviewResult;
  }

  if (unsupportedDeviceSetPreviewError) {
    return {
      preview: undefined,
      previewError: unsupportedDeviceSetPreviewError,
      isPreviewLoading: false,
    };
  }

  const hasCurrentResponse = requestState !== undefined && state.responseRequestKey === requestState.requestKey;
  const hasCurrentPreviewState = requestState !== undefined && state.requestKey === requestState.requestKey;
  const isCurrentPreviewLoading = requestState !== undefined && (!hasCurrentPreviewState || state.isPreviewLoading);
  const renderableResponse = hasCurrentResponse ? state.response : undefined;
  const previewValues =
    hasCurrentResponse || state.responseRequestValues === undefined
      ? values
      : { ...values, ...state.responseRequestValues };

  return {
    preview: renderableResponse !== undefined ? toCurtailmentPlanPreview(renderableResponse, previewValues) : undefined,
    previewError: hasCurrentPreviewState ? state.previewError : undefined,
    isPreviewLoading: isCurrentPreviewLoading,
  };
}
