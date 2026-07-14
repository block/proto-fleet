import { useCallback } from "react";
import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";

import { cohortClient } from "@/protoFleet/api/clients";
import {
  AdminReassignRequestSchema,
  type Cohort,
  type CohortDesiredConfig,
  type CohortDevice,
  CohortDeviceAssignment,
  type CohortDeviceFilter,
  CohortDeviceFilterSchema,
  CohortDeviceIdentifierListSchema,
  CohortDeviceSelectorSchema,
  type CohortSummary,
  CreateCohortRequestSchema,
  GetCohortFirmwareVersionHistoryRequestSchema,
  type GetCohortFirmwareVersionHistoryResponse,
  GetCohortRequestSchema,
  GetMyCohortsRequestSchema,
  ListCohortsRequestSchema,
  ListDevicesRequestSchema,
  ReleaseCohortRequestSchema,
  SetCohortFirmwareTargetRequestSchema,
  UpdateCohortRequestSchema,
} from "@/protoFleet/api/generated/cohort/v1/cohort_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface MutationCallbacks<T> {
  onSuccess?: (value: T) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface CreateCohortProps extends MutationCallbacks<Cohort> {
  label: string;
  purpose: string;
  claimOwnership?: boolean;
  expiresAt?: Date;
  desiredFirmwareFileId?: string;
  desiredConfig?: CohortDesiredConfig;
  deviceIdentifiers?: string[];
  sourceDeviceSetId?: bigint;
  selector?: {
    count: number;
    product?: string;
    model?: string;
  };
}

interface ReleaseCohortProps extends MutationCallbacks<Cohort> {
  cohortId: bigint;
}

interface ExtendCohortProps extends MutationCallbacks<Cohort> {
  cohortId: bigint;
  expiresAt: Date;
}

interface SetDesiredFirmwareProps extends MutationCallbacks<Cohort> {
  cohortId: bigint;
  manufacturer: string;
  model: string;
  firmwareFileId?: string;
}

interface SetDesiredPoolsProps extends MutationCallbacks<Cohort> {
  cohortId: bigint;
  desiredConfig?: CohortDesiredConfig;
}

interface AddRemoveDevicesProps extends MutationCallbacks<Cohort> {
  cohortId: bigint;
  deviceIdentifiers: string[];
}

interface AdminReassignProps extends MutationCallbacks<Cohort> {
  targetCohortId: bigint;
  deviceIdentifiers: string[];
}

export interface PagedCohortsResult {
  cohorts: CohortSummary[];
  nextPageToken: string;
  totalCount: number;
}

export interface PagedCohortDevicesResult {
  devices: CohortDevice[];
  nextPageToken: string;
  totalCount: number;
  availableCount: number;
  reservedCount: number;
}

interface ListCohortsProps extends MutationCallbacks<PagedCohortsResult> {
  includeReleased?: boolean;
  pageSize?: number;
  pageToken?: string;
  search?: string;
}

interface GetCohortProps extends MutationCallbacks<Cohort> {
  cohortId: bigint;
}

interface GetCohortFirmwareVersionHistoryProps extends MutationCallbacks<GetCohortFirmwareVersionHistoryResponse> {
  cohortId: bigint;
  startTime: Date;
  endTime: Date;
  granularitySeconds: number;
}

interface CohortDeviceFilterProps {
  assignments?: CohortDeviceAssignment[];
  cohortIds?: bigint[];
  ownerUserIds?: bigint[];
  includeUnowned?: boolean;
  manufacturers?: string[];
  models?: string[];
  search?: string;
}

interface ListDevicesProps extends MutationCallbacks<PagedCohortDevicesResult> {
  pageSize?: number;
  pageToken?: string;
  filter?: CohortDeviceFilterProps;
}

const allDevicesPageSize = 500;
const maxListAllPages = 200;

