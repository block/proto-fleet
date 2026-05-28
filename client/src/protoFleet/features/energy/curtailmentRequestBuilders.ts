import { create } from "@bufbuild/protobuf";

import {
  type FixedKwParams,
  FixedKwParamsSchema,
  CurtailmentLevel as ProtoCurtailmentLevel,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
  CurtailmentStrategy as ProtoCurtailmentStrategy,
  ScopeDeviceListSchema,
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

type CurtailmentRequestFields = Pick<
  StartCurtailmentRequest,
  "scope" | "mode" | "strategy" | "level" | "priority" | "modeParams" | "includeMaintenance" | "forceIncludeMaintenance"
>;

function parseOptionalNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }

  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function getOptionalUpdateUint32Setting(
  value: string,
  options: Parameters<typeof parseOptionalUint32Field>[1],
): number | undefined {
  const parsedField = parseOptionalUint32Field(value, options);
  if (parsedField.error) {
    throw new Error(parsedField.error);
  }

  return parsedField.parsed;
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
  return {
    scope: buildScope(values),
    mode: ProtoCurtailmentMode.FIXED_KW,
    // Server defaults unspecified strategy to least-efficient-first.
    strategy: ProtoCurtailmentStrategy.UNSPECIFIED,
    level: ProtoCurtailmentLevel.FULL,
    priority: getPriority(values.priority),
    modeParams: {
      case: "fixedKw",
      value: buildFixedKwParams(values),
    },
    includeMaintenance: values.includeMaintenance,
    forceIncludeMaintenance: values.includeMaintenance,
  };
}

export function buildStartCurtailmentRequest(values: CurtailmentSubmitValues): StartCurtailmentRequest {
  return create(StartCurtailmentRequestSchema, {
    ...buildCurtailmentRequestFields(values),
    maxDurationSeconds: getOptionalUint32Setting(values.maxDurationSec, {
      label: "max duration",
      max: curtailmentNumericFieldLimits.maxDurationSec,
    }),
    restoreBatchSize: getOptionalUint32Setting(values.restoreBatchSize, {
      label: "restore batch size",
      max: curtailmentNumericFieldLimits.restoreBatchSize,
    }),
    restoreBatchIntervalSec: getOptionalUint32Setting(values.restoreIntervalSec, {
      label: "restore batch interval",
      max: curtailmentNumericFieldLimits.restoreIntervalSec,
    }),
    minCurtailedDurationSec: getOptionalUint32Setting(values.minDurationSec, {
      label: "min curtailed duration",
      max: curtailmentNumericFieldLimits.minDurationSec,
    }),
    reason: values.reason.trim(),
  });
}

export function buildUpdateCurtailmentEventRequest(
  eventUuid: string,
  values: CurtailmentSubmitValues,
): UpdateCurtailmentEventRequest {
  return create(UpdateCurtailmentEventRequestSchema, {
    eventUuid,
    reason: values.reason.trim(),
    maxDurationSeconds: getOptionalUpdateUint32Setting(values.maxDurationSec, {
      label: "max duration",
      max: curtailmentNumericFieldLimits.maxDurationSec,
    }),
    restoreBatchSize: getOptionalUpdateUint32Setting(values.restoreBatchSize, {
      label: "restore batch size",
      max: curtailmentNumericFieldLimits.restoreBatchSize,
    }),
    restoreBatchIntervalSec: getOptionalUpdateUint32Setting(values.restoreIntervalSec, {
      label: "restore batch interval",
      max: curtailmentNumericFieldLimits.restoreIntervalSec,
    }),
  });
}
