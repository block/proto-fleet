import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { curtailmentClient } from "@/protoFleet/api/clients";
import {
  buildCurtailmentScopes,
  curtailmentScopeSchemaVersion,
  getCurtailmentScopeFormFields,
  getCurtailmentScopeSummary,
  getCurtailmentTerminalScope,
  normalizeCurtailmentSelectionValues,
  parseCurtailmentTerminalScopes,
} from "@/protoFleet/api/curtailmentScopes";
import {
  type CurtailmentResponseProfile as ApiCurtailmentResponseProfile,
  CreateCurtailmentResponseProfileRequestSchema,
  CurtailmentLevel,
  CurtailmentMode,
  CurtailmentPriority,
  CurtailmentStrategy,
  DeleteCurtailmentResponseProfileRequestSchema,
  FixedKwParamsSchema,
  ListCurtailmentResponseProfilesRequestSchema,
  type UpdateCurtailmentResponseProfileRequest,
  UpdateCurtailmentResponseProfileRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, createErrorWithCause, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import { getSiteDisplayName, type SiteNameById } from "@/protoFleet/api/siteNames";
import {
  curtailmentNumericFieldLimits,
  getOptionalUint32Setting,
  immediateRestoreBatchSize,
  parseOptionalUint32Field,
} from "@/protoFleet/features/energy/curtailmentNumericFields";
import {
  isResponseProfileAutomationReady,
  type ResponseProfile,
  type ResponseProfileFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useAuthErrors } from "@/protoFleet/store";

const defaultResponseDeadlineMinutes: string = "15";
const responseProfileUpdateConflictMessage = "curtailment response profile changed before update; retry";
const sessionFormValuesByProfileId = new Map<string, ResponseProfileFormValues>();
const restoreBatchSizeOptions = {
  label: "restore batch size",
  max: curtailmentNumericFieldLimits.restoreBatchSize,
};
const restoreBatchIntervalOptions = {
  label: "restore batch interval",
  max: curtailmentNumericFieldLimits.restoreIntervalSec,
};
export type UseCurtailmentResponseProfilesResult = {
  responseProfiles: ResponseProfile[];
  isLoading: boolean;
  isCreating: boolean;
  updatingProfileIds: ReadonlySet<string>;
  loadError: string | null;
  createError: string | null;
  listResponseProfiles: (signal?: AbortSignal) => Promise<ResponseProfile[]>;
  createResponseProfile: (values: ResponseProfileFormValues) => Promise<ResponseProfile>;
  updateResponseProfile: (profileId: string, values: ResponseProfileFormValues) => Promise<ResponseProfile>;
  deleteResponseProfile: (profileId: string) => Promise<void>;
};

interface UseCurtailmentResponseProfilesOptions {
  siteNameById?: SiteNameById;
}

type ResponseProfileScopeValues = Pick<
  ResponseProfileFormValues,
  | "siteSelection"
  | "siteId"
  | "siteName"
  | "siteIds"
  | "siteNamesById"
  | "buildingTargetIds"
  | "rackTargetIds"
  | "groupTargetIds"
  | "deviceIdentifiers"
  | "minerSelectionMode"
> & {
  scopeType?: ResponseProfileFormValues["scopeType"];
  readOnlyScopeSummary?: string;
};

function getUnknownResponseProfileScopeValues(): ResponseProfileScopeValues {
  return {
    scopeType: undefined,
    siteSelection: "none",
    siteId: "",
    siteName: "",
    siteIds: [],
    siteNamesById: {},
    buildingTargetIds: [],
    rackTargetIds: [],
    groupTargetIds: [],
    deviceIdentifiers: [],
    minerSelectionMode: "subset",
    readOnlyScopeSummary: "Unknown scope",
  };
}

function numberToInputValue(value: number | undefined): string {
  return value && Number.isFinite(value) && value > 0 ? value.toString() : "";
}

function isResponseProfileUpdateConflict(error: unknown): boolean {
  return (
    error instanceof ConnectError &&
    error.code === Code.FailedPrecondition &&
    error.rawMessage === responseProfileUpdateConflictMessage
  );
}

function numberToNonNegativeInputValue(value: number | undefined): string {
  return value !== undefined && Number.isFinite(value) && value >= 0 ? value.toString() : "";
}

function curtailBatchIntervalInputValue(profile: ApiCurtailmentResponseProfile): string {
  return (profile.curtailBatchSize ?? 0) > 0 ? numberToNonNegativeInputValue(profile.curtailBatchIntervalSec) : "";
}

function formatKw(value: number): string {
  return value.toLocaleString(undefined, { maximumFractionDigits: 2 });
}

const responseProfileScopeLabelByMode: Partial<Record<CurtailmentMode, string>> = {
  [CurtailmentMode.FIXED_KW]: "Whole fleet",
  [CurtailmentMode.FULL_FLEET]: "Whole fleet",
};

export function getResponseProfileScopeLabel(mode: CurtailmentMode): string {
  return responseProfileScopeLabelByMode[mode] ?? "Unknown scope";
}

export function getResponseProfileScopeLabelForActionType(actionType: ResponseProfileFormValues["actionType"]): string {
  return getResponseProfileScopeLabel(
    actionType === "fixedKwReduction" ? CurtailmentMode.FIXED_KW : CurtailmentMode.FULL_FLEET,
  );
}

function hasSameStringSet(left: readonly string[], right: readonly string[]): boolean {
  return left.length === right.length && left.every((value) => right.includes(value));
}

function getSelectedResponseProfileSiteIds(
  values: Pick<ResponseProfileFormValues, "siteSelection" | "siteId" | "siteIds">,
): string[] {
  const siteIds = normalizeCurtailmentSelectionValues(
    values.siteIds !== undefined && values.siteIds.length > 0 ? values.siteIds : values.siteId ? [values.siteId] : [],
  );

  return values.siteSelection === "site" ||
    values.siteSelection === "allSites" ||
    (values.siteSelection === undefined && siteIds.length > 0)
    ? siteIds
    : [];
}

function getResponseProfileSiteNameForId(values: Partial<ResponseProfileFormValues>, siteId: string): string {
  return values.siteNamesById?.[siteId]?.trim() || (values.siteId === siteId ? values.siteName?.trim() : "") || "";
}

function getResponseProfileSiteNamesById(
  values: ResponseProfileFormValues,
  siteIds: readonly string[],
): Record<string, string> {
  return Object.fromEntries(
    siteIds.map((siteId) => [siteId, getResponseProfileSiteNameForId(values, siteId) || getSiteDisplayName(siteId)]),
  );
}

function getPersistedResponseProfileFormValues(values: ResponseProfileFormValues): ResponseProfileFormValues {
  const terminalScope = getCurtailmentTerminalScope(values);
  if (terminalScope === undefined) {
    throw new Error("Select a curtailment target scope.");
  }
  const scopeFields = getCurtailmentScopeFormFields(terminalScope);
  const siteIds = scopeFields.siteIds;
  const siteId = siteIds[0] ?? "";
  const siteSelection =
    scopeFields.scopeType === "wholeOrg"
      ? "allSites"
      : values.siteSelection === "allSites" && scopeFields.scopeType === "site"
        ? "allSites"
        : siteIds.length > 0
          ? "site"
          : "none";

  return {
    ...values,
    facilityFanDeviceIds: [...new Set(values.facilityFanDeviceIds ?? [])],
    fanOffDelaySec: values.fanOffDelaySec?.trim() ?? "",
    fanRestoreDelaySec: values.fanRestoreDelaySec?.trim() ?? "",
    ...scopeFields,
    minerSelectionMode: scopeFields.scopeType === "wholeOrg" ? "all" : "subset",
    siteSelection,
    siteId,
    siteIds,
    siteName: siteId ? getResponseProfileSiteNameForId(values, siteId) : "",
    siteNamesById: getResponseProfileSiteNamesById(values, siteIds),
  };
}

function getResponseProfileSiteName(
  siteId: string,
  cachedFormValues?: ResponseProfileFormValues,
  siteNameById?: SiteNameById,
): string {
  const loadedSiteName = siteNameById?.get(siteId)?.trim();
  const cachedSiteName =
    cachedFormValues?.siteNamesById?.[siteId]?.trim() ||
    (cachedFormValues?.siteId === siteId ? cachedFormValues.siteName.trim() : "");
  return loadedSiteName || cachedSiteName || getSiteDisplayName(siteId);
}

function mapApiResponseProfile(profile: ApiCurtailmentResponseProfile, siteNameById?: SiteNameById): ResponseProfile {
  const cachedFormValues = sessionFormValuesByProfileId.get(profile.profileId.toString());
  const { readOnlyScopeSummary, ...scopeFormValues } = getApiResponseProfileScopeValues(
    profile,
    cachedFormValues,
    siteNameById,
  );
  const fixedKwParams = profile.modeParams.case === "fixedKw" ? profile.modeParams.value : undefined;
  const fixedKw = fixedKwParams?.targetKw;
  const actionType: ResponseProfileFormValues["actionType"] =
    profile.mode === CurtailmentMode.FIXED_KW ? "fixedKwReduction" : "fullFleet";
  const targetKw = numberToInputValue(fixedKw);
  const responseDeadlineMinutes = defaultResponseDeadlineMinutes;
  const restoreBehavior: ResponseProfileFormValues["restoreBehavior"] =
    profile.restoreBatchIntervalSec === 0 && profile.restoreBatchSize === immediateRestoreBatchSize
      ? "automaticImmediateRestore"
      : "automaticBatchRestore";
  const targetSummary =
    actionType === "fixedKwReduction" && fixedKw !== undefined ? `${formatKw(fixedKw)} kW target` : "100% reduction";

  const formValues: ResponseProfileFormValues | undefined = scopeFormValues.scopeType
    ? {
        name: profile.profileName,
        actionType,
        ...scopeFormValues,
        scopeType: scopeFormValues.scopeType,
        targetKw,
        toleranceKw: numberToNonNegativeInputValue(fixedKwParams?.toleranceKw),
        priority: profile.priority === CurtailmentPriority.EMERGENCY ? "emergency" : "normal",
        postEventCooldownSec: numberToNonNegativeInputValue(profile.postEventCooldownSec),
        selectionStrategy: "leastEfficientFirst",
        restoreBehavior,
        minDurationSec: "",
        maxDurationSec: "",
        curtailBatchSize: numberToInputValue(profile.curtailBatchSize),
        curtailBatchIntervalSec: curtailBatchIntervalInputValue(profile),
        restoreBatchSize: numberToNonNegativeInputValue(profile.restoreBatchSize),
        restoreIntervalSec: numberToNonNegativeInputValue(profile.restoreBatchIntervalSec),
        facilityFanDeviceIds: profile.facilityFanDeviceIds.map((id) => id.toString()),
        fanOffDelaySec: numberToNonNegativeInputValue(profile.fanOffDelaySec),
        fanRestoreDelaySec: numberToNonNegativeInputValue(profile.fanRestoreDelaySec),
        responseDeadlineMinutes,
        includeMaintenance: profile.includeMaintenance,
        forceIncludeAllPairedMiners: profile.forceIncludeAllPairedMiners,
      }
    : undefined;
  const mergedFormValues =
    formValues && cachedFormValues && !readOnlyScopeSummary
      ? {
          ...formValues,
          minDurationSec: cachedFormValues.minDurationSec,
          maxDurationSec: cachedFormValues.maxDurationSec,
          responseDeadlineMinutes: cachedFormValues.responseDeadlineMinutes,
          ...scopeFormValues,
        }
      : formValues;
  let scope: string;
  if (readOnlyScopeSummary) {
    scope = readOnlyScopeSummary;
  } else if (mergedFormValues) {
    scope = getResponseProfileScopeSummary(mergedFormValues, profile.mode);
  } else {
    throw new Error("Response profile scope is required");
  }

  return {
    id: profile.profileId.toString(),
    revision: profile.revision,
    name: profile.profileName,
    targetSummary,
    scope,
    selectionStrategy: "Least efficient first",
    restoreBehavior: restoreBehavior === "automaticImmediateRestore" ? "Restore immediately" : "Restore in batches",
    deadlineSummary: responseDeadlineMinutes === "1" ? "Within 1 min" : `Within ${responseDeadlineMinutes} min`,
    formValues: readOnlyScopeSummary ? undefined : mergedFormValues,
    isReadOnly: Boolean(readOnlyScopeSummary),
    isAutomationReady: isResponseProfileAutomationReady(scopeFormValues.scopeType),
  };
}

function getApiResponseProfileScopeValues(
  profile: ApiCurtailmentResponseProfile,
  cachedFormValues?: ResponseProfileFormValues,
  siteNameById?: SiteNameById,
): ResponseProfileScopeValues {
  const profileSiteId = profile.site?.siteId;
  if (profile.scopes.length === 0 && !profileSiteId) {
    return getUnknownResponseProfileScopeValues();
  }

  let terminalScope;
  if (profile.scopes.length > 0) {
    if (profile.scopeSchemaVersion !== curtailmentScopeSchemaVersion) {
      return getUnknownResponseProfileScopeValues();
    }
    try {
      terminalScope = parseCurtailmentTerminalScopes(profile.scopes);
    } catch {
      return getUnknownResponseProfileScopeValues();
    }
  } else {
    terminalScope = { type: "site" as const, siteIds: [profileSiteId?.toString() ?? ""] };
  }
  const scopeFields = getCurtailmentScopeFormFields(terminalScope);
  let siteSelection: ResponseProfileFormValues["siteSelection"] =
    scopeFields.scopeType === "wholeOrg" ? "allSites" : scopeFields.scopeType === "site" ? "site" : "none";
  const siteIds = scopeFields.siteIds;
  if (
    cachedFormValues?.siteSelection === "allSites" &&
    siteSelection === "site" &&
    hasSameStringSet(siteIds, getSelectedResponseProfileSiteIds(cachedFormValues))
  ) {
    siteSelection = "allSites";
  }
  const siteId = siteIds[0] ?? "";
  const siteNamesById = Object.fromEntries(
    siteIds.map((currentSiteId) => [
      currentSiteId,
      getResponseProfileSiteName(currentSiteId, cachedFormValues, siteNameById),
    ]),
  );
  return {
    ...scopeFields,
    siteSelection,
    siteId,
    siteName: siteId ? siteNamesById[siteId] : "",
    siteNamesById,
    minerSelectionMode: terminalScope.type === "wholeOrg" ? "all" : "subset",
    readOnlyScopeSummary: undefined,
  };
}

function getResponseProfileScopeSummary(values: ResponseProfileFormValues, mode: CurtailmentMode): string {
  return getCurtailmentScopeSummary(values, {
    fallbackLabel: getResponseProfileScopeLabel(mode),
    getSiteLabel: (siteId) => getResponseProfileSiteNameForId(values, siteId),
  });
}

export function clearCurtailmentResponseProfileSessionCacheForTest(): void {
  sessionFormValuesByProfileId.clear();
}

function getModeParams(values: ResponseProfileFormValues): UpdateCurtailmentResponseProfileRequest["modeParams"] {
  if (values.actionType !== "fixedKwReduction") {
    return { case: undefined };
  }

  return {
    case: "fixedKw",
    value: create(FixedKwParamsSchema, {
      targetKw: Number(values.targetKw),
      toleranceKw: Number(values.toleranceKw || "0"),
    }),
  };
}

function getRestoreBatchSize(values: ResponseProfileFormValues): number | undefined {
  const parsedField = parseOptionalUint32Field(values.restoreBatchSize, restoreBatchSizeOptions);
  if (parsedField.error) {
    throw new Error(parsedField.error);
  }
  if (values.restoreBehavior === "automaticImmediateRestore") {
    return immediateRestoreBatchSize;
  }
  if (parsedField.parsed === undefined || parsedField.parsed === 0) {
    throw new Error("Enter restore batch size greater than 0 for batch restore.");
  }

  return parsedField.parsed;
}

function getRestoreBatchIntervalSec(values: ResponseProfileFormValues): number | undefined {
  if (values.restoreBehavior === "automaticImmediateRestore") {
    return 0;
  }
  return getOptionalUint32Setting(values.restoreIntervalSec, restoreBatchIntervalOptions);
}

function getOptionalPositiveNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed === "") {
    return undefined;
  }

  const parsed = Number(trimmed);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
}

function getOptionalNonNegativeNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed === "") {
    return undefined;
  }

  const parsed = Number(trimmed);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
}

function buildResponseProfilePayload(values: ResponseProfileFormValues) {
  const scopes = buildCurtailmentScopes(values);
  if (scopes === undefined) {
    throw new Error("Select a curtailment target scope.");
  }
  // All-paired targeting requires a logical scope; explicit miner snapshots
  // remain unsupported. Enabling it also opts in maintenance-flagged miners,
  // mirroring the Start request builders. The
  // proto validator requires include_maintenance == force_include_maintenance.
  //
  // The maintenance pair derives SOLELY from the all-paired flag: the form
  // hydrates includeMaintenance from previously saved profiles (where the
  // coupling wrote it as true), and with the maintenance toggle gone from the
  // UI, unchecking "Target all paired miners" must also drop the admin-gated
  // maintenance inclusion instead of silently carrying it forward.
  const forceIncludeAllPairedMiners =
    values.actionType === "fullFleet" &&
    Boolean(values.forceIncludeAllPairedMiners) &&
    scopes.every((scope) => scope.scope.case !== "deviceIdentifiers");
  const includeMaintenance = forceIncludeAllPairedMiners;
  return {
    profileName: values.name.trim(),
    scopes,
    scopeSchemaVersion: curtailmentScopeSchemaVersion,
    mode: values.actionType === "fixedKwReduction" ? CurtailmentMode.FIXED_KW : CurtailmentMode.FULL_FLEET,
    strategy: CurtailmentStrategy.LEAST_EFFICIENT_FIRST,
    level: CurtailmentLevel.FULL,
    priority: values.priority === "emergency" ? CurtailmentPriority.EMERGENCY : CurtailmentPriority.NORMAL,
    modeParams: getModeParams(values),
    curtailBatchSize: getOptionalPositiveNumber(values.curtailBatchSize),
    curtailBatchIntervalSec: getOptionalNonNegativeNumber(values.curtailBatchIntervalSec),
    restoreBatchSize: getRestoreBatchSize(values),
    restoreBatchIntervalSec: getRestoreBatchIntervalSec(values),
    facilityFanDeviceIds: [...new Set(values.facilityFanDeviceIds ?? [])].map((id) => BigInt(id)),
    fanOffDelaySec: getOptionalNonNegativeNumber(values.fanOffDelaySec ?? ""),
    fanRestoreDelaySec: getOptionalNonNegativeNumber(values.fanRestoreDelaySec ?? ""),
    includeMaintenance,
    forceIncludeMaintenance: includeMaintenance,
    forceIncludeAllPairedMiners,
    postEventCooldownSec: getOptionalNonNegativeNumber(values.postEventCooldownSec),
  };
}

