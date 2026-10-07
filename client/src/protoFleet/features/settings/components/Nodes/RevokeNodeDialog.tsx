import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface RevokeNodeDialogProps {
  open?: boolean;
  nodeName: string;
  affectedDeviceTypes: string[];
  isLoadingImpact: boolean;
  impactError: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const RevokeNodeDialog = ({
  open,
  nodeName,
  affectedDeviceTypes,
  isLoadingImpact,
  impactError,
  onConfirm,
  onDismiss,
  isSubmitting,
}: RevokeNodeDialogProps) => {
  const protoRigCount = affectedDeviceTypes.filter((type) => type === "proto").length;
  const otherMinerCount = affectedDeviceTypes.length - protoRigCount;

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
          disabled: isLoadingImpact || !!impactError || isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        Are you sure you want to revoke "{nodeName}"? The node immediately loses access to Fleet, and its miner pairings
        and stored miner credentials are removed. Its miners keep running, but another node will need to discover and
        pair them again. This action cannot be undone.
      </div>
      {isLoadingImpact ? <div className="mt-3 text-300">Loading affected miners…</div> : null}
      {impactError ? (
        <div role="alert" className="mt-3 text-300 text-intent-critical-fill">
          {impactError}
        </div>
      ) : null}
      {!isLoadingImpact && !impactError ? (
        <div className="mt-3 text-300">
          {protoRigCount > 0 ? (
            <div>
              {protoRigCount} paired Proto {protoRigCount === 1 ? "Rig" : "Rigs"} will lose connection.
            </div>
          ) : null}
          {otherMinerCount > 0 ? (
            <div>
              {otherMinerCount} paired {otherMinerCount === 1 ? "miner" : "miners"} will lose connection.
            </div>
          ) : null}
          {affectedDeviceTypes.length === 0 ? <div>No paired miners will lose connection.</div> : null}
        </div>
      ) : null}
    </Dialog>
  );
};

export default RevokeNodeDialog;
