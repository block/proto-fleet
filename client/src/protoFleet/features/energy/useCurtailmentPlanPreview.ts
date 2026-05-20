import { useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  CurtailmentMode as ApiCurtailmentMode,
  CurtailmentPriority as ApiCurtailmentPriority,
  CurtailmentLevel,
  CurtailmentStrategy,
  FixedKwParamsSchema,
  type PreviewCurtailmentPlanRequest,
  PreviewCurtailmentPlanRequestSchema,
  type PreviewCurtailmentPlanResponse,
  ScopeDeviceListSchema,
  ScopeDeviceSetsSchema,
  ScopeWholeOrgSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import type { CurtailmentFormValues, CurtailmentPlanPreview } from "@/protoFleet/features/energy/curtailmentTypes";
import { useAuthErrors } from "@/protoFleet/store";

interface UseCurtailmentPlanPreviewOptions {
  open: boolean;
  values: CurtailmentFormValues;
  disabled?: boolean;
  debounceMs?: number;
}

interface CurtailmentPlanPreviewState {
  preview?: CurtailmentPlanPreview;
  previewError?: string;
  isPreviewLoading: boolean;
}

const emptyPreviewState: CurtailmentPlanPreviewState = {
  preview: undefined,
  previewError: undefined,
  isPreviewLoading: false,
};

interface PreviewStateWithRequestKey extends CurtailmentPlanPreviewState {
  requestKey?: string;
}

function parseNumber(value: string): number | undefined {
  if (value.trim() === "") {
    return undefined;
  }

  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function parsePositiveNumber(value: string): number | undefined {
  const parsed = parseNumber(value);
  return parsed !== undefined && parsed > 0 ? parsed : undefined;
}

function parseNonNegativeNumber(value: string): number | undefined {
  const parsed = parseNumber(value);
  return parsed !== undefined && parsed >= 0 ? parsed : undefined;
}

function parsePositiveInteger(value: string): number | undefined {
  const parsed = parsePositiveNumber(value);
  if (parsed === undefined || !Number.isInteger(parsed)) {
    return undefined;
  }

  return parsed;
}

function toApiPriority(priority: CurtailmentFormValues["priority"]): ApiCurtailmentPriority {
  return priority === "emergency" ? ApiCurtailmentPriority.EMERGENCY : ApiCurtailmentPriority.NORMAL;
}

function buildScope(values: CurtailmentFormValues): PreviewCurtailmentPlanRequest["scope"] | undefined {
  if (values.scopeType === "wholeOrg") {
    return {
      case: "wholeOrg",
      value: create(ScopeWholeOrgSchema, {}),
    };
  }

  if (values.scopeType === "deviceSet" && values.deviceSetIds.length > 0) {
    return {
      case: "deviceSetIds",
      value: create(ScopeDeviceSetsSchema, {
        deviceSetIds: values.deviceSetIds,
      }),
    };
  }

  if (values.scopeType === "explicitMiners" && values.deviceIdentifiers.length > 0) {
    return {
      case: "deviceIdentifiers",
      value: create(ScopeDeviceListSchema, {
        deviceIdentifiers: values.deviceIdentifiers,
      }),
    };
  }

  return undefined;
}

export function buildPreviewCurtailmentPlanRequest(
  values: CurtailmentFormValues,
): PreviewCurtailmentPlanRequest | undefined {
  const targetKw = parsePositiveNumber(values.targetKw);
  const scope = buildScope(values);

  if (targetKw === undefined || scope === undefined) {
    return undefined;
  }

  const toleranceKw = parseNonNegativeNumber(values.toleranceKw);

  return create(PreviewCurtailmentPlanRequestSchema, {
    scope,
    mode: ApiCurtailmentMode.FIXED_KW,
    strategy: CurtailmentStrategy.LEAST_EFFICIENT_FIRST,
    level: CurtailmentLevel.FULL,
    priority: toApiPriority(values.priority),
    modeParams: {
      case: "fixedKw",
      value: create(FixedKwParamsSchema, {
        targetKw,
        toleranceKw,
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

function formatDurationEstimate(seconds: number): string {
  if (seconds <= 0) {
    return "Immediately";
  }

  if (seconds < 60) {
    return `~${pluralize(seconds, "second")}`;
  }

  const minutes = Math.ceil(seconds / 60);
  if (minutes < 60) {
    return `~${pluralize(minutes, "minute")}`;
  }

  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;

  if (remainingMinutes === 0) {
    return `~${pluralize(hours, "hour")}`;
  }

  return `~${pluralize(hours, "hour")} ${pluralize(remainingMinutes, "minute")}`;
}

export function estimateRestoreDuration(values: CurtailmentFormValues, selectedMinerCount: number): string {
  const restoreBatchSize = parsePositiveInteger(values.restoreBatchSize);
  const restoreIntervalSec = parsePositiveInteger(values.restoreIntervalSec);

  if (restoreBatchSize === undefined || restoreIntervalSec === undefined) {
    return "Server default";
  }

  const restoreBatchCount = Math.ceil(selectedMinerCount / restoreBatchSize);
  return formatDurationEstimate(Math.max(restoreBatchCount - 1, 0) * restoreIntervalSec);
}

export function toCurtailmentPlanPreview(
  response: PreviewCurtailmentPlanResponse,
  values: CurtailmentFormValues,
): CurtailmentPlanPreview {
  const fixedKw = response.modeParams.case === "fixedKw" ? response.modeParams.value : undefined;
  const targetKw = fixedKw?.targetKw ?? parsePositiveNumber(values.targetKw) ?? 0;

  return {
    selectedMinerCount: response.candidates.length,
    targetKw,
    estimatedReductionKw: response.estimatedReductionKw,
    restoreEstimate: estimateRestoreDuration(values, response.candidates.length),
    scopeLabel: formatScopeLabel(values),
  };
}

function getValuesKey(values: CurtailmentFormValues): string {
  return [
    values.scopeType,
    values.scopeId ?? "",
    values.deviceSetIds.join(","),
    values.deviceIdentifiers.join(","),
    values.curtailmentMode,
    values.minerSelectionStrategy,
    values.targetKw,
    values.toleranceKw,
    values.priority,
    values.restoreBatchSize,
    values.restoreIntervalSec,
    String(values.includeMaintenance),
  ].join("|");
}

export function useCurtailmentPlanPreview({
  open,
  values,
  disabled = false,
  debounceMs = 300,
}: UseCurtailmentPlanPreviewOptions): CurtailmentPlanPreviewState {
  const { handleAuthErrors } = useAuthErrors();
  const [state, setState] = useState<PreviewStateWithRequestKey>(emptyPreviewState);
  const latestRequestId = useRef(0);
  const valuesKey = useMemo(() => getValuesKey(values), [values]);
  const request = useMemo(() => buildPreviewCurtailmentPlanRequest(values), [values]);
  const canPreview = open && !disabled && request !== undefined;

  useEffect(() => {
    if (!canPreview) {
      latestRequestId.current += 1;
      return;
    }

    const requestId = latestRequestId.current + 1;
    latestRequestId.current = requestId;
    const isCurrentRequest = () => latestRequestId.current === requestId;

    const timeoutId = setTimeout(() => {
      setState((current) => ({
        ...current,
        previewError: undefined,
        isPreviewLoading: true,
        requestKey: valuesKey,
      }));

      void curtailmentClient
        .previewCurtailmentPlan(request)
        .then((response) => {
          if (!isCurrentRequest()) {
            return;
          }

          setState({
            preview: toCurtailmentPlanPreview(response, values),
            previewError: undefined,
            isPreviewLoading: false,
            requestKey: valuesKey,
          });
        })
        .catch((error) => {
          if (!isCurrentRequest()) {
            return;
          }

          handleAuthErrors({
            error,
            onError: (err) => {
              if (!isCurrentRequest()) {
                return;
              }

              setState({
                preview: undefined,
                previewError: getErrorMessage(err, "Preview is unavailable."),
                isPreviewLoading: false,
                requestKey: valuesKey,
              });
            },
          });
        });
    }, debounceMs);

    return () => clearTimeout(timeoutId);
  }, [canPreview, debounceMs, handleAuthErrors, request, values, valuesKey]);

  if (!canPreview || state.requestKey !== valuesKey) {
    return emptyPreviewState;
  }

  return {
    preview: state.preview,
    previewError: state.previewError,
    isPreviewLoading: state.isPreviewLoading,
  };
}
