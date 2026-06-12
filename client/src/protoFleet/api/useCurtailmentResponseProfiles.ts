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

function getPersistedResponseProfileFormValues(values: ResponseProfileFormValues): ResponseProfileFormValues {
  return { ...values, deviceIdentifiers: [...values.deviceIdentifiers] };
}

function getMinerScopeSummary(deviceIdentifiers: readonly string[]): string | undefined {
  if (deviceIdentifiers.length === 0) {
    return undefined;
  }

  return `${deviceIdentifiers.length} ${deviceIdentifiers.length === 1 ? "miner" : "miners"}`;
}

function mapApiResponseProfile(
  profile: ApiCurtailmentResponseProfile,
  siteLabelsById?: ReadonlyMap<string, string>,
): ResponseProfile {
  const apiSiteId = profile.site?.siteId.toString() ?? "";
  const siteId = apiSiteId;
  const siteName = apiSiteId ? (siteLabelsById?.get(apiSiteId) ?? `Site ${apiSiteId}`) : "";
  const cachedFormValues = sessionFormValuesByProfileId.get(profile.profileId.toString());
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
  const cachedDeviceIdentifiers = cachedFormValues?.deviceIdentifiers ?? [];
  const hasCachedMinerScope = cachedDeviceIdentifiers.length > 0;
  const mergedFormValues = cachedFormValues
    ? {
        ...formValues,
        ...cachedFormValues,
        name: profile.profileName,
        siteId: hasCachedMinerScope ? "" : siteId,
        siteName: hasCachedMinerScope ? "" : siteName,
        deviceIdentifiers: [...cachedDeviceIdentifiers],
      }
    : formValues;
  const minerScopeSummary = getMinerScopeSummary(mergedFormValues.deviceIdentifiers);

  return {
    id: profile.profileId.toString(),
    name: profile.profileName,
    targetSummary,
    siteId: minerScopeSummary ? "" : siteId,
    scope: minerScopeSummary ?? (siteName || "Whole fleet"),
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

function getOptionalSiteId(value: string): bigint | undefined {
  const siteId = value.trim();
  if (siteId === "") {
    return undefined;
  }

  if (!/^\d+$/.test(siteId)) {
    throw new Error("Select a valid site before saving the response profile.");
  }

  return BigInt(siteId);
}

function buildResponseProfilePayload(values: ResponseProfileFormValues) {
  const siteId = getOptionalSiteId(values.siteId);

  return {
    profileName: values.name.trim(),
    site: siteId === undefined ? undefined : create(ScopeSiteSchema, { siteId }),
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

export default function useCurtailmentResponseProfiles(
  enabled = true,
  siteLabelsById?: ReadonlyMap<string, string>,
): UseCurtailmentResponseProfilesResult {
  const { handleAuthErrors } = useAuthErrors();
  const [apiProfiles, setApiProfiles] = useState<ApiCurtailmentResponseProfile[]>([]);
  const [isLoading, setIsLoading] = useState(enabled);
  const [isCreating, setIsCreating] = useState(false);
  const [updatingProfileIds, setUpdatingProfileIds] = useState<Set<string>>(() => new Set());
  const [loadError, setLoadError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const hasLoadedProfilesRef = useRef(false);

  const responseProfiles = useMemo(
    () => apiProfiles.map((profile) => mapApiResponseProfile(profile, siteLabelsById)),
    [apiProfiles, siteLabelsById],
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
    (profile: ApiCurtailmentResponseProfile): ResponseProfile => mapApiResponseProfile(profile, siteLabelsById),
    [siteLabelsById],
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
