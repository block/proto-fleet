import React, { useCallback, useState } from "react";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import { PasswordStrengthMeter, WeakPasswordWarning } from "@/shared/components/Setup";
import { isPasswordTooShort, isWeakPassword, passwordErrors } from "@/shared/components/Setup/authentication.constants";

interface UpdateMinerPasswordModalProps {
  open: boolean;
  hasThirdPartyMiners: boolean;
  onConfirm: (currentPassword: string, newPassword: string) => void;
  onDismiss: () => void;
}

const UpdateMinerPasswordModal = ({
  open,
  hasThirdPartyMiners,
  onConfirm,
  onDismiss,
}: UpdateMinerPasswordModalProps) => {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [score, setScore] = useState(0);
  const [validationError, setValidationError] = useState("");
  const [showWeakPasswordWarning, setShowWeakPasswordWarning] = useState(false);

  // Reset form when modal is dismissed
  const [prevOpen, setPrevOpen] = useState(open);
  if (prevOpen !== open) {
    setPrevOpen(open);
    if (!open) {
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setScore(0);
      setValidationError("");
      setShowWeakPasswordWarning(false);
    }
  }

  const handleConfirm = useCallback(
    (forceWeakPassword: boolean) => {
      setValidationError("");

      if (!currentPassword) {
        setValidationError("Current password is required");
        return;
      }

      if (!newPassword) {
        setValidationError("New password is required");
        return;
      }

      if (!confirmPassword) {
        setValidationError("Password confirmation is required");
        return;
      }

      if (newPassword !== confirmPassword) {
        setValidationError(passwordErrors.mismatch);
        return;
      }

      // Additional validation for Proto rigs only (centralized validation from authentication.constants.ts)
      if (!hasThirdPartyMiners) {
        if (isPasswordTooShort(newPassword)) {
          setValidationError(passwordErrors.tooShort);
          return;
        }

        if (!forceWeakPassword && isWeakPassword(score)) {
          setShowWeakPasswordWarning(true);
          return;
        }
      }

      setShowWeakPasswordWarning(false);
      onConfirm(currentPassword, newPassword);
    },
    [currentPassword, newPassword, confirmPassword, score, hasThirdPartyMiners, onConfirm],
  );

  const handleDismiss = () => {
    setShowWeakPasswordWarning(false);
    onDismiss();
  };

  const canConfirm = currentPassword && newPassword && confirmPassword;

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && canConfirm) {
        e.preventDefault();
        handleConfirm(false);
      }
    },
    [canConfirm, handleConfirm],
  );

  // Conditionally render one modal at a time for proper animations
  if (showWeakPasswordWarning) {
    return (
      <WeakPasswordWarning onReturn={() => setShowWeakPasswordWarning(false)} onContinue={() => handleConfirm(true)} />
    );
  }

  return (
    <Modal
      open={open}
      title="Update the admin login for your miners"
      onDismiss={handleDismiss}
      buttons={[
        {
          text: "Continue",
          variant: variants.primary,
          onClick: () => handleConfirm(false),
          disabled: !canConfirm,
          dismissModalOnClick: false,
        },
      ]}
      divider={false}
      className="w-full"
    >
      <div className="mb-6 text-300 text-text-primary-70">
        This password will be required to make any changes to pools or miner performance.
      </div>

      {validationError ? (
        <Callout className="mb-4" intent="danger" prefixIcon={<Alert />} title={validationError} />
      ) : null}

      <div className="flex flex-col gap-4" onKeyDown={handleKeyDown}>
        <Input
          id="currentPassword"
          label="Current miner password"
          type="password"
          onChange={(value) => setCurrentPassword(value)}
          autoFocus
        />

        <div className="flex flex-col gap-2">
          <Input
            id="newPassword"
            label="New miner password"
            type="password"
            onChange={(value) => setNewPassword(value)}
          />
          {!hasThirdPartyMiners && (
            <div className="flex items-center justify-between gap-5">
              <div className="text-200 text-text-primary-50">Password strength</div>
              <PasswordStrengthMeter score={score} onSetScore={setScore} password={newPassword} />
            </div>
          )}
        </div>

        <Input
          id="confirmPassword"
          label="Confirm new miner password"
          type="password"
          onChange={(value) => setConfirmPassword(value)}
        />
      </div>
    </Modal>
  );
};

export default UpdateMinerPasswordModal;
