import { useEffect, useMemo, useState } from "react";

import SiteOverviewSection from "../components/SiteOverviewSection";
import SitesEmptyState from "../components/SitesEmptyState";
import SitesPageHeader from "../components/SitesPageHeader";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";

// `/sites` operational overview. Phase 1a renders the scaffolding — header,
// per-site sections with placeholder metrics + FPO BuildingCards, and the
// empty-state CTA. Real metric components and the production BuildingCard
// land in #263.
const SitesPage = () => {
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);

  useEffect(() => {
    void listSites({
      onSuccess: setSites,
      onError: () => setSites([]),
    });
  }, [listSites]);

  const knownSiteIds = useMemo(() => {
    if (!sites) return new Set<string>();
    return new Set(sites.map((s) => (s.site?.id ?? 0n).toString()).filter((id) => id !== "0"));
  }, [sites]);

  const { activeSite } = useActiveSite({ knownSiteIds });

  const visibleSites = useMemo(() => {
    if (!sites) return [];
    if (activeSite.kind === "all") return sites;
    if (activeSite.kind === "site") {
      return sites.filter((s) => (s.site?.id ?? 0n).toString() === activeSite.id);
    }
    // "Unassigned" — /sites is a site-scoped surface, so there is nothing
    // to render here. The CTA to manage unassigned miners lives on the
    // miner list (Phase 1b).
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

  return (
    <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="sites-page">
      <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
      {sites.length === 0 ? (
        <SitesEmptyState />
      ) : visibleSites.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70">
          No sites match the current selection.
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          {visibleSites.map((site) => (
            <SiteOverviewSection key={(site.site?.id ?? 0n).toString()} site={site} />
          ))}
        </div>
      )}
    </div>
  );
};

export default SitesPage;
