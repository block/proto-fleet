import { useCallback, useState } from "react";
import { useUserManagement } from "@/protoFleet/api/useUserManagement";
import { Alert, Copy, Success } from "@/shared/assets/icons";
import Button, { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
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

  // Reset form state when modal closes
  const [prevVisible, setPrevVisible] = useState(isVisible);
  if (prevVisible !== isVisible) {
    setPrevVisible(isVisible);
    if (!isVisible) {
      setStep("enterUsername");
      setUsername("");
      setTemporaryPassword("");
      setIsSubmitting(false);
      setErrorMsg("");
    }
  }

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
        title="Add team member"
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

        {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

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
    <Dialog
      open={isVisible}
      testId="modal"
      title="Member added"
      subtitle="Save this password and share it with the user securely. It won't be shown again."
      subtitleSize="text-300"
      onDismiss={handleDone}
      icon={
        <DialogIcon intent="success">
          <Success />
        </DialogIcon>
      }
      buttonGroupVariant={groupVariants.rightAligned}
      buttons={[
        {
          text: "Done",
          onClick: handleDone,
          variant: variants.primary,
        },
      ]}
    >
      <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-6 py-6">
        <div className="font-mono text-300 break-all text-text-primary" data-testid="temporary-password">
          {temporaryPassword}
        </div>
        <Button variant="ghost" onClick={handleCopyPassword} ariaLabel="Copy password" prefixIcon={<Copy />} />
      </div>
    </Dialog>
  );
};

export default AddTeamMemberModal;
