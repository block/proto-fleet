import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

import FilterRow from "../components/FilterRow";
import FleetGroupListActionBar from "../components/FleetGroupActionsMenu/FleetGroupListActionBar";
import { useFleetOutletContext } from "../components/FleetLayout";
import SiteList from "../components/SiteList";
import { buildKnownSiteIds, useSites } from "@/protoFleet/api/sites";
import { issueOptions } from "@/protoFleet/components/DeviceSetList";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import NullState from "@/protoFleet/components/NullState";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import {
  FILTER_URL_PARAM_KEYS,
  fleetListTelemetryRangesFromURL,
  issueComponentTypesFromURL,
  parseUrlToActiveFilters,
  setTelemetryNumericFilterURLParams,
} from "@/protoFleet/features/fleetManagement/utils/filterUrlParams";
import {
  TELEMETRY_FILTER_BOUNDS,
  TELEMETRY_FILTER_KEYS,
  type TelemetryFilterKey,
} from "@/protoFleet/features/fleetManagement/utils/telemetryFilterBounds";
import SiteModals from "@/protoFleet/features/sites/components/SiteModals";
import { useSiteModals } from "@/protoFleet/features/sites/hooks/useSiteModals";
import { useHasPermission } from "@/protoFleet/store";
import { Alert, Site } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import FilterChipsBar, { type FilterChipsBarNumericFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import { usePoll } from "@/shared/hooks/usePoll";
import type { NumericRangeValue } from "@/shared/utils/filterValidation";

const LIST_WRAPPER = "pt-6";

const TELEMETRY_FILTER_CHIPS: FilterChipsBarNumericFilter[] = TELEMETRY_FILTER_KEYS.map((key) => ({
  key,
  title: TELEMETRY_FILTER_BOUNDS[key].label,
  bounds: TELEMETRY_FILTER_BOUNDS[key],
}));

const FleetSitesPage = () => {
  const { sites, sitesError, sitesLoaded, refetchSites } = useFleetOutletContext();
  const { listSites } = useSites();
  const [searchParams, setSearchParams] = useSearchParams();
  const errorComponentTypes = useMemo(() => issueComponentTypesFromURL(searchParams), [searchParams]);
  const telemetryRanges = useMemo(() => fleetListTelemetryRangesFromURL(searchParams), [searchParams]);
  const selectedNumericValues = useMemo(() => parseUrlToActiveFilters(searchParams).numericFilters, [searchParams]);
  const selectedIssues = useMemo(
    () =>
      Array.from(
        new Set(
          searchParams
            .getAll("issues")
            .map((v) => v.trim())
            .filter(Boolean),
        ),
      ),
    [searchParams],
  );
  const hasListFilters = errorComponentTypes.length > 0 || telemetryRanges.length > 0;
  const [filteredSites, setFilteredSites] = useState<typeof sites>(undefined);
  const [filteredSitesError, setFilteredSitesError] = useState<string | null>(null);
  const [filteredSitesLoaded, setFilteredSitesLoaded] = useState(false);
  const [selectedSiteIds, setSelectedSiteIds] = useState<string[]>([]);
  const [isBulkActionBusy, setIsBulkActionBusy] = useState(false);

  const knownSiteIds = useMemo(() => (sitesLoaded ? buildKnownSiteIds(sites) : undefined), [sites, sitesLoaded]);
  const { activeSite } = useActiveSite({ knownSiteIds });
  // Filtered sites use the same site:read gate as FleetLayout's unfiltered
  // cache, but keep their own cache because the request shape is URL-driven.
  const canReadSites = useHasPermission("site:read");
  // CreateSite + UpdateSite require site:manage server-side.
  const canManageSites = useHasPermission("site:manage");

  const filteredSitesKey = useMemo(() => {
    const telemetryKey = telemetryRanges
      .map(
        (range) =>
          `${range.field}:${range.min ?? ""}:${range.max ?? ""}:${range.minInclusive ? "1" : "0"}:${range.maxInclusive ? "1" : "0"}`,
      )
      .join(",");
    return [errorComponentTypes.join(","), telemetryKey].join("|");
  }, [errorComponentTypes, telemetryRanges]);

  const filteredSitesKeyRef = useRef(filteredSitesKey);
  const filteredSitesRequestIdRef = useRef(0);
  useEffect(() => {
    filteredSitesKeyRef.current = filteredSitesKey;
  }, [filteredSitesKey]);

  const fetchFilteredSites = useCallback(() => {
    const requestedFilterKey = filteredSitesKey;
    const requestId = ++filteredSitesRequestIdRef.current;
    return listSites({
      errorComponentTypes,
      telemetryRanges,
      onSuccess: (rows) => {
        if (requestId !== filteredSitesRequestIdRef.current || filteredSitesKeyRef.current !== requestedFilterKey) {
          return;
        }
        setFilteredSites(rows);
        setFilteredSitesError(null);
        setFilteredSitesLoaded(true);
      },
      onError: (msg) => {
        if (requestId !== filteredSitesRequestIdRef.current || filteredSitesKeyRef.current !== requestedFilterKey) {
          return;
        }
        setFilteredSitesError(msg);
        setFilteredSites((prev) => prev ?? []);
      },
    });
  }, [listSites, errorComponentTypes, telemetryRanges, filteredSitesKey]);

  const previousFilteredSitesKey = useRef(filteredSitesKey);
  useEffect(() => {
    if (!hasListFilters) {
      previousFilteredSitesKey.current = filteredSitesKey;
      /* eslint-disable react-hooks/set-state-in-effect -- clear the filtered-only cache when URL list filters are removed */
      setFilteredSites(undefined);
      setFilteredSitesError(null);
      setFilteredSitesLoaded(false);
      setSelectedSiteIds([]);
      /* eslint-enable react-hooks/set-state-in-effect */
      return;
    }
    if (previousFilteredSitesKey.current === filteredSitesKey) return;
    previousFilteredSitesKey.current = filteredSitesKey;
    /* eslint-disable react-hooks/set-state-in-effect -- hide stale filtered rows while the new filtered poll request loads */
    setFilteredSites(undefined);
    setFilteredSitesError(null);
    setFilteredSitesLoaded(false);
    setSelectedSiteIds([]);
    /* eslint-enable react-hooks/set-state-in-effect */
  }, [hasListFilters, filteredSitesKey]);

  usePoll({
    fetchData: fetchFilteredSites,
    params: filteredSitesKey,
    poll: true,
    pollIntervalMs: POLL_INTERVAL_MS,
    enabled: canReadSites && hasListFilters,
  });

  const handleModalRefreshSites = useCallback(() => {
    refetchSites();
    if (hasListFilters) void fetchFilteredSites();
  }, [fetchFilteredSites, hasListFilters, refetchSites]);
  const modals = useSiteModals({ refetchSites: handleModalRefreshSites });

  const displaySites = hasListFilters ? filteredSites : sites;
  const displaySitesError = hasListFilters ? filteredSitesError : sitesError;
  const displaySitesLoaded = hasListFilters ? filteredSitesLoaded : sitesLoaded;
  const handleRetrySites = useCallback(() => {
    if (hasListFilters) {
      void fetchFilteredSites();
      return;
    }
    refetchSites();
  }, [fetchFilteredSites, hasListFilters, refetchSites]);
  const visibleSiteScopes = useMemo(
    () =>
      displaySites?.flatMap((site) => {
        if (!site.site || site.site.id === 0n) return [];
        return [{ kind: "site" as const, id: site.site.id, name: site.site.name }];
      }) ?? [],
    [displaySites],
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

  const handleClearFilters = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        FILTER_URL_PARAM_KEYS.forEach((key) => next.delete(key));
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const filterChipsBarFilters = useMemo(
    () => [
      {
        key: "issues",
        title: "Issues",
        pluralTitle: "issues",
        options: issueOptions,
        selectedValues: selectedIssues,
        showGroupDivider: true,
      },
    ],
    [selectedIssues],
  );

  const handleFilterChange = useCallback(
    (key: string, values: string[]) => {
      if (key !== "issues") return;
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete("issues");
          values.forEach((value) => {
            const trimmed = value.trim();
            if (trimmed) next.append("issues", trimmed);
          });
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const handleNumericFilterChange = useCallback(
    (key: string, value: NumericRangeValue) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          setTelemetryNumericFilterURLParams(next, key as TelemetryFilterKey, value);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  if (displaySites === undefined) {
    return (
      <FilterRow>
        <div className="text-300 text-text-primary-70">Loading…</div>
      </FilterRow>
    );
  }

  // Full-page error only when the initial call never succeeded; later
  // failures surface inline so last-good content stays visible.
  if (displaySitesError && !displaySitesLoaded) {
    return (
      <FilterRow testId="fleet-sites-error">
        <Header title="Couldn't load sites" titleSize="text-heading-200" />
        <p className="text-300 text-text-primary-70">{displaySitesError}</p>
        <Button
          variant={variants.secondary}
          size={sizes.compact}
          text="Retry"
          onClick={handleRetrySites}
          testId="fleet-sites-retry"
        />
      </FilterRow>
    );
  }

  const inlineError =
    displaySitesError && displaySitesLoaded ? (
      <Callout
        intent="danger"
        prefixIcon={<Alert />}
        title="Couldn't refresh sites"
        subtitle={displaySitesError}
        buttonText="Retry"
        buttonOnClick={handleRetrySites}
        testId="fleet-sites-inline-error"
      />
    ) : null;

  const addSiteButton: ReactNode = canManageSites ? (
    <Button
      variant={variants.secondary}
      size={sizes.compact}
      text="Add site"
      onClick={modals.openCreate}
      testId="fleet-sites-add"
    />
  ) : null;

  const filterControls = (
    <div className="flex flex-row flex-wrap items-center gap-2">
      <FilterChipsBar
        filters={filterChipsBarFilters}
        onChange={handleFilterChange}
        numericFilters={TELEMETRY_FILTER_CHIPS}
        selectedNumericValues={selectedNumericValues}
        onNumericChange={handleNumericFilterChange}
        onClearAll={handleClearFilters}
      />
      <div className="ml-auto">{addSiteButton}</div>
    </div>
  );

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
  if (!hasListFilters && displaySites.length === 0) {
    pageContent = (
      <>
        {inlineError}
        <NullState
          icon={<Site width="w-5" />}
          title="No sites yet"
          description="Create your first site to organize miners by location."
          action={
            canManageSites ? (
              <Button variant={variants.primary} onClick={modals.openCreate} text="Add a site" />
            ) : undefined
          }
          testId="fleet-sites-page"
        />
      </>
    );
  } else if (hasListFilters && displaySites.length === 0) {
    pageContent = (
      <FilterRow testId="fleet-sites-page">
        {inlineError}
        {filterControls}
        <NoFilterResultsEmptyState hasActiveFilters onClearFilters={handleClearFilters} />
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
          {filterControls}
        </FilterRow>
        <div className={LIST_WRAPPER}>
          <SiteList
            sites={displaySites}
            totalUnfiltered={sites?.length}
            hasActiveFilters={hasListFilters}
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
      <SiteModals modals={modals} sites={sites ?? []} />
    </>
  );
};

export default FleetSitesPage;
