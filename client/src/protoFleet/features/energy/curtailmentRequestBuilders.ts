import { create } from "@bufbuild/protobuf";

import {
  type FixedKwParams,
  FixedKwParamsSchema,
  CurtailmentLevel as ProtoCurtailmentLevel,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
  CurtailmentStrategy as ProtoCurtailmentStrategy,
  ScopeDeviceListSchema,
  ScopeDeviceSetsSchema,
  ScopeWholeOrgSchema,
  type StartCurtailmentRequest,
  StartCurtailmentRequestSchema,
  type UpdateCurtailmentEventRequest,
  UpdateCurtailmentEventRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
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

function getOptionalNumericSetting(value: string): number {
  return parseOptionalNumber(value) ?? 0;
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
      if (values.deviceSetIds.length > 0) {
        return { case: "deviceSetIds", value: create(ScopeDeviceSetsSchema, { deviceSetIds: values.deviceSetIds }) };
      }
      break;
  }

  throw new Error("Select at least one rack, group, or miner for this curtailment.");
}

function buildCurtailmentRequestFields(values: CurtailmentSubmitValues): CurtailmentRequestFields {
  return {
    scope: buildScope(values),
    mode: ProtoCurtailmentMode.FIXED_KW,
    strategy: ProtoCurtailmentStrategy.LEAST_EFFICIENT_FIRST,
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
    maxDurationSeconds: getOptionalNumericSetting(values.maxDurationSec),
    restoreBatchSize: getOptionalNumericSetting(values.restoreBatchSize),
    restoreBatchIntervalSec: getOptionalNumericSetting(values.restoreIntervalSec),
    minCurtailedDurationSec: getOptionalNumericSetting(values.minDurationSec),
    reason: values.reason.trim(),
  });
}

export function buildUpdateCurtailmentRequest(
  eventId: string,
  values: CurtailmentSubmitValues,
): UpdateCurtailmentEventRequest {
  return create(UpdateCurtailmentEventRequestSchema, {
    eventUuid: eventId,
    reason: values.reason.trim(),
    maxDurationSeconds: getOptionalNumericSetting(values.maxDurationSec),
    restoreBatchSize: getOptionalNumericSetting(values.restoreBatchSize),
    restoreBatchIntervalSec: getOptionalNumericSetting(values.restoreIntervalSec),
  });
}
