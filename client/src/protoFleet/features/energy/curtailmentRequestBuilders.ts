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
    includeMaintenance: values.includeMaintenance,
    forceIncludeMaintenance: values.includeMaintenance,
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

  return create(StartCurtailmentRequestSchema, {
    ...buildCurtailmentRequestFields(values),
    maxDurationSeconds: getOptionalUint32Setting(values.maxDurationSec, maxDurationOptions),
    curtailBatchSize,
    curtailBatchIntervalSec,
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
