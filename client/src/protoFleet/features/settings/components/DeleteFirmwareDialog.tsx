import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

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
      titleSize="text-heading-300"
      testId="delete-firmware-dialog"
      onDismiss={onDismiss}
      icon={
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-5 text-intent-critical-fill">
          <Alert />
        </div>
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
