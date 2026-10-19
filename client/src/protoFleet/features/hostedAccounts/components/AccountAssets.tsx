import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { useRequiredAccountScope } from "../AccountScope";
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
import { formatListCountLabel } from "@/shared/components/List/listCountLabel";
import type { ColConfig, SortDirection } from "@/shared/components/List/types";
import ManageColumnsModal from "@/shared/components/ManageColumnsModal";
import Select from "@/shared/components/Select";

const ASSETS_PAGE_SIZE = MINERS_PAGE_SIZE;
const sortableColumns = new Set<AssetColumn>(["name", "model", "hashrate", "efficiency", "powerUsage", "temperature"]);

const countLabel = (count: number) => formatListCountLabel(count, { singular: "asset", plural: "assets" });
type Review = { intent: AssetAssignmentIntent; childId: string; assets: AccountAsset[] };

const AccountAssets = ({
  child,
  mode = "inventory",
  onAssigned,
}: {
  child?: Pick<AccountMiningBucket, "accountId" | "name" | "suspended">;
  mode?: "inventory" | "assign";
  onAssigned?: () => void;
}) => {
  const assigning = mode === "assign";
  const { account, client } = useRequiredAccountScope();
  const username = useFleetStore((state) => state.auth.username);
  const columnPreferences = useAssetColumnPreferences(username, account.accountId);
  const [managingColumns, setManagingColumns] = useState(false);
  const [sort, setSort] = useState<{ field: AssetColumn; direction: SortDirection }>({
    field: "name",
    direction: "asc",
  });
  const [search, setSearch] = useState("");
  const [owner, setOwner] = useState("");
  const [container, setContainer] = useState({ id: "", name: "" });
  const [pages, setPages] = useState([""]);
  const [selected, setSelected] = useState<string[]>([]);
  const [review, setReview] = useState<Review | null>(null);
  const [notice, setNotice] = useState("");
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
        },
        { signal },
      );
      return { inventory };
    },
    [account.accountId, client, pageToken, search, owner, container.id, assigning, child?.accountId, sort],
  );
  const query = useAccountQuery(load);
  const { invalidate } = query;
  const refresh = useCallback(() => {
    setSelected([]);
    setReview(null);
    invalidate();
  }, [invalidate]);
  useEffect(() => {
    const refreshOnFocus = () => {
      if (!assignmentPending.current) refresh();
    };
    window.addEventListener("focus", refreshOnFocus);
    return () => window.removeEventListener("focus", refreshOnFocus);
  }, [refresh]);
  const rows = useMemo(
    () => (query.data?.inventory.assets ?? []).map((asset) => ({ ...asset, key: allocationKey(asset) })),
    [query.data],
  );
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
        ? "Only available assets held directly by this account can be assigned."
        : `No active ${childLabel}s are available to receive these assets.`;
    } else {
      selectionHint = `Select assets assigned to one ${childLabel} to recall them together.`;
    }
  }
  const containers = new Map(
    rows.filter((row) => row.containerId > 0n).map((row) => [String(row.containerId), row.containerName]),
  );
  if (container.id) containers.set(container.id, container.name);
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
        ) : (
          <AccountAssetCell asset={asset} column={column} />
        ),
    };
  }

  return (
    <div className="flex min-w-0 flex-col gap-6">
      {!child ? (
        <SettingsPageHeader title={assigning ? "Assign assets" : "Mining assets"} description={account.name} />
      ) : null}
      {assigning ? (
        <p className="text-200 text-text-primary-50">
          Choose assets held directly by {account.name} to assign to {child?.name ?? "a subclient"}.
        </p>
      ) : !child ? (
        <p className="text-200 text-text-primary-50">
          {account.depth < 2
            ? "Assets allocated to this account, including its subclients."
            : "Assets allocated to this account."}
        </p>
      ) : null}
      <div className="flex flex-wrap items-center gap-3">
        {management && !child && !assigning ? (
          <Select
            id="account-assets-owner"
            label="Assignment"
            value={owner}
            variant="filter"
            options={[
              { value: "", label: "All account assets" },
              { value: account.accountId, label: account.depth === 0 ? "Available to assign" : "Directly assigned" },
              ...children.map((child) => ({ value: child.accountId, label: child.name })),
            ]}
            onChange={(value) => {
              resetSelection();
              setOwner(value);
            }}
          />
        ) : null}
        {containers.size ? (
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
        <Button
          className="ml-auto"
          ariaLabel="Manage columns"
          ariaHasPopup="dialog"
          variant="secondary"
          size={sizes.compact}
          prefixIcon={<Slider width="w-4" />}
          onClick={() => setManagingColumns(true)}
        />
      </div>
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
                    text="Assign selected"
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
            itemName={{ singular: "asset on this page", plural: "assets on this page" }}
            noDataElement={
              <SettingsEmptyState
                title={
                  search || owner || container.id
                    ? "No matching assets"
                    : account.depth === 0
                      ? "No mining assets"
                      : "No assets assigned"
                }
                description={
                  search || owner || container.id
                    ? "Try another search or assignment filter."
                    : account.depth === 0
                      ? "Paired miners and current container modules appear here."
                      : "Your parent account can assign miners or modules to this account."
                }
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
          title={`${review.intent === AssetAssignmentIntent.ASSIGN ? "Assign" : "Recall"} ${countLabel(review.assets.length)}?`}
          confirm={`${review.intent === AssetAssignmentIntent.ASSIGN ? "Assign" : "Recall"} ${countLabel(review.assets.length)}`}
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
                  setNotice("Assignments or equipment changed. Review the refreshed assets and select them again.");
                  return;
                }
                throw error;
              }
              setNotice(
                review.intent === AssetAssignmentIntent.ASSIGN
                  ? `Assigned ${countLabel(review.assets.length)} to ${reviewTarget!.name}.`
                  : `Recalled ${countLabel(review.assets.length)} to ${account.name}.`,
              );
              refresh();
              if (review.intent === AssetAssignmentIntent.ASSIGN) onAssigned?.();
            } finally {
              assignmentPending.current = false;
            }
          }}
        >
          {review.intent === AssetAssignmentIntent.ASSIGN ? (
            <>
              <p>Assign the selected assets from {account.name}.</p>
              {child ? (
                <p>To {child.name}</p>
              ) : (
                <Select
                  id="asset-assignment-target"
                  label={account.depth === 0 ? "Assign to client" : "Assign to subclient"}
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
