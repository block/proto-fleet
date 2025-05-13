import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/features/auth/contexts/AuthContext";

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

  return useMemo(() => ({ pending, startMining }), [pending, startMining]);
};

export { useMiningStart };
