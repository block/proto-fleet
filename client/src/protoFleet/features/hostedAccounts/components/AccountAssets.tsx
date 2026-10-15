import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { useRequiredAccountScope } from "../AccountScope";
import { can } from "../capabilities";
import { useAccountQuery } from "../useAccountQuery";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import {
  type AccountAsset,
  AssetAssignmentIntent,
  MiningAssetKind,
} from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import SettingsEmptyState from "@/protoFleet/features/settings/components/SettingsEmptyState";
import SettingsPageHeader from "@/protoFleet/features/settings/components/SettingsPageHeader";
import Button from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import List from "@/shared/components/List";
import { formatListCountLabel } from "@/shared/components/List/listCountLabel";
import Select from "@/shared/components/Select";
import { formatHashrateOrDash, formatPowerKwOrDash } from "@/shared/utils/telemetryFormat";

const assetKey = (asset: AccountAsset) => `${asset.kind}:${asset.id}`;
const kindName = (asset: AccountAsset) => (asset.kind === MiningAssetKind.MODULE ? "module" : "miner");
const assetName = (asset: AccountAsset) => asset.name || `Unavailable ${kindName(asset)} ${asset.id}`;
const countLabel = (count: number) => formatListCountLabel(count, { singular: "asset", plural: "assets" });
const stateLabel = (asset: AccountAsset) => {
  if (asset.outsidePolicy) return "Outside site policy";
  if (!asset.available) return "Unavailable";
  return (
    {
      hashing: "Hashing",
      attention: "Needs attention",
      isolated: "Isolated",
      offline: "Offline",
      sleeping: "Sleeping",
    }[asset.state] ?? "Unknown"
  );
};
type Review = { intent: AssetAssignmentIntent; childId: string; assets: AccountAsset[] };

