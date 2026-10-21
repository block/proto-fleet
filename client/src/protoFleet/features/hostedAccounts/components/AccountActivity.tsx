import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import {
  type ActivityEntry,
  type ActivityFilter,
  ActivityFilterSchema,
} from "@/protoFleet/api/generated/activity/v1/activity_pb";
import type { ListAccountActivitiesResponse } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { isAuthOrPermissionError } from "@/protoFleet/api/requestErrors";
import NoFilterResultsEmptyState from "@/protoFleet/components/NoFilterResultsEmptyState";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import ActivityFilters from "@/protoFleet/features/activity/components/ActivityFilters";
import ActivityTable from "@/protoFleet/features/activity/components/ActivityTable";
import Button, { sizes } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { downloadBlob, getFileName } from "@/shared/utils/utility";

const PAGE_SIZE = 50;

type FilterControls = Omit<Parameters<typeof ActivityFilters>[0], "eventTypes" | "scopeTypes" | "users" | "actions">;

const AccountActivityFeed = ({
  filter,
  clearFilters,
  controls,
}: {
  filter: ActivityFilter;
  clearFilters: () => void;
  controls: FilterControls;
}) => {
  const scope = useRequiredAccountScope();
  const { account, client, invalidateAccount } = scope;
  const [pageCount, setPageCount] = useState(1);
  const [activeFilter, setActiveFilter] = useState(filter);
  const [authority, setAuthority] = useState("");
  const [retained, setRetained] = useState<ListAccountActivitiesResponse | null>(null);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState<string | null>(null);
  const lifetime = useRef<AbortController | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => controller.abort();
  }, [filter, authority]);
  const load = useCallback(
    async (signal: AbortSignal): Promise<ListAccountActivitiesResponse & { restart?: boolean }> => {
      const request = { accountId: account.accountId, filter, pageSize: PAGE_SIZE };
      const first = await client.listAccountActivities(request, { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal });
      const activities: ActivityEntry[] = [...first.activities];
      let nextPageToken = first.nextPageToken;
      for (let index = 1; index < pageCount && nextPageToken; index++) {
        const page = await client.listAccountActivities(
          { ...request, pageToken: nextPageToken },
          { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
        );
        if (page.authorityFingerprint !== first.authorityFingerprint) return { ...page, activities: [], restart: true };
        activities.push(...page.activities);
        nextPageToken = page.nextPageToken;
      }
      return { ...first, activities, nextPageToken };
    },
    [account.accountId, client, filter, pageCount],
  );
  const query = useAccountQuery(load, true, true, POLL_INTERVAL_MS);
  const denied = query.errorCode === Code.PermissionDenied || query.errorCode === Code.Unauthenticated;
  const received = query.data;
  if (activeFilter !== filter || (received?.restart && pageCount > 1)) {
    setActiveFilter(filter);
    setPageCount(1);
    setRetained(null);
    setExportError(null);
    setExporting(false);
  }
  if (received && authority !== received.authorityFingerprint) {
    setAuthority(received.authorityFingerprint);
    setRetained(null);
    setExportError(null);
    setExporting(false);
  }
  const page = denied || received?.restart ? null : (received ?? retained);
  useEffect(() => {
    window.addEventListener("focus", query.poll);
    return () => window.removeEventListener("focus", query.poll);
  }, [query.poll]);
  const options = page?.filterOptions;
  const entries = page?.activities ?? [];
  const exportCsv = async () => {
    if (exporting) return;
    setExporting(true);
    setExportError(null);
    const signal = AbortSignal.any([scope.signal, lifetime.current!.signal]);
    try {
      const chunks: Uint8Array<ArrayBuffer>[] = [];
      for await (const chunk of client.exportAccountActivities({ accountId: account.accountId, filter }, { signal })) {
        if (signal.aborted) return;
        chunks.push(new Uint8Array(chunk.chunk));
      }
      if (!signal.aborted)
        downloadBlob(new Blob(chunks, { type: "text/csv;charset=utf-8;" }), getFileName("activity-export"));
    } catch (error) {
      if (!signal.aborted) {
        if (isAuthOrPermissionError(ConnectError.from(error))) invalidateAccount();
        setExportError(getErrorMessage(error, "Failed to export activities"));
      }
    } finally {
      if (!signal.aborted) setExporting(false);
    }
  };
  const active = filter.eventTypes.length > 0 || filter.scopeTypes.length > 0 || !!filter.searchText;
  return (
    <>
      {denied ? null : (
        <div className="pb-6">
          <ActivityFilters
            {...controls}
            eventTypes={options?.eventTypes ?? []}
            scopeTypes={options?.scopeTypes ?? []}
            users={[]}
            actions={
              <Button
                variant="secondary"
                size={sizes.compact}
                onClick={exportCsv}
                loading={exporting}
                disabled={exporting || !page?.totalCount}
              >
                Export activity CSV
              </Button>
            }
          />
        </div>
      )}
      {query.error ? (
        <AccountState error={query.error} retry={query.refresh} />
      ) : exportError ? (
        <AccountState error={exportError} retry={() => void exportCsv()} />
      ) : null}
      {query.loading && entries.length === 0 ? (
        <div className="flex justify-center p-10">
          <ProgressCircular indeterminate />
        </div>
      ) : null}
      <ActivityTable
        key={authority}
        activities={entries}
        noDataElement={
          query.loading || denied ? (
            <></>
          ) : active ? (
            <NoFilterResultsEmptyState hasActiveFilters onClearFilters={clearFilters} />
          ) : undefined
        }
      />
      {page?.nextPageToken && !denied ? (
        <div className="flex justify-center py-6">
          <Button
            variant="secondary"
            size={sizes.compact}
            onClick={() => {
              setRetained(page);
              setPageCount((count) => count + 1);
            }}
            disabled={query.loading}
          >
            Load more
          </Button>
        </div>
      ) : null}
    </>
  );
};

const AccountActivity = () => {
  const [search, setSearch] = useState("");
  const [types, setTypes] = useState<string[]>([]);
  const [scopes, setScopes] = useState<string[]>([]);
  const filter = useMemo(
    () => create(ActivityFilterSchema, { searchText: search, eventTypes: types, scopeTypes: scopes }),
    [search, types, scopes],
  );
  const clearFilters = () => {
    setSearch("");
    setTypes([]);
    setScopes([]);
  };
  return (
    <>
      <div className="pb-4">
        <Header title={<h1>Activity</h1>} titleSize="text-heading-300" />
      </div>
      <AccountActivityFeed
        filter={filter}
        clearFilters={clearFilters}
        controls={{
          searchValue: search,
          onSearchChange: setSearch,
          selectedTypes: types,
          selectedScopes: scopes,
          selectedUsers: [],
          onTypesChange: setTypes,
          onScopesChange: setScopes,
          onUsersChange: () => {},
        }}
      />
    </>
  );
};
export default AccountActivity;
