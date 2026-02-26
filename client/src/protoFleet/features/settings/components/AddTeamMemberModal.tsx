import { useCallback, useEffect, useState } from "react";
import { useUserManagement } from "@/protoFleet/api/useUserManagement";
import { Copy, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { copyToClipboard } from "@/shared/utils/utility";

interface AddTeamMemberModalProps {
  open?: boolean;
  onDismiss: () => void;
  onSuccess: () => void;
}

type ModalStep = "enterUsername" | "displayPassword";

const AddTeamMemberModal = ({ open, onDismiss, onSuccess }: AddTeamMemberModalProps) => {
  const isVisible = open ?? true;
  const { createUser } = useUserManagement();
  const [step, setStep] = useState<ModalStep>("enterUsername");
  const [username, setUsername] = useState("");
  const [temporaryPassword, setTemporaryPassword] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");

  useEffect(() => {
    if (isVisible) {
      return;
    }

    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset modal state on close
    setStep("enterUsername");
    setUsername("");
    setTemporaryPassword("");
    setIsSubmitting(false);
    setErrorMsg("");
  }, [isVisible]);

  const handleCreateUser = useCallback(() => {
    if (!username.trim()) {
      setErrorMsg("Username is required");
      return;
    }

    setIsSubmitting(true);
    setErrorMsg("");

    createUser({
      username: username.trim(),
      onSuccess: (_userId, _username, tempPassword) => {
        setTemporaryPassword(tempPassword);
        setStep("displayPassword");
        pushToast({
          message: `Team member ${username} created successfully`,
          status: STATUSES.success,
        });
      },
      onError: (error) => {
        setErrorMsg(error || "Failed to create user. Please try again.");
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }, [username, createUser]);

  const handleCopyPassword = useCallback(() => {
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
  }, [temporaryPassword]);

  const handleDone = useCallback(() => {
    onSuccess();
    onDismiss();
  }, [onSuccess, onDismiss]);

  if (step === "enterUsername") {
    return (
      <Modal
        open={isVisible}
        onDismiss={onDismiss}
        size="small"
        contentHeader="Add team member"
        buttons={[
          {
            text: "Save",
            onClick: handleCreateUser,
            variant: variants.primary,
            loading: isSubmitting,
            dismissModalOnClick: false,
          },
        ]}
        divider={false}
      >
        <div className="mb-6">
          Add a member by entering their username. Fleet generates a temporary password for you to share so they can log
          in and set a new one.
        </div>

        {errorMsg ? (
          <div className="mb-6 rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 text-intent-critical-text">
            {errorMsg}
          </div>
        ) : null}

        <Input
          id="username"
          label="Username"
          initValue={username}
          onChange={(value) => {
            setUsername(value);
            setErrorMsg("");
          }}
          autoFocus
        />
      </Modal>
    );
  }

  return (
    <Modal open={isVisible} onDismiss={handleDone} size="small" showHeader={false}>
      <div className="flex flex-col gap-6 py-6">
        <div className="flex items-start">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-intent-success-10">
            <Success />
          </div>
        </div>

        <div>
          <div className="mb-2 text-heading-300 text-text-primary">Member added</div>
          <div className="text-300 text-text-primary-70">
            Save this password and share it with the user securely. It won't be shown again.
          </div>
        </div>

        <div className="flex items-center gap-2">
          <div
            className="flex-1 rounded-lg bg-surface-elevated-base px-4 py-3 font-mono text-300"
            data-testid="temporary-password"
          >
            {temporaryPassword}
          </div>
          <button
            onClick={handleCopyPassword}
            className="flex h-10 w-10 items-center justify-center rounded-lg border border-border-10 bg-surface-base text-text-primary hover:bg-surface-elevated-base"
            aria-label="Copy password"
          >
            <Copy />
          </button>
        </div>

        <div className="flex justify-end">
          <Button variant={variants.primary} size={sizes.base} onClick={handleDone} text="Done" />
        </div>
      </div>
    </Modal>
  );
};

export default AddTeamMemberModal;
