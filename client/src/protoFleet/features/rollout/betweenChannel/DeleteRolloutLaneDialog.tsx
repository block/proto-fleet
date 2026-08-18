import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";

interface DeleteRolloutLaneDialogProps {
  open?: boolean;
  laneLabel: string;
  isSubmitting: boolean;
  error?: string | null;
  onConfirm: () => void;
  onDismiss: () => void;
}

export default function DeleteRolloutLaneDialog({
  open,
  laneLabel,
  isSubmitting,
  error,
  onConfirm,
  onDismiss,
}: DeleteRolloutLaneDialogProps) {
  return (
    <Dialog
      open={open}
      title={`Delete ${laneLabel}?`}
      testId="delete-rollout-lane-dialog"
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
          text: "Delete lane",
          onClick: onConfirm,
          variant: variants.danger,
          loading: isSubmitting,
        },
      ]}
    >
      <div className="grid gap-3 text-300 text-text-primary-70">
        <p>Miners in this lane will become unmanaged.</p>
        <p>Rollout and release history will be retained.</p>
        {error ? (
          <p role="alert" className="text-text-critical">
            {error}
          </p>
        ) : null}
      </div>
    </Dialog>
  );
}
