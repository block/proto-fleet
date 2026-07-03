import { useEffect, useMemo, useState } from "react";
import { Navigate } from "react-router-dom";

import { useBuildings } from "@/protoFleet/api/buildings";
import type { BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { useOptionalFleetOutletContext } from "@/protoFleet/features/fleetManagement/components/FleetLayout/outletContext";
import InfraDeviceList from "@/protoFleet/features/infrastructure/components/InfraDeviceList";
import {
  uniqueInfraBuildingOptions,
  uniqueSortedLocationNames,
} from "@/protoFleet/features/infrastructure/locationOptions";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useHasPermission } from "@/protoFleet/store";

const EMPTY_DEVICES: InfraDeviceItem[] = [];

interface FleetInfraPageProps {
  devices?: InfraDeviceItem[];
  canRead?: boolean;
  canManage?: boolean;
}

const FleetInfraPage = ({ devices = EMPTY_DEVICES, canRead, canManage }: FleetInfraPageProps) => {
  const canReadSites = useHasPermission("site:read");
  const canManageSites = useHasPermission("site:manage");
  const fleetContext = useOptionalFleetOutletContext();
  const { listAllBuildings } = useBuildings();
  const [buildingCatalog, setBuildingCatalog] = useState<BuildingWithCounts[] | undefined>();
  const canReadInfrastructure = canRead ?? canReadSites;
  const canManageInfrastructure = canManage ?? canManageSites;
  const sites = fleetContext?.sites;
  const sitesLoaded = fleetContext?.sitesLoaded ?? false;
  const knownSiteIds = useMemo(() => (sitesLoaded ? buildKnownSiteIds(sites) : undefined), [sites, sitesLoaded]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  const catalogSiteOptions = useMemo(() => {
    if (!sites) return undefined;
    return uniqueSortedLocationNames(sites.map((siteWithCounts) => siteWithCounts.site?.name ?? ""));
  }, [sites]);
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

  if (!canReadInfrastructure) {
    return <Navigate to="/fleet" replace />;
  }

  return (
    <InfraDeviceList
      devices={devices}
      canManage={canManageInfrastructure}
      siteOptions={catalogSiteOptions}
      buildingOptions={catalogBuildingOptions}
      initialSiteName={selectedSiteName}
    />
  );
};

export default FleetInfraPage;
