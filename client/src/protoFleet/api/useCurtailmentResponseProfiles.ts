import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";

import { curtailmentClient } from "@/protoFleet/api/clients";
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
  ScopeSiteSchema,
  type UpdateCurtailmentResponseProfileRequest,
  UpdateCurtailmentResponseProfileRequestSchema,
} from "@/protoFleet/api/generated/curtailment/v1/curtailment_pb";
import { assertNotAborted, isAbortError, toError } from "@/protoFleet/api/requestErrors";
import type {
  ResponseProfile,
  ResponseProfileFormValues,
} from "@/protoFleet/features/settings/components/Curtailment/types";
import { useAuthErrors } from "@/protoFleet/store";

const defaultResponseDeadlineMinutes: string = "15";
const immediateRestoreBatchSize = 10_000;
const sessionFormValuesByProfileId = new Map<string, ResponseProfileFormValues>();

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

function numberToInputValue(value: number | undefined): string {
  return value && Number.isFinite(value) && value > 0 ? value.toString() : "";
}

function numberToNonNegativeInputValue(value: number | undefined): string {
  return value !== undefined && Number.isFinite(value) && value >= 0 ? value.toString() : "";
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

function getPersistedResponseProfileFormValues(values: ResponseProfileFormValues): ResponseProfileFormValues {
  const siteId = values.siteId.trim();

  return {
    ...values,
    deviceIdentifiers: [],
    siteId,
    siteName: siteId ? values.siteName.trim() : "",
  };
}

function mapApiResponseProfile(profile: ApiCurtailmentResponseProfile): ResponseProfile {
  const cachedFormValues = sessionFormValuesByProfileId.get(profile.profileId.toString());
  const siteId = profile.site?.siteId ? profile.site.siteId.toString() : "";
  const siteName = siteId ? cachedFormValues?.siteName || `Site ${siteId}` : "";
  const fixedKw = profile.modeParams.case === "fixedKw" ? profile.modeParams.value.targetKw : undefined;
  const actionType: ResponseProfileFormValues["actionType"] =
    profile.mode === CurtailmentMode.FIXED_KW ? "fixedKwReduction" : "fullFleet";
  const targetKw = numberToInputValue(fixedKw);
  const responseDeadlineMinutes = defaultResponseDeadlineMinutes;
  const restoreBehavior: ResponseProfileFormValues["restoreBehavior"] =
    profile.restoreBatchIntervalSec === 0 && profile.restoreBatchSize >= immediateRestoreBatchSize
      ? "automaticImmediateRestore"
      : "automaticBatchRestore";
  const targetSummary =
    actionType === "fixedKwReduction" && fixedKw !== undefined ? `${formatKw(fixedKw)} kW target` : "100% reduction";

  const formValues: ResponseProfileFormValues = {
    name: profile.profileName,
    actionType,
    targetKw,
    deviceIdentifiers: [],
    siteId,
    siteName,
    selectionStrategy: "leastEfficientFirst",
    restoreBehavior,
    minDurationSec: "",
    maxDurationSec: "",
    curtailBatchSize: numberToInputValue(profile.curtailBatchSize),
    curtailBatchIntervalSec: numberToNonNegativeInputValue(profile.curtailBatchIntervalSec),
    restoreBatchSize: numberToInputValue(profile.restoreBatchSize),
    restoreIntervalSec: numberToNonNegativeInputValue(profile.restoreBatchIntervalSec),
    responseDeadlineMinutes,
    includeMaintenance: profile.includeMaintenance,
  };
  const mergedFormValues = cachedFormValues
    ? {
        ...formValues,
        ...cachedFormValues,
        name: profile.profileName,
        siteId,
        siteName,
        deviceIdentifiers: [],
      }
    : formValues;
  const scope = siteId ? siteName || `Site ${siteId}` : getResponseProfileScopeLabel(profile.mode);

  return {
    id: profile.profileId.toString(),
    name: profile.profileName,
    targetSummary,
    scope,
    selectionStrategy: "Least efficient first",
    restoreBehavior: restoreBehavior === "automaticImmediateRestore" ? "Restore immediately" : "Restore in batches",
    deadlineSummary: responseDeadlineMinutes === "1" ? "Within 1 min" : `Within ${responseDeadlineMinutes} min`,
    formValues: mergedFormValues,
  };
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
    }),
  };
}

