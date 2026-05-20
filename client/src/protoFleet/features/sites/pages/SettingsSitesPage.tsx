import { useEffect, useMemo, useState } from "react";

import SitesAllTable from "../components/SitesAllTable";
import SitesEmptyState from "../components/SitesEmptyState";
import SiteSettingsSingleView from "../components/SiteSettingsSingleView";
import SitesPageHeader from "../components/SitesPageHeader";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";

// `/settings/sites` config surface. Same data fetch shape as SitesPage; the
// difference is the layout — all-sites mode renders a flat table, single
// mode renders the configuration form. Site create/edit/delete modals land
// in #261 and #262.
const SettingsSitesPage = () => {
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);

  useEffect(() => {
    const controller = new AbortController();
    void listSites({
      signal: controller.signal,
      onSuccess: setSites,
      onError: () => setSites([]),
    });
    return () => controller.abort();
  }, [listSites]);

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);

  const { activeSite } = useActiveSite({ knownSiteIds });

  if (sites === undefined) {
    return (
      <div className="flex flex-col gap-6">
        <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  if (sites.length === 0) {
    return (
      <div className="flex flex-col gap-6" data-testid="settings-sites-page">
        <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
        <SitesEmptyState />
      </div>
    );
  }

  if (activeSite.kind === "site") {
    const match = sites.find((s) => (s.site?.id ?? 0n).toString() === activeSite.id);
    if (match) {
      return (
        <div data-testid="settings-sites-page">
          <SiteSettingsSingleView site={match} knownSiteIds={knownSiteIds} />
        </div>
      );
    }
    // Fall through to the All Sites layout if the stored selection no
    // longer exists; useActiveSite will reset the storage on the next
    // render.
  }

  return (
    <div className="flex flex-col gap-6" data-testid="settings-sites-page">
      <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
      <SitesAllTable sites={sites} />
    </div>
  );
};

export default SettingsSitesPage;
