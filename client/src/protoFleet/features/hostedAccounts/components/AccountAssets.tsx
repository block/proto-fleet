import { type ReactNode, useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import { useRequiredAccountScope } from "../AccountScope";
import { ACCOUNT_READ_TIMEOUT_MS } from "../api";
import { isStaleAssetSelection, reviewedAssetSelection } from "../assetSelection";
import { accountPath, can } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { defaultAssetColumns, useAssetColumnPreferences } from "../useAssetColumnPreferences";
import { allocationKey } from "../useFleetAccountInventory";
import { AccountAssetCell } from "./AccountAssetCell";
import { type AssetColumn, assetColumnTitles, assetColumnWidths, assetName } from "./accountAssetColumns";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { ReplaceHashboardDialog } from "./ReplaceHashboardDialog";
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
import { hasMiningGroupPermissions } from "@/protoFleet/features/containers/miningGroupPermissions";
import ReturnHashboardsToDefaultDialog from "@/protoFleet/features/containers/ReturnHashboardsToDefaultDialog";
import FleetListActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar/FleetListActionBar";
import {
  type MinerColumn,
  MINERS_PAGE_SIZE,
} from "@/protoFleet/features/fleetManagement/components/MinerList/constants";
import {
  getDefaultSortDirection,
  getSortField,
} from "@/protoFleet/features/fleetManagement/components/MinerList/sortConfig";
import { ConfigurationSelector } from "@/protoFleet/features/poolConfigurations/ConfigurationSelector";
import { PoolAssignments } from "@/protoFleet/features/poolConfigurations/PoolAssignments";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import { useFleetStore, usePermissions } from "@/protoFleet/store";
import { ChevronDown, Slider } from "@/shared/assets/icons";
import { ActionBarSelectionControls } from "@/shared/components/ActionBar";
import Button, { sizes } from "@/shared/components/Button";
import Checkbox from "@/shared/components/Checkbox";
import List from "@/shared/components/List";
import FilterChipsBar, { type FilterChipsBarFilter } from "@/shared/components/List/Filters/FilterChipsBar";
import { formatListCountLabel } from "@/shared/components/List/listCountLabel";
import type { ColConfig, SortDirection } from "@/shared/components/List/types";
import ManageColumnsModal from "@/shared/components/ManageColumnsModal";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";
import Select from "@/shared/components/Select";
import TabStrip, { TabStripItem } from "@/shared/components/Tab/TabStrip";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const ASSETS_PAGE_SIZE = MINERS_PAGE_SIZE;
const sortableColumns = new Set<AssetColumn>(["name", "model", "hashrate", "efficiency", "powerUsage", "temperature"]);

const countLabel = (count: number) => formatListCountLabel(count, { singular: "asset", plural: "assets" });
type Review = { intent: AssetAssignmentIntent; childId: string; assets: AccountAsset[] };

const AccountAssets = ({
  child,
  mode = "inventory",
  onAssigned,
  onDismiss,
}: {
  child?: Pick<AccountMiningBucket, "accountId" | "name" | "suspended">;
  mode?: "inventory" | "assign";
  onAssigned?: () => void;
  onDismiss?: () => void;
}) => {
  const assigning = mode === "assign";
  const isPicker = !!onDismiss;
  const { account, client, readOnly = false } = useRequiredAccountScope();
  const canReturnToDefault = hasMiningGroupPermissions(usePermissions(), "pools") && account.depth === 0;
  const inputId = useId();
  const [allocating, setAllocating] = useState(false);
  const fleetView = !child && !assigning && account.depth > 0;
  const username = useFleetStore((state) => state.auth.username);
  const columnPreferences = useAssetColumnPreferences(username, account.accountId);
  const [managingColumns, setManagingColumns] = useState(false);
  const [sort, setSort] = useState<{ field: AssetColumn; direction: SortDirection }>({
    field: "name",
    direction: "asc",
  });
  const [search, setSearch] = useState("");
  const [hashboardsOnly, setHashboardsOnly] = useState(false);
  const [expandedModules, setExpandedModules] = useState<string[]>([]);
  const [replacing, setReplacing] = useState<AccountAsset | null>(null);
  const [owner, setOwner] = useState("");
  const [model, setModel] = useState("");
  const [site, setSite] = useState({ id: "", name: "" });
  const [container, setContainer] = useState({ id: "", name: "" });
  const [pages, setPages] = useState([""]);
  const [selected, setSelected] = useState<string[]>([]);
  const [review, setReview] = useState<Review | null>(null);
  const [poolTargets, setPoolTargets] = useState<AccountAsset[]>();
  const [returnToDefaultBoards, setReturnToDefaultBoards] = useState<bigint[]>();
  const [conflictNotice, setConflictNotice] = useState("");
  const assignmentPending = useRef(false);
  const pageToken = pages[pages.length - 1];
  const load = useCallback(
    (signal: AbortSignal) =>
      client.listAccountAssets(
        {
          accountId: account.accountId,
          pageSize: ASSETS_PAGE_SIZE,
          pageToken,
          hashboardsOnly,
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
      ),
    [
      account.accountId,
      client,
      pageToken,
      hashboardsOnly,
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
    setExpandedModules([]);
    setReview(null);
    setReplacing(null);
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
    () => (query.data?.assets ?? []).map((asset) => ({ ...asset, key: allocationKey(asset) })),
    [query.data],
  );
  // At most three boards per page row. One bounded page lookup keeps opening
  // another module from discarding an existing board selection during its load.
  const pageBoardIds = JSON.stringify(rows.flatMap((asset) => asset.boards.map((board) => String(board.id))));
  const loadBoards = useCallback(
    (signal: AbortSignal) =>
      client.listAccountAssets(
        {
          accountId: account.accountId,
          selectedAssets: (JSON.parse(pageBoardIds) as string[]).map((id) => ({
            kind: MiningAssetKind.HASHBOARD,
            id: BigInt(id),
          })),
        },
        { timeoutMs: ACCOUNT_READ_TIMEOUT_MS, signal },
      ),
    [client, account.accountId, pageBoardIds],
  );
  const boardQuery = useAccountQuery(
    loadBoards,
    (expandedModules.length > 0 || selected.some((key) => key.startsWith(`${MiningAssetKind.HASHBOARD}:`))) &&
      pageBoardIds !== "[]",
    true,
    POLL_INTERVAL_MS,
  );
  const refreshBoards = boardQuery.refresh;
  useEffect(() => {
    window.addEventListener("focus", refreshBoards);
    return () => window.removeEventListener("focus", refreshBoards);
  }, [refreshBoards]);
  const boardRows = (boardQuery.data?.assets ?? []).filter(
    (asset) =>
      asset.kind === MiningAssetKind.HASHBOARD &&
      rows.some((module) =>
        module.boards.some((board) => board.id === asset.id && board.expectedRevision === asset.revision),
      ),
  );
  const selectableRows = [
    ...new Map(
      [...rows, ...boardRows.map((asset) => ({ ...asset, key: allocationKey(asset) }))].map((asset) => [
        asset.key,
        asset,
      ]),
    ).values(),
  ];
  const hasFilters = !!(hashboardsOnly || search || owner || container.id || model || site.id);
  const toolbarNeeded = rows.length > 0 || hasFilters || pages.length > 1;
  // Clearing the last filter reloads without data; keep the toolbar mounted until that reload settles.
  const [toolbarShown, setToolbarShown] = useState({ accountId: account.accountId, shown: false });
  if (query.data && (toolbarShown.accountId !== account.accountId || toolbarShown.shown !== toolbarNeeded))
    setToolbarShown({ accountId: account.accountId, shown: toolbarNeeded });
  const showToolbar =
    toolbarNeeded || (query.loading && toolbarShown.accountId === account.accountId && toolbarShown.shown);
  const children = query.data?.children ?? [];
  const management = !!query.data?.canViewAssignments;
  const showAssignment = management && !child && !assigning;
  const editable =
    !!query.data?.canManageAssignments && can(account, 16n) && account.depth < 2 && (assigning || !!child);
  const canOpenAllocation = !assigning && !!child && account.depth === 0 && editable;
  if (allocating && (!canOpenAllocation || child?.suspended || readOnly)) setAllocating(false);
  const poolEditable = fleetView && can(account, 4n) && !readOnly;
  const selectable = (editable && !readOnly) || poolEditable;
  if (!selectable && selected.length > 0) setSelected([]);
  if (!poolEditable && poolTargets) setPoolTargets(undefined);
  if (!canReturnToDefault && returnToDefaultBoards) setReturnToDefaultBoards(undefined);
  if ((!editable || readOnly) && (review !== null || replacing !== null)) {
    setReplacing(null);
    setReview(null);
  }
  const isAssetAssignable = (asset: AccountAsset) =>
    asset.available && !asset.outsidePolicy && asset.ownerAccountId === account.accountId;
  const isAssetSelectable = (asset: AccountAsset) =>
    poolEditable
      ? asset.canChangePools
      : !!asset.ownerAccountId && (assigning ? isAssetAssignable(asset) : asset.ownerAccountId !== account.accountId);
  // A background revalidation can retire, move or reassign selected rows.
  const selectedModuleBoardKeys = new Set(
    rows
      .filter((row) => selected.includes(row.key))
      .flatMap((row) => row.boards.map((board) => allocationKey({ kind: MiningAssetKind.HASHBOARD, id: board.id }))),
  );
  const liveSelection = [...new Set(selected)].filter(
    (key) =>
      !selectedModuleBoardKeys.has(key) && selectableRows.some((row) => row.key === key && isAssetSelectable(row)),
  );
  if (query.data && liveSelection.length !== selected.length) setSelected(liveSelection);
  const selectedAssets = selectableRows.filter((row) => liveSelection.includes(row.key));
  const recallChild = selectedAssets[0]?.ownerAccountId;
  const recallable =
    selectedAssets.length > 0 &&
    !!recallChild &&
    recallChild !== account.accountId &&
    selectedAssets.every((asset) => asset.ownerAccountId === recallChild) &&
    children.some((child) => child.accountId === recallChild);
  const hasAssignmentTarget = children.some(
    (target) => !target.suspended && !child?.suspended && (!child || target.accountId === child.accountId),
  );
  const canAllocate =
    assigning && selectedAssets.length > 0 && selectedAssets.every(isAssetAssignable) && hasAssignmentTarget;
  const canRecall = !assigning && recallable;
  const childLabel = account.depth === 0 ? "client" : "subclient";
  let selectionHint = "";
  if (editable && selected.length > 0 && !canAllocate && !canRecall) {
    if (assigning) {
      selectionHint = hasAssignmentTarget
        ? "Only available assets held directly by this account can be allocated."
        : `No active ${childLabel}s are available to receive these assets.`;
    } else {
      selectionHint = `Select assets allocated to one ${childLabel} to recall them together.`;
    }
  }
  const filterOptions = query.data?.filterOptions;
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
    ...(showAssignment
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
    setExpandedModules([]);
    setReview(null);
    setConflictNotice("");
  };
  const selectedInContainer = rows.filter(
    (row) => row.kind === MiningAssetKind.MODULE && String(row.containerId) === container.id && isAssetSelectable(row),
  );
  const firstItemIndex = (pages.length - 1) * ASSETS_PAGE_SIZE + 1;
  const lastItemIndex = (pages.length - 1) * ASSETS_PAGE_SIZE + rows.length;
  const reviewTarget = children.find((child) => child.accountId === review?.childId);
  const reviewAssignment = () =>
    setReview({ intent: AssetAssignmentIntent.ASSIGN, childId: child?.accountId ?? "", assets: selectedAssets });
  const reviewingAssignment = review?.intent === AssetAssignmentIntent.ASSIGN;
  const reviewAction = reviewingAssignment ? "Allocate" : "Recall";
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
      component: (asset) => {
        if (column === "assignedAccount") {
          return asset.ownerAccountId === account.accountId ? "This account" : asset.ownerAccountName || "—";
        }
        let cell = <AccountAssetCell asset={asset} column={column} />;
        if (column === "name" && account.depth > 0 && asset.available && !asset.outsidePolicy) {
          cell = (
            <Button
              variant="textOnly"
              textColor="text-text-primary"
              className="justify-start text-left font-normal"
              textOnlyUnderlineOnHover={false}
              to={`${accountPath(account.accountId, "assets")}/${asset.kind === MiningAssetKind.MODULE ? "module" : asset.kind === MiningAssetKind.HASHBOARD ? "hashboard" : "miner"}/${asset.id}`}
            >
              {cell}
            </Button>
          );
        }
        if (column === "name" && asset.kind === MiningAssetKind.MODULE && asset.boards.length) {
          const expanded = expandedModules.includes(allocationKey(asset));
          cell = (
            <div className="flex min-w-0 items-center">
              <button
                type="button"
                className="-ml-2 flex size-8 shrink-0 items-center justify-center rounded focus-visible:outline-2 focus-visible:outline-offset-2"
                aria-label={`${expanded ? "Collapse" : "Expand"} hashboards for ${assetName(asset)}`}
                aria-expanded={expanded}
                aria-controls={
                  expanded ? asset.boards.map((board) => `${inputId}-hashboard-${board.id}`).join(" ") : undefined
                }
                onClick={() => {
                  const key = allocationKey(asset);
                  setExpandedModules((previous) =>
                    expanded ? previous.filter((value) => value !== key) : [...previous, key],
                  );
                }}
              >
                <span aria-hidden className={expanded ? "" : "-rotate-90"}>
                  <ChevronDown />
                </span>
              </button>
              <div className="min-w-0 flex-1">{cell}</div>
            </div>
          );
        }
        return cell;
      },
    };
  }

  const content = (
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
        <p className="text-200 text-text-primary-50">Choose assets to allocate to {child?.name ?? "a subclient"}.</p>
      ) : !child && !fleetView ? (
        <p className="text-200 text-text-primary-50">
          {account.depth < 2
            ? "Assets allocated to this account, including its subclients."
            : "Assets allocated to this account."}
        </p>
      ) : null}
      <div className={clsx("flex flex-wrap items-center gap-3", fleetView && "laptop:pt-4")}>
        <Select
          id={`${inputId}-view`}
          label="Show"
          variant="filter"
          value={hashboardsOnly ? "boards" : "modules"}
          options={[
            { value: "modules", label: "All assets" },
            { value: "boards", label: "Individual hashboards" },
          ]}
          onChange={(value) => {
            resetSelection();
            setHashboardsOnly(value === "boards");
          }}
        />
        {showToolbar ? (
          <>
            {fleetView || isPicker ? (
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
                id={`${inputId}-owner`}
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
            {!fleetView && !isPicker && containers.size ? (
              <Select
                id={`${inputId}-container`}
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
              id={`${inputId}-search`}
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
            {editable && container.id && !hashboardsOnly ? (
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
          {showToolbar && !isPicker ? (
            <Button
              ariaLabel="Manage columns"
              ariaHasPopup="dialog"
              variant="secondary"
              size={sizes.compact}
              prefixIcon={<Slider width="w-4" />}
              onClick={() => setManagingColumns(true)}
            />
          ) : null}
          {canOpenAllocation ? (
            <Button
              variant="primary"
              size={sizes.compact}
              text="Allocate assets"
              disabled={readOnly || child?.suspended}
              onClick={() => setAllocating(true)}
            />
          ) : null}
        </div>
      </div>
      {conflictNotice ? (
        <p role="status" className="text-300">
          {conflictNotice}
        </p>
      ) : null}
      <AccountState loading={query.loading} error={query.error} retry={refresh} />
      {query.data ? (
        <>
          {selectable && selected.length > 0 && !isPicker ? (
            <FleetListActionBar
              isActionBusy={review !== null || poolTargets !== undefined}
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
                {poolEditable ? (
                  <Button
                    variant="secondary"
                    className="bg-grayscale-white-10! text-grayscale-white-90!"
                    text="Edit pool"
                    onClick={() => setPoolTargets(selectedAssets)}
                  />
                ) : null}
                {canAllocate ? (
                  <Button
                    variant="secondary"
                    className="bg-grayscale-white-10! text-grayscale-white-90!"
                    disabled={readOnly}
                    text="Allocate selected"
                    onClick={reviewAssignment}
                  />
                ) : null}
                {canRecall ? (
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
                {canRecall &&
                account.depth === 0 &&
                selectedAssets.length === 1 &&
                selectedAssets[0].kind === MiningAssetKind.HASHBOARD ? (
                  <Button
                    variant="secondary"
                    className="bg-grayscale-white-10! text-grayscale-white-90!"
                    disabled={readOnly}
                    text="Replace hashboard"
                    onClick={() => setReplacing(selectedAssets[0])}
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
          <PoolAssignments
            deviceIds={rows.filter((asset) => asset.kind === MiningAssetKind.MINER).map((asset) => asset.id)}
            moduleIds={rows.filter((asset) => asset.kind === MiningAssetKind.MODULE).map((asset) => asset.id)}
            hashboardIds={selectableRows
              .filter((asset) => asset.kind === MiningAssetKind.HASHBOARD)
              .map((asset) => asset.id)}
            enabled={activeColumns.includes("poolConfiguration")}
          >
            <List<(typeof rows)[number], string, AssetColumn>
              getChildRows={(module) => {
                if (!expandedModules.includes(module.key)) return [];
                return module.boards.map((expected) => {
                  const board = boardRows.find((asset) => asset.id === expected.id);
                  const key = allocationKey({ kind: MiningAssetKind.HASHBOARD, id: expected.id });
                  const moduleSelected = selected.includes(module.key);
                  const cells: Partial<Record<AssetColumn, ReactNode>> = {};
                  for (const column of activeColumns) {
                    if (!board) continue;
                    if (column === "assignedAccount") {
                      cells[column] =
                        board.ownerAccountId === account.accountId ? "This account" : board.ownerAccountName || "—";
                    } else {
                      cells[column] = (
                        <div className={column === "name" ? "pl-8" : undefined}>
                          <AccountAssetCell asset={board} column={column} />
                        </div>
                      );
                    }
                  }
                  if (!board)
                    cells.name = (
                      <div className="pl-8">
                        <span role={boardQuery.loading ? "status" : "alert"}>
                          {boardQuery.loading ? "Loading hashboard…" : "Hashboard unavailable"}
                        </span>
                        {!boardQuery.loading ? (
                          <Button
                            variant="textOnly"
                            text="Retry hashboards"
                            onClick={() => {
                              revalidate();
                              refreshBoards();
                            }}
                          />
                        ) : null}
                      </div>
                    );
                  return {
                    key: `${inputId}-hashboard-${expected.id}`,
                    label: `${board ? assetName(board) : "Hashboard"} in ${assetName(module)}`,
                    cells,
                    selection: selectable ? (
                      <Checkbox
                        ariaLabel={`Select ${board ? assetName(board) : "unavailable hashboard"} in ${assetName(module)}`}
                        disabled={
                          readOnly ||
                          !board ||
                          !isAssetSelectable(board) ||
                          (moduleSelected &&
                            !module.boards.every((member) =>
                              boardRows.some((asset) => asset.id === member.id && isAssetSelectable(asset)),
                            ))
                        }
                        checked={moduleSelected || selected.includes(key)}
                        onChange={(event) => {
                          const siblings = moduleSelected
                            ? boardRows
                                .filter(
                                  (asset) =>
                                    asset.id !== expected.id &&
                                    module.boards.some((member) => member.id === asset.id) &&
                                    isAssetSelectable(asset),
                                )
                                .map(allocationKey)
                            : [];
                          setSelected((previous) => [
                            ...new Set([
                              ...previous.filter((value) => value !== module.key && value !== key),
                              ...siblings,
                              ...(event.target.checked ? [key] : []),
                            ]),
                          ]);
                        }}
                      />
                    ) : undefined,
                  };
                });
              }}
              items={rows}
              itemKey="key"
              itemSelectable={selectable}
              pageScopedSelection
              rowSelectionLabel={(asset) => `Select ${assetName(asset)}`}
              isRowPartiallySelected={(asset) =>
                !selected.includes(asset.key) &&
                asset.boards.some((board) =>
                  selected.includes(allocationKey({ kind: MiningAssetKind.HASHBOARD, id: board.id })),
                )
              }
              customSelectedItems={rows
                .filter((row) => selected.includes(row.key) || selectedModuleBoardKeys.has(row.key))
                .map((row) => row.key)}
              customSetSelectedItems={(parents) => {
                const removedBoardKeys = new Set(
                  rows
                    .filter((row) => selectedModuleBoardKeys.has(row.key) && !parents.includes(row.key))
                    .map((row) => row.key),
                );
                const splitModules = rows.filter(
                  (row) =>
                    parents.includes(row.key) &&
                    row.boards.some((board) =>
                      removedBoardKeys.has(allocationKey({ kind: MiningAssetKind.HASHBOARD, id: board.id })),
                    ),
                );
                const remainingBoards = splitModules.flatMap((module) =>
                  boardRows
                    .filter(
                      (board) =>
                        module.boards.some((member) => member.id === board.id) &&
                        !removedBoardKeys.has(allocationKey(board)) &&
                        isAssetSelectable(board),
                    )
                    .map(allocationKey),
                );
                setSelected((previous) => [
                  ...parents.filter((key) => !splitModules.some((module) => module.key === key)),
                  ...remainingBoards,
                  ...previous.filter(
                    (key) =>
                      !rows.some((row) => row.key === key) &&
                      boardRows.some((board) => allocationKey(board) === key) &&
                      !rows.some(
                        (module) =>
                          parents.includes(module.key) &&
                          module.boards.some(
                            (board) => key === allocationKey({ kind: MiningAssetKind.HASHBOARD, id: board.id }),
                          ),
                      ),
                  ),
                ]);
              }}
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
                  ? pages.length === 1 && !query.data.nextPageToken
                    ? { singular: "mining asset", plural: "mining assets" }
                    : { singular: "mining asset on this page", plural: "mining assets on this page" }
                  : { singular: "asset on this page", plural: "assets on this page" }
              }
              noDataElement={
                <SettingsEmptyState
                  title={hasFilters ? "No matching assets" : "No mining assets"}
                  description={
                    hasFilters
                      ? "Try another search or filter."
                      : "Allocated miners, modules, and hashboards appear here."
                  }
                />
              }
            />
          </PoolAssignments>
          <ListPagination
            firstItemIndex={firstItemIndex}
            lastItemIndex={lastItemIndex}
            total={query.data.nextPageToken ? undefined : lastItemIndex}
            itemName="mining assets"
            hasPreviousPage={pages.length > 1}
            hasNextPage={!!query.data.nextPageToken}
            onPrevious={() => {
              setSelected([]);
              setExpandedModules([]);
              setPages((previous) => previous.slice(0, -1));
            }}
            onNext={() => {
              setSelected([]);
              setExpandedModules([]);
              setPages((previous) => [...previous, query.data!.nextPageToken]);
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
      {poolTargets && poolEditable ? (
        <ConfigurationSelector
          deviceIds={poolTargets.filter((asset) => asset.kind === MiningAssetKind.MINER).map((asset) => asset.id)}
          boards={
            poolTargets.some((asset) => asset.kind !== MiningAssetKind.MINER)
              ? {
                  hashboardIds: poolTargets
                    .filter((asset) => asset.kind === MiningAssetKind.HASHBOARD)
                    .map((asset) => asset.id),
                  moduleIds: poolTargets
                    .filter((asset) => asset.kind === MiningAssetKind.MODULE)
                    .map((asset) => asset.id),
                }
              : undefined
          }
          onDismiss={() => setPoolTargets(undefined)}
          onApplied={() => revalidate()}
        />
      ) : null}
      {review && editable ? (
        <ActionModal
          presentation="dialog"
          title={`${reviewAction} ${countLabel(review.assets.length)}?`}
          confirm={`${reviewAction} ${countLabel(review.assets.length)}`}
          valid={reviewTarget ? review.intent === AssetAssignmentIntent.RECALL || !reviewTarget.suspended : false}
          onDismiss={() => setReview(null)}
          onSubmit={async (current) => {
            if (assignmentPending.current) return;
            assignmentPending.current = true;
            try {
              try {
                await client.assignAccountAssets({
                  accountId: account.accountId,
                  childAccountId: review.childId,
                  intent: review.intent,
                  assets: review.assets.map(reviewedAssetSelection),
                });
              } catch (error) {
                if (isStaleAssetSelection(error)) {
                  refresh();
                  setConflictNotice(
                    "Allocations or equipment changed. Review the refreshed assets and select them again.",
                  );
                  return;
                }
                throw error;
              }
              if (!current()) return;
              if (!reviewingAssignment && canReturnToDefault) {
                const recalledBoards = review.assets.flatMap((asset) => {
                  if (asset.kind === MiningAssetKind.HASHBOARD) return [asset.id];
                  if (asset.kind === MiningAssetKind.MODULE) return asset.boards.map((board) => board.id);
                  return [];
                });
                if (recalledBoards.length) setReturnToDefaultBoards(recalledBoards);
              }
              const success = reviewingAssignment
                ? `Allocated ${countLabel(review.assets.length)} to ${reviewTarget!.name}.`
                : `Recalled ${countLabel(review.assets.length)} to ${account.name}.`;
              setConflictNotice("");
              pushToast({ message: success, status: STATUSES.success });
              refresh();
              if (reviewingAssignment) onAssigned?.();
            } finally {
              assignmentPending.current = false;
            }
          }}
        >
          {reviewingAssignment ? (
            <>
              <p>Allocate the selected assets from {account.name}.</p>
              {child ? (
                <p>To {child.name}</p>
              ) : (
                <Select
                  id={`${inputId}-assignment-target`}
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
              Only the boards in the reviewed modules are included. New or replacement boards are excluded.
            </p>
          ) : null}
          <p className="text-200 text-text-primary-50">
            The receiving account’s default pool configuration will be applied automatically. If it has no pool
            configurations, existing pools will be cleared. Members still need Change pools permission to change pools
            manually.
          </p>
        </ActionModal>
      ) : null}
      {canReturnToDefault && returnToDefaultBoards ? (
        <ReturnHashboardsToDefaultDialog
          hashboardIds={returnToDefaultBoards}
          onClose={() => setReturnToDefaultBoards(undefined)}
          onComplete={() => {
            setReturnToDefaultBoards(undefined);
            refresh();
          }}
        />
      ) : null}
      {replacing && editable && !readOnly ? (
        <ReplaceHashboardDialog
          oldBoard={replacing}
          onDismiss={() => setReplacing(null)}
          onConflict={() => {
            refresh();
            setConflictNotice("Allocations or equipment changed. Review the refreshed boards and select them again.");
          }}
          onReplaced={() => {
            refresh();
            setConflictNotice("");
          }}
        />
      ) : null}
    </div>
  );
  if (isPicker) {
    return (
      <Modal
        open
        title="Select assets"
        dialogLabel="Select assets"
        size="large"
        manageFocus
        onDismiss={review ? undefined : onDismiss}
        className="flex !h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden"
        bodyClassName="min-h-0 flex-1 overflow-y-auto"
        buttons={[
          {
            text: "Allocate selected",
            variant: "primary",
            disabled: !editable || !canAllocate || readOnly || !!review,
            onClick: reviewAssignment,
            dismissModalOnClick: false,
          },
        ]}
        fixedFooter={
          <div aria-live="polite">
            <ModalSelectAllFooter
              label={`${countLabel(selected.length)} selected`}
              onSelectAll={
                editable && pages.length === 1 && !query.data?.nextPageToken
                  ? () => setSelected(rows.filter(isAssetSelectable).map((asset) => asset.key))
                  : undefined
              }
              selectAllDisabled={readOnly || !!review || !rows.some(isAssetSelectable)}
              onSelectNone={selected.length > 0 && !review ? () => setSelected([]) : undefined}
            />
          </div>
        }
      >
        {content}
      </Modal>
    );
  }
  return (
    <>
      {content}
      {allocating && canOpenAllocation && child ? (
        <AccountAssets
          mode="assign"
          child={child}
          onDismiss={() => setAllocating(false)}
          onAssigned={() => {
            setAllocating(false);
            setSelected([]);
            setReview(null);
            revalidate();
          }}
        />
      ) : null}
    </>
  );
};
export default AccountAssets;
