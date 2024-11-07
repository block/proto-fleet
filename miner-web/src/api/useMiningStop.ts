import { useCallback, useState } from "react";

import { ErrorProps } from "apiResponseTypes";

import { useAuthContext } from "common/hooks/useAuthContext";
import { useAuthErrors } from "common/hooks/useAuthErrors";

import { api } from "./api";
import { getAuthHeader } from "./constants";

interface StopMiningProps {
  accessTokenValue?: string;
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStop = () => {
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const stopMining = useCallback(
    ({ accessTokenValue, onError, onSuccess }: StopMiningProps = {}) => {
      setPending(true);
      api
        .stopMining(
          getAuthHeader(accessTokenValue || authTokens.accessToken.value)
        )
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            onError,
            onSuccess: (accessTokenValue) => {
              stopMining({ accessTokenValue, onError, onSuccess });
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
    stopMining,
  };
};

export { useMiningStop };
