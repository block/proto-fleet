import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "react-router-dom";

import BuildingPageHeader from "../components/BuildingPageHeader";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import PlaceholderBlock from "@/protoFleet/features/sites/components/PlaceholderBlock";
import Header from "@/shared/components/Header";

// `/buildings/:id` page shell. The header + action buttons are real; the
// metrics row, diagnostics section, and performance section are placeholders
// pending #264. BuildingService has no GetBuilding RPC yet, so the page
// reads `?site=<id>` from the query string and locates the building via
// ListBuildings against the parent site. Without a site param we render
// an empty state with guidance — a follow-up should add GetBuilding so
// the page works on a bare URL.
const BuildingPage = () => {
  const { id } = useParams<{ id: string }>();
  const [searchParams] = useSearchParams();
  const siteParam = searchParams.get("site");
  const { listBuildingsBySite } = useBuildings();
  const [building, setBuilding] = useState<BuildingWithCounts | null | undefined>(undefined);

  // Parse the parent site id from the query param. A missing or malformed
  // value short-circuits the load below and the render falls through to the
  // "not found" empty state.
  const parsedSiteId: bigint | null = (() => {
    if (!siteParam) return null;
    try {
      return BigInt(siteParam);
    } catch {
      return null;
    }
  })();

  useEffect(() => {
    if (!id || parsedSiteId === null) return;
    void listBuildingsBySite({
      siteId: parsedSiteId,
      onSuccess: (rows) => {
        const match = rows.find((r) => (r.building?.id ?? 0n).toString() === id);
        setBuilding(match ?? null);
      },
      onError: () => setBuilding(null),
    });
  }, [listBuildingsBySite, id, parsedSiteId]);

  // When the URL is missing or malformed, fall through to "not found" without
  // ever entering the loading state.
  const effectiveBuilding = !id || parsedSiteId === null ? null : building;

  if (effectiveBuilding === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (!effectiveBuilding || !id) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="building-page-not-found">
        <Header title="Building not found" titleSize="text-heading-300" />
        <p className="text-300 text-text-primary-70">
          Either the building has been deleted, or this URL is missing the parent <code>?site=&lt;id&gt;</code> query
          parameter. Navigate from <code>/sites</code> to reach a building.
        </p>
      </div>
    );
  }

  const label = effectiveBuilding.building?.name ?? "(unnamed building)";

  return (
    <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="building-page">
      <BuildingPageHeader label={label} buildingId={id} />
      <PlaceholderBlock label="Metrics row (Hashrate, Power, Efficiency, Miners online) — #264" className="h-20" />
      <PlaceholderBlock label="Diagnostics (rack grid + health) — #264" className="h-64" />
      <PlaceholderBlock label="Performance — #264" className="h-64" />
    </div>
  );
};

export default BuildingPage;
