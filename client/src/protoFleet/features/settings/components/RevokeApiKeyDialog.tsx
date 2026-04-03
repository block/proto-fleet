import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";

interface RevokeApiKeyDialogProps {
  open?: boolean;
  keyName: string;
  onConfirm: () => void;
  onDismiss: () => void;
  isSubmitting: boolean;
}

const RevokeApiKeyDialog = ({ open, keyName, onConfirm, onDismiss, isSubmitting }: RevokeApiKeyDialogProps) => {
  return (
    <Dialog
      open={open}
      title="Revoke API key?"
      titleSize="text-heading-300"
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
        },
        {
          text: "Revoke key",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="text-300 text-text-primary-70">
        Are you sure you want to revoke the API key "{keyName}"? Any applications or scripts using this key will
        immediately lose access. This action cannot be undone.
      </div>
    </Dialog>
  );
};

export default RevokeApiKeyDialog;
