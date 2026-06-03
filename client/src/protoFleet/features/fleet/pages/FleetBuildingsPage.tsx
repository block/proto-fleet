import { useCallback, useEffect, useMemo, useState } from "react";

import BuildingsListTable from "../components/BuildingsListTable";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

// Buildings tab content for `/fleet/buildings`. List of every building
// across the org with its parent site. Row click navigates to
// /buildings/:id. The "Add building" CTA + per-row ellipsis menu actions
// land in PR 3 alongside the BuildingDetailsModal site-picker field —
// see plan J3 / J10.
const FleetBuildingsPage = () => {
  const { listAllBuildings } = useBuildings();
  const { listSites } = useSites();
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);

  const fetchAll = useCallback(() => {
    const controller = new AbortController();
    void listAllBuildings({
      signal: controller.signal,
      onSuccess: (rows) => {
        setBuildings(rows);
        setError(null);
      },
      onError: (msg) => {
        setError(msg);
        setBuildings([]);
      },
    });
    // Sites are joined client-side to render the site column. Errors here
    // collapse to an empty site map — the page still renders, just with
    // "—" in the Site column.
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => setSites(rows),
      onError: () => setSites([]),
    });
    return () => controller.abort();
  }, [listAllBuildings, listSites]);

  useEffect(() => fetchAll(), [fetchAll]);

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });

  // SitePicker filter: All Sites = all rows; specific site = rows whose
  // building.siteId matches; Unassigned = rows whose building.siteId is
  // 0n/unset. Buildings without a site are rare but the schema allows it
  // (placeholder buildings created ahead of site assignment, or buildings
  // whose site has been deleted).
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

  if (error) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-buildings-error">
        <Header title="Couldn't load buildings" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{error}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={fetchAll}
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
    // Org has buildings but none match the picker selection. Don't fall back
    // to the bare "No buildings yet" empty state — the operator needs to know
    // the filter is hiding rows, not that the org is empty.
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
      <BuildingsListTable buildings={visibleBuildings} sites={sites} />
    </div>
  );
};

export default FleetBuildingsPage;
