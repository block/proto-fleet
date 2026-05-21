import { useCallback, useEffect, useMemo, useState } from "react";

import SitesAllTable from "../components/SitesAllTable";
import SitesEmptyState from "../components/SitesEmptyState";
import SiteSettingsSingleView from "../components/SiteSettingsSingleView";
import SitesPageHeader from "../components/SitesPageHeader";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

// `/settings/sites` config surface. Same data fetch shape as SitesPage; the
// difference is the layout — all-sites mode renders a flat table, single
// mode renders the configuration form. Site create/edit/delete modals land
// in #261 and #262.
const SettingsSitesPage = () => {
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[] | undefined>(undefined);
  const [sitesError, setSitesError] = useState<string | null>(null);

  // Separate sites + sitesError so PermissionDenied / network failures don't
  // collapse into "no sites yet" empty-state and mislead the operator.
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

  if (sitesError) {
    return (
      <div className="flex flex-col gap-6" data-testid="settings-sites-page-error">
        <SitesPageHeader headline="Sites" subheadline="Manage your sites, buildings, and rack infrastructure." />
        <div className="flex flex-col items-start gap-3 rounded-xl border border-border-5 p-6">
          <Header title="Couldn't load sites" titleSize="text-heading-200" />
          <p className="text-300 text-text-primary-70">{sitesError}</p>
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Retry"
            onClick={fetchSites}
            testId="settings-sites-retry"
          />
        </div>
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
