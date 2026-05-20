import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";

import BuildingPageHeader from "../components/BuildingPageHeader";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type Building } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { parseBigIntId } from "@/protoFleet/api/sites";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

// `/buildings/:id` page shell. The header + action buttons are real; the
// metrics row, diagnostics section, and performance section are placeholders
// pending #264. Building data comes from the GetBuilding RPC keyed by the
// URL `:id` segment — no parent site_id required.
const BuildingPage = () => {
  const { id } = useParams<{ id: string }>();
  const { getBuilding } = useBuildings();

  const buildingId = useMemo(() => parseBigIntId(id), [id]);

  // Pair the in-flight building id with the response so rapid navigation
  // (back/forward between two building URLs) doesn't render the older
  // response against the newer URL while the new request is in flight.
  const [response, setResponse] = useState<{ id: bigint; building: Building | null } | undefined>(undefined);

  useEffect(() => {
    if (buildingId === null) return;
    const controller = new AbortController();
    void getBuilding({
      id: buildingId,
      signal: controller.signal,
      onSuccess: (b) => setResponse({ id: buildingId, building: b ?? null }),
      onError: () => setResponse({ id: buildingId, building: null }),
    });
    return () => controller.abort();
  }, [getBuilding, buildingId]);

  const effectiveBuilding =
    buildingId === null ? null : response && response.id === buildingId ? response.building : undefined;

  if (effectiveBuilding === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (!effectiveBuilding) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="building-page-not-found">
        <Header title="Building not found" titleSize="text-heading-300" />
        <p className="text-300 text-text-primary-70">
          Either the building has been deleted or the URL is invalid. Return to <Link to="/sites">/sites</Link> to find
          your building.
        </p>
      </div>
    );
  }

  const label = effectiveBuilding.name || "(unnamed building)";
  const idForHeader = effectiveBuilding.id.toString();

  return (
    <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="building-page">
      <BuildingPageHeader label={label} buildingId={idForHeader} />
      <PlaceholderBlock label="Metrics row (Hashrate, Power, Efficiency, Miners online) — #264" className="h-20" />
      <PlaceholderBlock label="Diagnostics (rack grid + health) — #264" className="h-64" />
      <PlaceholderBlock label="Performance — #264" className="h-64" />
    </div>
  );
};

export default BuildingPage;
