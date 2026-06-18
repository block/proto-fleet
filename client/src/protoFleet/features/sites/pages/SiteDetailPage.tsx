import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import SiteMetricsRow from "../components/SiteMetricsRow";
import SiteModals from "../components/SiteModals";
import { useSiteModals } from "../hooks/useSiteModals";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { buildKnownSiteIds, parseBigIntId, useSites } from "@/protoFleet/api/sites";
import { useSiteStats } from "@/protoFleet/api/useSiteStats";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingCard from "@/protoFleet/features/buildings/components/BuildingCard";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import { useDuration, useHasPermission, useSetDuration } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import { Breadcrumb } from "@/shared/components/Breadcrumb";
import type { BreadcrumbSegment } from "@/shared/components/Breadcrumb";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import { useStickyState } from "@/shared/hooks/useStickyState";

const ALL_MEASUREMENT_TYPES: MeasurementType[] = [
  MeasurementType.HASHRATE,
  MeasurementType.POWER,
  MeasurementType.TEMPERATURE,
  MeasurementType.EFFICIENCY,
  MeasurementType.UPTIME,
];

const ALL_AGGREGATION_TYPES: AggregationType[] = [AggregationType.AVERAGE, AggregationType.MIN, AggregationType.MAX];

const SiteDetailPage = () => {
  const navigate = useNavigate();
  const { id: idParam } = useParams<{ id?: string }>();
  const targetId = idParam ?? "";

  const { listSites } = useSites();
  const { listBuildingsBySite } = useBuildings();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [error, setError] = useState<string | null>(null);

  const fetchSites = useCallback(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setError(null);
      },
      onError: (msg) => {
        setError(msg);
        setSites((prev) => prev ?? []);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  const [retryCounter, setRetryCounter] = useState(0);
  const handleRetry = useCallback(() => setRetryCounter((n) => n + 1), []);

  useEffect(() => fetchSites(), [fetchSites, retryCounter]);

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  useEffect(() => {
    if (activeSite.kind !== "site") return;
    if (activeSite.id === targetId) return;
    navigate("/fleet", { replace: true });
  }, [activeSite, navigate, targetId]);

  const site = useMemo(() => {
    if (!sites) return undefined;
    const parsed = parseBigIntId(targetId);
    if (parsed === null) return undefined;
    return sites.find((s) => s.site?.id === parsed);
  }, [sites, targetId]);

  const canManageSites = useHasPermission("site:manage");

  const modals = useSiteModals({ refetchSites: fetchSites });
  const [buildingsRefreshKey, setBuildingsRefreshKey] = useState(0);
  const buildingModals = useBuildingModals({
    refetchBuildings: () => setBuildingsRefreshKey((n) => n + 1),
  });

  // Fetch buildings for this site.
  const siteId = site?.site?.id;
  const [buildingsResponse, setBuildingsResponse] = useState<
    { siteId: bigint; buildings: BuildingWithCounts[] } | undefined
  >(undefined);

  useEffect(() => {
    if (!siteId) return;
    const controller = new AbortController();
    void listBuildingsBySite({
      siteId,
      signal: controller.signal,
      onSuccess: (rows) => setBuildingsResponse({ siteId, buildings: rows }),
      onError: () => setBuildingsResponse({ siteId, buildings: [] }),
    });
    return () => controller.abort();
  }, [siteId, listBuildingsBySite, buildingsRefreshKey]);

  const buildings = siteId && buildingsResponse?.siteId === siteId ? buildingsResponse.buildings : undefined;

  // Site metrics (polled).
  const {
    stats,
    error: statsError,
    hasLoaded: statsHasLoaded,
    refetch: refetchStats,
  } = useSiteStats({
    siteId: siteId ?? 0n,
    enabled: siteId !== undefined && siteId !== 0n,
    pollIntervalMs: POLL_INTERVAL_MS,
  });

  const duration = useDuration();
  const setDuration = useSetDuration();
  const { refs } = useStickyState();

  // GetSiteStatsResponse doesn't carry device_identifiers yet, so we
  // derive a "has devices" flag from the aggregate deviceCount. The mock
  // transport's getCombinedMetrics ignores the deviceSelector anyway; in
  // production this will switch to stats.deviceIdentifiers once the
  // proto field lands.
  const hasDevices = stats !== undefined && stats.deviceCount > 0;
  const telemetryOptions = useMemo(
    () => ({
      deviceIds: [],
      measurementTypes: ALL_MEASUREMENT_TYPES,
      aggregations: ALL_AGGREGATION_TYPES,
      duration,
      enabled: hasDevices,
      pollIntervalMs: POLL_INTERVAL_MS,
    }),
    [duration, hasDevices],
  );
  const { data: telemetryData } = useTelemetryMetrics(telemetryOptions);
  const metrics = stats !== undefined && stats.deviceCount === 0 ? [] : telemetryData?.metrics;

  if (sites === undefined) {
    return (
      <div className="p-6 laptop:p-10">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (error && sites.length === 0) {
    return (
      <div className="flex flex-col gap-6 p-6 laptop:p-10">
        <Header title="Couldn't load site" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{error}</p>
        <Button variant={variants.secondary} onClick={handleRetry} testId="site-detail-retry">
          Retry
        </Button>
      </div>
    );
  }

  if (!site || !site.site) {
    return (
      <div className="flex flex-col gap-6 p-6 laptop:p-10" data-testid="site-detail-not-found">
        <Header title="Site not found" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">No site matches id {targetId}.</p>
        <Button variant={variants.primary} onClick={() => navigate("/fleet/sites")} testId="site-detail-back">
          Back to sites
        </Button>
      </div>
    );
  }

  const siteIdText = site.site.id.toString();

  const breadcrumbSegments: BreadcrumbSegment[] = [
    {
      label: site.site.name,
      siblings: sites
        ?.map((s) => ({
          label: s.site?.name ?? "",
          to: `/sites/${(s.site?.id ?? 0n).toString()}`,
          isActive: s.site?.id === site.site!.id,
        }))
        .filter((s) => s.label),
    },
  ];

  return (
    <div className="h-full" data-testid="site-detail-page">
      <div className="flex flex-col">
        {/* Header */}
        <div className="p-6 pb-0 laptop:p-10 laptop:pb-0">
          <div className="flex flex-col gap-6">
            {error ? (
              <Callout
                intent="danger"
                prefixIcon={<Alert />}
                title="Couldn't refresh site"
                subtitle={error}
                buttonText="Retry"
                buttonOnClick={handleRetry}
                testId="site-detail-inline-error"
              />
            ) : null}
            <Breadcrumb segments={breadcrumbSegments} testId="site-detail-breadcrumb" />
            <Header title={site.site.name} titleSize="text-heading-300" inline>
              <div className="ml-3 flex items-center gap-3">
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => navigate(`/fleet/buildings?site=${siteIdText}`)}
                  testId="site-detail-view-buildings"
                >
                  View buildings
                </Button>
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => navigate(`/fleet/racks?site=${siteIdText}`)}
                  testId="site-detail-view-racks"
                >
                  View racks
                </Button>
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => navigate(`/fleet/miners?site=${siteIdText}`)}
                  testId="site-detail-view-miners"
                >
                  View miners
                </Button>
                {canManageSites ? (
                  <Button
                    variant={variants.primary}
                    size={sizes.compact}
                    onClick={() => modals.openManageEdit(site.site!)}
                    testId="site-detail-edit"
                  >
                    Edit site
                  </Button>
                ) : null}
              </div>
            </Header>
          </div>
        </div>

        {/* Stats error banner */}
        {statsError && !statsHasLoaded ? (
          <div className="px-6 pt-6 laptop:px-10 laptop:pt-10">
            <div
              className="flex items-center justify-between gap-3 rounded-xl border border-intent-critical-20 bg-intent-critical-10 px-4 py-3 text-200 text-intent-critical-text"
              data-testid="site-detail-stats-error"
            >
              <span>Couldn&apos;t load site metrics: {statsError}</span>
              <button
                type="button"
                onClick={() => refetchStats()}
                className="shrink-0 underline hover:opacity-80"
                data-testid="site-detail-stats-retry"
              >
                Retry
              </button>
            </div>
          </div>
        ) : null}

        {/* Metrics row */}
        <section className="px-6 pt-6 laptop:px-10 laptop:pt-10">
          <SiteMetricsRow
            locationCity={site.site.locationCity}
            locationState={site.site.locationState}
            powerCapacityMw={site.site.powerCapacityMw}
            buildingCount={stats?.buildingCount ?? Number(site.buildingCount)}
            metrics={stats}
            testId="site-detail-metrics"
          />
        </section>

        {/* Buildings section */}
        <section className="p-6 laptop:p-10">
          <div className="rounded-2xl border border-border-5 bg-surface-base p-6 shadow-[0_4px_24px_rgba(0,0,0,0.05)] laptop:p-10">
            {buildings === undefined ? (
              <div className="text-200 text-text-primary-50">Loading buildings…</div>
            ) : buildings.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
                No buildings in this site yet.
              </div>
            ) : (
              <div className="grid auto-rows-fr grid-cols-1 gap-1 tablet:grid-cols-2 laptop:grid-cols-3">
                {buildings.map((b) => (
                  <BuildingCard key={(b.building?.id ?? 0n).toString()} building={b} />
                ))}
              </div>
            )}
          </div>
        </section>

        {/* Performance section */}
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
                  <span>Site</span>
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
      <SiteModals
        modals={modals}
        sites={sites}
        onAddBuilding={(sid, siteName) => buildingModals.openDetailsCreate(sid, siteName)}
        onEditBuilding={(row, siteName) => buildingModals.openDetailsEdit(row, siteName)}
        buildingsRefreshKey={buildingsRefreshKey}
      />
      <BuildingModals modals={buildingModals} sites={sites} />
    </div>
  );
};

export default SiteDetailPage;
