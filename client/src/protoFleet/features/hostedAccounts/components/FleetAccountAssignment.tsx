import { useEffect, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { can } from "../capabilities";
import { useHostedAccounts } from "../HostedAccountsContext";
import { allocationKey, type AssetReference, useFleetAccountInventory } from "../useFleetAccountInventory";
import { AccountState } from "./AccountState";
import { ActionModal } from "./ActionModal";
import { AssetAssignmentIntent } from "@/protoFleet/api/generated/hosted/v1/hosted_pb";
import { useFleetStore } from "@/protoFleet/store";
import Button from "@/shared/components/Button";
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
      title={`Assign ${assets.length} ${assets.length === 1 ? "asset" : "assets"}?`}
      confirm={`Assign ${assets.length} ${assets.length === 1 ? "asset" : "assets"}`}
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
            assets: rows.map((row) => ({ kind: row.kind, id: row.id, expectedRevision: row.revision })),
          });
        } catch (error) {
          if ([Code.Aborted, Code.FailedPrecondition, Code.NotFound].includes(ConnectError.from(error).code)) {
            setStale(true);
          }
          throw error;
        }
        onSuccess();
      }}
    >
      {stale ? (
        <p>Assignments or equipment changed. Close this dialog, refresh Fleet, and select the assets again.</p>
      ) : null}
      <AccountState loading={inventory.loading} error={inventory.error} retry={inventory.refresh} />
      {inventory.data ? (
        !complete ? (
          <p>Some selected assets are no longer available or accessible. Refresh Fleet and select them again.</p>
        ) : !eligible ? (
          <p>
            Only available, unassigned assets can be assigned. Recall existing allocations from the client’s Mining
            assets view first.
          </p>
        ) : destinations.length === 0 ? (
          <p>No active clients are available. Create a client in Clients first.</p>
        ) : (
          <Select
            id="fleet-asset-assignment-target"
            label="Assign to client"
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
}: {
  assets: AssetReference[];
  exactSelection: boolean;
  onActionStart: () => void;
  onActionComplete: () => void;
  onAssigned: () => void;
}) => {
  const directory = useHostedAccounts();
  const root = directory?.accounts.find((account) => account.depth === 0 && can(account, 16n));
  const generation = useFleetStore((state) => state.auth.sessionGeneration);
  const scope = `${root?.accountId ?? ""}:${generation}`;
  const [snapshot, setSnapshot] = useState<{ scope: string; assets: AssetReference[] } | null>(null);
  if (snapshot && (!root || snapshot.scope !== scope)) setSnapshot(null);
  if (!root) return null;
  const close = () => {
    setSnapshot(null);
    onActionComplete();
  };
  return (
    <>
      <span title={!exactSelection ? "Select specific assets on this page to assign them." : undefined}>
        <Button
          variant="secondary"
          className="bg-grayscale-white-10! text-grayscale-white-90! disabled:opacity-40"
          text="Assign to account"
          disabled={!exactSelection || assets.length === 0 || assets.length > 500}
          onClick={() => {
            setSnapshot({ scope, assets });
            onActionStart();
          }}
        />
      </span>
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
