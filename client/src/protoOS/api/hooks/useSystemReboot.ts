import { useCallback, useMemo, useState } from "react";

import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import {
  getAuthHeader,
  useAuthContext,
  useAuthErrors,
} from "@/protoOS/features/auth/contexts/AuthContext";

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
          setPending(false);
          handleAuthErrors({
            error,
            onError,
            onSuccess: (accessTokenValue) => {
              rebootSystem({ accessTokenValue, onError, onSuccess });
            },
          });
        });
    },
    [authTokens.accessToken.value, handleAuthErrors, api],
  );

  return useMemo(() => ({ pending, rebootSystem }), [pending, rebootSystem]);
};

export { useSystemReboot };
