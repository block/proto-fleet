import { useCallback, useEffect, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";

import { useBuildings } from "@/protoFleet/api/buildings";
import type { BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import useInfrastructureDevices from "@/protoFleet/api/useInfrastructureDevices";
import { siteFilterFromActive, useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { useOptionalFleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout/outletContext";
import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import {
  uniqueInfraBuildingOptions,
  uniqueSortedLocationNames,
} from "@/protoFleet/features/infrastructure/locationOptions";
import type { InfraDeviceDraft, InfraDeviceItem, InfraDevicePatch } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

interface FleetInfraPageProps {
  // Test/story override: when provided, the API hook is disabled and
  // the given devices render with no-op mutations.
  devices?: InfraDeviceItem[];
  canRead?: boolean;
  canManage?: boolean;
}

const NO_DEVICES: InfraDeviceItem[] = [];

const FleetInfraPage = ({ devices: devicesOverride, canRead, canManage }: FleetInfraPageProps) => {
  const canReadSites = useHasPermission("site:read");
  const canManageSites = useHasPermission("site:manage");
  const fleetContext = useOptionalFleetOutletContext();
  const { listAllBuildings } = useBuildings();
  const [buildingCatalog, setBuildingCatalog] = useState<BuildingWithCounts[] | undefined>();
  const canReadInfrastructure = canRead ?? canReadSites;
  const canManageInfrastructure = canManage ?? canManageSites;
  const sites = fleetContext?.sites;
  const hasDevicesOverride = devicesOverride !== undefined;
  // Validate scope against catalog access (authoritative now), not sitesLoaded:
  // a mid-session PermissionDenied clears `sites` to [] with sitesLoaded still
  // true, which would otherwise strip a reachable scoped route.
  const siteCatalogAccessGranted = fleetContext?.siteCatalogAccessGranted ?? false;
  const knownSiteIds = useMemo(
    () => (siteCatalogAccessGranted ? buildKnownSiteIds(sites) : undefined),
    [siteCatalogAccessGranted, sites],
  );
  const { activeSite } = useActiveSite({ knownSiteIds });
  // Scope the list to the topbar site selection. Infra devices are
  // always site-assigned, so the "unassigned" scope matches nothing:
  // skip the fetch and render an empty list.
  const scopeFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite]);
  const isUnassignedScope = scopeFilter.includeUnassigned;
  const {
    devices: apiDevices,
    isLoading,
    loadError,
    updatingDeviceIds,
    listDevices,
    createDevice,
    updateDevice,
    setDeviceEnabled,
    deleteDevice,
  } = useInfrastructureDevices(canReadInfrastructure && !hasDevicesOverride && !isUnassignedScope, scopeFilter.siteIds);
  // A scoped page must not create devices under (or move devices to)
  // another site: the forms only offer in-scope sites, and resolveSiteId
  // enforces the same boundary for anything that slips past the picker.
  // The unassigned scope matches no site at all (infra devices are
  // always site-assigned), so writes are match-none there — otherwise a
  // create from that empty view would land under some site and
  // immediately vanish. null means unscoped (full catalog).
  const scopedSiteIdSet = useMemo(() => {
    if (scopeFilter.includeUnassigned) return new Set<string>();
    return scopeFilter.siteIds.length > 0 ? new Set(scopeFilter.siteIds.map(String)) : null;
  }, [scopeFilter]);
  const catalogSiteOptions = useMemo(() => {
    if (!sites) return undefined;
    const inScopeSites = scopedSiteIdSet
      ? sites.filter((siteWithCounts) => {
          const site = siteWithCounts.site;
          return site !== undefined && scopedSiteIdSet.has(site.id.toString());
        })
      : sites;
    return uniqueSortedLocationNames(inScopeSites.map((siteWithCounts) => siteWithCounts.site?.name ?? ""));
  }, [scopedSiteIdSet, sites]);
  const siteNameById = useMemo(() => {
    const next = new Map<string, string>();
    for (const siteWithCounts of sites ?? []) {
      const site = siteWithCounts.site;
      if (site) {
        next.set(site.id.toString(), site.name);
      }
    }
    return next;
  }, [sites]);
  // The add/edit forms select sites by catalog name; the API needs IDs.
  const siteIdByName = useMemo(() => {
    const next = new Map<string, string>();
    for (const [siteId, siteName] of siteNameById) {
      next.set(siteName, siteId);
    }
    return next;
  }, [siteNameById]);
  const selectedSiteName = useMemo(
    () => (activeSite.kind === "site" ? siteNameById.get(activeSite.id) : undefined),
    [activeSite, siteNameById],
  );
  const catalogBuildingOptions = useMemo(() => {
    if (!buildingCatalog) return undefined;
    return uniqueInfraBuildingOptions(
      buildingCatalog.flatMap((buildingWithCounts) => {
        const building = buildingWithCounts.building;
        if (!building?.siteId) return [];
        const siteName = siteNameById.get(building.siteId.toString());
        if (!siteName) return [];
        return [{ siteName, buildingName: building.name }];
      }),
    );
  }, [buildingCatalog, siteNameById]);

  useEffect(() => {
    if (!canReadInfrastructure) {
      return;
    }

    const controller = new AbortController();
    void listAllBuildings({
      signal: controller.signal,
      onSuccess: setBuildingCatalog,
      onError: () => setBuildingCatalog(undefined),
    });

    return () => controller.abort();
  }, [canReadInfrastructure, listAllBuildings]);

  const resolveSiteId = useCallback(
    (siteName: string): string => {
      const siteId = siteIdByName.get(siteName);
      if (!siteId) {
        throw new Error("Select a site from the catalog.");
      }
      if (scopedSiteIdSet && !scopedSiteIdSet.has(siteId)) {
        throw new Error("Select a site within the current site scope.");
      }
      return siteId;
    },
    [scopedSiteIdSet, siteIdByName],
  );

  const handleCreateDevice = useCallback(
    async (draft: InfraDeviceDraft) => {
      await createDevice({
        siteId: resolveSiteId(draft.siteName),
        buildingName: draft.buildingName,
        name: draft.name,
        deviceKind: draft.deviceKind,
        fanCount: draft.fanCount,
        driverType: draft.driverType,
        driverConfig: draft.driverConfig,
      });
    },
    [createDevice, resolveSiteId],
  );

  const handleUpdateDevice = useCallback(
    async (patch: InfraDevicePatch) => {
      await updateDevice({
        id: patch.id,
        // Only an operator-picked site change resolves through the
        // catalog (and gets scope-validated); an unchanged save reuses
        // the row's stored siteId, so it can't fail on a renamed site
        // or an unavailable site catalog.
        ...(patch.siteName !== undefined ? { siteId: resolveSiteId(patch.siteName) } : {}),
        buildingName: patch.buildingName,
        name: patch.name,
        enabled: patch.enabled,
        driverConfig: patch.driverConfig,
      });
    },
    [resolveSiteId, updateDevice],
  );

  const handleSetDeviceEnabled = useCallback(
    async (device: InfraDeviceItem, enabled: boolean) => {
      await setDeviceEnabled(device, enabled);
    },
    [setDeviceEnabled],
  );

  const handleDeleteDevice = useCallback(
    async (deviceId: string) => {
      await deleteDevice(deviceId);
    },
    [deleteDevice],
  );

  const handleRetry = useCallback(() => {
    void listDevices().catch(() => {});
  }, [listDevices]);

  if (!canReadInfrastructure) {
    return <Navigate to="/fleet" replace />;
  }

  // The unassigned scope can't contain infra devices, so hide the
  // management surface there (its only visible control would be an
  // Add device button whose result could never render in this view).
  const canManageInScope = canManageInfrastructure && !isUnassignedScope;

  return (
    <InfraDeviceList
      // The hook keeps its last result while disabled, so the unassigned
      // scope must not leak the previous scope's devices.
      devices={devicesOverride ?? (isUnassignedScope ? NO_DEVICES : apiDevices)}
      isLoading={hasDevicesOverride ? false : isLoading}
      loadError={hasDevicesOverride || isUnassignedScope ? null : loadError}
      onRetry={handleRetry}
      canManage={canManageInScope}
      siteOptions={catalogSiteOptions}
      buildingOptions={catalogBuildingOptions}
      initialSiteName={selectedSiteName}
      updatingDeviceIds={updatingDeviceIds}
      {...(hasDevicesOverride
        ? {}
        : {
            onCreateDevice: handleCreateDevice,
            onUpdateDevice: handleUpdateDevice,
            onDeleteDevice: handleDeleteDevice,
            onSetDeviceEnabled: handleSetDeviceEnabled,
          })}
    />
  );
};

export default FleetInfraPage;
