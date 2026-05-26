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
  ScopeDeviceSetsSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
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
  | "deviceSetIds"
  | "deviceIdentifiers"
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

interface PreviewRequestState {
  request: PreviewCurtailmentPlanRequest;
  requestKey: string;
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

const emptyCandidatesPreviewError = "No miners match this curtailment.";

function parseNumber(value: string, isValid: (value: number) => boolean): number | undefined {
  const trimmed = value.trim();
  if (trimmed === "") {
    return undefined;
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || !isValid(parsed)) {
    return undefined;
  }

  return parsed;
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
    case "deviceSet":
      if (values.deviceSetIds.length === 0) {
        return undefined;
      }

      return {
        case: "deviceSetIds",
        value: create(ScopeDeviceSetsSchema, {
          deviceSetIds: values.deviceSetIds,
        }),
      };
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
  const targetKw = parsePositiveNumber(values.targetKw);
  const scope = buildScope(values);

  if (targetKw === undefined || scope === undefined) {
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

function pluralize(value: number, singular: string): string {
  return `${value} ${singular}${value === 1 ? "" : "s"}`;
}

function formatSelectedScopeLabel(count: number, singular: string): string {
  return count === 1 ? `from 1 selected ${singular}` : `from ${count} selected ${singular}s`;
}

function formatScopeLabel(values: CurtailmentFormValues): string {
  switch (values.scopeType) {
    case "deviceSet":
      if (values.scopeId === "racks") {
        return formatSelectedScopeLabel(values.deviceSetIds.length, "rack");
      }

      if (values.scopeId === "groups") {
        return formatSelectedScopeLabel(values.deviceSetIds.length, "group");
      }

      return formatSelectedScopeLabel(values.deviceSetIds.length, "set");
    case "explicitMiners":
      return formatSelectedScopeLabel(values.deviceIdentifiers.length, "miner");
    case "wholeOrg":
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

function estimateCurtailDuration(values: CurtailmentFormValues): string {
  const minDurationSec = parseNonNegativeInteger(values.minDurationSec);
  const maxDurationSec = parseNonNegativeInteger(values.maxDurationSec);
  const hasMinDuration = minDurationSec !== undefined && minDurationSec > 0;
  const hasMaxDuration = maxDurationSec !== undefined && maxDurationSec > 0;

  if (hasMinDuration && hasMaxDuration) {
    if (minDurationSec === maxDurationSec) {
      return formatDurationEstimate(minDurationSec, false);
    }

    return `${formatDurationEstimate(minDurationSec, false)} - ${formatDurationEstimate(maxDurationSec, false)}`;
  }

  if (hasMinDuration) {
    return `${formatDurationEstimate(minDurationSec, false)} - server default`;
  }

  if (hasMaxDuration) {
    return `Up to ${formatDurationEstimate(maxDurationSec, false)}`;
  }

  return "Server default";
}

function estimateRestoreDuration(values: CurtailmentFormValues, selectedMinerCount: number): string {
  const restoreBatchSize = parsePositiveInteger(values.restoreBatchSize);
  const restoreIntervalSec = parsePositiveInteger(values.restoreIntervalSec);

  if (restoreBatchSize === undefined || restoreIntervalSec === undefined) {
    return "Server default";
  }

  const restoreBatchCount = Math.ceil(selectedMinerCount / restoreBatchSize);
  return formatDurationEstimate(Math.max(restoreBatchCount - 1, 0) * restoreIntervalSec);
}

function toCurtailmentPlanPreview(
  response: PreviewCurtailmentPlanResponse,
  values: CurtailmentFormValues,
): CurtailmentPlanPreview {
  const fixedKw = response.modeParams.case === "fixedKw" ? response.modeParams.value : undefined;

  return {
    selectedMinerCount: response.candidates.length,
    targetKw: fixedKw?.targetKw ?? parsePositiveNumber(values.targetKw) ?? 0,
    estimatedReductionKw: response.estimatedReductionKw,
    curtailEstimate: estimateCurtailDuration(values),
    restoreEstimate: estimateRestoreDuration(values, response.candidates.length),
    scopeLabel: formatScopeLabel(values),
  };
}

function createPreviewErrorState(requestKey: string, previewError: string): CurtailmentPlanPreviewState {
  return {
    response: undefined,
    responseRequestKey: undefined,
    responseRequestValues: undefined,
    previewError,
    isPreviewLoading: false,
    requestKey,
  };
}

function createPreviewResponseState(
  response: PreviewCurtailmentPlanResponse,
  requestKey: string,
  requestValues: CurtailmentPlanPreviewRequestValues,
): CurtailmentPlanPreviewState {
  return {
    response,
    responseRequestKey: requestKey,
    responseRequestValues: cloneRequestValues(requestValues),
    previewError: undefined,
    isPreviewLoading: false,
    requestKey,
  };
}

function getPreviewValues(
  values: CurtailmentFormValues,
  state: CurtailmentPlanPreviewState,
  hasCurrentResponse: boolean,
): CurtailmentFormValues {
  if (hasCurrentResponse || state.responseRequestValues === undefined) {
    return values;
  }

  return { ...values, ...state.responseRequestValues };
}

function getPreviewResult(
  requestState: PreviewRequestState | undefined,
  state: CurtailmentPlanPreviewState,
  values: CurtailmentFormValues,
): CurtailmentPlanPreviewResult {
  if (requestState === undefined) {
    return emptyPreviewResult;
  }

  const hasCurrentPreviewState = state.requestKey === requestState.requestKey;
  const hasCurrentResponse = state.responseRequestKey === requestState.requestKey;
  const previewValues = getPreviewValues(values, state, hasCurrentResponse);

  return {
    preview: state.response ? toCurtailmentPlanPreview(state.response, previewValues) : undefined,
    previewError: hasCurrentPreviewState ? state.previewError : undefined,
    isPreviewLoading: hasCurrentPreviewState ? state.isPreviewLoading : false,
  };
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
      deviceSetIds: values.deviceSetIds,
      deviceIdentifiers: values.deviceIdentifiers,
      targetKw: values.targetKw,
      toleranceKw: values.toleranceKw,
      priority: values.priority,
      includeMaintenance: values.includeMaintenance,
    }),
    [
      values.deviceSetIds,
      values.deviceIdentifiers,
      values.includeMaintenance,
      values.priority,
      values.scopeId,
      values.scopeType,
      values.targetKw,
      values.toleranceKw,
    ],
  );
  const requestState = useMemo<PreviewRequestState | undefined>(() => {
    const request = buildPreviewCurtailmentPlanRequest(requestValues);

    if (request === undefined) {
      return undefined;
    }

    return {
      request,
      requestKey: toJsonString(PreviewCurtailmentPlanRequestSchema, request),
    };
  }, [requestValues]);
  useEffect(() => {
    if (!open || disabled) {
      return;
    }

    if (requestState === undefined) {
      return;
    }

    let isActive = true;
    const abortController = new AbortController();
    const loadPreview = async (): Promise<void> => {
      try {
        const response = await curtailmentClient.previewCurtailmentPlan(requestState.request, {
          signal: abortController.signal,
        });

        if (!isActive) {
          return;
        }

        if (response.candidates.length === 0) {
          setState(createPreviewErrorState(requestState.requestKey, emptyCandidatesPreviewError));
          return;
        }

        setState(createPreviewResponseState(response, requestState.requestKey, requestValues));
      } catch (error) {
        if (!isActive) {
          return;
        }

        handleAuthErrors({
          error,
          onError: (err) => {
            if (!isActive) {
              return;
            }

            setState(createPreviewErrorState(requestState.requestKey, getErrorMessage(err, "Preview is unavailable.")));
          },
        });
      }
    };
    const timeoutId = setTimeout(() => {
      setState((current) => ({
        ...current,
        previewError: undefined,
        isPreviewLoading: true,
        requestKey: requestState.requestKey,
      }));

      void loadPreview();
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

  return getPreviewResult(requestState, state, values);
}
