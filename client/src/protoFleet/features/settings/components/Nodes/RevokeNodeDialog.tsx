import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface RevokeNodeDialogProps {
  open?: boolean;
  nodeName: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const RevokeNodeDialog = ({ open, nodeName, onConfirm, onDismiss, isSubmitting }: RevokeNodeDialogProps) => {
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
        },
        {
          text: "Revoke node",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        Are you sure you want to revoke "{nodeName}"? The node immediately loses access to Fleet, and its miner pairings
        and stored miner credentials are removed. Its miners keep running, but another node will need to discover and
        pair them again. This action cannot be undone.
      </div>
    </Dialog>
  );
};

export default RevokeNodeDialog;
