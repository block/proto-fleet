import { useCallback } from "react";
import { Copy, Lock, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Modal from "@/shared/components/Modal";
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
        title="Reset member password?"
        titleSize="text-heading-300"
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
    <Modal open={open} onDismiss={onDismiss} size="small" showHeader={false}>
      <div className="flex flex-col gap-6 py-6">
        <div className="flex items-start">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-5 text-intent-success-fill">
            <Success />
          </div>
        </div>

        <div>
          <div className="mb-2 text-heading-300 text-text-primary">Password reset</div>
          <div className="text-300 text-text-primary-70">
            {username}'s password has been reset. Save this password and share it with the user securely. It won't be
            shown again.
          </div>
        </div>

        <div className="flex items-center gap-2">
          <div
            className="flex-1 rounded-lg bg-surface-elevated-base px-4 py-3 font-mono text-300"
            data-testid="temporary-password"
          >
            {temporaryPassword}
          </div>
          <Button variant="ghost" onClick={handleCopyPassword} ariaLabel="Copy password" prefixIcon={<Copy />} />
        </div>

        <div className="flex justify-end">
          <Button variant={variants.primary} size={sizes.base} onClick={onDismiss} text="Done" />
        </div>
      </div>
    </Modal>
  );
};

export default ResetPasswordModal;
