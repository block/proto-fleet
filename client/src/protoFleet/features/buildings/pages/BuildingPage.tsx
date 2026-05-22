import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";

import BuildingModals from "../components/BuildingModals";
import BuildingPageHeader from "../components/BuildingPageHeader";
import { useBuildingModals } from "../hooks/useBuildingModals";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type Building, BuildingWithCountsSchema } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
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
  const navigate = useNavigate();
  const { getBuilding } = useBuildings();

  const buildingId = useMemo(() => parseBigIntId(id), [id]);

  // Pair the in-flight building id with the response so rapid navigation
  // (back/forward between two building URLs) doesn't render the older
  // response against the newer URL while the new request is in flight.
  const [response, setResponse] = useState<{ id: bigint; outcome: FetchOutcome } | undefined>(undefined);

  // Hold the latest in-flight AbortController in a ref so retries (and rapid
  // re-mounts) abort the previous request before issuing a new one. Without
  // this, two retry clicks would race and the later result could be
  // overwritten by the earlier one resolving last.
  const inflightControllerRef = useRef<AbortController | null>(null);

  const fetchBuilding = useCallback(
    (targetId: bigint) => {
      inflightControllerRef.current?.abort();
      const controller = new AbortController();
      inflightControllerRef.current = controller;
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
    },
    [getBuilding],
  );

  useEffect(() => {
    if (buildingId === null) return;
    fetchBuilding(buildingId);
  }, [fetchBuilding, buildingId]);

  // Mount BuildingModals at the page level so the manage flow can stack
  // BuildingDetailsModal on top without re-rendering the page shell. The
  // delete-from-manage rule (per plan PR 3) redirects to /sites — the
  // manage modal's anchor is the now-deleted building so we can't stay.
  const buildingModals = useBuildingModals({
    refetchBuildings: () => {
      if (buildingId !== null) fetchBuilding(buildingId);
    },
    onDeleteFromManage: () => navigate("/sites"),
  });

  // Unmount cleanup aborts whatever's currently in flight — including
  // retry-spawned controllers that didn't come from the effect above.
  // Without this, clicking Retry then navigating away leaks a request
  // whose onSuccess/onError still fires setResponse on the unmounted page.
  useEffect(() => {
    return () => {
      inflightControllerRef.current?.abort();
      inflightControllerRef.current = null;
    };
  }, []);

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
      <BuildingPageHeader
        label={label}
        buildingId={idForHeader}
        // Synthesize a BuildingWithCounts row for the modal hook. Real
        // rack_count surfaces with Phase 1b enrichment (#264); zero here
        // drives the cascade dialog into the simpler "Are you sure?" copy.
        onEditBuilding={() =>
          buildingModals.openManage(create(BuildingWithCountsSchema, { building: effectiveBuilding, rackCount: 0n }))
        }
      />
      <PlaceholderBlock label="Metrics row (Hashrate, Power, Efficiency, Miners online) — #264" className="h-20" />
      <PlaceholderBlock label="Diagnostics (rack grid + health) — #264" className="h-64" />
      <PlaceholderBlock label="Performance — #264" className="h-64" />
      <BuildingModals modals={buildingModals} />
    </div>
  );
};

export default BuildingPage;
