import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface DeactivateUserDialogProps {
  username: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const DeactivateUserDialog = ({
  username,
  onConfirm,
  onDismiss,
  isSubmitting,
}: DeactivateUserDialogProps) => {
  return (
    <Dialog
      show
      title="Deactivate member?"
      titleSize="text-heading-300"
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
          text: "Confirm deactivation",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        Are you sure you want to deactivate this member ({username})? They will
        be hidden and removed from your account. This action cannot be undone.
      </div>
    </Dialog>
  );
};

export default DeactivateUserDialog;
