import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";

import BuildingPageHeader from "../components/BuildingPageHeader";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type Building } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { parseBigIntId } from "@/protoFleet/api/sites";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

// `/buildings/:id` page shell. The header + action buttons are real; the
// metrics row, diagnostics section, and performance section are placeholders
// pending #264. Building data comes from the GetBuilding RPC keyed by the
// URL `:id` segment — no parent site_id required.
//
// Response state distinguishes three outcomes so the UI can render each
// honestly: NotFound (server confirmed the id doesn't exist), error (any
// other failure — permission denied, network, 5xx), and success. Lumping
// all failures into "not found" would mask real outages.
type FetchOutcome =
  | { status: "found"; building: Building }
  | { status: "notFound" }
  | { status: "error"; message: string };

const BuildingPage = () => {
  const { id } = useParams<{ id: string }>();
  const { getBuilding } = useBuildings();

  const buildingId = useMemo(() => parseBigIntId(id), [id]);

  // Pair the in-flight building id with the response so rapid navigation
  // (back/forward between two building URLs) doesn't render the older
  // response against the newer URL while the new request is in flight.
  const [response, setResponse] = useState<{ id: bigint; outcome: FetchOutcome } | undefined>(undefined);

  const fetchBuilding = useCallback(
    (targetId: bigint) => {
      const controller = new AbortController();
      void getBuilding({
        id: targetId,
        signal: controller.signal,
        onSuccess: (b) =>
          setResponse({
            id: targetId,
            outcome: b ? { status: "found", building: b } : { status: "notFound" },
          }),
        onError: (message) => setResponse({ id: targetId, outcome: { status: "error", message } }),
      });
      return () => controller.abort();
    },
    [getBuilding],
  );

  useEffect(() => {
    if (buildingId === null) return;
    return fetchBuilding(buildingId);
  }, [fetchBuilding, buildingId]);

  const effectiveOutcome: FetchOutcome | "loading" | "invalid" =
    buildingId === null ? "invalid" : response && response.id === buildingId ? response.outcome : "loading";

  if (effectiveOutcome === "loading") {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (effectiveOutcome === "invalid" || effectiveOutcome.status === "notFound") {
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

  if (effectiveOutcome.status === "error") {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="building-page-error">
        <Header title="Couldn't load building" titleSize="text-heading-300" />
        <p className="text-300 text-text-primary-70">{effectiveOutcome.message}</p>
        <div>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Retry"
            onClick={() => {
              if (buildingId !== null) fetchBuilding(buildingId);
            }}
            testId="building-page-retry"
          />
        </div>
      </div>
    );
  }

  const effectiveBuilding = effectiveOutcome.building;

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
