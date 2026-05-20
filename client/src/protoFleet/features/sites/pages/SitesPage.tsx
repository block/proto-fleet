import { useCallback, useEffect, useMemo, useState } from "react";

import SiteOverviewSection from "../components/SiteOverviewSection";
import SitesEmptyState from "../components/SitesEmptyState";
import SitesPageHeader from "../components/SitesPageHeader";
import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";

// `/sites` operational overview. Phase 1a renders the scaffolding — header,
// per-site sections with placeholder metrics + FPO BuildingCards, and the
// empty-state CTA. Real metric components and the production BuildingCard
// land in #263.
const SitesPage = () => {
  const { listSites } = useSites();
  const { listAllBuildings } = useBuildings();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [sitesError, setSitesError] = useState<string | null>(null);
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);

  // Track sites + sitesError separately so transient failures (network,
  // PermissionDenied for non-admins) don't collapse into "no sites yet"
  // and mislead the operator into thinking the org has no sites.
  const fetchSites = useCallback(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: (rows) => {
        setSites(rows);
        setSitesError(null);
      },
      onError: (msg) => {
        setSitesError(msg);
        setSites([]);
      },
    });
    return () => controller.abort();
  }, [listSites]);

  useEffect(() => fetchSites(), [fetchSites]);

  // One ListBuildings call at the page level, then we bucket the rows by
  // siteId client-side so each SiteOverviewSection can render synchronously
  // from props. Avoids the N+1 per-section ListBuildings concurrency that
  // the earlier scaffold had.
  useEffect(() => {
    const controller = new AbortController();
    void listAllBuildings({
      signal: controller.signal,
      onSuccess: setBuildings,
      onError: () => setBuildings([]),
    });
    return () => controller.abort();
  }, [listAllBuildings]);

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);

  const { activeSite } = useActiveSite({ knownSiteIds });

  const buildingsBySite = useMemo(() => {
    const grouped = new Map<string, BuildingWithCounts[]>();
    if (!buildings) return grouped;
    for (const b of buildings) {
      const siteId = b.building?.siteId;
      if (siteId === undefined) continue;
      const key = siteId.toString();
      const existing = grouped.get(key);
      if (existing) existing.push(b);
      else grouped.set(key, [b]);
    }
    return grouped;
  }, [buildings]);

  const visibleSites = useMemo(() => {
    if (!sites) return [];
    if (activeSite.kind === "all") return sites;
    if (activeSite.kind === "site") {
      return sites.filter((s) => (s.site?.id ?? 0n).toString() === activeSite.id);
    }
    // "Unassigned" is handled outside this list — see the dedicated branch
    // below. Return [] here so the "no matches" path isn't triggered.
    return [];
  }, [sites, activeSite]);

  if (sites === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (sitesError) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="sites-page-error">
        <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
        <div
          className="flex flex-col items-start gap-3 rounded-xl border border-border-5 p-6"
          data-testid="sites-page-error-card"
        >
          <Header title="Couldn't load sites" titleSize="text-heading-200" />
          <p className="text-300 text-text-primary-70">{sitesError}</p>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Retry"
            onClick={fetchSites}
            testId="sites-page-retry"
          />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="sites-page">
      <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
      {sites.length === 0 ? (
        <SitesEmptyState />
      ) : activeSite.kind === "unassigned" ? (
        // "Unassigned" filters miners, not sites — there is no site-scoped
        // surface to render here. Stand a placeholder in for now so reviewers
        // see the affordance until #273 lands the real miner-filter view.
        <PlaceholderBlock label='"Unassigned" filters miners, not sites. See #273.' className="h-32" />
      ) : visibleSites.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
          No sites match the current selection.
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          {visibleSites.map((site) => {
            const siteId = (site.site?.id ?? 0n).toString();
            return (
              <SiteOverviewSection
                key={siteId}
                site={site}
                buildings={buildingsBySite.get(siteId) ?? (buildings === undefined ? undefined : [])}
              />
            );
          })}
        </div>
      )}
    </div>
  );
};

export default SitesPage;
