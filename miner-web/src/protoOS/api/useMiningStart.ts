import { useCallback, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/contexts/AuthContext";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface StartMiningProps {
  accessTokenValue?: string;
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStart = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const startMining = useCallback(
    ({ accessTokenValue, onError, onSuccess }: StartMiningProps = {}) => {
      if (!api) return;

      setPending(true);
      api
        .startMining(
          getAuthHeader(accessTokenValue || authTokens.accessToken.value),
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
    [authTokens.accessToken.value, handleAuthErrors, api],
  );

  return {
    pending,
    startMining,
  };
};

export { useMiningStart };
