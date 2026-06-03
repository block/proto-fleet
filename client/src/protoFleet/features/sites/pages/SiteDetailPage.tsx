import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import SiteModals from "../components/SiteModals";
import { useSiteModals } from "../hooks/useSiteModals";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { parseBigIntId, useSites } from "@/protoFleet/api/sites";
import BuildingModals from "@/protoFleet/features/buildings/components/BuildingModals";
import { useBuildingModals } from "@/protoFleet/features/buildings/hooks/useBuildingModals";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

// `/sites/:id` detail page — Phase 1a shell. Header + "Edit site" button +
// FPO placeholder body. Real metric row, Details table, and Buildings grid
// land in Phase 1b. See J3a in 2026-05-05-multi-site-support-plan.md.
const SiteDetailPage = () => {
  const navigate = useNavigate();
  // useParams returns `id?: string` at runtime even when the typed generic
  // claims otherwise; the optional shape matches what React Router actually
  // provides for dynamic segments that may be missing during transitions.
  const { id: idParam } = useParams<{ id?: string }>();
  const targetId = idParam ?? "";

  const { listSites } = useSites();
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
        // Preserve last-good list on transient errors; only clear it on the
        // initial-load failure path so the not-found branch can distinguish
        // "no sites in org" from "fetch failed and we have nothing".
        setSites((prev) => prev ?? []);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  // Retry bumps a counter so the useEffect re-runs with a fresh
  // AbortController under cleanup ownership — never a leaked controller.
  const [retryCounter, setRetryCounter] = useState(0);
  const handleRetry = useCallback(() => setRetryCounter((n) => n + 1), []);

  useEffect(() => fetchSites(), [fetchSites, retryCounter]);

  const site = useMemo(() => {
    if (!sites) return undefined;
    const parsed = parseBigIntId(targetId);
    if (parsed === null) return undefined;
    return sites.find((s) => s.site?.id === parsed);
  }, [sites, targetId]);

  const modals = useSiteModals({ refetchSites: fetchSites });
  const [buildingsRefreshKey, setBuildingsRefreshKey] = useState(0);
  const buildingModals = useBuildingModals({
    refetchBuildings: () => setBuildingsRefreshKey((n) => n + 1),
  });

  if (sites === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (error) {
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
          onClick={() => navigate("/fleet/sites")}
          testId="site-detail-back"
        />
      </div>
    );
  }

  const address = formatSiteAddress(site.site);

  return (
    <>
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="site-detail-page">
        <div className="flex items-start justify-between gap-4">
          <Header title={site.site.name} titleSize="text-heading-300" subtitle={address || undefined} />
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Edit site"
            onClick={() => modals.openManageEdit(site.site!)}
            testId="site-detail-edit"
          />
        </div>
        <PlaceholderBlock label="Metrics row — coming soon" className="h-20" />
        <PlaceholderBlock label="Details table — coming soon" className="h-40" />
        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between">
            <Header title="Buildings" titleSize="text-heading-200" />
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Add building"
              onClick={() => buildingModals.openDetailsCreate(site.site!.id, site.site!.name)}
              testId="site-detail-add-building"
            />
          </div>
          <PlaceholderBlock label="Buildings grid — coming soon" className="h-40" />
        </div>
      </div>
      <SiteModals
        modals={modals}
        sites={sites}
        onAddBuilding={(siteId, siteName) => buildingModals.openDetailsCreate(siteId, siteName)}
        onEditBuilding={(row, siteName) => buildingModals.openDetailsEdit(row, siteName)}
        buildingsRefreshKey={buildingsRefreshKey}
      />
      <BuildingModals modals={buildingModals} />
    </>
  );
};

export default SiteDetailPage;
