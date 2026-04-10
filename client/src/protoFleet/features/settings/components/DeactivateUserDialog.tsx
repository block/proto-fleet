import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface DeactivateUserDialogProps {
  open?: boolean;
  username: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const DeactivateUserDialog = ({ open, username, onConfirm, onDismiss, isSubmitting }: DeactivateUserDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Deactivate member?"
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
          text: "Confirm deactivation",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        Are you sure you want to deactivate this member ({username})? They will be hidden and removed from your account.
        This action cannot be undone.
      </div>
    </Dialog>
  );
};

export default DeactivateUserDialog;
