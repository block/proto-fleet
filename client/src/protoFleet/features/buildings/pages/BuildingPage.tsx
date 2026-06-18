import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { create } from "@bufbuild/protobuf";

import BuildingMetricsRow from "../components/BuildingMetricsRow";
import BuildingModals from "../components/BuildingModals";
import BuildingPageHeader from "../components/BuildingPageHeader";
import { BuildingRackGrid } from "../components/BuildingRackGrid";
import { useBuildingModals } from "../hooks/useBuildingModals";
import { useBuildings } from "@/protoFleet/api/buildings";
import {
  type Building,
  type BuildingWithCounts,
  BuildingWithCountsSchema,
} from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { parseBigIntId, useSites } from "@/protoFleet/api/sites";
import { useBuildingStats } from "@/protoFleet/api/useBuildingStats";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import { useDuration, useSetDuration } from "@/protoFleet/store";
import type { BreadcrumbSegment } from "@/shared/components/Breadcrumb";
import Button, { sizes, variants } from "@/shared/components/Button";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useStickyState } from "@/shared/hooks/useStickyState";

// Same measurement / aggregation slate the rack-overview page uses, so the
// performance charts render identically across both surfaces.
const ALL_MEASUREMENT_TYPES: MeasurementType[] = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
  MeasurementType.UPTIME,
];

const ALL_AGGREGATION_TYPES: AggregationType[] = [AggregationType.AVERAGE, AggregationType.MIN, AggregationType.MAX];

// `/buildings/:id` page shell. Mirrors RackOverviewPage: header, metric row,
// diagnostics (rack-health module FPO + component health), and performance
// charts. The diagnostics rack grid stays FPO pending #264; everything else
// is wired against GetBuildingStats + the same telemetry hooks the rack
// page uses.
//
// Response state distinguishes three outcomes so the UI can render each
// honestly: NotFound (server confirmed the id doesn't exist), error (any
// other failure — permission denied, network, 5xx), and success.
type FetchOutcome =
  | { status: "found"; building: Building }
  | { status: "notFound" }
  | { status: "error"; message: string };

