import { useCallback, useState } from "react";

import { ErrorProps } from "apiResponseTypes";

import { useAuthContext } from "common/hooks/useAuthContext";
import { useAuthErrors } from "common/hooks/useAuthErrors";

import { api } from "./api";
import { getAuthHeader } from "./constants";

interface StartMiningProps {
  accessTokenValue?: string;
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStart = () => {
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const startMining = useCallback(
    ({ accessTokenValue, onError, onSuccess }: StartMiningProps = {}) => {
      setPending(true);
      api
        .startMining(
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
              startMining({ accessTokenValue, onError, onSuccess });
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
