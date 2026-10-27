import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";
import clsx from "clsx";
import { Code, ConnectError } from "@connectrpc/connect";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { accountPath, can } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { defaultAssetColumns, useAssetColumnPreferences } from "../useAssetColumnPreferences";
import { allocationKey } from "../useFleetAccountInventory";
import { AccountAssetCell } from "./AccountAssetCell";
import { type AssetColumn, assetColumnTitles, assetColumnWidths, assetName } from "./accountAssetColumns";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { SortDirection as ApiSortDirection, SortField } from "@/protoFleet/api/generated/common/v1/sort_pb";
import {
  type AccountAsset,
  type AccountMiningBucket,
  AssetAssignmentIntent,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import ListPagination from "@/protoFleet/components/ListPagination";
import ListSearchInput from "@/protoFleet/components/ListSearchInput";
import { POLL_INTERVAL_MS } from "@/protoFleet/constants/polling";
import FleetListActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar/FleetListActionBar";
import {
  type MinerColumn,
  MINERS_PAGE_SIZE,
} from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import {
  getDefaultSortDirection,
  getSortField,
} from "@/protoFleet/features/fleetManagement/components/MinerList/sortConfig";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useFleetStore } from "@/protoFleet/store";
import { Slider } from "@/shared/assets/icons";
import { ActionBarSelectionControls } from "@/shared/components/ActionBar";
import Button, { sizes } from "@/shared/components/Button";
import List from "@/shared/components/List";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import { formatListCountLabel } from "@/shared/components/List/listCountLabel";
import type { ColConfig, SortDirection } from "@/shared/components/List/types";
import ManageColumnsModal from "@/shared/components/ManageColumnsModal";
import Select from "@/shared/components/Select";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";

const ASSETS_PAGE_SIZE = MINERS_PAGE_SIZE;
const sortableColumns = new Set<AssetColumn>(["name", "model", "hashrate", "efficiency", "powerUsage", "temperature"]);

const countLabel = (count: number) => formatListCountLabel(count, { singular: "asset", plural: "assets" });
type Review = { intent: AssetAssignmentIntent; childId: string; assets: AccountAsset[] };

