import { useCallback, useEffect, useState } from "react";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { isStaleAssetSelection } from "../assetSelection";
import { useAccountQuery } from "../useAccountQuery";
import { assetName } from "./accountAssetColumns";
import { ActionModal } from "./ActionModal";
import type { AccountAsset, ListAccountAssetsResponse } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import Search from "@/shared/components/Search";
import Select from "@/shared/components/Select";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const SEARCH_DEBOUNCE_MS = 250;

export const ReplaceHashboardDialog = ({
  oldBoard,
  onDismiss,
  onReplaced,
  onConflict,
}: {
  oldBoard: AccountAsset;
  onDismiss: () => void;
  onReplaced: () => void;
  onConflict: () => void;
}) => {
  const { account, client, readOnly } = useRequiredAccountScope();
  const [search, setSearch] = useState("");
  const [debouncedSearch, setDebouncedSearch] = useState("");
  const [pageToken, setPageToken] = useState("");
  const [loaded, setLoaded] = useState<{ page: ListAccountAssetsResponse; assets: AccountAsset[] } | null>(null);
  const [replacement, setReplacement] = useState<AccountAsset | null>(null);
  const searchPending = debouncedSearch !== search;
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search), SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [search]);
  const load = useCallback(
    (signal: AbortSignal) =>
      client.listAccountAssets(
        {
          accountId: account.accountId,
          ownerAccountId: account.accountId,
          hashboardsOnly: true,
          replacementFor: { id: oldBoard.id, expectedRevision: oldBoard.revision },
          pageSize: 50,
          pageToken,
          search: debouncedSearch.trim(),
        },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
      ),
    [account.accountId, client, pageToken, debouncedSearch, oldBoard.id, oldBoard.revision],
  );
  const query = useAccountQuery(load, !searchPending);
  if (query.data && query.data !== loaded?.page) {
    const previousAssets = pageToken ? (loaded?.assets ?? []) : [];
    const assetsById = new Map(previousAssets.map((board) => [board.id, board]));
    for (const board of query.data.assets) assetsById.set(board.id, board);
    setLoaded({ page: query.data, assets: Array.from(assetsById.values()) });
  }
  const visibleAssets = searchPending || query.error ? [] : (loaded?.assets ?? []);
  const candidates = visibleAssets.filter(
    (board) => board.available && !board.outsidePolicy && board.id !== oldBoard.id,
  );
  const canReplace = replacement !== null && !readOnly && !searchPending && query.error === null;
  const loadMore = useCallback(() => {
    if (!query.loading && !query.error && query.data?.nextPageToken) setPageToken(query.data.nextPageToken);
  }, [query.loading, query.error, query.data]);
  return (
    <ActionModal
      presentation="dialog"
      title="Replace hashboard"
      confirm="Transfer allocation"
      valid={canReplace}
      onDismiss={onDismiss}
      onSubmit={async () => {
        if (!replacement || readOnly) return;
        try {
          await client.replaceAccountHashboard({
            accountId: account.accountId,
            oldBoard: { id: oldBoard.id, expectedRevision: oldBoard.revision },
            replacementBoard: { id: replacement.id, expectedRevision: replacement.revision },
          });
        } catch (error) {
          if (isStaleAssetSelection(error)) {
            onConflict();
            return;
          }
          throw error;
        }
        pushToast({ message: "Hashboard allocation transferred.", status: STATUSES.success });
        onReplaced();
      }}
    >
      <p>
        Transfer the allocation for {assetName(oldBoard)} to the selected replacement. The client and subclient stay the
        same. Past readings stay with the old board.
      </p>
      <Select
        id="replacement-hashboard"
        label="Replacement hashboard"
        placeholder="Select an unallocated hashboard"
        emptyMessage="No matching hashboards"
        value={replacement ? String(replacement.id) : ""}
        options={candidates.map((board) => ({ value: String(board.id), label: assetName(board) }))}
        onChange={(value) => setReplacement(candidates.find((board) => String(board.id) === value) ?? null)}
        search={
          <Search
            id="replacement-hashboard-search"
            label="Search hashboards"
            variant="toolbar"
            initValue={search}
            onChange={(value) => {
              setSearch(value);
              setReplacement(null);
              setLoaded(null);
              setPageToken("");
              if (!value) setDebouncedSearch("");
            }}
          />
        }
        isLoading={query.loading || searchPending}
        hasMore={!!query.data?.nextPageToken}
        onLoadMore={loadMore}
        loadError={query.error}
        onRetry={query.refresh}
      />
    </ActionModal>
  );
};
