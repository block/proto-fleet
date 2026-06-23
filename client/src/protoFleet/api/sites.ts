import { useCallback } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { sitesClient } from "@/protoFleet/api/clients";
import type { FleetListTelemetryRangeFilter } from "@/protoFleet/api/generated/common/v1/fleet_list_stats_pb";
import type { ComponentType } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import { type PerDeviceConflict, type Site, type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAuthErrors } from "@/protoFleet/store";

interface ListSitesProps {
  signal?: AbortSignal;
  errorComponentTypes?: ComponentType[];
  telemetryRanges?: FleetListTelemetryRangeFilter[];
  onSuccess?: (sites: SiteWithCounts[]) => void;
  // ListSites is gated server-side on org-scoped site:read; useHasPermission
  // returns true even for site-scoped-only roles, so callers that fall back
  // to a permission-blocked UX need to distinguish PermissionDenied from
  // transient transport failures.
  onError?: (message: string, code?: Code) => void;
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
// response. Centralised so SitePicker, SitesPage, and Fleet tabs can't drift
// on the derivation rule.
export const buildKnownSiteIds = (sites: SiteWithCounts[] | undefined): Set<string> | undefined => {
  if (!sites) return undefined;
  return new Set(sites.map((s) => (s.site?.id ?? 0n).toString()).filter((id) => id !== "0"));
};

// Shared shape passed between SiteDetailsModal and ManageSiteModal so the
// create flow can hold the in-progress draft in memory while the operator
// switches between the two surfaces.
export interface SiteFormValues {
  name: string;
  address: string;
  locationCity: string;
  locationState: string;
  postalCode: string;
  country: string;
  // IANA timezone id. Form seeds this from inferTimezone(country, state)
  // when the operator changes state, but they can override for sub-state
  // edges (e.g. N Idaho is Pacific, not Mountain).
  timezone: string;
  powerCapacityMw: number;
  networkConfig: string;
  notes: string;
}

export const emptySiteFormValues = (): SiteFormValues => ({
  name: "",
  address: "",
  locationCity: "",
  locationState: "",
  postalCode: "",
  country: "US",
  timezone: "",
  powerCapacityMw: 0,
  networkConfig: "",
  notes: "",
});

export const siteFormValuesFromSite = (site: Site): SiteFormValues => ({
  name: site.name,
  address: site.address,
  locationCity: site.locationCity,
  locationState: site.locationState,
  postalCode: site.postalCode,
  country: site.country || "US",
  timezone: site.timezone,
  powerCapacityMw: site.powerCapacityMw,
  networkConfig: site.networkConfig,
  notes: site.notes,
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

interface AssignDevicesToSiteProps {
  // Unset routes the devices to the "Unassigned" bucket; the create flow
  // always supplies a target so this is typically set in practice.
  targetSiteId?: bigint;
  deviceIdentifiers: string[];
  // When true, the server clears any conflicting rack memberships
  // inside the same transaction as the site write. Lets cross-site
  // reparent skip the client-side remove-from-rack loop and the
  // orphan window it created. When false/unset the server returns
  // DEVICE_IN_RACK_AT_OTHER_SITE conflicts (today's behavior).
  forceClearConflictingRackMembership?: boolean;
  signal?: AbortSignal;
  onSuccess?: (reassignedCount: bigint) => void;
  // conflicts is populated when the server rejects the batch on
  // per-device validation (DEVICE_NOT_FOUND, DEVICE_IN_RACK_AT_OTHER_SITE).
  // Transport / auth failures pass an empty conflicts array.
  onError?: (message: string, conflicts: PerDeviceConflict[]) => void;
  onFinally?: () => void;
}

interface AssignBuildingsToSiteProps {
  // Bulk-friendly. Pass a single-element array for the singular case.
  buildingIds: bigint[];
  // Unset moves the buildings to "Unassigned".
  targetSiteId?: bigint;
  signal?: AbortSignal;
  onSuccess?: (reassignedRackCount: bigint, reassignedDeviceCount: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

interface AssignRacksToSiteProps {
  // Bulk-friendly. Pass a single-element array for the singular case.
  rackIds: bigint[];
  // Unset moves the racks to "Unassigned".
  targetSiteId?: bigint;
  signal?: AbortSignal;
  // onSuccess args: device cascade count, count of racks whose
  // building was auto-cleared because the move crossed sites.
  // TODO(issue-420 follow-up): consumers must surface clearedBuildingCount
  // to the operator (toast or modal) — buildings belong to a single site,
  // so crossing sites silently clears the rack's building. No UI consumer
  // of this RPC exists yet; when one is wired, push a toast on
  // clearedBuildingCount > 0 directing the operator to reassign.
  onSuccess?: (reassignedDeviceCount: bigint, clearedBuildingCount: bigint) => void;
  onError?: (message: string) => void;
  onFinally?: () => void;
}

const useSites = () => {
  const { handleAuthErrors } = useAuthErrors();

  const listSites = useCallback(
    async ({ signal, errorComponentTypes, telemetryRanges, onSuccess, onError, onFinally }: ListSitesProps = {}) => {
      try {
        const response = await sitesClient.listSites(
          {
            errorComponentTypes: errorComponentTypes ?? [],
            telemetryRanges: telemetryRanges ?? [],
          },
          { signal },
        );
        if (signal?.aborted) return;
        onSuccess?.(response.sites);
      } catch (err) {
        if (signal?.aborted) return;
        handleAuthErrors({
          error: err,
          onError: (error) => {
            const code = error instanceof ConnectError ? error.code : undefined;
            onError?.(getErrorMessage(error), code);
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
            locationCity: values.locationCity,
            locationState: values.locationState,
            timezone: values.timezone,
            powerCapacityMw: values.powerCapacityMw,
            networkConfig: values.networkConfig,
            address: values.address,
            postalCode: values.postalCode,
            country: values.country,
            notes: values.notes,
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
            locationCity: values.locationCity,
            locationState: values.locationState,
            timezone: values.timezone,
            powerCapacityMw: values.powerCapacityMw,
            networkConfig: values.networkConfig,
            address: values.address,
            postalCode: values.postalCode,
            country: values.country,
            notes: values.notes,
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

  const assignDevicesToSite = useCallback(
    async ({
      targetSiteId,
      deviceIdentifiers,
      forceClearConflictingRackMembership,
      signal,
      onSuccess,
      onError,
      onFinally,
    }: AssignDevicesToSiteProps) => {
      try {
        const response = await sitesClient.assignDevicesToSite(
          {
            targetSiteId,
            deviceIdentifiers,
            forceClearConflictingRackMembership,
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

  const assignBuildingsToSite = useCallback(
    async ({ buildingIds, targetSiteId, signal, onSuccess, onError, onFinally }: AssignBuildingsToSiteProps) => {
      try {
        const response = await sitesClient.assignBuildingsToSite(
          {
            buildingIds,
            targetSiteId,
          },
          { signal },
        );
        if (signal?.aborted) return;
        onSuccess?.(response.reassignedRackCount, response.reassignedDeviceCount);
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

  const assignRacksToSite = useCallback(
    async ({ rackIds, targetSiteId, signal, onSuccess, onError, onFinally }: AssignRacksToSiteProps) => {
      try {
        const response = await sitesClient.assignRacksToSite(
          {
            rackIds,
            targetSiteId,
          },
          { signal },
        );
        if (signal?.aborted) return;
        onSuccess?.(response.reassignedDeviceCount, response.clearedBuildingCount);
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

  return {
    listSites,
    createSite,
    updateSite,
    deleteSite,
    assignDevicesToSite,
    assignBuildingsToSite,
    assignRacksToSite,
  };
};

export { useSites };
