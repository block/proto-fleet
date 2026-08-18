import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Navigate } from "react-router-dom";
import { create } from "@bufbuild/protobuf";

import { ActivityFilterSchema } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { useActivity } from "@/protoFleet/api/useActivity";
import { useActivityFilterOptions } from "@/protoFleet/api/useActivityFilterOptions";
import { useExportActivity } from "@/protoFleet/api/useExportActivity";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import { siteFilterFromActive, useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import ActivityFilters from "@/protoFleet/features/activity/components/ActivityFilters";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import {
  activityEntryFromAlert,
  ALERT_TYPE_OPTION,
  alertsMatchFilter,
  mergeAlertEntries,
} from "@/protoFleet/features/activity/utils/alertEntries";
import { useAlertsEnabledState } from "@/protoFleet/features/alerts/api/useAlertsEnabled";
import { EMPTY_PAGED_ALERTS, usePagedAlerts } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import { useHasPermission } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { debounce } from "@/shared/utils/utility";

const PAGE_SIZE = 50;

const ActivityPageContent = () => {
  const [searchText, setSearchText] = useState("");
  const [debouncedSearchText, setDebouncedSearchText] = useState("");
  const [selectedTypes, setSelectedTypes] = useState<string[]>([]);
  const [selectedScopes, setSelectedScopes] = useState<string[]>([]);
  const [selectedUsers, setSelectedUsers] = useState<string[]>([]);

  // Path scope (/{site}/activity) → server-side site_ids / include_unassigned,
  // the same additive filter ListBuildings / ListRacks / ListMiners use. The
  // route segment is the source of truth for the active site, so we only read
  // it here — the globally-mounted SitePicker already fetches ListSites and
  // owns the knownSiteIds staleness validation (resetting a deleted/inaccessible
  // site back to all-sites), so this page does not re-fetch sites. Activity has
  // no `?site=` deep-link facet, so the scope filter is passed straight through
  // (no intersectSiteFilters). `/activity` resolves to { kind: "all" } → both
  // empty → org-wide feed, unchanged from before.
  const { activeSite } = useActiveSite({});
  const scopeFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite]);

  const debouncedSetSearch = useMemo(() => debounce((text: string) => setDebouncedSearchText(text), 300), []);
  useEffect(() => () => debouncedSetSearch.cancel(), [debouncedSetSearch]);

  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchText(value);
      if (value === "") {
        debouncedSetSearch.cancel();
        setDebouncedSearchText("");
      } else {
        debouncedSetSearch(value);
      }
    },
    [debouncedSetSearch],
  );

  const filter = useMemo(
    () =>
      create(ActivityFilterSchema, {
        eventTypes: selectedTypes,
        scopeTypes: selectedScopes,
        userIds: selectedUsers,
        searchText: debouncedSearchText,
        siteIds: scopeFilter.siteIds,
        includeUnassigned: scopeFilter.includeUnassigned,
      }),
    [selectedTypes, selectedScopes, selectedUsers, debouncedSearchText, scopeFilter],
  );

  // Each feed is fetched only under its own read permission, mirroring the server's per-RPC gates,
  // so an alert:read-only viewer still gets the history feed without firing denied activity RPCs.
  const canReadActivity = useHasPermission("activity:read");
  const { activities, totalCount, isLoading, error, hasMore, loadMore } = useActivity({
    filter,
    pageSize: PAGE_SIZE,
    enabled: canReadActivity,
  });
  const { exportCsv, isExportingCsv } = useExportActivity();
  const { eventTypes, scopeTypes, users } = useActivityFilterOptions({ enabled: canReadActivity });

  // Alert history is org-scoped (no site filter on ListAlerts) and gated behind its own permission, so the
  // merged feed only carries alerts on the org-wide route for viewers the alerts feature is on for.
  const canReadAlerts = useHasPermission("alert:read");
  const { enabled: alertsEnabled, resolved: alertsProbeResolved } = useAlertsEnabledState();
  const alertsAvailable = canReadAlerts && alertsEnabled;
  const orgWideScope = activeSite.kind === "all";
  const canViewAlerts = alertsAvailable && orgWideScope;
  const includeAlerts = canViewAlerts && alertsMatchFilter(filter);

  // Fetch on the stable gate so filter toggles hide/show loaded alerts instead of refetching them.
  const alertFeed = usePagedAlerts({}, "Failed to load alert history", { enabled: canViewAlerts });
  const alerts = includeAlerts ? alertFeed : EMPTY_PAGED_ALERTS;
  const alertEntries = useMemo(() => alerts.items.map(activityEntryFromAlert), [alerts.items]);
  const entries = useMemo(
    () => mergeAlertEntries(activities, hasMore, alertEntries, alerts.hasMore),
    [activities, hasMore, alertEntries, alerts.hasMore],
  );
  const typeOptions = useMemo(
    () => (canViewAlerts ? [...eventTypes, ALERT_TYPE_OPTION] : eventTypes),
    [canViewAlerts, eventTypes],
  );
  // A denied org-scoped read (alert:read revoked mid-session) degrades to an empty feed — the hook
  // already cleared its rows and cursor — but only when the activity feed remains as a fallback;
  // an alert-only viewer gets the access error, not a page that claims there is no activity.
  const feedError = error ?? (alerts.denied && canReadActivity ? null : alerts.error);
  const feedHasMore = hasMore || alerts.hasMore;
  const feedLoading = isLoading || alerts.loading;
  // Both loadMore calls no-op internally when their feed has nothing to page or is already loading.
  const loadMoreFeed = () => {
    loadMore();
    alerts.loadMore();
  };

  const hasStartedLoadingRef = useRef(false);
  const hasLoadedRef = useRef(false);
  useEffect(() => {
    if (feedLoading) {
      hasStartedLoadingRef.current = true;
    } else if (hasStartedLoadingRef.current) {
      hasLoadedRef.current = true;
    }
  }, [feedLoading]);

  const isInitialLoad = feedLoading && entries.length === 0 && !hasLoadedRef.current;
  const isLoadingMore = feedLoading && entries.length > 0;

  const hasActiveFilters =
    selectedTypes.length > 0 || selectedScopes.length > 0 || selectedUsers.length > 0 || debouncedSearchText !== "";

  const handleClearFilters = useCallback(() => {
    setSearchText("");
    setDebouncedSearchText("");
    debouncedSetSearch.cancel();
    setSelectedTypes([]);
    setSelectedScopes([]);
    setSelectedUsers([]);
  }, [debouncedSetSearch]);

  // An alert-only viewer on a deployment without the alerts feature has no feed at all; say so rather
  // than presenting a permanently empty table — but until the probe answers, "not enabled" only means
  // "unknown", so show loading, not a false claim.
  const noFeed = !canReadActivity && !alertsEnabled;
  if (noFeed && alertsProbeResolved) {
    return (
      <div className="flex h-full items-center justify-center p-10 text-center text-text-primary-50">
        Alert history isn't available because alerts aren't enabled on this server.
      </div>
    );
  }

  if (noFeed || isInitialLoad) {
    return (
      <div className="flex h-full items-center justify-center">
        <ProgressCircular indeterminate />
      </div>
    );
  }

  return (
    <>
      <div className="sticky left-0 z-3 px-6 pt-6 laptop:px-10 laptop:pt-10">
        <div className="pb-4">
          <Header title="Activity" titleSize="text-heading-300" />
        </div>
        <div className="pb-6">
          <ActivityFilters
            searchValue={searchText}
            onSearchChange={handleSearchChange}
            hideSearch={!canReadActivity}
            eventTypes={typeOptions}
            scopeTypes={scopeTypes}
            users={users}
            selectedTypes={selectedTypes}
            selectedScopes={selectedScopes}
            selectedUsers={selectedUsers}
            onTypesChange={setSelectedTypes}
            onScopesChange={setSelectedScopes}
            onUsersChange={setSelectedUsers}
            actions={
              canReadActivity ? (
                <Button
                  variant={variants.secondary}
                  size={sizes.compact}
                  onClick={() => exportCsv(filter)}
                  loading={isExportingCsv}
                  disabled={isExportingCsv || totalCount === 0}
                >
                  Export activity CSV
                </Button>
              ) : undefined
            }
          />
        </div>
        {alertsAvailable && !orgWideScope ? (
          <p className="pb-4 text-200 text-text-primary-50">
            Alert history is organization-wide and appears only on the all-sites activity feed.
          </p>
        ) : null}
      </div>

      {feedError ? (
        <Callout className="mx-6 mb-4 laptop:mx-10" intent="danger" prefixIcon={<Alert />} title={feedError} />
      ) : null}

      <div className="p-6 pt-0 laptop:p-10 laptop:pt-0">
        <ActivityTable
          activities={entries}
          noDataElement={
            feedLoading ? (
              <></>
            ) : hasActiveFilters ? (
              <NoFilterResultsEmptyState hasActiveFilters onClearFilters={handleClearFilters} />
            ) : undefined
          }
        />
        {feedHasMore ? (
          <div className="flex justify-center py-6">
            <Button
              variant={variants.secondary}
              size={sizes.compact}
              onClick={loadMoreFeed}
              loading={isLoadingMore}
              disabled={isLoadingMore}
            >
              Load more
            </Button>
          </div>
        ) : null}
      </div>
    </>
  );
};

const ActivityPage = () => {
  const canReadActivity = useHasPermission("activity:read");
  const canReadAlerts = useHasPermission("alert:read");

  // Either read grants a feed on this page; the content gates each fetch on its own permission.
  if (!canReadActivity && !canReadAlerts) {
    return <Navigate to="/" replace />;
  }

  return <ActivityPageContent />;
};

export default ActivityPage;
