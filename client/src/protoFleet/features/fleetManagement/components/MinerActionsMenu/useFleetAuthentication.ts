import { useCallback, useState } from "react";

interface UseFleetAuthenticationParams {
  onAuthenticated: (purpose: "security" | "pool", username: string, password: string) => void;
  onDismiss: () => void;
}

export const useFleetAuthentication = ({ onAuthenticated, onDismiss }: UseFleetAuthenticationParams) => {
  const [showAuthenticateFleetModal, setShowAuthenticateFleetModal] = useState(false);
  const [authenticationPurpose, setAuthenticationPurpose] = useState<"security" | "pool" | null>(null);
  const [fleetCredentials, setFleetCredentials] = useState<{ username: string; password: string } | undefined>(
    undefined,
  );

  const startAuthentication = useCallback((purpose: "security" | "pool") => {
    setAuthenticationPurpose(purpose);
    setShowAuthenticateFleetModal(true);
  }, []);

  const handleFleetAuthenticated = useCallback(
    (username: string, password: string) => {
      setFleetCredentials({ username, password });
      setShowAuthenticateFleetModal(false);
      if (authenticationPurpose) {
        onAuthenticated(authenticationPurpose, username, password);
      }
    },
    [authenticationPurpose, onAuthenticated],
  );

  const handleAuthDismiss = useCallback(() => {
    setShowAuthenticateFleetModal(false);
    setAuthenticationPurpose(null);
    setFleetCredentials(undefined);
    onDismiss();
  }, [onDismiss]);

  const resetAuthState = useCallback(() => {
    setShowAuthenticateFleetModal(false);
    setAuthenticationPurpose(null);
    setFleetCredentials(undefined);
  }, []);

  return {
    showAuthenticateFleetModal,
    authenticationPurpose,
    fleetCredentials,
    startAuthentication,
    handleFleetAuthenticated,
    handleAuthDismiss,
    resetAuthState,
  };
};