function buildCohortDeviceFilter(filter?: CohortDeviceFilterProps): CohortDeviceFilter | undefined {
  if (!filter) return undefined;
  const hasFilter =
    (filter.assignments?.length ?? 0) > 0 ||
    (filter.cohortIds?.length ?? 0) > 0 ||
    (filter.ownerUserIds?.length ?? 0) > 0 ||
    Boolean(filter.includeUnowned) ||
    (filter.manufacturers?.length ?? 0) > 0 ||
    (filter.models?.length ?? 0) > 0 ||
    Boolean(filter.search?.trim());
  if (!hasFilter) return undefined;
  return create(CohortDeviceFilterSchema, {
    assignments: filter.assignments ?? [],
    cohortIds: filter.cohortIds ?? [],
    ownerUserIds: filter.ownerUserIds ?? [],
    includeUnowned: filter.includeUnowned,
    manufacturers: filter.manufacturers ?? [],
    models: filter.models ?? [],
    search: filter.search?.trim(),
  });
}

function timestampFromDate(date: Date) {
  return create(TimestampSchema, {
    seconds: BigInt(Math.floor(date.getTime() / 1000)),
    nanos: (date.getTime() % 1000) * 1_000_000,
  });
}

export function useCohortApi() {
  const { handleAuthErrors } = useAuthErrors();

  const handleError = useCallback(
    (error: unknown, onError?: (message: string) => void) => {
      handleAuthErrors({ error });
      onError?.(getErrorMessage(error));
    },
    [handleAuthErrors],
  );

  const listCohorts = useCallback(
    async ({ includeReleased, pageSize, pageToken, search, onSuccess, onError, onFinally }: ListCohortsProps = {}) => {
      try {
        const response = await cohortClient.listCohorts(
          create(ListCohortsRequestSchema, { includeReleased, pageSize, pageToken, search }),
        );
        const result = {
          cohorts: response.cohorts,
          nextPageToken: response.nextPageToken,
          totalCount: response.totalCount,
        };
        onSuccess?.(result);
        return result;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const getMyCohorts = useCallback(
    async ({ includeReleased, pageSize, pageToken, search, onSuccess, onError, onFinally }: ListCohortsProps = {}) => {
      try {
        const response = await cohortClient.getMyCohorts(
          create(GetMyCohortsRequestSchema, { includeReleased, pageSize, pageToken, search }),
        );
        const result = {
          cohorts: response.cohorts,
          nextPageToken: response.nextPageToken,
          totalCount: response.totalCount,
        };
        onSuccess?.(result);
        return result;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const getCohort = useCallback(
    async ({ cohortId, onSuccess, onError, onFinally }: GetCohortProps) => {
      try {
        const response = await cohortClient.getCohort(create(GetCohortRequestSchema, { cohortId }));
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const getFirmwareVersionHistory = useCallback(
    async ({
      cohortId,
      startTime,
      endTime,
      granularitySeconds,
      onSuccess,
      onError,
      onFinally,
    }: GetCohortFirmwareVersionHistoryProps) => {
      try {
        const response = await cohortClient.getCohortFirmwareVersionHistory(
          create(GetCohortFirmwareVersionHistoryRequestSchema, {
            cohortId,
            startTime: timestampFromDate(startTime),
            endTime: timestampFromDate(endTime),
            granularity: { seconds: BigInt(granularitySeconds), nanos: 0 },
          }),
        );
        onSuccess?.(response);
        return response;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const listDevices = useCallback(
    async ({ pageSize, pageToken, filter, onSuccess, onError, onFinally }: ListDevicesProps = {}) => {
      try {
        const response = await cohortClient.listDevices(
          create(ListDevicesRequestSchema, {
            pageSize,
            pageToken,
            filter: buildCohortDeviceFilter(filter),
          }),
        );
        const result = {
          devices: response.devices,
          nextPageToken: response.nextPageToken,
          totalCount: response.totalCount,
          availableCount: response.availableCount,
          reservedCount: response.reservedCount,
        };
        onSuccess?.(result);
        return result;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const listAllDevices = useCallback(
    async ({ filter, onError, onFinally }: Omit<ListDevicesProps, "pageSize" | "pageToken" | "onSuccess"> = {}) => {
      try {
        const devices: CohortDevice[] = [];
        let pageToken = "";
        for (let page = 0; page < maxListAllPages; page += 1) {
          const response = await cohortClient.listDevices(
            create(ListDevicesRequestSchema, {
              pageSize: allDevicesPageSize,
              pageToken,
              filter: buildCohortDeviceFilter(filter),
            }),
          );
          devices.push(...response.devices);
          if (!response.nextPageToken) return devices;
          pageToken = response.nextPageToken;
        }
        throw new Error("Too many cohort device pages to load at once");
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const createCohort = useCallback(
    async (props: CreateCohortProps) => {
      const { onSuccess, onError, onFinally, deviceIdentifiers, sourceDeviceSetId, selector, ...values } = props;
      try {
        const response = await cohortClient.createCohort(
          create(CreateCohortRequestSchema, {
            ...values,
            expiresAt: values.expiresAt ? timestampFromDate(values.expiresAt) : undefined,
            initialMembers:
              sourceDeviceSetId !== undefined
                ? { case: "sourceDeviceSetId", value: sourceDeviceSetId }
                : selector !== undefined
                  ? {
                      case: "select",
                      value: create(CohortDeviceSelectorSchema, {
                        count: selector.count,
                        product: selector.product || undefined,
                        model: selector.model || undefined,
                      }),
                    }
                  : {
                      case: "deviceIdentifiers",
                      value: create(CohortDeviceIdentifierListSchema, { deviceIdentifiers: deviceIdentifiers ?? [] }),
                    },
          }),
        );
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const releaseCohort = useCallback(
    async ({ cohortId, onSuccess, onError, onFinally }: ReleaseCohortProps) => {
      try {
        const response = await cohortClient.releaseCohort(create(ReleaseCohortRequestSchema, { cohortId }));
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const extendCohort = useCallback(
    async ({ cohortId, expiresAt, onSuccess, onError, onFinally }: ExtendCohortProps) => {
      try {
        const response = await cohortClient.updateCohort(
          create(UpdateCohortRequestSchema, { cohortId, expiresAt: timestampFromDate(expiresAt) }),
        );
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const setDesiredFirmware = useCallback(
    async ({
      cohortId,
      manufacturer,
      model,
      firmwareFileId,
      onSuccess,
      onError,
      onFinally,
    }: SetDesiredFirmwareProps) => {
      try {
        const response = await cohortClient.setCohortFirmwareTarget(
          create(SetCohortFirmwareTargetRequestSchema, {
            cohortId,
            manufacturer,
            model,
            firmwareFileId: firmwareFileId ?? "",
          }),
        );
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const setDesiredPools = useCallback(
    async ({ cohortId, desiredConfig, onSuccess, onError, onFinally }: SetDesiredPoolsProps) => {
      try {
        const response = await cohortClient.updateCohort(
          create(UpdateCohortRequestSchema, {
            cohortId,
            desiredConfig,
            clearDesiredConfig: desiredConfig === undefined,
          }),
        );
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const addDevices = useCallback(
    async ({ cohortId, deviceIdentifiers, onSuccess, onError, onFinally }: AddRemoveDevicesProps) => {
      try {
        const response = await cohortClient.addDevicesToCohort({ cohortId, deviceIdentifiers });
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const removeDevices = useCallback(
    async ({ cohortId, deviceIdentifiers, onSuccess, onError, onFinally }: AddRemoveDevicesProps) => {
      try {
        const response = await cohortClient.removeDevicesFromCohort({ cohortId, deviceIdentifiers });
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  const adminReassign = useCallback(
    async ({ targetCohortId, deviceIdentifiers, onSuccess, onError, onFinally }: AdminReassignProps) => {
      try {
        const response = await cohortClient.adminReassign(
          create(AdminReassignRequestSchema, { targetCohortId, deviceIdentifiers }),
        );
        if (!response.cohort) throw new Error("Cohort response was empty");
        onSuccess?.(response.cohort);
        return response.cohort;
      } catch (error) {
        handleError(error, onError);
        throw error;
      } finally {
        onFinally?.();
      }
    },
    [handleError],
  );

  return {
    listCohorts,
    getMyCohorts,
    getCohort,
    getFirmwareVersionHistory,
    listDevices,
    listAllDevices,
    createCohort,
    releaseCohort,
    extendCohort,
    setDesiredFirmware,
    setDesiredPools,
    addDevices,
    removeDevices,
    adminReassign,
  };
}