export default function useCurtailmentResponseProfiles(
  enabled = true,
  options: UseCurtailmentResponseProfilesOptions = {},
): UseCurtailmentResponseProfilesResult {
  const { siteNameById } = options;
  const siteNameByIdRef = useRef<SiteNameById | undefined>(siteNameById);
  siteNameByIdRef.current = siteNameById;
  const { handleAuthErrors } = useAuthErrors();
  const [apiProfiles, setApiProfiles] = useState<ApiCurtailmentResponseProfile[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [isCreating, setIsCreating] = useState(false);
  const [updatingProfileIds, setUpdatingProfileIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const hasLoadedProfilesRef = useRef(false);

  const responseProfiles = useMemo(
    () => apiProfiles.map((profile) => mapApiResponseProfile(profile, siteNameById)),
    [apiProfiles, siteNameById],
  );

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string): Error => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const mapProfile = useCallback(
    (profile: ApiCurtailmentResponseProfile): ResponseProfile => mapApiResponseProfile(profile, siteNameById),
    [siteNameById],
  );

  const listResponseProfiles = useCallback(
    async (signal?: AbortSignal): Promise<ResponseProfile[]> => {
      const shouldShowLoading = !hasLoadedProfilesRef.current;
      if (shouldShowLoading) {
        setIsLoading(true);
      }

      try {
        assertNotAborted(signal);
        const response = await curtailmentClient.listCurtailmentResponseProfiles(
          create(ListCurtailmentResponseProfilesRequestSchema, {}),
          signal ? { signal } : undefined,
        );
        assertNotAborted(signal);

        setApiProfiles(response.profiles);
        hasLoadedProfilesRef.current = true;
        setLoadError(null);
        return response.profiles.map((profile) => mapApiResponseProfile(profile, siteNameByIdRef.current));
      } catch (error) {
        if (isAbortError(error, signal)) {
          throw error;
        }

        const resolvedError = handleFailure(error, "Failed to load response profiles.");
        setLoadError(resolvedError.message);
        throw resolvedError;
      } finally {
        if (shouldShowLoading) {
          setIsLoading(false);
        }
      }
    },
    [handleFailure],
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const abortController = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    void listResponseProfiles(abortController.signal).catch(() => {});

    return () => {
      abortController.abort();
    };
  }, [enabled, listResponseProfiles]);

  const createResponseProfile = useCallback(
    async (values: ResponseProfileFormValues): Promise<ResponseProfile> => {
      setIsCreating(true);
      setCreateError(null);

      try {
        const response = await curtailmentClient.createCurtailmentResponseProfile(
          create(CreateCurtailmentResponseProfileRequestSchema, buildResponseProfilePayload(values)),
        );
        if (!response.profile) {
          throw new Error("Created response profile response was missing a profile.");
        }

        const createdProfile = response.profile;
        sessionFormValuesByProfileId.set(
          createdProfile.profileId.toString(),
          getPersistedResponseProfileFormValues(values),
        );
        setApiProfiles((currentProfiles) => [
          ...currentProfiles.filter((currentProfile) => currentProfile.profileId !== createdProfile.profileId),
          createdProfile,
        ]);
        return mapProfile(createdProfile);
      } catch (error) {
        const resolvedError = handleFailure(error, "Failed to create response profile.");
        setCreateError(resolvedError.message);
        throw resolvedError;
      } finally {
        setIsCreating(false);
      }
    },
    [handleFailure, mapProfile],
  );

  const updateResponseProfile = useCallback(
    async (profileId: string, values: ResponseProfileFormValues): Promise<ResponseProfile> => {
      setUpdatingProfileIds((currentIds) => new Set(currentIds).add(profileId));

      try {
        const currentProfile = apiProfiles.find((profile) => profile.profileId.toString() === profileId);
        if (!currentProfile?.revision) {
          throw new Error("Reload the response profile before updating it.");
        }
        const response = await curtailmentClient.updateCurtailmentResponseProfile(
          create(UpdateCurtailmentResponseProfileRequestSchema, {
            profileId: BigInt(profileId),
            expectedRevision: currentProfile.revision,
            ...buildResponseProfilePayload(values),
            replaceFacilityFanSettings: true,
          }),
        );
        if (!response.profile) {
          throw new Error("Updated response profile response was missing a profile.");
        }

        const updatedProfile = response.profile;
        sessionFormValuesByProfileId.set(
          updatedProfile.profileId.toString(),
          getPersistedResponseProfileFormValues(values),
        );
        setApiProfiles((currentProfiles) =>
          currentProfiles.map((currentProfile) =>
            currentProfile.profileId === updatedProfile.profileId ? updatedProfile : currentProfile,
          ),
        );
        return mapProfile(updatedProfile);
      } catch (error) {
        if (!isResponseProfileUpdateConflict(error)) {
          throw handleFailure(error, "Failed to update response profile.");
        }
        let conflictMessage =
          "This response profile changed in another session. The latest values have been loaded; review them before trying again.";
        try {
          sessionFormValuesByProfileId.delete(profileId);
          await listResponseProfiles();
        } catch {
          conflictMessage = "This response profile changed in another session. Reload the page before trying again.";
        }
        throw createErrorWithCause(conflictMessage, error);
      } finally {
        setUpdatingProfileIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(profileId);
          return nextIds;
        });
      }
    },
    [apiProfiles, handleFailure, listResponseProfiles, mapProfile],
  );

  const deleteResponseProfile = useCallback(
    async (profileId: string): Promise<void> => {
      setUpdatingProfileIds((currentIds) => new Set(currentIds).add(profileId));

      try {
        await curtailmentClient.deleteCurtailmentResponseProfile(
          create(DeleteCurtailmentResponseProfileRequestSchema, {
            profileId: BigInt(profileId),
          }),
        );
        setApiProfiles((currentProfiles) =>
          currentProfiles.filter((currentProfile) => currentProfile.profileId.toString() !== profileId),
        );
      } catch (error) {
        throw handleFailure(error, "Failed to delete response profile.");
      } finally {
        setUpdatingProfileIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(profileId);
          return nextIds;
        });
      }
    },
    [handleFailure],
  );

  return useMemo(
    () => ({
      responseProfiles,
      isLoading: enabled ? isLoading : false,
      isCreating,
      updatingProfileIds,
      loadError,
      createError,
      listResponseProfiles,
      createResponseProfile,
      updateResponseProfile,
      deleteResponseProfile,
    }),
    [
      responseProfiles,
      enabled,
      isLoading,
      isCreating,
      updatingProfileIds,
      loadError,
      createError,
      listResponseProfiles,
      createResponseProfile,
      updateResponseProfile,
      deleteResponseProfile,
    ],
  );
}
