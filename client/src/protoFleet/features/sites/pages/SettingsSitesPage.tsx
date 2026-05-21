import { useCallback, useEffect, useMemo, useState } from "react";

import SiteModals from "../components/SiteModals";
import SitesAllTable from "../components/SitesAllTable";
import SitesEmptyState from "../components/SitesEmptyState";
import SiteSettingsSingleView from "../components/SiteSettingsSingleView";
import SitesPageHeader from "../components/SitesPageHeader";
import { useSiteModals } from "../hooks/useSiteModals";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

// `/settings/sites` config surface. Same data fetch shape as SitesPage; the
// difference is the layout — all-sites mode renders a flat table, single
// mode renders the configuration form. Site CRUD modals are mounted at the
// page level so they survive the All Sites ↔ single-site re-render.
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

  const modals = useSiteModals({ refetchSites: fetchSites });

  // SiteDetailsModal.onDeleteRequested needs a SiteWithCounts row (not just
  // a Site) to drive the cascade dialog copy. Look it up from the page's
  // current ListSites cache.
  const handleDeleteFromDetailsEdit = useCallback(() => {
    if (modals.state.kind !== "detailsEdit") return;
    const id = modals.state.site.id.toString();
    const match = sites?.find((s) => (s.site?.id ?? 0n).toString() === id);
    if (match) modals.openDeleteConfirm(match);
  }, [modals, sites]);

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

  const activeMatch =
    activeSite.kind === "site" ? sites.find((s) => (s.site?.id ?? 0n).toString() === activeSite.id) : undefined;

  const body =
    sites.length === 0 ? (
      <div className="flex flex-col gap-6" data-testid="settings-sites-page">
        <SitesPageHeader
          headline="Sites"
          subheadline="Manage your sites, buildings, and rack infrastructure."
          onAddSite={modals.openCreate}
        />
        <SitesEmptyState onAddSite={modals.openCreate} />
      </div>
    ) : activeMatch ? (
      <div data-testid="settings-sites-page">
        <SiteSettingsSingleView
          site={activeMatch}
          knownSiteIds={knownSiteIds}
          onManage={() => {
            if (activeMatch.site) modals.openManageEdit(activeMatch.site);
          }}
        />
      </div>
    ) : (
      <div className="flex flex-col gap-6" data-testid="settings-sites-page">
        <SitesPageHeader
          headline="Sites"
          subheadline="Manage your sites, buildings, and rack infrastructure."
          onAddSite={modals.openCreate}
        />
        <SitesAllTable sites={sites} />
      </div>
    );

  return (
    <>
      {body}
      <SiteModals modals={modals} onDeleteFromDetailsEdit={handleDeleteFromDetailsEdit} />
    </>
  );
};

export default SettingsSitesPage;
