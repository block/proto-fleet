import { useCallback } from "react";
import { Copy, Lock, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup/constants";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

interface ResetPasswordModalProps {
  open?: boolean;
  username: string;
  temporaryPassword: string | null;
  onConfirm: () => void;
  onDismiss: () => void;
  isResetting: boolean;
}

const ResetPasswordModal = ({
  open,
  username,
  temporaryPassword,
  onConfirm,
  onDismiss,
  isResetting,
}: ResetPasswordModalProps) => {
  const handleCopyPassword = useCallback(() => {
    if (temporaryPassword) {
      copyToClipboard(temporaryPassword)
        .then(() => {
          pushToast({
            message: "Password copied to clipboard",
            status: STATUSES.success,
          });
        })
        .catch(() => {
          pushToast({
            message: "Failed to copy password",
            status: STATUSES.error,
          });
        });
    }
  }, [temporaryPassword]);

  // Step 1: Confirmation
  if (!temporaryPassword) {
    return (
      <Dialog
        open={open}
        testId="modal"
        title="Reset member password?"
        onDismiss={onDismiss}
        icon={
          <DialogIcon>
            <Lock />
          </DialogIcon>
        }
        buttons={[
          {
            text: "Cancel",
            onClick: onDismiss,
            variant: variants.secondary,
          },
          {
            text: "Reset member password",
            onClick: onConfirm,
            variant: variants.primary,
            loading: isResetting,
          },
        ]}
      >
        <div className="text-300 text-text-primary-70">
          Fleet generates a temporary password for you to share so they can log in and set a new one.
        </div>
      </Dialog>
    );
  }

  // Step 2: Show temporary password
  return (
    <Dialog
      open={open}
      testId="modal"
      title="Password reset"
      subtitle={`${username}'s password has been reset. Save this password and share it with the user securely. It won't be shown again.`}
      subtitleSize="text-300"
      subtitleClassName="text-text-primary-70"
      onDismiss={onDismiss}
      icon={
        <DialogIcon intent="success">
          <Success />
        </DialogIcon>
      }
      buttonGroupVariant={groupVariants.rightAligned}
      buttons={[
        {
          text: "Done",
          onClick: onDismiss,
          variant: variants.primary,
        },
      ]}
    >
      <div className="flex items-center gap-2 rounded-lg bg-surface-5 px-4 py-3">
        <div className="flex-1 font-mono text-300 break-all" data-testid="temporary-password">
          {temporaryPassword}
        </div>
        <button onClick={handleCopyPassword} className="shrink-0 text-text-primary" aria-label="Copy password">
          <Copy />
        </button>
      </div>
    </Dialog>
  );
};

export default ResetPasswordModal;
