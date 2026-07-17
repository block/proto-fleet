import { useCallback, useEffect, useMemo, useState } from "react";
import { useParams } from "react-router-dom";

import SiteMetricsRow from "../components/SiteMetricsRow";
import SiteModals from "../components/SiteModals";
import { useSiteModals } from "../hooks/useSiteModals";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { AggregationType, MeasurementType } from "@/protoFleet/api/generated/telemetry/v1/telemetry_pb";
import { buildKnownSiteIds, parseBigIntId } from "@/protoFleet/api/sites";
import { useSitesContext } from "@/protoFleet/api/SitesContext";
import { useSiteStats } from "@/protoFleet/api/useSiteStats";
import { useTelemetryMetrics } from "@/protoFleet/api/useTelemetryMetrics";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import BuildingSummaryCard from "@/protoFleet/features/buildings/components/BuildingSummaryCard";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { DeviceSetPerformanceSection } from "@/protoFleet/features/groupManagement/components/DeviceSetPerformanceSection";
import { entityScopeTarget, useSyncScopeToEntity } from "@/protoFleet/hooks/useSyncScopeToEntity";
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
  const { id: idParam } = useParams<{ id?: string }>();
  const targetId = idParam ?? "";

  const { listBuildingsBySite } = useBuildings();
  // Site catalog is owned by the shell-level SitesProvider; this page reads it
  // (and triggers a refresh after rename/delete via refetchSites) instead of
  // firing its own ListSites.
  const { sites, sitesError: error, siteCatalogAccessGranted, refetchSites } = useSitesContext();
  const [buildings, setBuildings] = useState<{ siteId: string; rows: BuildingWithCounts[] } | undefined>(undefined);
  const [buildingsError, setBuildingsError] = useState<{ siteId: string; message: string } | null>(null);

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

  // Bounce to /fleet when SitePicker switches to a different specific
  // site — "All sites" / "Unassigned" don't conflict with this view.
  // Only validate the picker selection against an authoritative catalog: on a
  // mid-session PermissionDenied the provider clears `sites` to [] but keeps
  // sitesLoaded true, so keying off siteCatalogAccessGranted avoids treating
  // the denied (empty) catalog as a loaded set.
  const knownSiteIds = useMemo(
    () => (siteCatalogAccessGranted ? buildKnownSiteIds(sites) : undefined),
    [siteCatalogAccessGranted, sites],
  );
  // Keep the deleted-site guard (resets a stored scope that points at a site
  // the viewer lost access to); setActiveSite drives breadcrumb sibling nav.
  const { setActiveSite } = useActiveSite({ knownSiteIds });

  const site = useMemo(() => {
    if (!sites) return undefined;
    const parsed = parseBigIntId(targetId);
    if (parsed === null) return undefined;
    return sites.find((s) => s.site?.id === parsed);
  }, [sites, targetId]);

  // This is a headerless route, so the persisted scope can point at an
  // unrelated site on deep-link/bookmark. Align it with the site being viewed
  // (leaving "all-sites" as-is) rather than bouncing away to /fleet (#764).
  useSyncScopeToEntity(site ? entityScopeTarget(site.site?.id, sites) : undefined);

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
          onClick={refetchSites}
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
          ? () => setActiveSite({ kind: "site", id: siblingId, slug: siblingSite.slug })
          : undefined,
      };
    });

  return (
    <>
      <div className="flex flex-col gap-10 px-4 py-6 laptop:px-8 laptop:py-10" data-testid="site-detail-page">
        {error ? (
          <Callout
            intent="danger"
            prefixIcon={<Alert />}
            title="Couldn't refresh site"
            subtitle={error}
            buttonText="Retry"
            buttonOnClick={refetchSites}
            testId="site-detail-inline-error"
          />
        ) : null}
        <div className="flex flex-col gap-3 px-2" data-testid="site-detail-heading">
          <Breadcrumb
            segments={[
              { label: "Sites", to: "/fleet/sites" },
              { label: site.site.name, siblings: siteSiblings.length > 1 ? siteSiblings : undefined },
            ]}
            testId="site-detail-breadcrumb"
          />
          <Header
            title={site.site.name}
            titleSize="truncate text-heading-300"
            inline
            centerButton
            stackButtonsOnPhone={false}
            testId="site-detail-title"
          >
            {canManageSites ? (
              <div className="ml-3 shrink-0">
                <Button
                  variant={variants.primary}
                  size={sizes.compact}
                  text="Edit site"
                  onClick={() => modals.openManageEdit(site.site!)}
                  testId="site-detail-edit"
                />
              </div>
            ) : null}
          </Header>
        </div>
        <div className="flex flex-col gap-3 px-2" data-testid="site-detail-metrics-section">
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
        <div className="flex flex-col gap-3" data-testid="site-detail-buildings-section">
          <div className="flex items-center justify-between gap-3 px-2">
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
          <div className="overflow-visible p-2">
            <div className="rounded-xl bg-surface-elevated-base p-10 shadow-100 phone:p-6">
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
                <div className="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-3">
                  {visibleBuildings.map((building) => (
                    <div key={(building.building?.id ?? 0n).toString()} className="min-w-0">
                      <BuildingSummaryCard building={building} />
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
        {canReadFleet ? (
          <div className="flex flex-col gap-3" data-testid="site-detail-performance">
            <div className="flex flex-col gap-3 px-2 tablet:flex-row tablet:items-center tablet:justify-between">
              <div className="tablet:flex-1">
                <Header title="Performance" titleSize="text-heading-200" />
              </div>
              <div className="flex items-center gap-3 text-200 text-core-primary-50">
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
            <DeviceSetPerformanceSection className="p-2" duration={duration} gapClassName="gap-1" metrics={metrics} />
          </div>
        ) : null}
      </div>
      <SiteModals modals={modals} sites={sites} buildingsRefreshKey={buildingsRefreshKey} />
      <BuildingModals modals={buildingModals} sites={sites} />
    </>
  );
};

export default SiteDetailPage;
