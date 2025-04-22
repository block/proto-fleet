import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/contexts/AuthContext";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface RebootSystemProps {
  accessTokenValue?: string;
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useSystemReboot = () => {
  const { api } = useMinerHosting();
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const rebootSystem = useCallback(
    ({ accessTokenValue, onError, onSuccess }: RebootSystemProps = {}) => {
      if (!api) return;

      setPending(true);
      api
        .rebootSystem(
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
              rebootSystem({ accessTokenValue, onError, onSuccess });
            },
          });
        })
        .finally(() => {
          setPending(false);
        });
    },
    [authTokens.accessToken.value, handleAuthErrors, api],
  );

  return useMemo(() => ({ pending, rebootSystem }), [pending, rebootSystem]);
};

export { useSystemReboot };
