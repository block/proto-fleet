import { useCallback } from "react";

import { sitesClient } from "@/protoFleet/api/clients";
import { type PerDeviceConflict, type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListSitesProps {
  signal?: AbortSignal;
  onSuccess?: (sites: SiteWithCounts[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

// Parse a string-encoded bigint id (the form we get from URL params and
// localStorage). Rejects empty strings, non-numeric input, and non-positive
// values so callers can short-circuit cleanly on bad input.
export const parseBigIntId = (value: unknown): bigint | null => {
  if (typeof value !== "string" || value.trim() === "") return null;
  try {
    const parsed = BigInt(value);
    return parsed > 0n ? parsed : null;
  } catch {
    return null;
  }
};

// Build the set of known site ids (decimal-string form) from a ListSites
// response. Centralised so SitePicker, SitesPage, SettingsSitesPage, and
// SitesAllTable can't drift on the derivation rule.
export const buildKnownSiteIds = (sites: SiteWithCounts[] | undefined): Set<string> => {
  if (!sites) return new Set();
  return new Set(sites.map((s) => (s.site?.id ?? 0n).toString()).filter((id) => id !== "0"));
};

// Shared shape passed between SiteDetailsModal and ManageSiteModal so the
// create flow can hold the in-progress draft in memory while the operator
// switches between the two surfaces.
export interface SiteFormValues {
  name: string;
  locationCity: string;
  locationState: string;
  timezone: string;
  powerCapacityMw: number;
  networkConfig: string;
}

export const emptySiteFormValues = (): SiteFormValues => ({
  name: "",
  locationCity: "",
  locationState: "",
  timezone: "",
  powerCapacityMw: 0,
  networkConfig: "",
});

export const siteFormValuesFromSite = (site: Site): SiteFormValues => ({
  name: site.name,
  locationCity: site.locationCity,
  locationState: site.locationState,
  timezone: site.timezone,
  powerCapacityMw: site.powerCapacityMw,
  networkConfig: site.networkConfig,
});

interface CreateSiteProps {
  values: SiteFormValues;
  signal?: AbortSignal;
  onSuccess?: (site: Site, warnings: string[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface UpdateSiteProps {
  id: bigint;
  values: SiteFormValues;
  signal?: AbortSignal;
  onSuccess?: (site: Site, warnings: string[]) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface DeleteSiteCounts {
  unassignedDeviceCount: bigint;
  deletedBuildingCount: bigint;
  unassignedRackCount: bigint;
}

interface DeleteSiteProps {
  id: bigint;
  signal?: AbortSignal;
  onSuccess?: (counts: DeleteSiteCounts) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface ReassignDevicesToSiteProps {
  // Unset routes the devices to the "Unassigned" bucket; the create flow
  // always supplies a target so this is typically set in practice.
  targetSiteId?: bigint;
  deviceIdentifiers: string[];
  signal?: AbortSignal;
  onSuccess?: (reassignedCount: bigint) => void;
  // conflicts is populated when the server rejects the batch on
  // per-device validation (DEVICE_NOT_FOUND, DEVICE_IN_RACK_AT_OTHER_SITE).
  // Transport / auth failures pass an empty conflicts array.
  onError?: (message: string, conflicts: PerDeviceConflict[]) => void;
  onFinally?: () => void;
}

const useSites = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listSites = useCallback(
    async ({ signal, onSuccess, onError, onFinally }: ListSitesProps = {}) => {
      try {
        const response = await sitesClient.listSites({}, { signal });
        if (signal?.aborted) return;
        onSuccess?.(response.sites);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const createSite = useCallback(
    async ({ values, signal, onSuccess, onError, onFinally }: CreateSiteProps) => {
      try {
        const response = await sitesClient.createSite(
          {
            name: values.name,
            // `description` (notes) input is deferred per master plan §J3;
            // send empty string so the column is unset until the BE
            // follow-up + FE input ship together.
            description: "",
            locationCity: values.locationCity,
            locationState: values.locationState,
            timezone: values.timezone,
            powerCapacityMw: values.powerCapacityMw,
            networkConfig: values.networkConfig,
          },
          { signal },
        );
        if (signal?.aborted) return;
        if (!response.site) {
          onError?.("Server returned no site");
          return;
        }
        onSuccess?.(response.site, response.networkConfigWarnings);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const updateSite = useCallback(
    async ({ id, values, signal, onSuccess, onError, onFinally }: UpdateSiteProps) => {
      try {
        const response = await sitesClient.updateSite(
          {
            id,
            name: values.name,
            description: "",
            locationCity: values.locationCity,
            locationState: values.locationState,
            timezone: values.timezone,
            powerCapacityMw: values.powerCapacityMw,
            networkConfig: values.networkConfig,
          },
          { signal },
        );
        if (signal?.aborted) return;
        if (!response.site) {
          onError?.("Server returned no site");
          return;
        }
        onSuccess?.(response.site, response.networkConfigWarnings);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const deleteSite = useCallback(
    async ({ id, signal, onSuccess, onError, onFinally }: DeleteSiteProps) => {
      try {
        const response = await sitesClient.deleteSite({ id }, { signal });
        if (signal?.aborted) return;
        onSuccess?.({
          unassignedDeviceCount: response.unassignedDeviceCount,
          deletedBuildingCount: response.deletedBuildingCount,
          unassignedRackCount: response.unassignedRackCount,
        });
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error));
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  const reassignDevicesToSite = useCallback(
    async ({ targetSiteId, deviceIdentifiers, signal, onSuccess, onError, onFinally }: ReassignDevicesToSiteProps) => {
      try {
        const response = await sitesClient.reassignDevicesToSite(
          {
            targetSiteId,
            deviceIdentifiers,
          },
          { signal },
        );
        if (signal?.aborted) return;
        if (response.conflicts.length > 0) {
          onError?.("Some devices could not be reassigned", response.conflicts);
          return;
        }
        onSuccess?.(response.reassignedCount);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            onError?.(getErrorMessage(error), []);
          },
        });
      } finally {
        onFinally?.();
      }
    },
    [handleAuthErrors],
  );

  return { listSites, createSite, updateSite, deleteSite, reassignDevicesToSite };
};

export { useSites };
