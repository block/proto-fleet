import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import SiteModals from "../components/SiteModals";
import { useSiteModals } from "../hooks/useSiteModals";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, parseBigIntId, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import BuildingSummaryCard from "@/protoFleet/features/buildings/components/BuildingSummaryCard";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

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

  // Bump retryCounter to re-run the effect so the cleanup AbortController
  // stays owned by useEffect and isn't leaked by an imperative call.
  const [retryCounter, setRetryCounter] = useState(0);
  const handleRetry = useCallback(() => setRetryCounter((n) => n + 1), []);

  useEffect(() => fetchSites(), [fetchSites, retryCounter]);

  // Bounce to /fleet when SitePicker switches to a different specific
  // site — "All sites" / "Unassigned" don't conflict with this view.
  const knownSiteIds = useMemo(() => (sitesLoaded ? buildKnownSiteIds(sites) : undefined), [sites, sitesLoaded]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  useEffect(() => {
    if (activeSite.kind !== "site") return;
    if (activeSite.id === targetId) return;
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

  const [buildingsRefreshKey, setBuildingsRefreshKey] = useState(0);
  const refetchBuildings = useCallback(() => setBuildingsRefreshKey((n) => n + 1), []);
  // Membership saves in ManageSiteModal also affect building rows, so share
  // the same refresh signal used for direct building mutations.
  const modals = useSiteModals({ refetchSites: fetchSites, refetchBuildings });
  const buildingModals = useBuildingModals({ refetchBuildings });

  const siteId = site?.site?.id;
  const siteIdText = siteId?.toString();
  const visibleBuildings = buildings && buildings.siteId === siteIdText ? buildings.rows : undefined;
  const visibleBuildingsError = buildingsError && buildingsError.siteId === siteIdText ? buildingsError.message : null;

  useEffect(() => {
    if (siteId === undefined) return undefined;
    return fetchBuildings(siteId);
  }, [fetchBuildings, siteId, buildingsRefreshKey]);

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
        <Header title="Site not found" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">No site matches id {targetId}.</p>
        <Button
          variant={variants.primary}
          size={sizes.compact}
          text="Back to sites"
          onClick={() => navigate(scopedPath("/fleet/sites", activeSite))}
          testId="site-detail-back"
        />
      </div>
    );
  }

  const address = formatSiteAddress(site.site);

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
        <div className="flex items-start justify-between gap-4">
          <Header title={site.site.name} titleSize="text-heading-300" subtitle={address || undefined} />
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
        <PlaceholderBlock label="Metrics row — coming soon" className="h-20" />
        <PlaceholderBlock label="Details table — coming soon" className="h-40" />
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
          <div className="rounded-[24px] border border-border-5 bg-surface-base p-6 shadow-300 tablet:p-8 laptop:p-10">
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
      </div>
      <SiteModals modals={modals} sites={sites} buildingsRefreshKey={buildingsRefreshKey} />
      <BuildingModals modals={buildingModals} sites={sites} />
    </>
  );
};

export default SiteDetailPage;
