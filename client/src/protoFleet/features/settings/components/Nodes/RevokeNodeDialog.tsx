import type { FleetNodeDeviceSummary } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
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
  haRole?: string;
  isHaMember?: boolean;
  hasOtherMembers?: boolean;
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
  haRole,
  isHaMember = false,
  hasOtherMembers = false,
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

  const retainsPairings = isHaMember && hasOtherMembers;

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
        Are you sure you want to revoke "{nodeName}"? The node immediately loses access to Fleet.
        {retainsPairings ? (
          <>
            {" "}
            Its shared miner pairings and stored miner credentials are retained while another member remains.
            {haRole === "active"
              ? " Any ready standby can take over after the active lease expires."
              : " This removes this node's standby protection."}
          </>
        ) : (
          <>
            {" "}
            {isHaMember ? "This is the final member. " : ""}Its miner pairings and stored miner credentials are removed.
            Its miners keep running, but another node will need to discover and pair them again.
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
      {!isLoadingImpact && !impactError && !retainsPairings ? (
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
