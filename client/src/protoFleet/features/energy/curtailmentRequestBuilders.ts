import { create } from "@bufbuild/protobuf";

import {
  buildCurtailmentScopes,
  curtailmentScopeSchemaVersion,
  type CurtailmentScopeSelection,
  normalizeCurtailmentSelectionValues,
  parseCurtailmentTargetId,
} from "@/protoFleet/api/curtailmentScopes";
import {
  type FixedKwParams,
  FixedKwParamsSchema,
  CurtailmentLevel as ProtoCurtailmentLevel,
  CurtailmentMode as ProtoCurtailmentMode,
  CurtailmentPriority as ProtoCurtailmentPriority,
  CurtailmentStrategy as ProtoCurtailmentStrategy,
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

export const customResponseProfileId = "customPlan";
export const curtailmentExecutionSchemaVersion = 1;

type CurtailmentRequestFields = Pick<
  StartCurtailmentRequest,
  | "scopes"
  | "scopeSchemaVersion"
  | "executionSchemaVersion"
  | "mode"
  | "strategy"
  | "level"
  | "priority"
  | "modeParams"
  | "includeMaintenance"
  | "forceIncludeMaintenance"
  | "forceIncludeAllPairedMiners"
  | "postEventCooldownSec"
>;

type ResponseProfileExecutionFields = Pick<
  StartCurtailmentRequest,
  "responseProfileId" | "expectedResponseProfileRevision"
>;

const maxDurationOptions: OptionalUint32FieldOptions = {
  label: "max duration",
  max: curtailmentNumericFieldLimits.maxDurationSec,
};
const minCurtailedDurationOptions: OptionalUint32FieldOptions = {
  label: "min curtailed duration",
  max: curtailmentNumericFieldLimits.minDurationSec,
};
const curtailBatchSizeOptions: OptionalUint32FieldOptions = {
  label: "curtail batch size",
  max: curtailmentNumericFieldLimits.curtailBatchSize,
};
const curtailBatchIntervalOptions: OptionalUint32FieldOptions = {
  label: "curtail batch interval",
  max: curtailmentNumericFieldLimits.curtailBatchIntervalSec,
};
const restoreBatchSizeOptions: OptionalUint32FieldOptions = {
  label: "restore batch size",
  max: curtailmentNumericFieldLimits.restoreBatchSize,
};
const restoreBatchIntervalOptions: OptionalUint32FieldOptions = {
  label: "restore batch interval",
  max: curtailmentNumericFieldLimits.restoreIntervalSec,
};
const fanOffDelayOptions: OptionalUint32FieldOptions = {
  label: "fan off delay",
  max: curtailmentNumericFieldLimits.fanDelaySec,
};
const fanRestoreDelayOptions: OptionalUint32FieldOptions = {
  label: "fan restore delay",
  max: curtailmentNumericFieldLimits.fanDelaySec,
};
const postEventCooldownOptions: OptionalUint32FieldOptions = {
  label: "post-event cooldown",
  max: curtailmentNumericFieldLimits.postEventCooldownSec,
};

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

function getOptionalPositiveUint32Setting(value: string, options: OptionalUint32FieldOptions): number | undefined {
  const nextValue = getOptionalUpdateUint32Setting(value, options);
  if (nextValue === 0) {
    throw new Error(`Enter ${options.label} greater than 0.`);
  }

  return nextValue;
}

function getChangedUpdateStringSetting(value: string, initialValue?: string): string | undefined {
  const trimmedValue = value.trim();
  if (initialValue === undefined) {
    return trimmedValue;
  }

  return trimmedValue === initialValue.trim() ? undefined : trimmedValue;
}

function getChangedParsedUpdateUint32Setting(
  nextValue: number | undefined,
  initialValue: string | undefined,
  options: OptionalUint32FieldOptions,
): number | undefined {
  if (initialValue === undefined || initialValue.trim() === "") {
    return nextValue;
  }

  const previousValue = getOptionalUpdateUint32Setting(initialValue, options);
  if (nextValue === undefined || nextValue === previousValue) {
    return undefined;
  }

  return nextValue;
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

  return getChangedParsedUpdateUint32Setting(nextValue, initialValue, options);
}

function getChangedUpdateUint32Setting(
  value: string,
  initialValue: string | undefined,
  options: OptionalUint32FieldOptions,
): number | undefined {
  const nextValue = getOptionalUpdateUint32Setting(value, options);
  return getChangedParsedUpdateUint32Setting(nextValue, initialValue, options);
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

export function getResponseProfileExecutionFields(
  values: Pick<CurtailmentSubmitValues, "responseProfileId" | "responseProfileRevision">,
): ResponseProfileExecutionFields | undefined {
  if (values.responseProfileId === customResponseProfileId) {
    return { responseProfileId: 0n, expectedResponseProfileRevision: "" };
  }

  const responseProfileId = parseCurtailmentTargetId(values.responseProfileId);
  const expectedResponseProfileRevision = values.responseProfileRevision?.trim();
  if (responseProfileId === undefined || !expectedResponseProfileRevision) {
    return undefined;
  }

  return { responseProfileId, expectedResponseProfileRevision };
}

// Logical placement scopes can back the durable all-paired policy. Explicit
// miner lists remain snapshots until their closed-loop lifecycle is supported.
export function supportsAllPairedTargeting(
  values: CurtailmentScopeSelection & Pick<CurtailmentSubmitValues, "curtailmentMode">,
): boolean {
  if (values.curtailmentMode !== "fullFleet") {
    return false;
  }
  const scopes = buildCurtailmentScopes(values);
  return scopes !== undefined && scopes.every((scope) => scope.scope.case !== "deviceIdentifiers");
}

// Targeting all paired miners also opts in miners flagged for maintenance:
// parking them as unavailable would contradict the operator's explicit
// "all paired" choice, and both flags sit behind the same server-side admin
// gate as the all-paired control itself. Saved profiles may independently opt
// into maintenance miners, so executions must preserve that stored setting.
// Custom plans still derive maintenance inclusion solely from the visible
// all-paired control.
export function buildForceInclusionFields(
  values: CurtailmentScopeSelection &
    Pick<
      CurtailmentSubmitValues,
      "responseProfileId" | "curtailmentMode" | "includeMaintenance" | "forceIncludeAllPairedMiners"
    >,
): Pick<CurtailmentRequestFields, "includeMaintenance" | "forceIncludeMaintenance" | "forceIncludeAllPairedMiners"> {
  const forceIncludeAllPairedMiners = values.forceIncludeAllPairedMiners && supportsAllPairedTargeting(values);
  const includeMaintenance =
    forceIncludeAllPairedMiners || (values.responseProfileId !== customResponseProfileId && values.includeMaintenance);
  // The proto validator requires include_maintenance == force_include_maintenance.
  return {
    includeMaintenance,
    forceIncludeMaintenance: includeMaintenance,
    forceIncludeAllPairedMiners,
  };
}

function buildCurtailmentRequestFields(values: CurtailmentSubmitValues): CurtailmentRequestFields {
  const scopes = buildCurtailmentScopes(values);
  if (scopes === undefined) {
    throw new Error("Unsupported curtailment target scope.");
  }
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
    scopes,
    scopeSchemaVersion: curtailmentScopeSchemaVersion,
    executionSchemaVersion: curtailmentExecutionSchemaVersion,
    ...fixedKwModeFields,
    // Server defaults unspecified strategy to least-efficient-first.
    strategy: ProtoCurtailmentStrategy.UNSPECIFIED,
    level: ProtoCurtailmentLevel.FULL,
    priority: getPriority(values.priority),
    postEventCooldownSec: getOptionalUint32Setting(values.postEventCooldownSec ?? "", postEventCooldownOptions),
    ...buildForceInclusionFields(values),
  };
}

export function buildStartCurtailmentRequest(values: CurtailmentSubmitValues): StartCurtailmentRequest {
  const responseProfileExecutionFields = getResponseProfileExecutionFields(values);
  if (responseProfileExecutionFields === undefined) {
    throw new Error("Reload the response profile before starting curtailment.");
  }
  const curtailBatchSize = getOptionalPositiveUint32Setting(values.curtailBatchSize, curtailBatchSizeOptions);
  const curtailBatchIntervalSec = getOptionalUpdateUint32Setting(
    values.curtailBatchIntervalSec,
    curtailBatchIntervalOptions,
  );
  if (curtailBatchSize === undefined && curtailBatchIntervalSec !== undefined) {
    throw new Error("Enter curtail batch size before adding a curtail batch interval.");
  }

  const facilityFanDeviceIds = [
    ...new Set(
      normalizeCurtailmentSelectionValues(values.facilityFanDeviceIds ?? []).map((value) => {
        const id = parseCurtailmentTargetId(value);
        if (id === undefined) {
          throw new Error("Facility fan IDs must be positive integers.");
        }
        return id;
      }),
    ),
  ];

  return create(StartCurtailmentRequestSchema, {
    ...buildCurtailmentRequestFields(values),
    ...responseProfileExecutionFields,
    maxDurationSeconds: getOptionalUint32Setting(values.maxDurationSec, maxDurationOptions),
    curtailBatchSize,
    curtailBatchIntervalSec,
    restoreBatchSize: getOptionalUint32Setting(values.restoreBatchSize, restoreBatchSizeOptions),
    restoreBatchIntervalSec: getOptionalUint32Setting(values.restoreIntervalSec, restoreBatchIntervalOptions),
    minCurtailedDurationSec: getOptionalUint32Setting(values.minDurationSec, minCurtailedDurationOptions),
    facilityFanDeviceIds,
    fanOffDelaySec: getOptionalUint32Setting(values.fanOffDelaySec ?? "", fanOffDelayOptions),
    fanRestoreDelaySec: getOptionalUint32Setting(values.fanRestoreDelaySec ?? "", fanRestoreDelayOptions),
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
    restoreBatchIntervalSec: getChangedUpdateUint32Setting(
      values.restoreIntervalSec,
      initialValues?.restoreIntervalSec,
      restoreBatchIntervalOptions,
    ),
  });
}
