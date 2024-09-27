import { useCallback, useState } from "react";

import { ErrorProps } from "apiResponseTypes";

import { useAuthContext } from "common/hooks/useAuthContext";
import { useAuthErrors } from "common/hooks/useAuthErrors";

import { api } from "./api";
import { getAuthHeader } from "./constants";

interface RebootSystemProps {
  onError?: (err: ErrorProps) => void;
  onSuccess?: () => void;
}

const useSystemReboot = () => {
  const [pending, setPending] = useState<boolean>(false);
  const { authTokens } = useAuthContext();
  const { handleAuthErrors } = useAuthErrors();

  const rebootSystem = useCallback(
    ({ onError, onSuccess }: RebootSystemProps = {}) => {
      setPending(true);
      api
        .rebootSystem(getAuthHeader(authTokens.accessToken.value))
        .then(() => {
          onSuccess?.();
        })
        .catch((error) => {
          handleAuthErrors({
            error,
            onError,
            onSuccess: () => {
              rebootSystem({ onError, onSuccess });
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
    rebootSystem,
  };
};

export { useSystemReboot };
