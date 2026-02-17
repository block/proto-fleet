import React, { useCallback, useEffect, useState } from "react";
import { authClient } from "@/protoFleet/api/clients";
import { variants } from "@/shared/components/Button";
import ButtonGroup from "@/shared/components/ButtonGroup";
import { groupVariants } from "@/shared/components/ButtonGroup/constants";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal/Modal";

const purposeToSettingsType: Record<string, string> = {
  security: "security",
  pool: "pool",
};

interface AuthenticateFleetModalProps {
  show: boolean;
  purpose?: "security" | "pool";
  onAuthenticated: (username: string, password: string) => void;
  onDismiss: () => void;
}

const AuthenticateFleetModal = ({ show, purpose, onAuthenticated, onDismiss }: AuthenticateFleetModalProps) => {
  const settingsType = purpose ? purposeToSettingsType[purpose] : undefined;
  const title = settingsType ? `Log in to update your ${settingsType} settings` : "Log in to update settings";
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const [isVerifying, setIsVerifying] = useState(false);

  // Reset form when modal is dismissed
  useEffect(() => {
    if (!show) {
      setUsername("");
      setPassword("");
      setErrorMessage("");
      setIsVerifying(false);
    }
  }, [show]);

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
      // Verify credentials using the Authenticate API
      await authClient.authenticate({ username, password });

      // If successful, call onAuthenticated with the credentials
      onAuthenticated(username, password);
    } catch (error) {
      // Show error message
      const message = error instanceof Error ? error.message : "Authentication failed";
      setErrorMessage(message);
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
      show={show}
      title={title}
      description="Contact your system administrator if you need access to edit settings."
      onDismiss={onDismiss}
      icon={null}
      size="small"
      divider={false}
      className="!max-w-[400px]"
      bodyClassName="-mt-2"
    >
      {errorMessage && (
        <div className="mb-4 rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 text-intent-critical-text">
          {errorMessage}
        </div>
      )}

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
