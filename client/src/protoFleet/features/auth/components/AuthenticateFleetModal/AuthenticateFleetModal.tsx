import React, { useCallback, useEffect, useState } from "react";
import { authClient } from "@/protoFleet/api/clients";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import ButtonGroup from "@/shared/components/ButtonGroup";
import { groupVariants } from "@/shared/components/ButtonGroup/constants";
import Callout from "@/shared/components/Callout";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";

interface AuthenticateFleetModalProps {
  open: boolean;
  purpose?: "security" | "pool" | "workerNames";
  onAuthenticated: (username: string, password: string) => void;
  onDismiss: () => void;
}

const modalTitlesByPurpose = {
  security: "Log in to update your security settings",
  pool: "Log in to update your pool settings",
  workerNames: "Log in to update worker names",
} satisfies Record<NonNullable<AuthenticateFleetModalProps["purpose"]>, string>;

const AuthenticateFleetModal = ({ open, purpose, onAuthenticated, onDismiss }: AuthenticateFleetModalProps) => {
  const title = purpose ? modalTitlesByPurpose[purpose] : "Log in to update settings";
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const [isVerifying, setIsVerifying] = useState(false);

  // Reset form when modal is dismissed
  useEffect(() => {
    if (!open) {
      setUsername("");
      setPassword("");
      setErrorMessage("");
      setIsVerifying(false);
    }
  }, [open]);

  const canContinue = username && password && !isVerifying;

  const handleContinue = useCallback(async () => {
    // Clear previous error
    setErrorMessage("");

    // Validate fields
    if (!username || !password) {
      setErrorMessage("Username and password are required");
      return;
    }

    setIsVerifying(true);

    try {
      await authClient.verifyCredentials({ username, password });

      // If successful, call onAuthenticated with the credentials
      onAuthenticated(username, password);
    } catch {
      setErrorMessage("Invalid credentials entered.");
    } finally {
      setIsVerifying(false);
    }
  }, [username, password, onAuthenticated]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === "Enter" && canContinue) {
        e.preventDefault();
        handleContinue();
      }
    },
    [canContinue, handleContinue],
  );

  return (
    <Modal
      open={open}
      title={title}
      description="Contact your system administrator if you need access to edit settings."
      onDismiss={onDismiss}
      icon={null}
      divider={false}
      bodyClassName="-mt-2"
    >
      {errorMessage ? <Callout className="mb-4" intent="danger" prefixIcon={<Alert />} title={errorMessage} /> : null}

      <div className="flex flex-col gap-4" onKeyDown={handleKeyDown}>
        <Input
          id="username"
          label="Username"
          type="text"
          onChange={(value) => setUsername(value)}
          disabled={isVerifying}
          autoFocus
        />

        <Input
          id="password"
          label="Password"
          type="password"
          onChange={(value) => setPassword(value)}
          disabled={isVerifying}
        />
      </div>

      <ButtonGroup
        className="mt-4"
        variant={groupVariants.fill}
        buttons={[
          {
            text: "Cancel",
            variant: variants.secondary,
            onClick: onDismiss,
            disabled: isVerifying,
          },
          {
            text: "Continue",
            variant: variants.primary,
            onClick: handleContinue,
            disabled: !canContinue,
            loading: isVerifying,
          },
        ]}
      />
    </Modal>
  );
};

export default AuthenticateFleetModal;
