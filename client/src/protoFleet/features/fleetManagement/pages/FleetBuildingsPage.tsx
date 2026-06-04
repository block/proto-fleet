import { useCallback, useMemo, useState } from "react";

import BuildingList from "../components/BuildingList";
import { useFleetOutletContext } from "../components/FleetLayout";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
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

  if (buildings === undefined || sites === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (buildingsError && buildings.length === 0) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-buildings-error">
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

  if (buildings.length === 0) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-buildings-page">
        <div className="flex flex-col items-start gap-3 rounded-xl border border-dashed border-border-5 p-6">
          <Header title="No buildings yet" titleSize="text-heading-200" />
          <p className="text-300 text-text-primary-70">
            Add a building from a site detail page to start organizing racks. Building creation from this tab lands in a
            follow-up release.
          </p>
        </div>
      </div>
    );
  }

  if (visibleBuildings.length === 0) {
    const message =
      activeSite.kind === "unassigned"
        ? "No buildings without a site. Switch the picker to All Sites to see every building."
        : "No buildings in this site yet.";
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-buildings-page">
        <div
          className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
          data-testid="fleet-buildings-filter-empty"
        >
          {message}
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-buildings-page">
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
      <BuildingList buildings={visibleBuildings} sites={sites} />
    </div>
  );
};

export default FleetBuildingsPage;
