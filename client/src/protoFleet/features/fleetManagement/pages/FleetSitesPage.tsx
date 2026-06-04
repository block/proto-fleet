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

// Each layout element owns its own top padding so callers don't have to
// reason about combined gaps:
//   - Heading + tab strip: pt-6 (FleetLayout)
//   - FilterRow: pt-10
//   - List: pt-6 (LIST_WRAPPER)
const LIST_WRAPPER = "pt-6";

const FleetSitesPage = () => {
  const { sites, sitesError, sitesLoaded, refetchSites } = useFleetOutletContext();

  const knownSiteIds = useMemo(() => buildKnownSiteIds(sites), [sites]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // CreateSite is gated on `site:manage` server-side; hide the CTAs for
  // read-only roles so they don't fill out a modal that fails on submit.
  const canManageSites = useHasPermission("site:manage");

  const modals = useSiteModals({ refetchSites });

  // Initial-load gate: sites is `undefined` until the first response lands
  // (success or failure). The error path inside FleetLayout sets sites=[]
  // on any failure, so this branch only fires while the very first call is
  // in flight.
  if (sites === undefined) {
    return (
      <FilterRow>
        <div className="text-300 text-text-primary-70">Loading…</div>
      </FilterRow>
    );
  }

  // Full-page error only when the initial listSites call never succeeded.
  // After any successful response, sitesLoaded stays true through later
  // poll/retry failures so zero-site orgs keep their empty-state CTA and
  // populated orgs keep their last-good list, with a transient inline
  // error banner above the body.
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

  // Inline banner reused across the empty-state, unassigned-note, and
  // populated-list branches when a post-success refetch fails.
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

  // Empty state takes precedence over any picker state. After the last
  // site is deleted the stale "site"-kind picker selection can't reset
  // (useActiveSite skips its validator when knownSiteIds is empty), but
  // the operator still needs to see the create CTA — see codex thread
  // on this branch.
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

  // FleetLayout normally hides + redirects away from /fleet/sites when the
  // picker resolves to an existing single site (J2), but the redirect
  // effect needs a render to fire. Show a transitional placeholder rather
  // than the All-Sites list. Only meaningful when sites is non-empty —
  // empty-sites + stale "site" picker hits the branch above.
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
