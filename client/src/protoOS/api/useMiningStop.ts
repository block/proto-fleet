import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/contexts/AuthContext";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface StopMiningProps {
  accessTokenValue?: string;
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useMiningStop = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const stopMining = useCallback(
    ({ accessTokenValue, onError, onSuccess }: StopMiningProps = {}) => {
      if (!api) return;

      setPending(true);
      api
        .stopMining(
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
              stopMining({ accessTokenValue, onError, onSuccess });
            },
          });
        })
        .finally(() => {
          setPending(false);
        });
    },
    [authTokens.accessToken.value, handleAuthErrors, api],
  );

  return useMemo(() => ({ pending, stopMining }), [pending, stopMining]);
};

export { useMiningStop };
