import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface DeleteFirmwareDialogProps {
  open?: boolean;
  filename: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const DeleteFirmwareDialog = ({ open, filename, onConfirm, onDismiss, isSubmitting }: DeleteFirmwareDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Delete firmware file?"
      testId="delete-firmware-dialog"
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
          text: "Delete",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        This will permanently delete "{filename}". This action cannot be undone.
      </div>
    </Dialog>
  );
};

export default DeleteFirmwareDialog;