const BuildingPage = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { getBuilding, listBuildingRacks } = useBuildings();

  const { listSites } = useSites();
  const { listBuildingsBySite } = useBuildings();

  const buildingId = useMemo(() => parseBigIntId(id), [id]);

  // Breadcrumb hierarchy: parent site + sibling buildings.
  const [parentSite, setParentSite] = useState<SiteWithCounts | undefined>(undefined);
  const [siblingBuildings, setSiblingBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);

  // Pair the in-flight building id with the response so rapid navigation
  // (back/forward between two building URLs) doesn't render the older
  // response against the newer URL while the new request is in flight.
  const [response, setResponse] = useState<{ id: bigint; outcome: FetchOutcome } | undefined>(undefined);
  // Parallel-fetched rack count keyed by buildingId so it can't race a
  // navigation. Used to populate the cascade-delete dialog's count copy.
  const [rackCountResponse, setRackCountResponse] = useState<{ id: bigint; count: bigint } | undefined>(undefined);
  const inflightControllerRef = useRef<AbortController | null>(null);
  const racksInflightRef = useRef<AbortController | null>(null);

  const fetchBuilding = useCallback(
    (targetId: bigint) => {
      inflightControllerRef.current?.abort();
      racksInflightRef.current?.abort();
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

      const racksController = new AbortController();
      racksInflightRef.current = racksController;
      void listBuildingRacks({
        buildingId: targetId,
        signal: racksController.signal,
        onSuccess: (racks) => setRackCountResponse({ id: targetId, count: BigInt(racks.length) }),
        onError: () => {
          // Leave rackCountResponse stale on error; cascade dialog falls
          // back to "Are you sure?" rather than blocking the page.
        },
      });
    },
    [getBuilding, listBuildingRacks],
  );

  useEffect(() => {
    if (buildingId === null) return;
    fetchBuilding(buildingId);
  }, [fetchBuilding, buildingId]);

  // Fetch parent site + sibling buildings for breadcrumb once building loads.
  const resolvedSiteId =
    response && buildingId !== null && response.id === buildingId && response.outcome.status === "found"
      ? response.outcome.building.siteId
      : undefined;

  useEffect(() => {
    if (!resolvedSiteId) return;
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (sites) => {
        const match = sites.find((s) => s.site?.id === resolvedSiteId);
        setParentSite(match);
      },
    });
    void listBuildingsBySite({
      siteId: resolvedSiteId,
      signal: controller.signal,
      onSuccess: (buildings) => setSiblingBuildings(buildings),
    });
    return () => controller.abort();
  }, [resolvedSiteId, listSites, listBuildingsBySite]);

  const buildingModals = useBuildingModals({
    refetchBuildings: () => {
      if (buildingId !== null) fetchBuilding(buildingId);
    },
    onDeleteFromManage: () => navigate("/sites"),
  });

  useEffect(() => {
    return () => {
      inflightControllerRef.current?.abort();
      inflightControllerRef.current = null;
      racksInflightRef.current?.abort();
      racksInflightRef.current = null;
    };
  }, []);

  // Server-rolled metrics for the header strip. The response now carries
  // device_identifiers, so telemetry + component-error consumers can scope
  // themselves directly without a second ListMinerStateSnapshots paginate.
  const {
    stats,
    error: statsError,
    hasLoaded: statsHasLoaded,
    refetch: refetchStats,
  } = useBuildingStats({
    buildingId: buildingId ?? 0n,
    enabled: buildingId !== null,
    pollIntervalMs: POLL_INTERVAL_MS,
  });
  // `undefined` while stats are loading (skeletons); `string[]` once the
  // response lands. Empty array = building genuinely has no members
  // (telemetry hooks then short-circuit to "no data").
  const memberDeviceIds: string[] | null = stats ? stats.deviceIdentifiers : null;

  const duration = useDuration();
  const setDuration = useSetDuration();
  const { refs } = useStickyState();

  const telemetryEnabled = memberDeviceIds !== null && memberDeviceIds.length > 0;
  const telemetryOptions = useMemo(
    () => ({
      deviceIds: memberDeviceIds ?? [],
      measurementTypes: ALL_MEASUREMENT_TYPES,
      aggregations: ALL_AGGREGATION_TYPES,
      duration,
      enabled: telemetryEnabled,
      pollIntervalMs: POLL_INTERVAL_MS,
    }),
    [memberDeviceIds, duration, telemetryEnabled],
  );
  const { data: telemetryData } = useTelemetryMetrics(telemetryOptions);
  // For empty buildings, surface a defined-but-empty metrics array so the
  // performance section renders "No data" instead of an indefinite
  // skeleton.
  const isEmptyBuilding = memberDeviceIds !== null && memberDeviceIds.length === 0;
  const metrics = isEmptyBuilding ? [] : telemetryData?.metrics;

  const effectiveOutcome: FetchOutcome | "loading" | "invalid" =
    buildingId === null ? "invalid" : response && response.id === buildingId ? response.outcome : "loading";

  // Breadcrumb segments must be computed before early returns to satisfy
  // the rules-of-hooks constraint. The values are only meaningful when
  // effectiveOutcome is "found", but the memo itself is always called.
  const breadcrumbSegments: BreadcrumbSegment[] = useMemo(() => {
    if (typeof effectiveOutcome === "string" || effectiveOutcome.status !== "found") return [];
    const building = effectiveOutcome.building;
    const segments: BreadcrumbSegment[] = [];
    if (parentSite?.site) {
      segments.push({
        label: parentSite.site.name,
        to: `/sites/${parentSite.site.id.toString()}`,
      });
    }
    segments.push({
      label: building.name || "(unnamed building)",
      siblings: siblingBuildings?.map((b) => ({
        label: b.building?.name ?? "(unnamed)",
        to: `/buildings/${(b.building?.id ?? 0n).toString()}`,
        isActive: b.building?.id === building.id,
      })),
    });
    return segments;
  }, [effectiveOutcome, parentSite, siblingBuildings]);

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

  // Edit/Delete require the rack count to render an accurate cascade
  // warning ("deleting this building will unassign N racks"). Prefer the
  // precise count from the parallel ListBuildingRacks call when it
  // landed, but never gate the Edit button on it — a transient
  // listBuildingRacks failure would otherwise leave Edit permanently
  // disabled with no recovery path. Falls back to the polled
  // stats.rackCount (live, refreshes with the page) and finally to 0
  // (cascade dialog renders a generic warning).
  const racksFromList =
    rackCountResponse !== undefined && rackCountResponse.id === effectiveBuilding.id ? rackCountResponse.count : null;
  const fallbackRackCount = racksFromList ?? (stats ? BigInt(stats.rackCount) : 0n);
  const handleEditBuilding = () => {
    buildingModals.openManage(
      create(BuildingWithCountsSchema, {
        building: effectiveBuilding,
        rackCount: fallbackRackCount,
      }),
    );
  };

  return (
    <div className="h-full" data-testid="building-page">
      <div className="flex flex-col">
        <div className="p-6 pb-0 laptop:p-10 laptop:pb-0">
          <BuildingPageHeader
            label={label}
            buildingId={idForHeader}
            onEditBuilding={handleEditBuilding}
            breadcrumbSegments={breadcrumbSegments}
          />
        </div>

        {/* Stats fetch failure on initial load — surface it inline so the
            metrics row, diagnostics, and performance section don't sit
            indefinitely in skeleton state with no recovery affordance. */}
        {statsError && !statsHasLoaded ? (
          <div className="px-6 pt-6 laptop:px-10 laptop:pt-10">
            <div
              className="flex items-center justify-between gap-3 rounded-xl border border-intent-critical-20 bg-intent-critical-10 px-4 py-3 text-200 text-intent-critical-text"
              data-testid="building-page-stats-error"
            >
              <span>Couldn&apos;t load building metrics: {statsError}</span>
              <button
                type="button"
                onClick={() => refetchStats()}
                className="shrink-0 underline hover:opacity-80"
                data-testid="building-page-stats-retry"
              >
                Retry
              </button>
            </div>
          </div>
        ) : null}

        {/* Metrics row */}
        <section className="px-6 pt-6 laptop:px-10 laptop:pt-10">
          <BuildingMetricsRow powerCapacityKw={effectiveBuilding.powerKw} stats={stats} />
        </section>

        {/* Diagnostics: rack-health grid */}
        <section className="p-6 laptop:p-10">
          {stats === undefined ? (
            <SkeletonBar className="h-64 w-full rounded-2xl" />
          ) : (
            <BuildingRackGrid
              rackHealth={stats.rackHealth}
              aisles={effectiveBuilding.aisles}
              racksPerAisle={effectiveBuilding.racksPerAisle}
              testId="building-page-rack-grid"
            />
          )}
        </section>

        {/* Performance section — identical wiring to RackOverviewPage */}
        <section className="pb-6">
          <div ref={refs.vertical.start} />
          <div className="sticky top-0 z-2 bg-surface-5 px-6 pt-6 pb-6 laptop:px-10 laptop:pt-10 dark:bg-surface-base">
            <div className="flex flex-col gap-4 tablet:flex-row tablet:items-center tablet:justify-between">
              <div className="text-heading-200 text-text-primary">Performance</div>
              <div className="flex items-center gap-6 text-200 text-core-primary-50">
                <div className="flex items-center gap-2">
                  <svg width="24" height="4">
                    <line
                      x1="0"
                      y1="2"
                      x2="24"
                      y2="2"
                      stroke="var(--color-core-primary-fill)"
                      strokeWidth="3"
                      strokeLinecap="round"
                    />
                  </svg>
                  <span>Building</span>
                </div>
                <div className="flex items-center gap-2">
                  <svg width="24" height="4">
                    <line
                      x1="0"
                      y1="2"
                      x2="24"
                      y2="2"
                      stroke="var(--color-core-primary-50)"
                      strokeWidth="3"
                      strokeLinecap="round"
                      strokeDasharray="1 6"
                      strokeOpacity="0.5"
                    />
                  </svg>
                  <span>Max</span>
                </div>
                <div className="flex items-center gap-2">
                  <svg width="24" height="4">
                    <line
                      x1="0"
                      y1="2"
                      x2="24"
                      y2="2"
                      stroke="var(--color-intent-critical-fill)"
                      strokeWidth="3"
                      strokeLinecap="round"
                      strokeDasharray="1 6"
                      strokeOpacity="0.5"
                    />
                  </svg>
                  <span>Min</span>
                </div>
              </div>
              <div className="flex items-center">
                <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
              </div>
            </div>
          </div>

          <div className="px-6 laptop:px-10">
            <DeviceSetPerformanceSection duration={duration} metrics={metrics} />
          </div>
          {/* eslint-disable-next-line react-hooks/refs -- ref object from useStickyState is passed to <div ref>; React writes .current during commit, not read during render */}
          <div ref={refs.vertical.end} />
        </section>
      </div>
      {/* BuildingPage never opens detailsCreate (only manage/edit on the
          current building), so an empty sites list is fine here — the Site
          dropdown is unused on this surface. */}
      <BuildingModals modals={buildingModals} sites={[]} />
    </div>
  );
};

export default BuildingPage;
