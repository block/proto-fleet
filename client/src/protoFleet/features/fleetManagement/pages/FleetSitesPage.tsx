import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";

import FilterRow from "../components/FilterRow";
import FleetGroupListActionBar from "../components/FleetGroupActionsMenu/FleetGroupListActionBar";
import { useFleetOutletContext } from "../components/FleetLayout";
import SiteList from "../components/SiteList";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import SitesEmptyState from "@/protoFleet/features/sites/components/SitesEmptyState";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";

const LIST_WRAPPER = "pt-6";

const FleetSitesPage = () => {
  const { sites, sitesError, sitesLoaded, refetchSites } = useFleetOutletContext();
  const [selectedSiteIds, setSelectedSiteIds] = useState<string[]>([]);
  const [isBulkActionBusy, setIsBulkActionBusy] = useState(false);

  const knownSiteIds = useMemo(() => (sitesLoaded ? buildKnownSiteIds(sites) : undefined), [sites, sitesLoaded]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // CreateSite + UpdateSite require site:manage server-side.
  const canManageSites = useHasPermission("site:manage");

  const modals = useSiteModals({ refetchSites });
  const visibleSiteScopes = useMemo(
    () =>
      sites?.flatMap((site) => {
        if (!site.site || site.site.id === 0n) return [];
        return [{ kind: "site" as const, id: site.site.id, name: site.site.name }];
      }) ?? [],
    [sites],
  );
  const selectedSiteScopes = useMemo(() => {
    const selected = new Set(selectedSiteIds);
    return visibleSiteScopes.filter((site) => selected.has(site.id.toString()));
  }, [selectedSiteIds, visibleSiteScopes]);
  useEffect(() => {
    const visible =
      activeSite.kind === "all" ? new Set(visibleSiteScopes.map((site) => site.id.toString())) : new Set<string>();
    // Keep selection scoped to the active SitePicker branch, including
    // branches below that unmount SiteList.
    // eslint-disable-next-line react-hooks/set-state-in-effect -- selection mirrors externally controlled visible rows.
    setSelectedSiteIds((prev) => {
      const next = prev.filter((id) => visible.has(id));
      return next.length === prev.length ? prev : next;
    });
  }, [activeSite.kind, visibleSiteScopes]);
  const handleSelectAllVisibleSites = useCallback(
    () => setSelectedSiteIds(visibleSiteScopes.map((site) => site.id.toString())),
    [visibleSiteScopes],
  );
  const handleClearSiteSelection = useCallback(() => setSelectedSiteIds([]), []);
  const handleSelectedSiteIdsChange = useCallback(
    (ids: string[]) => {
      if (isBulkActionBusy) return;
      setSelectedSiteIds(ids);
    },
    [isBulkActionBusy],
  );

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
      <Callout
        intent="danger"
        prefixIcon={<Alert />}
        title="Couldn't refresh sites"
        subtitle={sitesError}
        buttonText="Retry"
        buttonOnClick={refetchSites}
        testId="fleet-sites-inline-error"
      />
    ) : null;

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

  const bulkActionBar =
    selectedSiteScopes.length > 0 || isBulkActionBusy ? (
      <FleetGroupListActionBar
        selectedScopes={selectedSiteScopes}
        kind="site"
        onClearSelection={handleClearSiteSelection}
        onSelectAllVisible={handleSelectAllVisibleSites}
        onActionBusyChange={setIsBulkActionBusy}
      />
    ) : null;

  let pageContent: ReactNode;
  // Empty state always wins over the picker branches below: after the
  // last site is deleted the stale "site"-kind picker can't reset
  // (useActiveSite skips its validator when knownSiteIds is empty), and
  // the operator still needs the create CTA.
  if (sites.length === 0) {
    pageContent = (
      <FilterRow testId="fleet-sites-page">
        {inlineError}
        <SitesEmptyState onAddSite={canManageSites ? modals.openCreate : undefined} />
      </FilterRow>
    );
  } else if (activeSite.kind === "site") {
    // Transitional placeholder while FleetLayout's redirect effect fires —
    // avoids briefly showing the All-Sites list under a single-site picker.
    pageContent = (
      <FilterRow testId="fleet-sites-page">
        {inlineError}
        <div className="text-300 text-text-primary-70" data-testid="fleet-sites-redirecting">
          Loading…
        </div>
      </FilterRow>
    );
  } else if (activeSite.kind === "unassigned") {
    pageContent = (
      <FilterRow testId="fleet-sites-page">
        {inlineError}
        <div
          className="rounded-xl border border-dashed border-border-5 p-6 text-center text-300 text-text-primary-70"
          data-testid="fleet-sites-unassigned-note"
        >
          &quot;Unassigned&quot; filters miners, not sites. Switch the picker to All Sites to see every site.
        </div>
      </FilterRow>
    );
  } else {
    pageContent = (
      <>
        <FilterRow testId="fleet-sites-page">
          {inlineError}
          {addSiteButton}
        </FilterRow>
        <div className={LIST_WRAPPER}>
          <SiteList
            sites={sites}
            onEditSite={canManageSites ? modals.openManageEdit : undefined}
            selectedIds={selectedSiteIds}
            onSelectedIdsChange={handleSelectedSiteIdsChange}
          />
        </div>
      </>
    );
  }

  return (
    <>
      {pageContent}
      {bulkActionBar}
      <SiteModals modals={modals} sites={sites} />
    </>
  );
};

export default FleetSitesPage;
