import { type ReactNode, useEffect, useState } from "react";

import { isStaleAssetSelection, reviewedAssetSelection } from "../assetSelection";
import { can } from "../capabilities";
import { useHostedAccounts } from "../HostedAccountsContext";
import { allocationKey, type AssetReference, useFleetAccountInventory } from "../useFleetAccountInventory";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { AssetAssignmentIntent } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import type { BulkAction } from "@/protoFleet/features/fleetManagement/components/BulkActions/types";
import { useFleetStore } from "@/protoFleet/store";
import { ChevronDown, Groups } from "@/shared/assets/icons";
import RowActionsMenu from "@/shared/components/RowActionsMenu";
import Select from "@/shared/components/Select";

const AssignmentDialog = ({
  assets,
  onClose,
  onSuccess,
  onActionComplete,
}: {
  assets: AssetReference[];
  onClose: () => void;
  onSuccess: () => void;
  onActionComplete: () => void;
}) => {
  useEffect(() => () => onActionComplete(), [onActionComplete]);
  const inventory = useFleetAccountInventory(assets);
  const [target, setTarget] = useState("");
  const [stale, setStale] = useState(false);
  const rows = assets.flatMap((asset) => {
    const row = inventory.byAsset.get(allocationKey(asset));
    return row ? [row] : [];
  });
  const complete = rows.length === assets.length;
  const eligible =
    complete &&
    rows.every((row) => row.available && !row.outsidePolicy && row.ownerAccountId === inventory.root?.accountId);
  const destinations = inventory.data?.children.filter((child) => !child.suspended) ?? [];
  return (
    <ActionModal
      presentation="dialog"
      title={`Allocate ${assets.length} ${assets.length === 1 ? "asset" : "assets"}?`}
      confirm={`Allocate ${assets.length} ${assets.length === 1 ? "asset" : "assets"}`}
      valid={
        !!inventory.root && !!inventory.data?.canManageAssignments && eligible && !stale
          ? destinations.some((child) => child.accountId === target)
          : false
      }
      onDismiss={onClose}
      onSubmit={async () => {
        try {
          await inventory.client.assignAccountAssets({
            accountId: inventory.root!.accountId,
            childAccountId: target,
            intent: AssetAssignmentIntent.ASSIGN,
            assets: rows.map(reviewedAssetSelection),
          });
        } catch (error) {
          if (isStaleAssetSelection(error)) {
            setStale(true);
          }
          throw error;
        }
        onSuccess();
      }}
    >
      {stale ? (
        <p>Allocations or equipment changed. Close this dialog, refresh Fleet, and select the assets again.</p>
      ) : null}
      <AccountState
        title={inventory.data ? "Could not refresh selected assets" : undefined}
        loading={inventory.loading}
        error={inventory.error}
        retry={inventory.refresh}
      />
      {inventory.data ? (
        !complete ? (
          <p>Some selected assets are no longer available or accessible. Refresh Fleet and select them again.</p>
        ) : !eligible ? (
          <p>
            Only available, unallocated assets can be allocated. Recall existing allocations from the client’s Mining
            assets view first.
          </p>
        ) : destinations.length === 0 ? (
          <p>No active clients are available. Create a client in Clients first.</p>
        ) : (
          <Select
            id="fleet-asset-assignment-target"
            label="Allocate to client"
            value={target}
            options={destinations.map((child) => ({ value: child.accountId, label: child.name }))}
            onChange={setTarget}
          />
        )
      ) : null}
      <p className="text-200 text-text-primary-50">
        Reporting only. Equipment location and pool configuration stay unchanged.
      </p>
    </ActionModal>
  );
};

export const FleetAccountAssignment = ({
  assets,
  exactSelection,
  onActionStart,
  onActionComplete,
  onAssigned,
  renderMenu,
}: {
  assets: AssetReference[];
  exactSelection: boolean;
  onActionStart: () => void;
  onActionComplete: () => void;
  onAssigned: () => void;
  renderMenu?: (action: BulkAction<string> | undefined) => ReactNode;
}) => {
  const directory = useHostedAccounts();
  const root = directory?.accounts.find((account) => account.depth === 0 && can(account, 16n));
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const scope = `${root?.accountId ?? ""}:${generation}`;
  const [snapshot, setSnapshot] = useState<{ scope: string; assets: AssetReference[] } | null>(null);
  if (snapshot && (!root || snapshot.scope !== scope)) setSnapshot(null);
  if (!root) return renderMenu?.(undefined) ?? null;
  const close = () => {
    setSnapshot(null);
    onActionComplete();
  };
  const action: BulkAction<string> = {
    action: "allocateAssets",
    title: "Allocate to client",
    icon: <Groups />,
    requiresConfirmation: false,
    disabled: !exactSelection || assets.length === 0 || assets.length > 500,
    disabledReason: "Select up to 500 specific assets on this page to allocate them.",
    actionHandler: () => {
      setSnapshot({ scope, assets });
      onActionStart();
    },
  };
  return (
    <>
      {renderMenu ? (
        renderMenu(action)
      ) : (
        <RowActionsMenu
          ariaLabel="More asset actions"
          triggerLabel="More"
          triggerVariant="secondary"
          triggerSuffixIcon={<ChevronDown />}
          triggerClassName="bg-grayscale-white-10! text-grayscale-white-90!"
          actions={[
            {
              label: action.title,
              disabled: action.disabled,
              disabledReason: action.disabledReason,
              icon: action.icon,
              onClick: action.actionHandler,
            },
          ]}
        />
      )}
      {snapshot ? (
        <AssignmentDialog
          key={snapshot.scope}
          assets={snapshot.assets}
          onClose={close}
          onActionComplete={onActionComplete}
          onSuccess={() => {
            close();
            onAssigned();
          }}
        />
      ) : null}
    </>
  );
};
