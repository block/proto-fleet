import { create } from "@bufbuild/protobuf";

import {
  type CurtailmentScope,
  CurtailmentScopeSchema,
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
  | "scopes"
  | "mode"
  | "strategy"
  | "level"
  | "priority"
  | "modeParams"
  | "includeMaintenance"
  | "forceIncludeMaintenance"
  | "forceIncludeAllPairedMiners"
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

type CurtailmentScopeValues = Pick<
  CurtailmentSubmitValues,
  "scopeType" | "siteSelection" | "siteId" | "siteIds" | "deviceSetIds" | "deviceIdentifiers" | "minerSelectionMode"
>;

function uniqueNonEmptyStrings(values: readonly string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function getSelectedSiteIds(
  values: CurtailmentScopeValues,
  siteSelection: CurtailmentScopeValues["siteSelection"],
): string[] {
  if (siteSelection !== "site" && siteSelection !== "allSites") {
    return [];
  }

  const siteIds =
    values.siteIds !== undefined && values.siteIds.length > 0 ? values.siteIds : values.siteId ? [values.siteId] : [];
  return uniqueNonEmptyStrings(siteIds);
}

export function buildCurtailmentScopes(values: CurtailmentScopeValues): CurtailmentScope[] | undefined {
  const siteSelection = values.siteSelection ?? (values.scopeType === "site" ? "site" : "none");
  if (values.minerSelectionMode === "all") {
    return [create(CurtailmentScopeSchema, { scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) } })];
  }

  const scopes: CurtailmentScope[] = [];
  if (siteSelection === "site" || siteSelection === "allSites") {
    const siteIds: bigint[] = [];
    for (const siteIdValue of getSelectedSiteIds(values, siteSelection)) {
      const siteId = parseCurtailmentSiteId(siteIdValue);
      if (siteId === undefined) {
        return undefined;
      }
      siteIds.push(siteId);
    }
    if (siteIds.length === 0) {
      return undefined;
    }
    for (const siteId of siteIds) {
      scopes.push(
        create(CurtailmentScopeSchema, {
          scope: { case: "site", value: create(ScopeSiteSchema, { siteId }) },
        }),
      );
    }
  }

  const deviceIdentifiers = uniqueNonEmptyStrings(values.deviceIdentifiers);
  if (deviceIdentifiers.length > 0) {
    scopes.push(
      create(CurtailmentScopeSchema, {
        scope: { case: "deviceIdentifiers", value: create(ScopeDeviceListSchema, { deviceIdentifiers }) },
      }),
    );
  }

  if (scopes.length > 0) {
    return scopes;
  }

  if (values.scopeType === "deviceSet" || values.scopeType === "explicitMiners") {
    return undefined;
  }

  return [create(CurtailmentScopeSchema, { scope: { case: "wholeOrg", value: create(ScopeWholeOrgSchema, {}) } })];
}

// All-paired targeting requires a closed-loop scope (whole org or sites).
// The policy's durable-ownership loop (release on unpair, reopen on re-pair)
// does not run for explicit miner selections, and the server rejects the
// combination.
export function supportsAllPairedTargeting(
  values: CurtailmentScopeValues & Pick<CurtailmentSubmitValues, "curtailmentMode">,
): boolean {
  if (values.curtailmentMode !== "fullFleet") {
    return false;
  }
  const scopes = buildCurtailmentScopes(values);
  return scopes !== undefined && scopes.every((s) => s.scope.case === "wholeOrg" || s.scope.case === "site");
}

// Targeting all paired miners also opts in miners flagged for maintenance:
// parking them as unavailable would contradict the operator's explicit
// "all paired" choice, and both flags sit behind the same server-side admin
// gate as the all-paired control itself.
//
// The maintenance pair derives SOLELY from the all-paired flag: the UI no
// longer exposes an independent maintenance toggle, so a stale
// values.includeMaintenance (hydrated from a profile or past event saved when
// the pair was coupled) must not survive unchecking "Target all paired
// miners" — it would silently keep the admin-gated maintenance inclusion with
// nothing in the UI showing it.
export function buildForceInclusionFields(
  values: CurtailmentScopeValues & Pick<CurtailmentSubmitValues, "curtailmentMode" | "forceIncludeAllPairedMiners">,
): Pick<CurtailmentRequestFields, "includeMaintenance" | "forceIncludeMaintenance" | "forceIncludeAllPairedMiners"> {
  const forceIncludeAllPairedMiners = values.forceIncludeAllPairedMiners && supportsAllPairedTargeting(values);
  // The proto validator requires include_maintenance == force_include_maintenance.
  return {
    includeMaintenance: forceIncludeAllPairedMiners,
    forceIncludeMaintenance: forceIncludeAllPairedMiners,
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
    ...fixedKwModeFields,
    // Server defaults unspecified strategy to least-efficient-first.
    strategy: ProtoCurtailmentStrategy.UNSPECIFIED,
    level: ProtoCurtailmentLevel.FULL,
    priority: getPriority(values.priority),
    ...buildForceInclusionFields(values),
  };
}

export function buildStartCurtailmentRequest(values: CurtailmentSubmitValues): StartCurtailmentRequest {
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
      uniqueNonEmptyStrings(values.facilityFanDeviceIds ?? []).map((value) => {
        const id = parseCurtailmentSiteId(value);
        if (id === undefined) {
          throw new Error("Facility fan IDs must be positive integers.");
        }
        return id;
      }),
    ),
  ];

  return create(StartCurtailmentRequestSchema, {
    ...buildCurtailmentRequestFields(values),
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
