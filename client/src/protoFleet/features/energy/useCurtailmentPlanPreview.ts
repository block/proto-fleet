import { useEffect, useMemo, useState } from "react";
import { create, toJsonString } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CurtailmentMode,
  CurtailmentPriority,
  FixedKwParamsSchema,
  type PreviewCurtailmentPlanRequest,
  PreviewCurtailmentPlanRequestSchema,
  type PreviewCurtailmentPlanResponse,
  ScopeDeviceListSchema,
  ScopeSiteSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { parseCurtailmentSiteId } from "@/protoFleet/features/energy/curtailmentRequestBuilders";
import type { CurtailmentFormValues, CurtailmentPlanPreview } from "@/protoFleet/features/energy/CurtailmentStartModal";
import { useAuthErrors } from "@/protoFleet/store";

interface UseCurtailmentPlanPreviewOptions {
  open: boolean;
  values: CurtailmentFormValues;
  disabled?: boolean;
  debounceMs?: number;
}

type CurtailmentPlanPreviewRequestValues = Pick<
  CurtailmentFormValues,
  | "scopeType"
  | "scopeId"
  | "siteId"
  | "deviceSetIds"
  | "deviceIdentifiers"
  | "curtailmentMode"
  | "targetKw"
  | "toleranceKw"
  | "priority"
  | "includeMaintenance"
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
    deviceSetIds: [...values.deviceSetIds],
    deviceIdentifiers: [...values.deviceIdentifiers],
  };
}

function buildScope(values: CurtailmentPlanPreviewRequestValues): PreviewCurtailmentPlanRequest["scope"] | undefined {
  switch (values.scopeType) {
    case "wholeOrg":
      return {
        case: "wholeOrg",
        value: create(ScopeWholeOrgSchema, {}),
      };
    case "site": {
      const siteId = parseCurtailmentSiteId(values.siteId);
      if (siteId === undefined) {
        return undefined;
      }
      return {
        case: "site",
        value: create(ScopeSiteSchema, { siteId }),
      };
    }
    case "deviceSet":
      return undefined;
    case "explicitMiners":
      if (values.deviceIdentifiers.length === 0) {
        return undefined;
      }
      return {
        case: "deviceIdentifiers",
        value: create(ScopeDeviceListSchema, {
          deviceIdentifiers: values.deviceIdentifiers,
        }),
      };
  }
}

export function buildPreviewCurtailmentPlanRequest(
  values: CurtailmentPlanPreviewRequestValues,
): PreviewCurtailmentPlanRequest | undefined {
  const scope = buildScope(values);

  if (scope === undefined) {
    return undefined;
  }

  if (values.curtailmentMode === "fullFleet") {
    return create(PreviewCurtailmentPlanRequestSchema, {
      scope,
      mode: CurtailmentMode.FULL_FLEET,
      priority: toApiPriority(values.priority),
      includeMaintenance: values.includeMaintenance,
      forceIncludeMaintenance: values.includeMaintenance,
    });
  }

  const targetKw = parsePositiveNumber(values.targetKw);

  if (targetKw === undefined) {
    return undefined;
  }

  return create(PreviewCurtailmentPlanRequestSchema, {
    scope,
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

function formatScopeLabel(values: CurtailmentFormValues): string {
  switch (values.scopeType) {
    case "site":
      return values.siteId ? `at site ${values.siteId}` : "at one site";
    case "deviceSet":
      if (values.scopeId === "racks") {
        return formatSelectedScopeLabel(values.deviceSetIds?.length ?? 0, "rack");
      }

      if (values.scopeId === "groups") {
        return formatSelectedScopeLabel(values.deviceSetIds?.length ?? 0, "group");
      }

      return formatSelectedScopeLabel(values.deviceSetIds?.length ?? 0, "set");
    case "explicitMiners":
      return formatSelectedScopeLabel(values.deviceIdentifiers?.length ?? 0, "miner");
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

function estimateBatchDuration(batchSizeValue: string, intervalSecValue: string, selectedMinerCount: number): string {
  const batchSize = parsePositiveInteger(batchSizeValue);
  const intervalSec = parseNonNegativeInteger(intervalSecValue);

  if (batchSize === undefined || intervalSec === undefined) {
    return "Server default";
  }

  const batchCount = Math.ceil(selectedMinerCount / batchSize);
  return formatDurationEstimate(Math.max(batchCount - 1, 0) * intervalSec);
}

function estimateCurtailDuration(values: CurtailmentFormValues, selectedMinerCount: number): string {
  return estimateBatchDuration(values.curtailBatchSize, values.curtailBatchIntervalSec, selectedMinerCount);
}

function estimateRestoreDuration(values: CurtailmentFormValues, selectedMinerCount: number): string {
  return estimateBatchDuration(values.restoreBatchSize, values.restoreIntervalSec, selectedMinerCount);
}

export function createCurtailmentPlanPreview(
  values: CurtailmentFormValues,
  source: CurtailmentPlanPreviewSource,
): CurtailmentPlanPreview {
  const selectedMinerCount = Number.isFinite(source.selectedMinerCount) ? source.selectedMinerCount : 0;
  const fallbackTargetKw =
    values.curtailmentMode === "fullFleet" && Number.isFinite(source.estimatedReductionKw)
      ? source.estimatedReductionKw
      : (parsePositiveNumber(values.targetKw) ?? 0);
  const targetKw =
    source.targetKw !== undefined && Number.isFinite(source.targetKw) ? source.targetKw : fallbackTargetKw;
  const estimatedReductionKw = Number.isFinite(source.estimatedReductionKw) ? source.estimatedReductionKw : targetKw;

  return {
    selectedMinerCount,
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
    selectedMinerCount: response.candidates.length,
    targetKw,
    estimatedReductionKw: response.estimatedReductionKw,
  });
}

export function useCurtailmentPlanPreview({
  open,
  values,
  disabled = false,
  debounceMs = 300,
}: UseCurtailmentPlanPreviewOptions): CurtailmentPlanPreviewResult {
  const { handleAuthErrors } = useAuthErrors();
  const [state, setState] = useState<CurtailmentPlanPreviewState>(emptyPreviewState);
  const requestValues = useMemo<CurtailmentPlanPreviewRequestValues>(
    () => ({
      scopeType: values.scopeType,
      scopeId: values.scopeId,
      siteId: values.siteId,
      deviceSetIds: values.deviceSetIds,
      deviceIdentifiers: values.deviceIdentifiers,
      curtailmentMode: values.curtailmentMode,
      targetKw: values.targetKw,
      toleranceKw: values.toleranceKw,
      priority: values.priority,
      includeMaintenance: values.includeMaintenance,
    }),
    [
      values.deviceSetIds,
      values.deviceIdentifiers,
      values.curtailmentMode,
      values.includeMaintenance,
      values.priority,
      values.scopeId,
      values.siteId,
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
      setState((current) => ({
        ...current,
        previewError: undefined,
        isPreviewLoading: true,
        requestKey: requestState.requestKey,
      }));

      void curtailmentClient
        .previewCurtailmentPlan(requestState.request, { signal: abortController.signal })
        .then((response) => {
          if (!isActive) {
            return;
          }

          if (response.candidates.length === 0) {
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
          if (!isActive) {
            return;
          }

          handleAuthErrors({
            error,
            onError: (err) => {
              if (!isActive) {
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
