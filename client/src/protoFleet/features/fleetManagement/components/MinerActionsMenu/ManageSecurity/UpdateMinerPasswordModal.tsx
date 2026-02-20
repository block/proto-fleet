import React, { useCallback, useEffect, useState } from "react";
import { variants } from "@/shared/components/Button";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";
import { PasswordStrengthMeter, WeakPasswordWarning } from "@/shared/components/Setup";
import { isPasswordTooShort, isWeakPassword, passwordErrors } from "@/shared/components/Setup/authentication.constants";

interface UpdateMinerPasswordModalProps {
  show: boolean;
  hasThirdPartyMiners: boolean;
  onConfirm: (currentPassword: string, newPassword: string) => void;
  onDismiss: () => void;
}

const UpdateMinerPasswordModal = ({
  show,
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
  useEffect(() => {
    if (!show) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- Form reset on modal close is intentional
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      setScore(0);
      setValidationError("");
      setShowWeakPasswordWarning(false);
    }
  }, [show]);

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
      show={show}
      contentHeader="Update the admin login for your miners"
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
      size="small"
      divider={false}
      className="w-full"
      buttonSize="base"
      contentHeaderClassName="text-heading-300"
    >
      <div className="mb-6 text-300 text-text-primary-70">
        This password will be required to make any changes to pools or miner performance.
      </div>

      {validationError && (
        <div className="mb-4 rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 text-intent-critical-text">
          {validationError}
        </div>
      )}

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
