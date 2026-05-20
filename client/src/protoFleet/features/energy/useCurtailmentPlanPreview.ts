import { useEffect, useMemo, useState } from "react";
import { create } from "@bufbuild/protobuf";

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

interface CurtailmentPlanPreviewState {
  preview?: CurtailmentPlanPreview;
  previewError?: string;
  isPreviewLoading: boolean;
  requestKey?: string;
}

const emptyPreviewState: CurtailmentPlanPreviewState = {
  preview: undefined,
  previewError: undefined,
  isPreviewLoading: false,
};

function parseNumber(value: string, isValid: (value: number) => boolean): number | undefined {
  const trimmed = value.trim();
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

function buildScope(values: CurtailmentFormValues): PreviewCurtailmentPlanRequest["scope"] | undefined {
  switch (values.scopeType) {
    case "wholeOrg":
      return {
        case: "wholeOrg",
        value: create(ScopeWholeOrgSchema, {}),
      };
    case "deviceSet":
      return values.deviceSetIds.length > 0
        ? {
            case: "deviceSetIds",
            value: create(ScopeDeviceSetsSchema, {
              deviceSetIds: values.deviceSetIds,
            }),
          }
        : undefined;
    case "explicitMiners":
      return values.deviceIdentifiers.length > 0
        ? {
            case: "deviceIdentifiers",
            value: create(ScopeDeviceListSchema, {
              deviceIdentifiers: values.deviceIdentifiers,
            }),
          }
        : undefined;
  }
}

export function buildPreviewCurtailmentPlanRequest(
  values: CurtailmentFormValues,
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
    return minDurationSec === maxDurationSec
      ? formatDurationEstimate(minDurationSec, false)
      : `${formatDurationEstimate(minDurationSec, false)} - ${formatDurationEstimate(maxDurationSec, false)}`;
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

export function useCurtailmentPlanPreview({
  open,
  values,
  disabled = false,
  debounceMs = 300,
}: UseCurtailmentPlanPreviewOptions): CurtailmentPlanPreviewState {
  const { handleAuthErrors } = useAuthErrors();
  const [state, setState] = useState<CurtailmentPlanPreviewState>(emptyPreviewState);
  const valuesKey = useMemo(() => JSON.stringify(values), [values]);
  const request = useMemo(() => buildPreviewCurtailmentPlanRequest(values), [values]);

  useEffect(() => {
    if (!open || disabled) {
      return;
    }

    if (!request) {
      return;
    }

    let isActive = true;
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
          if (!isActive) {
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
                preview: undefined,
                previewError: getErrorMessage(err, "Preview is unavailable."),
                isPreviewLoading: false,
                requestKey: valuesKey,
              });
            },
          });
        });
    }, debounceMs);

    return () => {
      isActive = false;
      clearTimeout(timeoutId);
    };
  }, [debounceMs, disabled, handleAuthErrors, open, request, values, valuesKey]);

  if (!open || disabled) {
    return emptyPreviewState;
  }

  const hasCurrentPreviewState = request !== undefined && state.requestKey === valuesKey;

  return {
    preview: hasCurrentPreviewState ? state.preview : undefined,
    previewError: hasCurrentPreviewState ? state.previewError : undefined,
    isPreviewLoading: hasCurrentPreviewState ? state.isPreviewLoading : false,
  };
}
