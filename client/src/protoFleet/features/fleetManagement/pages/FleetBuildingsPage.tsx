import { useCallback, useMemo, useState } from "react";

import BuildingList from "../components/BuildingList";
import { useFleetOutletContext } from "../components/FleetLayout";
import SiteSelectModal from "../components/SiteSelectModal";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import { usePoll } from "@/shared/hooks/usePoll";

const FleetBuildingsPage = () => {
  const { sites, sitesError, refetchSites } = useFleetOutletContext();

  const { listAllBuildings } = useBuildings();
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  const [buildingsError, setBuildingsError] = useState<string | null>(null);

  // Returning the promise lets usePoll schedule the next tick from response
  // completion — matches the legacy /sites overview polling cadence.
  const fetchBuildings = useCallback(
    () =>
      listAllBuildings({
        onSuccess: (rows) => {
          setBuildings(rows);
          setBuildingsError(null);
        },
        onError: (msg) => {
          setBuildingsError(msg);
          // Preserve last-good list across transient errors; only fall to []
          // on the initial-load failure path.
          setBuildings((prev) => prev ?? []);
        },
      }),
    [listAllBuildings],
  );

  usePoll({ fetchData: fetchBuildings, poll: true, pollIntervalMs: POLL_INTERVAL_MS });

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });

  const visibleBuildings = useMemo(() => {
    if (!buildings) return [];
    if (activeSite.kind === "all") return buildings;
    if (activeSite.kind === "unassigned") {
      return buildings.filter((b) => !b.building?.siteId || b.building.siteId === 0n);
    }
    return buildings.filter((b) => (b.building?.siteId ?? 0n).toString() === activeSite.id);
  }, [buildings, activeSite]);

  const buildingModals = useBuildingModals({ refetchBuildings: fetchBuildings });
  const [showSiteSelect, setShowSiteSelect] = useState(false);

  // Picks a site for the new building when the user clicks Add building.
  // The picker has three branches:
  //   1. SitePicker is pinned to a single existing site → use it directly.
  //   2. Org has exactly one site → use it directly (no point asking).
  //   3. Otherwise → open SiteSelectModal so the operator picks which site.
  // Once #371 ships a Site dropdown inside BuildingSettingsModal this layer
  // collapses to a single openDetailsCreate(undefined) call.
  const handleAddBuilding = useCallback(() => {
    const validSites = sites?.filter((s) => s.site !== undefined) ?? [];
    if (validSites.length === 0) return;
    if (activeSite.kind === "site") {
      const match = validSites.find((s) => s.site!.id.toString() === activeSite.id);
      if (match) {
        buildingModals.openDetailsCreate(match.site!.id, match.site!.name);
        return;
      }
    }
    if (validSites.length === 1) {
      const only = validSites[0]!;
      buildingModals.openDetailsCreate(only.site!.id, only.site!.name);
      return;
    }
    setShowSiteSelect(true);
  }, [sites, activeSite, buildingModals]);

  const handleSiteSelected = useCallback(
    (siteId: bigint, siteName: string) => {
      setShowSiteSelect(false);
      buildingModals.openDetailsCreate(siteId, siteName);
    },
    [buildingModals],
  );

  const hasSites = (sites?.filter((s) => s.site !== undefined).length ?? 0) > 0;

  if (buildings === undefined || sites === undefined) {
    return (
      <div className="flex flex-col gap-6 px-6 pt-6 laptop:px-10 laptop:pt-10">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (buildingsError && buildings.length === 0) {
    return (
      <div className="flex flex-col gap-6 px-6 pt-6 laptop:px-10 laptop:pt-10" data-testid="fleet-buildings-error">
        <Header title="Couldn't load buildings" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{buildingsError}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={fetchBuildings}
          testId="fleet-buildings-retry"
        />
      </div>
    );
  }

  // Add-building action button — shared between the empty state, the
  // filter-empty state, and the populated list. Disabled when no sites
  // exist since every building requires a parent site.
  const addBuildingButton = (
    <Button
      variant={variants.primary}
      size={sizes.compact}
      text="Add building"
      onClick={handleAddBuilding}
      disabled={!hasSites}
      testId="fleet-buildings-add"
    />
  );

  if (buildings.length === 0) {
    return (
      <>
        <div className="flex flex-col gap-6 px-6 pt-6 laptop:px-10 laptop:pt-10" data-testid="fleet-buildings-page">
          <div className="flex items-center justify-end">{addBuildingButton}</div>
          <div className="flex flex-col items-start gap-3 rounded-xl border border-dashed border-border-5 p-6">
            <Header title="No buildings yet" titleSize="text-heading-200" />
            <p className="text-300 text-text-primary-70">
              {hasSites
                ? "Add a building to start organizing racks."
                : "Create a site first, then add buildings to organize racks."}
            </p>
          </div>
        </div>
        <BuildingModals modals={buildingModals} />
        <SiteSelectModal
          open={showSiteSelect}
          sites={sites}
          onSelect={handleSiteSelected}
          onDismiss={() => setShowSiteSelect(false)}
        />
      </>
    );
  }

  if (visibleBuildings.length === 0) {
    const message =
      activeSite.kind === "unassigned"
        ? "No buildings without a site. Switch the picker to All Sites to see every building."
        : "No buildings in this site yet.";
    return (
      <>
        <div className="flex flex-col gap-6 px-6 pt-6 laptop:px-10 laptop:pt-10" data-testid="fleet-buildings-page">
          <div className="flex items-center justify-end">{addBuildingButton}</div>
          <div
            className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
            data-testid="fleet-buildings-filter-empty"
          >
            {message}
          </div>
        </div>
        <BuildingModals modals={buildingModals} />
        <SiteSelectModal
          open={showSiteSelect}
          sites={sites}
          onSelect={handleSiteSelected}
          onDismiss={() => setShowSiteSelect(false)}
        />
      </>
    );
  }

  return (
    <>
      <div className="flex flex-col gap-6 px-6 pt-6 laptop:px-10 laptop:pt-10" data-testid="fleet-buildings-page">
        {sitesError ? (
          <div
            className="flex items-center justify-between rounded-xl border border-border-5 p-4"
            data-testid="fleet-buildings-sites-error"
          >
            <span className="text-300 text-text-primary-70">
              Couldn&apos;t load sites for the Site column: {sitesError}
            </span>
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Retry"
              onClick={refetchSites}
              testId="fleet-buildings-sites-retry"
            />
          </div>
        ) : null}
        {buildingsError ? (
          <div
            className="flex items-center justify-between rounded-xl border border-border-5 p-4"
            data-testid="fleet-buildings-inline-error"
          >
            <span className="text-300 text-text-primary-70">Couldn&apos;t refresh buildings: {buildingsError}</span>
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Retry"
              onClick={fetchBuildings}
              testId="fleet-buildings-inline-retry"
            />
          </div>
        ) : null}
        <div className="flex items-center justify-end">{addBuildingButton}</div>
        <BuildingList buildings={visibleBuildings} sites={sites} />
      </div>
      <BuildingModals modals={buildingModals} />
      <SiteSelectModal
        open={showSiteSelect}
        sites={sites}
        onSelect={handleSiteSelected}
        onDismiss={() => setShowSiteSelect(false)}
      />
    </>
  );
};

export default FleetBuildingsPage;