const AccountAssets = ({
  child,
  mode = "inventory",
  onAssigned,
  initialNotice = "",
  actions,
}: {
  child?: Pick<AccountMiningBucket, "accountId" | "name" | "suspended">;
  mode?: "inventory" | "assign";
  onAssigned?: (notice: string) => void;
  initialNotice?: string;
  actions?: ReactNode;
}) => {
  const assigning = mode === "assign";
  const { account, client, readOnly = false } = useRequiredAccountScope();
  const fleetView = !child && !assigning && account.depth > 0;
  const username = useFleetStore((state) => state.auth.username);
  const columnPreferences = useAssetColumnPreferences(username, account.accountId);
  const [managingColumns, setManagingColumns] = useState(false);
  const [sort, setSort] = useState<{ field: AssetColumn; direction: SortDirection }>({
    field: "name",
    direction: "asc",
  });
  const [search, setSearch] = useState("");
  const [owner, setOwner] = useState("");
  const [model, setModel] = useState("");
  const [site, setSite] = useState({ id: "", name: "" });
  const [container, setContainer] = useState({ id: "", name: "" });
  const [pages, setPages] = useState([""]);
  const [selected, setSelected] = useState<string[]>([]);
  const [review, setReview] = useState<Review | null>(null);
  const [notice, setNotice] = useState(initialNotice);
  const assignmentPending = useRef(false);
  const pageToken = pages[pages.length - 1];
  const load = useCallback(
    async (signal: AbortSignal) => {
      const inventory = await client.listAccountAssets(
        {
          accountId: account.accountId,
          pageSize: ASSETS_PAGE_SIZE,
          pageToken,
          search: search.trim(),
          sort: {
            field: getSortField(sort.field as MinerColumn) ?? SortField.NAME,
            direction: sort.direction === "desc" ? ApiSortDirection.DESC : ApiSortDirection.ASC,
          },
          ownerAccountId: assigning ? account.accountId : child?.accountId || owner || undefined,
          containerId: container.id ? BigInt(container.id) : undefined,
          model: model || undefined,
          siteId: site.id ? BigInt(site.id) : undefined,
        },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
      );
      return { inventory };
    },
    [
      account.accountId,
      client,
      pageToken,
      search,
      owner,
      container.id,
      model,
      site.id,
      assigning,
      child?.accountId,
      sort,
    ],
  );
  const query = useAccountQuery(load, true, true);
  const { invalidate, refresh: revalidate } = query;
  const refresh = useCallback(() => {
    setSelected([]);
    setReview(null);
    invalidate();
  }, [invalidate]);
  // Returning to the window revalidates in place: an open review and the current
  // selection survive, and the server's revision check rejects stale snapshots.
  useEffect(() => {
    const refreshOnFocus = () => {
      if (!assignmentPending.current) revalidate();
    };
    const timer = window.setInterval(refreshOnFocus, POLL_INTERVAL_MS);
    window.addEventListener("focus", refreshOnFocus);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", refreshOnFocus);
    };
  }, [revalidate]);
  const rows = useMemo(
    () => (query.data?.inventory.assets ?? []).map((asset) => ({ ...asset, key: allocationKey(asset) })),
    [query.data],
  );
  const hasFilters = !!(search || owner || container.id || model || site.id);
  const toolbarNeeded = rows.length > 0 || hasFilters || pages.length > 1;
  // Clearing the last filter reloads without data; keep the toolbar mounted until that reload settles.
  const [toolbarShown, setToolbarShown] = useState({ accountId: account.accountId, shown: false });
  if (query.data && (toolbarShown.accountId !== account.accountId || toolbarShown.shown !== toolbarNeeded))
    setToolbarShown({ accountId: account.accountId, shown: toolbarNeeded });
  const showToolbar =
    toolbarNeeded || (query.loading && toolbarShown.accountId === account.accountId && toolbarShown.shown);
  const children = query.data?.inventory.children ?? [];
  const management = !!query.data?.inventory.canViewAssignments;
  const editable =
    !!query.data?.inventory.canManageAssignments && can(account, 16n) && account.depth < 2 && (assigning || !!child);
  if (!editable && (selected.length > 0 || review !== null)) {
    setSelected([]);
    setReview(null);
  }
  const isAssetSelectable = (asset: AccountAsset) =>
    !!asset.ownerAccountId &&
    (assigning
      ? asset.ownerAccountId === account.accountId && asset.available && !asset.outsidePolicy
      : asset.ownerAccountId !== account.accountId);
  // A background revalidation can retire, move or reassign selected rows.
  const liveSelection = selected.filter((key) => rows.some((row) => row.key === key && isAssetSelectable(row)));
  if (query.data && liveSelection.length !== selected.length) setSelected(liveSelection);
  const selectedAssets = rows.filter((row) => selected.includes(row.key));
  const assignable =
    selectedAssets.length > 0 &&
    selectedAssets.every(
      (asset) => asset.available && !asset.outsidePolicy && asset.ownerAccountId === account.accountId,
    );
  const recallChild = selectedAssets[0]?.ownerAccountId;
  const recallable =
    selectedAssets.length > 0 &&
    !!recallChild &&
    recallChild !== account.accountId &&
    selectedAssets.every((asset) => asset.ownerAccountId === recallChild) &&
    children.some((child) => child.accountId === recallChild);
  const hasAssignmentTarget = children.some(
    (target) => !target.suspended && (!child || target.accountId === child.accountId),
  );
  const childLabel = account.depth === 0 ? "client" : "subclient";
  let selectionHint = "";
  if (selected.length > 0) {
    if ((assigning && assignable && hasAssignmentTarget) || (!assigning && recallable)) {
      selectionHint = "";
    } else if (assigning) {
      selectionHint = hasAssignmentTarget
        ? "Only available assets held directly by this account can be allocated."
        : `No active ${childLabel}s are available to receive these assets.`;
    } else {
      selectionHint = `Select assets allocated to one ${childLabel} to recall them together.`;
    }
  }
  const filterOptions = query.data?.inventory.filterOptions;
  const containers = new Map((filterOptions?.containers ?? []).map((option) => [String(option.id), option.name]));
  if (container.id) containers.set(container.id, container.name);
  const sites = new Map((filterOptions?.sites ?? []).map((option) => [String(option.id), option.name]));
  if (site.id) sites.set(site.id, site.name);
  const models = new Set(filterOptions?.models ?? []);
  if (model) models.add(model);
  const assignmentOptions = [
    { id: account.accountId, label: account.depth === 0 ? "Available to allocate" : "Directly allocated" },
    ...children.map((child) => ({ id: child.accountId, label: child.name })),
    ...(owner && owner !== account.accountId && !children.some((child) => child.accountId === owner)
      ? [{ id: owner, label: "Unavailable allocation" }]
      : []),
  ];
  const inventoryFilters: FilterChipsBarFilter[] = [
    ...(management && !child && !assigning
      ? [{ key: "assignment", title: "Allocated to", options: assignmentOptions, selectedValues: owner ? [owner] : [] }]
      : []),
    {
      key: "model",
      title: "Model",
      options: Array.from(models, (value) => ({ id: value, label: value })),
      selectedValues: model ? [model] : [],
    },
    {
      key: "site",
      title: "Site",
      options: Array.from(sites, ([id, label]) => ({ id, label })),
      selectedValues: site.id ? [site.id] : [],
    },
    {
      key: "container",
      title: "Container",
      options: Array.from(containers, ([id, label]) => ({ id, label })),
      selectedValues: container.id ? [container.id] : [],
    },
  ];
  const resetSelection = () => {
    setPages([""]);
    setSelected([]);
    setReview(null);
    setNotice("");
  };
  const selectedInContainer = rows.filter(
    (row) => row.kind === MiningAssetKind.MODULE && String(row.containerId) === container.id && isAssetSelectable(row),
  );
  const firstItemIndex = (pages.length - 1) * ASSETS_PAGE_SIZE + 1;
  const lastItemIndex = (pages.length - 1) * ASSETS_PAGE_SIZE + rows.length;
  const reviewTarget = children.find((child) => child.accountId === review?.childId);
  const showAssignment = management && !child && !assigning;
  const configurableColumns = useMemo(
    () => columnPreferences.columns.filter((column) => column.id !== "assignedAccount" || showAssignment),
    [columnPreferences.columns, showAssignment],
  );
  const defaults = useMemo(
    () => defaultAssetColumns.filter((column) => column.id !== "assignedAccount" || showAssignment),
    [showAssignment],
  );
  const activeColumns: AssetColumn[] = [
    "name",
    ...configurableColumns.filter((column) => column.visible).map((column) => column.id),
  ];
  const columnConfig: ColConfig<(typeof rows)[number], string, AssetColumn> = {};
  for (const column of Object.keys(assetColumnTitles) as AssetColumn[]) {
    columnConfig[column] = {
      width: assetColumnWidths[column],
      component: (asset) =>
        column === "assignedAccount" ? (
          asset.ownerAccountId === account.accountId ? (
            "This account"
          ) : (
            asset.ownerAccountName || "—"
          )
        ) : column === "name" && account.depth > 0 && asset.available && !asset.outsidePolicy ? (
          <Button
            variant="textOnly"
            textColor="text-text-primary"
            className="justify-start text-left font-normal"
            textOnlyUnderlineOnHover={false}
            to={`${accountPath(account.accountId, "assets")}/${asset.kind === MiningAssetKind.MODULE ? "module" : "miner"}/${asset.id}`}
          >
            <AccountAssetCell asset={asset} column={column} />
          </Button>
        ) : (
          <AccountAssetCell asset={asset} column={column} />
        ),
    };
  }

  return (
    <div className="flex min-w-0 flex-col gap-6">
      {fleetView ? (
        <div className="flex flex-col gap-4">
          <h1 className="text-heading-300 text-text-primary">Fleet</h1>
          <TabStrip activeId="miners" onSelect={() => {}} ariaLabel="Fleet sections">
            <TabStripItem id="miners" label="Miners" />
          </TabStrip>
        </div>
      ) : !child ? (
        <SettingsPageHeader title={assigning ? "Allocate assets" : "Mining assets"} />
      ) : null}
      {assigning ? (
        <p className="text-200 text-text-primary-50">
          Choose assets held directly by {account.name} to allocate to {child?.name ?? "a subclient"}.
        </p>
      ) : !child && !fleetView ? (
        <p className="text-200 text-text-primary-50">
          {account.depth < 2
            ? "Assets allocated to this account, including its subclients."
            : "Assets allocated to this account."}
        </p>
      ) : null}
      {showToolbar || actions ? (
        <div className={clsx("flex flex-wrap items-center gap-3", fleetView && "laptop:pt-4")}>
          {showToolbar ? (
            <>
              {fleetView ? (
                <FilterChipsBar
                  filters={inventoryFilters}
                  onChange={(key, values) => {
                    resetSelection();
                    const value = values.slice(-1)[0] ?? "";
                    if (key === "assignment") setOwner(value);
                    if (key === "model") setModel(value);
                    if (key === "site") setSite({ id: value, name: sites.get(value) ?? "" });
                    if (key === "container") setContainer({ id: value, name: containers.get(value) ?? "" });
                  }}
                  onClearAll={() => {
                    resetSelection();
                    setOwner("");
                    setModel("");
                    setSite({ id: "", name: "" });
                    setContainer({ id: "", name: "" });
                  }}
                />
              ) : null}
              {!fleetView && management && !child && !assigning ? (
                <Select
                  id="account-assets-owner"
                  label="Allocation"
                  value={owner}
                  variant="filter"
                  options={[
                    { value: "", label: "All account assets" },
                    {
                      value: account.accountId,
                      label: account.depth === 0 ? "Available to allocate" : "Directly allocated",
                    },
                    ...children.map((child) => ({ value: child.accountId, label: child.name })),
                  ]}
                  onChange={(value) => {
                    resetSelection();
                    setOwner(value);
                  }}
                />
              ) : null}
              {!fleetView && containers.size ? (
                <Select
                  id="account-assets-container"
                  label="Container"
                  value={container.id}
                  variant="filter"
                  options={[
                    { value: "", label: "All containers and miners" },
                    ...Array.from(containers, ([value, label]) => ({ value, label })),
                  ]}
                  onChange={(value) => {
                    resetSelection();
                    setContainer({ id: value, name: containers.get(value) ?? "" });
                  }}
                />
              ) : null}
              {!child && !assigning && owner && owner !== account.accountId && can(account, 16n) ? (
                <Button
                  variant="secondary"
                  size={sizes.compact}
                  disabled={readOnly}
                  text="Manage subclient assets"
                  to={`${accountPath(account.accountId, "children")}?assets=${encodeURIComponent(owner)}`}
                />
              ) : null}
              <ListSearchInput
                id="account-assets-search"
                label="Search mining assets"
                className="phone:w-full"
                initialValue={search}
                onQueryInput={() => {
                  setSelected([]);
                  setReview(null);
                }}
                onQueryChange={(value) => {
                  resetSelection();
                  setSearch(value);
                }}
                collapsible
              />
              {editable && container.id ? (
                <Button
                  variant="textOnly"
                  text="Select modules on this page"
                  disabled={!selectedInContainer.length}
                  onClick={() => setSelected(selectedInContainer.map((asset) => asset.key))}
                />
              ) : null}
            </>
          ) : null}
          <div className="ml-auto flex items-center gap-2">
            {showToolbar ? (
              <Button
                ariaLabel="Manage columns"
                ariaHasPopup="dialog"
                variant="secondary"
                size={sizes.compact}
                prefixIcon={<Slider width="w-4" />}
                onClick={() => setManagingColumns(true)}
              />
            ) : null}
            {actions}
          </div>
        </div>
      ) : null}
      {notice ? (
        <p role="status" className="text-300">
          {notice}
        </p>
      ) : null}
      <AccountState loading={query.loading} error={query.error} retry={refresh} />
      {query.data ? (
        <>
          {editable && selected.length > 0 ? (
            <FleetListActionBar
              isActionBusy={review !== null}
              selectedItems={selected}
              selectionMode="subset"
              itemNoun={{ singular: "mining asset", plural: "mining assets" }}
              onClose={() => {
                setSelected([]);
                setReview(null);
              }}
              selectionControls={
                <ActionBarSelectionControls
                  visibleOnly
                  onSelectAll={() => setSelected(rows.filter(isAssetSelectable).map((asset) => asset.key))}
                  onSelectNone={() => setSelected([])}
                />
              }
            >
              <div className="flex flex-wrap items-center justify-end gap-3">
                {assigning && assignable && hasAssignmentTarget ? (
                  <Button
                    variant="secondary"
                    className="bg-grayscale-white-10! text-grayscale-white-90!"
                    disabled={readOnly}
                    text="Allocate selected"
                    onClick={() =>
                      setReview({
                        intent: AssetAssignmentIntent.ASSIGN,
                        childId: child?.accountId ?? "",
                        assets: selectedAssets,
                      })
                    }
                  />
                ) : null}
                {!assigning && recallable ? (
                  <Button
                    variant="secondary"
                    className="bg-grayscale-white-10! text-grayscale-white-90!"
                    disabled={readOnly}
                    text="Recall selected"
                    onClick={() =>
                      setReview({ intent: AssetAssignmentIntent.RECALL, childId: recallChild, assets: selectedAssets })
                    }
                  />
                ) : null}
                {selectionHint ? (
                  <p className="text-200 text-grayscale-white-90" aria-live="polite">
                    {selectionHint}
                  </p>
                ) : null}
              </div>
            </FleetListActionBar>
          ) : null}
          <List<(typeof rows)[number], string, AssetColumn>
            items={rows}
            itemKey="key"
            itemSelectable={editable}
            pageScopedSelection
            rowSelectionLabel={(asset) => `Select ${assetName(asset)}`}
            customSelectedItems={selected}
            customSetSelectedItems={setSelected}
            isRowSelectable={isAssetSelectable}
            stickyFirstColumn
            tableClassName={fleetView ? "mb-4 inline-table w-max !min-w-fit !table-fixed" : undefined}
            applyColumnWidthsToCells={fleetView}
            activeCols={activeColumns}
            colTitles={assetColumnTitles}
            colConfig={columnConfig}
            sortableColumns={sortableColumns}
            currentSort={sort}
            getDefaultSortDirection={(field) => getDefaultSortDirection(field as MinerColumn)}
            onSort={(field, direction) => {
              resetSelection();
              setSort({ field, direction });
            }}
            total={rows.length}
            itemName={
              fleetView
                ? pages.length === 1 && !query.data.inventory.nextPageToken
                  ? { singular: "mining asset", plural: "mining assets" }
                  : { singular: "mining asset on this page", plural: "mining assets on this page" }
                : { singular: "asset on this page", plural: "assets on this page" }
            }
            noDataElement={
              <SettingsEmptyState
                title={hasFilters ? "No matching assets" : "No mining assets"}
                description={hasFilters ? "Try another search or filter." : "Allocated miners and modules appear here."}
              />
            }
          />
          <ListPagination
            firstItemIndex={firstItemIndex}
            lastItemIndex={lastItemIndex}
            total={query.data.inventory.nextPageToken ? undefined : lastItemIndex}
            itemName="mining assets"
            hasPreviousPage={pages.length > 1}
            hasNextPage={!!query.data.inventory.nextPageToken}
            onPrevious={() => {
              setSelected([]);
              setPages((previous) => previous.slice(0, -1));
            }}
            onNext={() => {
              setSelected([]);
              setPages((previous) => [...previous, query.data!.inventory.nextPageToken]);
            }}
          />
        </>
      ) : null}
      {managingColumns ? (
        <ManageColumnsModal
          columns={configurableColumns}
          defaultColumns={defaults}
          titles={assetColumnTitles}
          onDismiss={() => setManagingColumns(false)}
          onSave={(columns) => {
            columnPreferences.save(columns);
            if (!columns.some((column) => column.id === sort.field && column.visible) && sort.field !== "name") {
              resetSelection();
              setSort({ field: "name", direction: "asc" });
            }
            setManagingColumns(false);
          }}
        />
      ) : null}
      {review && editable ? (
        <ActionModal
          presentation="dialog"
          title={`${review.intent === AssetAssignmentIntent.ASSIGN ? "Allocate" : "Recall"} ${countLabel(review.assets.length)}?`}
          confirm={`${review.intent === AssetAssignmentIntent.ASSIGN ? "Allocate" : "Recall"} ${countLabel(review.assets.length)}`}
          valid={reviewTarget ? review.intent === AssetAssignmentIntent.RECALL || !reviewTarget.suspended : false}
          onDismiss={() => setReview(null)}
          onSubmit={async () => {
            if (assignmentPending.current) return;
            assignmentPending.current = true;
            try {
              try {
                await client.assignAccountAssets({
                  accountId: account.accountId,
                  childAccountId: review.childId,
                  intent: review.intent,
                  assets: review.assets.map((asset) => ({
                    kind: asset.kind,
                    id: asset.id,
                    expectedRevision: asset.revision,
                  })),
                });
              } catch (error) {
                if ([Code.Aborted, Code.FailedPrecondition, Code.NotFound].includes(ConnectError.from(error).code)) {
                  refresh();
                  setNotice("Allocations or equipment changed. Review the refreshed assets and select them again.");
                  return;
                }
                throw error;
              }
              const success =
                review.intent === AssetAssignmentIntent.ASSIGN
                  ? `Allocated ${countLabel(review.assets.length)} to ${reviewTarget!.name}.`
                  : `Recalled ${countLabel(review.assets.length)} to ${account.name}.`;
              setNotice(success);
              refresh();
              if (review.intent === AssetAssignmentIntent.ASSIGN) onAssigned?.(success);
            } finally {
              assignmentPending.current = false;
            }
          }}
        >
          {review.intent === AssetAssignmentIntent.ASSIGN ? (
            <>
              <p>Allocate the selected assets from {account.name}.</p>
              {child ? (
                <p>To {child.name}</p>
              ) : (
                <Select
                  id="asset-assignment-target"
                  label={account.depth === 0 ? "Allocate to client" : "Allocate to subclient"}
                  value={review.childId}
                  options={children
                    .filter((child) => !child.suspended)
                    .map((child) => ({ value: child.accountId, label: child.name }))}
                  onChange={(value) => setReview({ ...review, childId: value })}
                />
              )}
            </>
          ) : (
            <p>
              Recall the selected assets from {reviewTarget?.name} to {account.name}.{" "}
              {account.depth === 0 ? "Any subclient allocation on these assets will also be recalled." : ""}
            </p>
          )}
          {review.assets.some((asset) => asset.kind === MiningAssetKind.MODULE) ? (
            <p className="text-200 text-text-primary-50">
              Only the selected modules are included. New or replacement modules are excluded.
            </p>
          ) : null}
          <p className="text-200 text-text-primary-50">
            Reporting only. Equipment location and pool configuration stay unchanged.
          </p>
        </ActionModal>
      ) : null}
    </div>
  );
};
export default AccountAssets;
