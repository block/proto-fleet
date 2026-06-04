import { useMemo } from "react";

import { useFleetOutletContext } from "../components/FleetLayout";
import SiteList from "../components/SiteList";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import SitesEmptyState from "@/protoFleet/features/sites/components/SitesEmptyState";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

// Filter / Add-button band. Owns the full top + bottom spacing between the
// tab strip and the list below it (`pt-6 laptop:pt-10` on top, `pb-6`
// underneath) so the List itself can sit flush — matches the Racks tab
// structure on /fleet/racks.
const BAND_CLASSES = "flex flex-col gap-4 px-6 pt-6 pb-6 laptop:px-10 laptop:pt-10";

const FleetSitesPage = () => {
  const { sites, sitesError, refetchSites } = useFleetOutletContext();

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });

  const modals = useSiteModals({ refetchSites });

  if (sites === undefined) {
    return (
      <div className={BAND_CLASSES}>
        <div className="text-300 text-text-primary-70">Loading…</div>
      </div>
    );
  }

  // Show the full-page error only when we have no last-good data; once the
  // layout has rendered sites at least once, transient failures surface in
  // the inline banner below so the existing list stays visible.
  if (sitesError && sites.length === 0) {
    return (
      <div className={BAND_CLASSES} data-testid="fleet-sites-error">
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

  if (sites.length === 0) {
    return (
      <>
        <div className={BAND_CLASSES} data-testid="fleet-sites-page">
          <SitesEmptyState onAddSite={modals.openCreate} />
        </div>
        <SiteModals modals={modals} sites={sites} />
      </>
    );
  }

  // FleetLayout normally hides + redirects away from this tab when the
  // picker resolves to an existing single site (J2), but the redirect
  // effect needs a render to fire. Show a transitional placeholder here
  // so the operator never sees a contradictory "All Sites" list under a
  // single-site picker selection.
  if (activeSite.kind === "site") {
    return (
      <>
        <div className={BAND_CLASSES} data-testid="fleet-sites-page">
          <div className="text-300 text-text-primary-70" data-testid="fleet-sites-redirecting">
            Loading…
          </div>
        </div>
        <SiteModals modals={modals} sites={sites} />
      </>
    );
  }

  if (activeSite.kind === "unassigned") {
    return (
      <>
        <div className={BAND_CLASSES} data-testid="fleet-sites-page">
          <div
            className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
            data-testid="fleet-sites-unassigned-note"
          >
            &quot;Unassigned&quot; filters miners, not sites. Switch the picker to All Sites to see every site.
          </div>
        </div>
        <SiteModals modals={modals} sites={sites} />
      </>
    );
  }

  return (
    <>
      <div data-testid="fleet-sites-page">
        <div className={BAND_CLASSES}>
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
          <div className="flex items-center justify-end">
            <Button
              variant={variants.primary}
              size={sizes.compact}
              text="Add site"
              onClick={modals.openCreate}
              testId="fleet-sites-add"
            />
          </div>
        </div>
        <SiteList sites={sites} />
      </div>
      <SiteModals modals={modals} sites={sites} />
    </>
  );
};

export default FleetSitesPage;