const AccountAssets = () => {
  const { account, client } = useRequiredAccountScope();
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
          pageSize: 100,
          pageToken,
          search,
          ownerAccountId: owner || undefined,
          containerId: container.id ? BigInt(container.id) : undefined,
        },
        { signal },
      );
      return { inventory };
    },
    [account.accountId, client, pageToken, search, owner, container.id],
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
    () => (query.data?.inventory.assets ?? []).map((asset) => ({ ...asset, key: assetKey(asset) })),
    [query.data],
  );
  const children = query.data?.inventory.children ?? [];
  const management = !!query.data?.inventory.canViewAssignments;
  const editable = !!query.data?.inventory.canManageAssignments && can(account, 16n) && account.depth < 2;
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
    (row) =>
      row.kind === MiningAssetKind.MODULE &&
      String(row.containerId) === container.id &&
      (row.ownerAccountId !== account.accountId || (row.available && !row.outsidePolicy)),
  );
  const reviewTarget = children.find((child) => child.accountId === review?.childId);
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <SettingsPageHeader title="Mining assets" />
        <Button variant="secondary" text="Refresh" onClick={refresh} />
      </div>
      <p className="text-200 text-text-primary-50">
        Assignments control account reporting. They do not move equipment or apply pools.
      </p>
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-48 grow">
          <Input
            id="account-assets-search"
            label="Search mining assets"
            initValue={search}
            maxLength={255}
            onChange={(value) => {
              resetSelection();
              setSearch(value);
            }}
          />
        </div>
        {management ? (
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
      </div>
      {notice ? (
        <p role="status" className="text-300">
          {notice}
        </p>
      ) : null}
      <AccountState loading={query.loading} error={query.error} retry={refresh} />
      {query.data ? (
        <>
          {editable ? (
            <div className="flex flex-wrap items-center gap-3">
              <Button
                variant="primary"
                text="Assign selected"
                disabled={!assignable || !children.some((child) => !child.suspended)}
                onClick={() => setReview({ intent: AssetAssignmentIntent.ASSIGN, childId: "", assets: selectedAssets })}
              />
              <Button
                variant="secondary"
                text="Recall selected"
                disabled={!recallable}
                onClick={() =>
                  setReview({ intent: AssetAssignmentIntent.RECALL, childId: recallChild, assets: selectedAssets })
                }
              />
              {container.id ? (
                <Button
                  variant="textOnly"
                  text="Select modules on this page"
                  disabled={!selectedInContainer.length}
                  onClick={() => setSelected(selectedInContainer.map((asset) => asset.key))}
                />
              ) : null}
              <span className="text-200 text-text-primary-50">{selected.length} selected on this page</span>
            </div>
          ) : null}
          <List<
            (typeof rows)[number],
            string,
            "name" | "kind" | "site" | "container" | "assignment" | "state" | "hashrate" | "power"
          >
            items={rows}
            itemKey="key"
            itemSelectable={editable}
            pageScopedSelection
            rowSelectionLabel={(asset) => `Select ${assetName(asset)}`}
            customSelectedItems={selected}
            customSetSelectedItems={setSelected}
            isRowSelectable={(asset) =>
              !!asset.ownerAccountId &&
              (asset.ownerAccountId !== account.accountId || (asset.available && !asset.outsidePolicy))
            }
            stickyFirstColumn={false}
            activeCols={[
              "name",
              "kind",
              "site",
              "container",
              ...(management ? ["assignment" as const] : []),
              "state",
              "hashrate",
              "power",
            ]}
            colTitles={{
              name: "Asset",
              kind: "Type",
              site: "Site",
              container: "Container",
              assignment: "Assigned to",
              state: "Status",
              hashrate: "Hashrate",
              power: "Equipment power",
            }}
            colConfig={{
              name: { width: "min-w-56", component: assetName },
              kind: {
                width: "min-w-24",
                component: (asset) => (asset.kind === MiningAssetKind.MODULE ? "Module" : "Miner"),
              },
              site: { width: "min-w-40", component: (asset) => asset.siteName || "—" },
              container: { width: "min-w-48", component: (asset) => asset.containerName || "—" },
              assignment: {
                width: "min-w-40",
                component: (asset) =>
                  asset.ownerAccountId === account.accountId
                    ? account.depth === 0
                      ? "Available to assign"
                      : "This account"
                    : asset.ownerAccountName || "—",
              },
              state: { width: "min-w-40", component: stateLabel },
              hashrate: {
                width: "min-w-40",
                component: (asset) => (
                  <div>
                    {formatHashrateOrDash(asset.hashrateHs === undefined ? null : asset.hashrateHs / 1e12)}
                    {asset.partialCoverage || asset.unknownCoverage ? (
                      <div className="text-200 text-text-primary-50">
                        {asset.partialCoverage ? "Partial module reading" : "Module coverage unknown"}
                      </div>
                    ) : null}
                  </div>
                ),
              },
              power: {
                width: "min-w-40",
                component: (asset) => formatPowerKwOrDash(asset.powerW === undefined ? undefined : asset.powerW / 1000),
              },
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
          <div className="flex items-center justify-between gap-4">
            <p className="text-200 text-text-primary-50">Page {pages.length} · Up to 100 assets per page</p>
            <div className="flex gap-3">
              <Button
                variant="secondary"
                text="Previous"
                disabled={pages.length === 1}
                onClick={() => {
                  setSelected([]);
                  setPages((previous) => previous.slice(0, -1));
                }}
              />
              <Button
                variant="secondary"
                text="Next"
                disabled={!query.data.inventory.nextPageToken}
                onClick={() => {
                  setSelected([]);
                  setPages((previous) => [...previous, query.data!.inventory.nextPageToken]);
                }}
              />
            </div>
          </div>
        </>
      ) : null}
      {review ? (
        <ActionModal
          title={review.intent === AssetAssignmentIntent.ASSIGN ? "Assign assets" : "Recall assets"}
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
            } finally {
              assignmentPending.current = false;
            }
          }}
        >
          <p>
            {review.assets.filter((asset) => asset.kind === MiningAssetKind.MINER).length}{" "}
            {review.assets.filter((asset) => asset.kind === MiningAssetKind.MINER).length === 1 ? "miner" : "miners"} ·{" "}
            {review.assets.filter((asset) => asset.kind === MiningAssetKind.MODULE).length}{" "}
            {review.assets.filter((asset) => asset.kind === MiningAssetKind.MODULE).length === 1 ? "module" : "modules"}
          </p>
          {review.intent === AssetAssignmentIntent.ASSIGN ? (
            <>
              <p>From {account.name}</p>
              <Select
                id="asset-assignment-target"
                label={account.depth === 0 ? "Assign to customer" : "Assign to subclient"}
                value={review.childId}
                options={children
                  .filter((child) => !child.suspended)
                  .map((child) => ({ value: child.accountId, label: child.name }))}
                onChange={(value) => setReview({ ...review, childId: value })}
              />
            </>
          ) : (
            <p>
              From {reviewTarget?.name} to {account.name}.{" "}
              {account.depth === 0
                ? "Any subclient allocation on these assets will also be recalled."
                : "These assets will be directly assigned to this account."}
            </p>
          )}
          <ul className="max-h-48 overflow-y-auto text-200 text-text-primary-70">
            {review.assets.map((asset) => (
              <li key={assetKey(asset)}>{assetName(asset)}</li>
            ))}
          </ul>
          {review.assets.some((asset) => asset.kind === MiningAssetKind.MODULE) ? (
            <p className="text-200 text-text-primary-50">
              This changes only the modules selected on this page. Newly discovered or replacement modules are not
              included.
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
