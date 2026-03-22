import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface DeleteAllFirmwareDialogProps {
  open?: boolean;
  fileCount: number;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const DeleteAllFirmwareDialog = ({
  open,
  fileCount,
  onConfirm,
  onDismiss,
  isSubmitting,
}: DeleteAllFirmwareDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Delete all firmware files?"
      titleSize="text-heading-300"
      testId="delete-all-firmware-dialog"
      onDismiss={onDismiss}
      icon={
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-intent-critical-10">
          <Alert />
        </div>
      }
      buttons={[
        {
          text: "Cancel",
          onClick: onDismiss,
          variant: variants.secondary,
        },
        {
          text: "Delete all",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        This will permanently delete {fileCount === 1 ? "1 firmware file" : `all ${fileCount} firmware files`}. This
        action cannot be undone.
      </div>
    </Dialog>
  );
};

export default DeleteAllFirmwareDialog;
