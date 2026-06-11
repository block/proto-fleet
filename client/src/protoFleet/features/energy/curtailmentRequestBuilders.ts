import { create } from "@bufbuild/protobuf";

import {
  type FixedKwParams,
  FixedKwParamsSchema,
  CurtailmentLevel as ProtoCurtailmentLevel,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
  CurtailmentStrategy as ProtoCurtailmentStrategy,
  ScopeDeviceListSchema,
  ScopeSiteSchema,
  ScopeWholeOrgSchema,
  type StartCurtailmentRequest,
  StartCurtailmentRequestSchema,
  type UpdateCurtailmentEventRequest,
  UpdateCurtailmentEventRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import {
  curtailmentNumericFieldLimits,
  getOptionalUint32Setting,
  parseOptionalUint32Field,
} from "@/protoFleet/features/energy/curtailmentNumericFields";
import type { CurtailmentSubmitValues } from "@/protoFleet/features/energy/CurtailmentStartModal";

type OptionalUint32FieldOptions = Parameters<typeof parseOptionalUint32Field>[1];

type CurtailmentRequestFields = Pick<
  StartCurtailmentRequest,
  "scope" | "mode" | "strategy" | "level" | "priority" | "modeParams" | "includeMaintenance" | "forceIncludeMaintenance"
>;

const maxDurationOptions: OptionalUint32FieldOptions = {
  label: "max duration",
  max: curtailmentNumericFieldLimits.maxDurationSec,
};
const minCurtailedDurationOptions: OptionalUint32FieldOptions = {
  label: "min curtailed duration",
  max: curtailmentNumericFieldLimits.minDurationSec,
};
const restoreBatchSizeOptions: OptionalUint32FieldOptions = {
  label: "restore batch size",
  max: curtailmentNumericFieldLimits.restoreBatchSize,
};
const restoreBatchIntervalOptions: OptionalUint32FieldOptions = {
  label: "restore batch interval",
  max: curtailmentNumericFieldLimits.restoreIntervalSec,
};
const maxInt64 = 9_223_372_036_854_775_807n;
const baseTenIntegerPattern = /^[0-9]+$/;

export function parseCurtailmentSiteId(value: string | undefined): bigint | undefined {
  const trimmed = value?.trim() ?? "";
  if (!baseTenIntegerPattern.test(trimmed)) {
    return undefined;
  }

  const parsed = BigInt(trimmed);
  return parsed > 0n && parsed <= maxInt64 ? parsed : undefined;
}

function parseOptionalNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }

  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function getOptionalUpdateUint32Setting(value: string, options: OptionalUint32FieldOptions): number | undefined {
  const parsedField = parseOptionalUint32Field(value, options);
  if (parsedField.error) {
    throw new Error(parsedField.error);
  }

  return parsedField.parsed;
}

function getChangedUpdateStringSetting(value: string, initialValue?: string): string | undefined {
  const trimmedValue = value.trim();
  if (initialValue === undefined) {
    return trimmedValue;
  }

  return trimmedValue === initialValue.trim() ? undefined : trimmedValue;
}

function getChangedUpdatePositiveUint32Setting(
  value: string,
  initialValue: string | undefined,
  options: OptionalUint32FieldOptions,
): number | undefined {
  const nextValue = getOptionalUpdateUint32Setting(value, options);
  if (nextValue === 0) {
    throw new Error(`Enter ${options.label} greater than 0.`);
  }

  if (initialValue === undefined || initialValue.trim() === "") {
    return nextValue;
  }

  const previousValue = getOptionalUpdateUint32Setting(initialValue, options);
  if (nextValue === undefined || nextValue === previousValue) {
    return undefined;
  }

  return nextValue;
}

function getPriority(priority: CurtailmentSubmitValues["priority"]): ProtoCurtailmentPriority {
  return priority === "emergency" ? ProtoCurtailmentPriority.EMERGENCY : ProtoCurtailmentPriority.NORMAL;
}

function buildFixedKwParams(values: CurtailmentSubmitValues): FixedKwParams {
  return create(FixedKwParamsSchema, {
    targetKw: Number(values.targetKw),
    toleranceKw: parseOptionalNumber(values.toleranceKw),
  });
}

function buildScope(values: CurtailmentSubmitValues): StartCurtailmentRequest["scope"] {
  switch (values.scopeType) {
    case "wholeOrg":
      return { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) };
    case "site":
      {
        const siteId = parseCurtailmentSiteId(values.siteId);
        if (siteId !== undefined) {
          return { case: "site", value: create(ScopeSiteSchema, { siteId }) };
        }
      }
      break;
    case "explicitMiners":
      if (values.deviceIdentifiers.length > 0) {
        return {
          case: "deviceIdentifiers",
          value: create(ScopeDeviceListSchema, { deviceIdentifiers: values.deviceIdentifiers }),
        };
      }
      break;
    case "deviceSet":
      break;
  }

  throw new Error("Unsupported curtailment target scope.");
}

function buildCurtailmentRequestFields(values: CurtailmentSubmitValues): CurtailmentRequestFields {
  const fixedKwModeFields =
    values.curtailmentMode === "fixedKwReduction"
      ? {
          mode: ProtoCurtailmentMode.FIXED_KW,
          modeParams: {
            case: "fixedKw" as const,
            value: buildFixedKwParams(values),
          },
        }
      : {
          mode: ProtoCurtailmentMode.FULL_FLEET,
          modeParams: { case: undefined },
        };

  return {
    scope: buildScope(values),
    ...fixedKwModeFields,
    // Server defaults unspecified strategy to least-efficient-first.
    strategy: ProtoCurtailmentStrategy.UNSPECIFIED,
    level: ProtoCurtailmentLevel.FULL,
    priority: getPriority(values.priority),
    includeMaintenance: values.includeMaintenance,
    forceIncludeMaintenance: values.includeMaintenance,
  };
}

export function buildStartCurtailmentRequest(values: CurtailmentSubmitValues): StartCurtailmentRequest {
  return create(StartCurtailmentRequestSchema, {
    ...buildCurtailmentRequestFields(values),
    maxDurationSeconds: getOptionalUint32Setting(values.maxDurationSec, maxDurationOptions),
    restoreBatchSize: getOptionalUint32Setting(values.restoreBatchSize, restoreBatchSizeOptions),
    restoreBatchIntervalSec: getOptionalUint32Setting(values.restoreIntervalSec, restoreBatchIntervalOptions),
    minCurtailedDurationSec: getOptionalUint32Setting(values.minDurationSec, minCurtailedDurationOptions),
    reason: values.reason.trim(),
  });
}

export function buildUpdateCurtailmentEventRequest(
  eventUuid: string,
  values: CurtailmentSubmitValues,
  initialValues?: Partial<CurtailmentSubmitValues>,
): UpdateCurtailmentEventRequest {
  return create(UpdateCurtailmentEventRequestSchema, {
    eventUuid,
    reason: getChangedUpdateStringSetting(values.reason, initialValues?.reason),
    maxDurationSeconds: getChangedUpdatePositiveUint32Setting(
      values.maxDurationSec,
      initialValues?.maxDurationSec,
      maxDurationOptions,
    ),
    restoreBatchIntervalSec: getChangedUpdatePositiveUint32Setting(
      values.restoreIntervalSec,
      initialValues?.restoreIntervalSec,
      restoreBatchIntervalOptions,
    ),
  });
}
