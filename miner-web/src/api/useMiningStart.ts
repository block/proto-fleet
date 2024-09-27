import { useCallback, useState } from "react";

import { ErrorProps } from "apiResponseTypes";

import { useAuthContext } from "common/hooks/useAuthContext";
import { useAuthErrors } from "common/hooks/useAuthErrors";

import { api } from "./api";
import { getAuthHeader } from "./constants";

interface StartMiningProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStart = () => {
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const startMining = useCallback(
    ({ onError, onSuccess }: StartMiningProps = {}) => {
      setPending(true);
      api
        .startMining(getAuthHeader(authTokens.accessToken.value))
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            onError,
            onSuccess: () => {
              startMining({ onError, onSuccess });
            },
          });
        })
        .finally(() => {
          setPending(false);
        });
    },
    [authTokens.accessToken.value, handleAuthErrors]
  );

  return {
    pending,
    startMining,
  };
};

export { useMiningStart };
