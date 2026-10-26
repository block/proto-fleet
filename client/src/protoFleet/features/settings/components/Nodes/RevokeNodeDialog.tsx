import type { FleetNodeDeviceSummary } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { FleetNodeHARole } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface RevokeNodeDialogProps {
  open?: boolean;
  nodeName: string;
  affectedDevices: Pick<FleetNodeDeviceSummary, "manufacturer" | "model">[];
  isLoadingImpact: boolean;
  impactError: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
  haRole?: FleetNodeHARole;
  haStandbyReady?: boolean;
  peerNode?: Pick<FleetNodeItem, "name" | "controlStreamConnected"> | null;
  peerName?: string;
}

const RevokeNodeDialog = ({
  open,
  nodeName,
  affectedDevices,
  isLoadingImpact,
  impactError,
  onConfirm,
  onDismiss,
  isSubmitting,
  haRole = FleetNodeHARole.FLEET_NODE_HA_ROLE_UNSPECIFIED,
  haStandbyReady = false,
  peerNode,
  peerName,
}: RevokeNodeDialogProps) => {
  const countsByHardware = new Map<string, { label: string; count: number }>();
  for (const device of affectedDevices) {
    const manufacturer = device.manufacturer.trim() || "Unknown manufacturer";
    const model = device.model.trim() || "Unknown model";
    const key = JSON.stringify([manufacturer, model]);
    const group = countsByHardware.get(key);
    if (group) group.count++;
    else countsByHardware.set(key, { label: `${manufacturer} ${model}`, count: 1 });
  }
  const groupedHardware = [...countsByHardware].sort(([, first], [, second]) =>
    first.label.localeCompare(second.label),
  );

  const isStandby = haRole === FleetNodeHARole.FLEET_NODE_HA_ROLE_STANDBY;
  const peerLabel = peerNode?.name || peerName || (isStandby ? "The active node" : "the standby");
  const isActive = haRole === FleetNodeHARole.FLEET_NODE_HA_ROLE_ACTIVE;
  const handsOffDevices = isActive && haStandbyReady && !!peerNode?.controlStreamConnected;
  const keepsDevices = handsOffDevices || isStandby;

  return (
    <Dialog
      open={open}
      title="Revoke node?"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent="critical">
          <Alert />
        </DialogIcon>
      }
      buttons={[
        {
          text: "Cancel",
          onClick: onDismiss,
          variant: variants.secondary,
          disabled: isSubmitting,
        },
        {
          text: "Revoke node",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
          disabled: isLoadingImpact || !!impactError,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        {isStandby ? (
          <>
            Are you sure you want to revoke "{nodeName}"? The node immediately loses access to Fleet and the HA pair is
            removed. {peerLabel} keeps its devices.
          </>
        ) : handsOffDevices ? (
          <>
            Are you sure you want to revoke "{nodeName}"? Its devices move to {peerLabel} before the node is revoked,
            and the HA pair is removed.
          </>
        ) : (
          <>
            Are you sure you want to revoke "{nodeName}"? The node immediately loses access to Fleet, and its miner
            pairings and stored miner credentials are removed. Its miners keep running, but another node will need to
            discover and pair them again.
            {isActive
              ? ` The HA pair is removed, since ${peerLabel} ${haStandbyReady ? "isn't connected" : "isn't ready"} to take over.`
              : ""}
          </>
        )}{" "}
        This action cannot be undone.
      </div>
      {isLoadingImpact ? <div className="mt-3 text-300">Loading affected miners…</div> : null}
      {impactError ? (
        <div role="alert" className="mt-3 text-300 text-intent-critical-fill">
          {impactError}
        </div>
      ) : null}
      {!isLoadingImpact && !impactError && !keepsDevices ? (
        groupedHardware.length > 0 ? (
          <ul className="mt-3 max-h-32 list-disc overflow-y-auto pl-5 text-300">
            {groupedHardware.map(([key, { label, count }]) => {
              return (
                <li key={key}>
                  {count} paired {label} {count === 1 ? "miner" : "miners"} will lose connection.
                </li>
              );
            })}
          </ul>
        ) : (
          <div className="mt-3 text-300">No paired miners will lose connection.</div>
        )
      ) : null}
    </Dialog>
  );
};

export default RevokeNodeDialog;
