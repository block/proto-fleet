import { getValidSiteScopeIds, withDefaultSiteScope, withResponseProfileScope } from "./curtailmentFormScope";
import type {
  CurtailmentFormErrors,
  CurtailmentFormValues,
  CurtailmentSiteOption,
  CurtailmentStartModalMode,
  CurtailmentStartModalVariant,
} from "@/protoFleet/api/curtailmentTypes";
import {
  curtailmentNumericFieldLimits,
  nonAdminBatchIntervalMax,
  parseOptionalUint32Field,
} from "@/protoFleet/features/energy/curtailmentNumericFields";
import { customResponseProfileId } from "@/protoFleet/features/energy/curtailmentRequestBuilders";

type ParsedNumberField = { parsed?: number; error?: string };

type EditableCurtailmentField = "reason" | "restoreIntervalSec";

const defaultValues: CurtailmentFormValues = {
  scopeType: "wholeOrg",
  scopeId: "whole-org",
  siteSelection: "none",
  siteId: "",
  siteIds: [],
  siteNamesById: {},
  buildingTargetIds: [],
  rackTargetIds: [],
  groupTargetIds: [],
  deviceIdentifiers: [],
  minerSelectionMode: "subset",
  responseProfileId: customResponseProfileId,
  // Whole-fleet shutdown is the primary operator flow; fixed-kW sizing is
  // the opt-in refinement (matches the response-profile form default).
  curtailmentMode: "fullFleet",
  minerSelectionStrategy: "leastEfficientFirst",
  targetKw: "",
  toleranceKw: "",
  priority: "normal",
  minDurationSec: "",
  maxDurationSec: "",
  curtailBatchSize: "",
  curtailBatchIntervalSec: "",
  restoreBatchSize: "",
  restoreIntervalSec: "",
  facilityFanDeviceIds: [],
  fanOffDelaySec: "",
  fanRestoreDelaySec: "",
  reason: "",
  // Maintenance-flagged miners are excluded by default: force_include_maintenance
  // is admin-gated server-side, so sending it from every start would lock
  // non-admin operators with curtailment:manage out of Start entirely. Admins
  // opt the maintenance population in via "Target all paired miners".
  includeMaintenance: false,
  forceIncludeAllPairedMiners: false,
};

const editableCurtailmentFields: EditableCurtailmentField[] = ["reason", "restoreIntervalSec"];

export function getInitialValues(
  initialValues?: Partial<CurtailmentFormValues>,
  variant: CurtailmentStartModalVariant = "curtailment",
  defaultSiteScope?: CurtailmentSiteOption,
): CurtailmentFormValues {
  const values = {
    ...defaultValues,
    ...initialValues,
  };
  if (initialValues?.siteSelection === undefined && getValidSiteScopeIds(values.siteIds, values.siteId).length > 0) {
    values.siteSelection = "site";
  }

  const valuesWithDefaultSiteScope = withDefaultSiteScope(values, defaultSiteScope);

  return variant === "responseProfile"
    ? withResponseProfileScope(valuesWithDefaultSiteScope)
    : valuesWithDefaultSiteScope;
}

function parseRequiredPositiveNumberField(value: string, fieldLabel: string): ParsedNumberField {
  const trimmed = value.trim();
  if (trimmed === "") {
    return { error: `Enter ${fieldLabel}.` };
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return { error: `Enter ${fieldLabel} greater than 0.` };
  }

  return { parsed };
}

function parseOptionalNonNegativeNumberField(value: string, fieldLabel: string): ParsedNumberField {
  const trimmed = value.trim();
  if (trimmed === "") {
    return {};
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return { error: `Enter ${fieldLabel} of 0 or more.` };
  }

  return { parsed };
}

function parseComparableUint32Field(value: string, max: number): number {
  const parsedField = parseOptionalUint32Field(value, { label: "value", max });
  return parsedField.parsed ?? 0;
}

export function hasEditableCurtailmentChanges(
  values: CurtailmentFormValues,
  initialValues: CurtailmentFormValues,
): boolean {
  return editableCurtailmentFields.some((field) => {
    if (field === "reason") {
      return values.reason.trim() !== initialValues.reason.trim();
    }

    return (
      parseComparableUint32Field(values.restoreIntervalSec, curtailmentNumericFieldLimits.restoreIntervalSec) !==
      parseComparableUint32Field(initialValues.restoreIntervalSec, curtailmentNumericFieldLimits.restoreIntervalSec)
    );
  });
}

