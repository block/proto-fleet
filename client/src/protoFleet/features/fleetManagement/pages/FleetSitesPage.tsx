import { useMemo } from "react";

import FilterRow from "../components/FilterRow";
import { useFleetOutletContext } from "../components/FleetLayout";
import SiteList from "../components/SiteList";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import SitesEmptyState from "@/protoFleet/features/sites/components/SitesEmptyState";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

// Each layout element owns its own top padding so callers don't have to
// reason about combined gaps:
//   - Heading + tab strip: pt-6 (FleetLayout)
//   - FilterRow: pt-10
//   - List: pt-6 (LIST_WRAPPER)
const LIST_WRAPPER = "pt-6";

const FleetSitesPage = () => {
  const { sites, sitesError, refetchSites } = useFleetOutletContext();

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });

  const modals = useSiteModals({ refetchSites });

  if (sites === undefined) {
    return (
      <FilterRow>
        <div className="text-300 text-text-primary-70">Loading…</div>
      </FilterRow>
    );
  }

  // Full-page error path only when no last-good data; transient failures on
  // subsequent fetches surface in the inline banner below so the existing
  // list stays visible.
  if (sitesError && sites.length === 0) {
    return (
      <FilterRow testId="fleet-sites-error">
        <Header title="Couldn't load sites" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{sitesError}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={refetchSites}
          testId="fleet-sites-retry"
        />
      </FilterRow>
    );
  }

  if (sites.length === 0) {
    return (
      <>
        <FilterRow testId="fleet-sites-page">
          <SitesEmptyState onAddSite={modals.openCreate} />
        </FilterRow>
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
        <FilterRow testId="fleet-sites-page">
          <div className="text-300 text-text-primary-70" data-testid="fleet-sites-redirecting">
            Loading…
          </div>
        </FilterRow>
        <SiteModals modals={modals} sites={sites} />
      </>
    );
  }

  if (activeSite.kind === "unassigned") {
    return (
      <>
        <FilterRow testId="fleet-sites-page">
          <div
            className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
            data-testid="fleet-sites-unassigned-note"
          >
            &quot;Unassigned&quot; filters miners, not sites. Switch the picker to All Sites to see every site.
          </div>
        </FilterRow>
        <SiteModals modals={modals} sites={sites} />
      </>
    );
  }

  return (
    <>
      <FilterRow testId="fleet-sites-page">
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
            variant={variants.secondary}
            size={sizes.compact}
            text="Add site"
            onClick={modals.openCreate}
            testId="fleet-sites-add"
          />
        </div>
      </FilterRow>
      <div className={LIST_WRAPPER}>
        <SiteList sites={sites} />
      </div>
      <SiteModals modals={modals} sites={sites} />
    </>
  );
};

export default FleetSitesPage;
