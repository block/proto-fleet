import { useMemo } from "react";

import { useFleetOutletContext } from "../components/FleetLayout";
import SitesListTable from "../components/SitesListTable";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import SitesEmptyState from "@/protoFleet/features/sites/components/SitesEmptyState";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

// Sites tab content for `/fleet/sites`. Reads the sites list from the
// FleetLayout outlet context (single shared fetch) and mirrors the original
// /settings/sites All Sites table — Add Site CTA, flat list, modals. Rows
// navigate to /sites/:id (J3a) instead of narrowing the topbar SitePicker.
// Phase 1a uses placeholder metrics; the live metric columns land with the
// rest of Phase 1b enrichment.
const FleetSitesPage = () => {
  const { sites, sitesError, refetchSites } = useFleetOutletContext();

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });

  const modals = useSiteModals({ refetchSites });

  if (sites === undefined) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6">
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  // Full-page error path only when we have no last-good data. Once the layout
  // has rendered sites at least once, a transient failure surfaces inline
  // below and the existing list stays visible — see FleetLayout's onError.
  if (sitesError && sites.length === 0) {
    return (
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-sites-error">
        <Header title="Couldn't load sites" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{sitesError}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={refetchSites}
          testId="fleet-sites-retry"
        />
      </div>
    );
  }

  // "Unassigned" filters miners with no site, not sites. The Sites tab
  // can't meaningfully scope itself to an Unassigned bucket, so we render
  // a note redirecting the operator. The "site" picker case is impossible
  // here — FleetLayout hides this tab + redirects away when a single site
  // is selected (J2).
  const body =
    sites.length === 0 ? (
      <SitesEmptyState onAddSite={modals.openCreate} />
    ) : activeSite.kind === "unassigned" ? (
      <div
        className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
        data-testid="fleet-sites-unassigned-note"
      >
        &quot;Unassigned&quot; filters miners, not sites. Switch the picker to All Sites to see every site.
      </div>
    ) : (
      <>
        <div className="flex items-center justify-end">
          <Button
            variant={variants.primary}
            size={sizes.compact}
            text="Add site"
            onClick={modals.openCreate}
            testId="fleet-sites-add"
          />
        </div>
        <SitesListTable sites={sites} />
      </>
    );

  return (
    <>
      <div className="flex flex-col gap-6 p-10 phone:p-6" data-testid="fleet-sites-page">
        {sitesError ? (
          // Post-init listSites failure: keep last-good data rendered below
          // and surface the failure as an inline retry banner.
          <div
            className="flex items-center justify-between rounded-xl border border-border-5 p-4"
            data-testid="fleet-sites-inline-error"
          >
            <span className="text-300 text-text-primary-70">Couldn&apos;t refresh sites: {sitesError}</span>
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              text="Retry"
              onClick={refetchSites}
              testId="fleet-sites-inline-retry"
            />
          </div>
        ) : null}
        {body}
      </div>
      <SiteModals modals={modals} sites={sites} />
    </>
  );
};

export default FleetSitesPage;
