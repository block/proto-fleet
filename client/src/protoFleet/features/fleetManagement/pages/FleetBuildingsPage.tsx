import { useCallback, useMemo, useState } from "react";

import BuildingList from "../components/BuildingList";
import FilterRow from "../components/FilterRow";
import { useFleetOutletContext } from "../components/FleetLayout";
import SiteSelectModal from "../components/SiteSelectModal";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import { usePoll } from "@/shared/hooks/usePoll";

const LIST_WRAPPER = "pt-6";

const FleetBuildingsPage = () => {
  const { sites, sitesError, refetchSites } = useFleetOutletContext();

  const { listAllBuildings } = useBuildings();
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  const [buildingsError, setBuildingsError] = useState<string | null>(null);

  // Returning the promise lets usePoll schedule the next tick from response
  // completion (not from request start) so slow responses can't overlap.
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

  // Skip the picker when the target is unambiguous: a pinned single-site
  // selection or an org with exactly one site.
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
  // CreateBuilding requires site:manage server-side.
  const canManageBuildings = useHasPermission("site:manage");

  if (buildings === undefined || sites === undefined) {
    return (
      <FilterRow>
        <div className="text-300 text-text-primary-70">Loading…</div>
      </FilterRow>
    );
  }

  if (buildingsError && buildings.length === 0) {
    return (
      <FilterRow testId="fleet-buildings-error">
        <Header title="Couldn't load buildings" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{buildingsError}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={fetchBuildings}
          testId="fleet-buildings-retry"
        />
      </FilterRow>
    );
  }

  const addBuildingButton = canManageBuildings ? (
    <Button
      variant={variants.secondary}
      size={sizes.compact}
      text="Add building"
      onClick={handleAddBuilding}
      disabled={!hasSites}
      testId="fleet-buildings-add"
    />
  ) : null;

  if (buildings.length === 0) {
    return (
      <>
        <FilterRow testId="fleet-buildings-page">
          <div className="flex items-center justify-end">{addBuildingButton}</div>
          <div className="flex flex-col items-start gap-3 rounded-xl border border-dashed border-border-5 p-6">
            <Header title="No buildings yet" titleSize="text-heading-200" />
            <p className="text-300 text-text-primary-70">
              {!canManageBuildings
                ? "No buildings have been added to this fleet yet."
                : hasSites
                  ? "Add a building to start organizing racks."
                  : "Create a site first, then add buildings to organize racks."}
            </p>
          </div>
        </FilterRow>
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
        <FilterRow testId="fleet-buildings-page">
          <div className="flex items-center justify-end">{addBuildingButton}</div>
          <div
            className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
            data-testid="fleet-buildings-filter-empty"
          >
            {message}
          </div>
        </FilterRow>
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
      <FilterRow testId="fleet-buildings-page">
        {sitesError ? (
          <Callout
            intent="danger"
            prefixIcon={<Alert />}
            title="Couldn't load sites for the Site column"
            subtitle={sitesError}
            buttonText="Retry"
            buttonOnClick={refetchSites}
            testId="fleet-buildings-sites-error"
          />
        ) : null}
        {buildingsError ? (
          <Callout
            intent="danger"
            prefixIcon={<Alert />}
            title="Couldn't refresh buildings"
            subtitle={buildingsError}
            buttonText="Retry"
            buttonOnClick={fetchBuildings}
            testId="fleet-buildings-inline-error"
          />
        ) : null}
        <div className="flex items-center justify-end">{addBuildingButton}</div>
      </FilterRow>
      <div className={LIST_WRAPPER}>
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
