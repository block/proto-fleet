import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import BuildingSummaryCard from "@/protoFleet/features/buildings/components/BuildingSummaryCard";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useDuration, useHasPermission, useSetDuration } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import DurationSelector, { fleetDurations } from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";

// Same measurement / aggregation slate the group, rack, and building overview
// pages use, so the performance charts render identically across surfaces.
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
  const [sitesLoaded, setSitesLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [buildings, setBuildings] = useState<{ siteId: string; rows: BuildingWithCounts[] } | undefined>(undefined);
  const [buildingsError, setBuildingsError] = useState<{ siteId: string; message: string } | null>(null);
  const breadcrumbSiteSelectionRef = useRef<string | null>(null);

  const fetchSites = useCallback(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setSitesLoaded(true);
        setError(null);
      },
      onError: (msg) => {
        setError(msg);
        // Preserve last-good list across transient errors; only fall to []
        // on the initial-load failure path.
        setSites((prev) => prev ?? []);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  const fetchBuildings = useCallback(
    (siteId: bigint) => {
      const siteIdText = siteId.toString();
      const controller = new AbortController();
      void listBuildingsBySite({
        siteId,
        signal: controller.signal,
        onSuccess: (rows) => {
          setBuildings({ siteId: siteIdText, rows });
          setBuildingsError(null);
        },
        onError: (msg) => {
          setBuildingsError({ siteId: siteIdText, message: msg });
          // Keep any last-good building rows visible through transient
          // refresh failures, matching the site detail refresh behavior.
          setBuildings((prev) => (prev?.siteId === siteIdText ? prev : { siteId: siteIdText, rows: [] }));
        },
      });
      return () => controller.abort();
    },
    [listBuildingsBySite],
  );

  // Bump retryCounter / sitesRefreshKey to re-run the effect so the cleanup
  // AbortController stays owned by useEffect and isn't leaked by an
  // imperative callback.
  const [retryCounter, setRetryCounter] = useState(0);
  const [sitesRefreshKey, setSitesRefreshKey] = useState(0);
  const handleRetry = useCallback(() => setRetryCounter((n) => n + 1), []);
  const refetchSites = useCallback(() => setSitesRefreshKey((n) => n + 1), []);

  useEffect(() => fetchSites(), [fetchSites, retryCounter, sitesRefreshKey]);

  // Bounce to /fleet when SitePicker switches to a different specific
  // site — "All sites" / "Unassigned" don't conflict with this view.
  const knownSiteIds = useMemo(() => (sitesLoaded ? buildKnownSiteIds(sites) : undefined), [sites, sitesLoaded]);
  const { activeSite, setActiveSite } = useActiveSite({ knownSiteIds });
  useEffect(() => {
    if (activeSite.kind !== "site") return;
    if (activeSite.id === targetId) {
      breadcrumbSiteSelectionRef.current = null;
      return;
    }
    if (breadcrumbSiteSelectionRef.current === activeSite.id) return;
    navigate(scopedPath("/fleet", activeSite), { replace: true });
  }, [activeSite, navigate, targetId]);

  const site = useMemo(() => {
    if (!sites) return undefined;
    const parsed = parseBigIntId(targetId);
    if (parsed === null) return undefined;
    return sites.find((s) => s.site?.id === parsed);
  }, [sites, targetId]);

  // UpdateSite + CreateBuilding require site:manage server-side.
  const canManageSites = useHasPermission("site:manage");

  // The performance charts hit TelemetryService.GetCombinedMetrics, whose
  // handler requires org-default `fleet:read` (an empty ResourceContext — it
  // is NOT site-scoped, unlike GetSiteStats which authorizes the metrics row
  // against the requested SiteID). A site-scoped operator can therefore reach
  // this page and load the metrics row but would be denied the telemetry call,
  // leaving the charts stuck loading. `useHasPermission` reads that same
  // org-default authority, so gate the whole section on it: skip the fetch and
  // hide the charts unless GetCombinedMetrics would actually succeed.
  const canReadFleet = useHasPermission("fleet:read");

  const [buildingsRefreshKey, setBuildingsRefreshKey] = useState(0);
  const refetchBuildings = useCallback(() => setBuildingsRefreshKey((n) => n + 1), []);
  // Membership saves in ManageSiteModal also affect building rows, so share
  // the same refresh signal used for direct building mutations.
  const modals = useSiteModals({ refetchSites, refetchBuildings });

  const siteId = site?.site?.id;
  const siteIdText = siteId?.toString();
  const visibleBuildings = buildings && buildings.siteId === siteIdText ? buildings.rows : undefined;
  const visibleBuildingsError = buildingsError && buildingsError.siteId === siteIdText ? buildingsError.message : null;

  useEffect(() => {
    if (siteId === undefined) return undefined;
    return fetchBuildings(siteId);
  }, [fetchBuildings, siteId, buildingsRefreshKey]);

  // Server-rolled metrics for the header strip (hashrate / power / efficiency
  // + building count). Scope is server-side: every device whose site_id
  // matches, racked or site-direct, so the row matches the miner list.
  const {
    stats: siteStats,
    error: siteStatsError,
    refetch: refetchSiteStats,
  } = useSiteStats({ siteId: siteId ?? 0n, enabled: siteId !== undefined, pollIntervalMs: POLL_INTERVAL_MS });
  const handleBuildingMutationSuccess = useCallback(() => {
    refetchSites();
    refetchSiteStats();
  }, [refetchSites, refetchSiteStats]);
  const buildingModals = useBuildingModals({ refetchBuildings, onMutationSuccess: handleBuildingMutationSuccess });

  // Performance charts — mirrors the group/rack/building overview pages, but
  // scopes telemetry by site rather than by explicit device-set membership.
  // GetCombinedMetrics expands the site into its devices server-side, so no
  // separate member-id fetch is needed here.
  const duration = useDuration();
  const setDuration = useSetDuration();
  const telemetrySiteIds = useMemo(() => (siteId !== undefined ? [siteId] : []), [siteId]);
  const telemetryOptions = useMemo(
    () => ({
      siteIds: telemetrySiteIds,
      measurementTypes: ALL_MEASUREMENT_TYPES,
      aggregations: ALL_AGGREGATION_TYPES,
      duration,
      enabled: siteId !== undefined && canReadFleet,
      pollIntervalMs: POLL_INTERVAL_MS,
    }),
    [telemetrySiteIds, duration, siteId, canReadFleet],
  );
  const { data: telemetryData } = useTelemetryMetrics(telemetryOptions);
  // `undefined` while the first response is in flight (skeletons); a defined
  // (possibly empty) array once it lands, so empty sites show "No data".
  const metrics = telemetryData?.metrics;

  if (sites === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  // Full-page error only when no last-good data; later failures surface
  // inline so the operator isn't stranded after a successful detail load.
  if (error && sites.length === 0) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <Header title="Couldn't load site" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{error}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={handleRetry}
          testId="site-detail-retry"
        />
      </div>
    );
  }

  if (!site || !site.site) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <Breadcrumb
          segments={[{ label: "Sites", to: "/fleet/sites" }, { label: "Site not found" }]}
          testId="site-detail-breadcrumb"
        />
        <Header title="Site not found" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">No site matches id {targetId}.</p>
      </div>
    );
  }

  const hasLoadedVisibleBuildings = visibleBuildings !== undefined && visibleBuildingsError === null;
  const detailBuildingCount =
    siteStats?.buildingCount ?? (hasLoadedVisibleBuildings ? visibleBuildings.length : Number(site.buildingCount));

  const siteSiblings = sites
    .filter((row) => row.site !== undefined)
    .map((row) => {
      const siblingSite = row.site!;
      const siblingId = siblingSite.id.toString();
      return {
        label: siblingSite.name,
        to: `/sites/${siblingId}`,
        isActive: siblingSite.id === site.site!.id,
        onSelect: siblingSite.slug
          ? () => {
              breadcrumbSiteSelectionRef.current = siblingId;
              setActiveSite({ kind: "site", id: siblingId, slug: siblingSite.slug });
            }
          : undefined,
      };
    });

  return (
    <>
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="site-detail-page">
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
        <Breadcrumb
          segments={[
            { label: "Sites", to: "/fleet/sites" },
            { label: site.site.name, siblings: siteSiblings.length > 1 ? siteSiblings : undefined },
          ]}
          testId="site-detail-breadcrumb"
        />
        <div className="flex items-start justify-between gap-4">
          <Header title={site.site.name} titleSize="text-heading-300" testId="site-detail-title" />
          {canManageSites ? (
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Edit site"
              onClick={() => modals.openManageEdit(site.site!)}
              testId="site-detail-edit"
            />
          ) : null}
        </div>
        <div className="flex flex-col gap-4">
          {siteStatsError ? (
            <Callout
              intent="danger"
              prefixIcon={<Alert />}
              title="Couldn't load site metrics"
              subtitle={siteStatsError}
              buttonText="Retry"
              buttonOnClick={() => refetchSiteStats()}
              testId="site-detail-metrics-error"
            />
          ) : null}
          <SiteMetricsRow
            locationCity={site.site.locationCity}
            locationState={site.site.locationState}
            powerCapacityMw={site.site.powerCapacityMw}
            buildingCount={detailBuildingCount}
            metrics={siteStats}
            variant="compact"
            testId="site-detail-metrics-row"
          />
        </div>
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between">
            <Header title="Buildings" titleSize="text-heading-200" />
            {canManageSites ? (
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Add building"
                onClick={() => buildingModals.openDetailsCreate(site.site!.id, site.site!.name)}
                testId="site-detail-add-building"
              />
            ) : null}
          </div>
          {visibleBuildingsError ? (
            <Callout
              intent="danger"
              prefixIcon={<Alert />}
              title="Couldn't load buildings"
              subtitle={visibleBuildingsError}
              buttonText="Retry"
              buttonOnClick={() => {
                if (site.site) setBuildingsRefreshKey((n) => n + 1);
              }}
              testId="site-detail-buildings-error"
            />
          ) : null}
          <div className="rounded-xl bg-surface-base p-10 dark:bg-core-primary-5 phone:p-6">
            {visibleBuildings === undefined ? (
              <div className="text-200 text-text-primary-50">Loading buildings…</div>
            ) : visibleBuildings.length === 0 ? (
              <div
                className="rounded-2xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
                data-testid="site-detail-buildings-empty"
              >
                No buildings in this site yet.
              </div>
            ) : (
              <div className="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-4">
                {visibleBuildings.map((building) => (
                  <div key={(building.building?.id ?? 0n).toString()} className="min-w-0">
                    <BuildingSummaryCard building={building} />
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
        {canReadFleet ? (
          <div className="flex flex-col gap-4" data-testid="site-detail-performance">
            <div className="flex flex-col gap-4 tablet:flex-row tablet:items-center tablet:justify-between">
              <div className="tablet:flex-1">
                <Header title="Performance" titleSize="text-heading-200" />
              </div>
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
              <div className="flex items-center tablet:flex-1 tablet:justify-end">
                <DurationSelector duration={duration} durations={fleetDurations} onSelect={setDuration} />
              </div>
            </div>
            <DeviceSetPerformanceSection duration={duration} metrics={metrics} />
          </div>
        ) : null}
      </div>
      <SiteModals modals={modals} sites={sites} buildingsRefreshKey={buildingsRefreshKey} />
      <BuildingModals modals={buildingModals} sites={sites} />
    </>
  );
};

export default SiteDetailPage;