function getRestoreBatchSize(values: ResponseProfileFormValues): number | undefined {
  if (values.restoreBatchSize.trim() === "") {
    return values.restoreBehavior === "automaticImmediateRestore" ? immediateRestoreBatchSize : undefined;
  }

  const batchSize = Number(values.restoreBatchSize);
  if (Number.isFinite(batchSize) && batchSize > 0) {
    return batchSize;
  }

  return values.restoreBehavior === "automaticImmediateRestore" ? immediateRestoreBatchSize : undefined;
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

function getResponseProfileSite(values: ResponseProfileFormValues) {
  const siteId = values.siteId.trim();
  if (!/^[1-9]\d*$/.test(siteId)) {
    return undefined;
  }

  return create(ScopeSiteSchema, { siteId: BigInt(siteId) });
}

function buildResponseProfilePayload(values: ResponseProfileFormValues) {
  return {
    profileName: values.name.trim(),
    site: getResponseProfileSite(values),
    mode: values.actionType === "fixedKwReduction" ? CurtailmentMode.FIXED_KW : CurtailmentMode.FULL_FLEET,
    strategy: CurtailmentStrategy.LEAST_EFFICIENT_FIRST,
    level: CurtailmentLevel.FULL,
    priority: CurtailmentPriority.NORMAL,
    modeParams: getModeParams(values),
    curtailBatchSize: getOptionalPositiveNumber(values.curtailBatchSize),
    curtailBatchIntervalSec: getOptionalNonNegativeNumber(values.curtailBatchIntervalSec),
    restoreBatchSize: getRestoreBatchSize(values),
    restoreBatchIntervalSec: getOptionalNonNegativeNumber(values.restoreIntervalSec),
    includeMaintenance: values.includeMaintenance,
    forceIncludeMaintenance: values.includeMaintenance,
  };
}

export default function useCurtailmentResponseProfiles(enabled = true): UseCurtailmentResponseProfilesResult {
  const { handleAuthErrors } = useAuthErrors();
  const [apiProfiles, setApiProfiles] = useState<ApiCurtailmentResponseProfile[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [isCreating, setIsCreating] = useState(false);
  const [updatingProfileIds, setUpdatingProfileIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const hasLoadedProfilesRef = useRef(false);

  const responseProfiles = useMemo(() => apiProfiles.map((profile) => mapApiResponseProfile(profile)), [apiProfiles]);

  const handleFailure = useCallback(
    (error: unknown, fallbackMessage: string): Error => {
      const resolvedError = toError(error, fallbackMessage);
      handleAuthErrors({ error });
      return resolvedError;
    },
    [handleAuthErrors],
  );

  const mapProfile = useCallback(
    (profile: ApiCurtailmentResponseProfile): ResponseProfile => mapApiResponseProfile(profile),
    [],
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
        return response.profiles.map(mapProfile);
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
    [handleFailure, mapProfile],
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
        const response = await curtailmentClient.updateCurtailmentResponseProfile(
          create(UpdateCurtailmentResponseProfileRequestSchema, {
            profileId: BigInt(profileId),
            ...buildResponseProfilePayload(values),
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
        throw handleFailure(error, "Failed to update response profile.");
      } finally {
        setUpdatingProfileIds((currentIds) => {
          const nextIds = new Set(currentIds);
          nextIds.delete(profileId);
          return nextIds;
        });
      }
    },
    [handleFailure, mapProfile],
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
