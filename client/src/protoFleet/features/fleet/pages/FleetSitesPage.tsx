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

  // Show the full-page error only when we have no last-good data; once the
  // layout has rendered sites at least once, transient failures surface in
  // the inline banner below so the existing list stays visible.
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