export function validateCurtailmentFormValues(
  values: CurtailmentFormValues,
  mode: CurtailmentStartModalMode = "create",
  initialValues: CurtailmentFormValues = defaultValues,
  variant: CurtailmentStartModalVariant = "curtailment",
  canUseAdminControls = false,
): CurtailmentFormErrors {
  const localErrors: CurtailmentFormErrors = {};
  const isEditMode = mode === "edit";
  const isResponseProfileVariant = variant === "responseProfile";
  const shouldValidateFullFormFields = !isEditMode || isResponseProfileVariant;
  const unchangedRestoreInterval =
    isEditMode &&
    !isResponseProfileVariant &&
    parseComparableUint32Field(values.restoreIntervalSec, curtailmentNumericFieldLimits.restoreIntervalSec) ===
      parseComparableUint32Field(initialValues.restoreIntervalSec, curtailmentNumericFieldLimits.restoreIntervalSec);
  const restoreInterval = parseOptionalUint32Field(values.restoreIntervalSec, {
    label: "batch interval",
    max:
      canUseAdminControls || unchangedRestoreInterval
        ? curtailmentNumericFieldLimits.restoreIntervalSec
        : nonAdminBatchIntervalMax,
  });
  const curtailBatchSize = parseOptionalUint32Field(values.curtailBatchSize, {
    label: "batch size",
    max: curtailmentNumericFieldLimits.curtailBatchSize,
  });
  const curtailBatchInterval = parseOptionalUint32Field(values.curtailBatchIntervalSec, {
    label: "batch interval",
    max: canUseAdminControls ? curtailmentNumericFieldLimits.curtailBatchIntervalSec : nonAdminBatchIntervalMax,
  });
  const fanOffDelay = parseOptionalUint32Field(values.fanOffDelaySec ?? "", {
    label: "fan-off delay",
    max: curtailmentNumericFieldLimits.fanDelaySec,
  });
  const fanRestoreDelay = parseOptionalUint32Field(values.fanRestoreDelaySec ?? "", {
    label: "fan restore delay",
    max: curtailmentNumericFieldLimits.fanDelaySec,
  });

  if (values.reason.trim() === "") {
    localErrors.reason = variant === "responseProfile" ? "Enter a profile name." : "Enter a reason.";
  }
  if (restoreInterval.error) {
    localErrors.restoreIntervalSec = restoreInterval.error;
  }
  if (shouldValidateFullFormFields && curtailBatchSize.error) {
    localErrors.curtailBatchSize = curtailBatchSize.error;
  }
  if (shouldValidateFullFormFields && curtailBatchSize.error === undefined && curtailBatchSize.parsed === 0) {
    localErrors.curtailBatchSize = "Enter batch size greater than 0.";
  }
  if (shouldValidateFullFormFields && curtailBatchInterval.error) {
    localErrors.curtailBatchIntervalSec = curtailBatchInterval.error;
  }
  if (shouldValidateFullFormFields && fanOffDelay.error) {
    localErrors.fanOffDelaySec = fanOffDelay.error;
  }
  if (shouldValidateFullFormFields && fanRestoreDelay.error) {
    localErrors.fanRestoreDelaySec = fanRestoreDelay.error;
  }
  if (
    shouldValidateFullFormFields &&
    curtailBatchInterval.error === undefined &&
    curtailBatchSize.parsed === undefined &&
    curtailBatchInterval.parsed !== undefined
  ) {
    localErrors.curtailBatchIntervalSec = "Enter batch size before adding a batch interval.";
  }
  if (
    isEditMode &&
    restoreInterval.error === undefined &&
    values.restoreIntervalSec.trim() === "" &&
    initialValues.restoreIntervalSec.trim() !== ""
  ) {
    localErrors.restoreIntervalSec = "Restore interval cannot be cleared.";
  }
  if (isEditMode) {
    return localErrors;
  }

  const targetKw =
    values.curtailmentMode === "fixedKwReduction"
      ? parseRequiredPositiveNumberField(values.targetKw, "a target reduction")
      : {};
  const toleranceKw =
    values.curtailmentMode === "fixedKwReduction"
      ? parseOptionalNonNegativeNumberField(values.toleranceKw, "a tolerance")
      : {};
  const restoreBatchSize = parseOptionalUint32Field(values.restoreBatchSize, {
    label: "batch size",
    max: curtailmentNumericFieldLimits.restoreBatchSize,
  });

  if (targetKw.error) {
    localErrors.targetKw = targetKw.error;
  }
  if (toleranceKw.error) {
    localErrors.toleranceKw = toleranceKw.error;
  }
  if (restoreBatchSize.error) {
    localErrors.restoreBatchSize = restoreBatchSize.error;
  }
  return localErrors;
}
