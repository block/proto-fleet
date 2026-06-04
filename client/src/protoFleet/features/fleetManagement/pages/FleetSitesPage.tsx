import { type ReactNode, useMemo } from "react";

import FilterRow from "../components/FilterRow";
import { useFleetOutletContext } from "../components/FleetLayout";
import SiteList from "../components/SiteList";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import SitesEmptyState from "@/protoFleet/features/sites/components/SitesEmptyState";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import { useHasPermission } from "@/protoFleet/store";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

const LIST_WRAPPER = "pt-6";

const FleetSitesPage = () => {
  const { sites, sitesError, sitesLoaded, refetchSites } = useFleetOutletContext();

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // CreateSite + UpdateSite require site:manage server-side.
  const canManageSites = useHasPermission("site:manage");

  const modals = useSiteModals({ refetchSites });

  if (sites === undefined) {
    return (
      <FilterRow>
        <div className="text-300 text-text-primary-70">Loading…</div>
      </FilterRow>
    );
  }

  // Full-page error only when the initial call never succeeded; later
  // failures surface inline so last-good content stays visible.
  if (sitesError && !sitesLoaded) {
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

  const inlineError =
    sitesError && sitesLoaded ? (
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
    ) : null;

  // Empty state always wins over the picker branches below: after the
  // last site is deleted the stale "site"-kind picker can't reset
  // (useActiveSite skips its validator when knownSiteIds is empty), and
  // the operator still needs the create CTA.
  if (sites.length === 0) {
    return (
      <>
        <FilterRow testId="fleet-sites-page">
          {inlineError}
          <SitesEmptyState onAddSite={canManageSites ? modals.openCreate : undefined} />
        </FilterRow>
        <SiteModals modals={modals} sites={sites} />
      </>
    );
  }

  // Transitional placeholder while FleetLayout's redirect effect fires —
  // avoids briefly showing the All-Sites list under a single-site picker.
  if (activeSite.kind === "site") {
    return (
      <>
        <FilterRow testId="fleet-sites-page">
          {inlineError}
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
          {inlineError}
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

  const addSiteButton: ReactNode = canManageSites ? (
    <div className="flex items-center justify-end">
      <Button
        variant={variants.secondary}
        size={sizes.compact}
        text="Add site"
        onClick={modals.openCreate}
        testId="fleet-sites-add"
      />
    </div>
  ) : null;

  return (
    <>
      <FilterRow testId="fleet-sites-page">
        {inlineError}
        {addSiteButton}
      </FilterRow>
      <div className={LIST_WRAPPER}>
        <SiteList sites={sites} />
      </div>
      <SiteModals modals={modals} sites={sites} />
    </>
  );
};

export default FleetSitesPage;
