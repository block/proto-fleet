import { useCallback, useMemo } from "react";

import { useAuthContext } from "./useAuthContext";
import { useRefresh } from "@/protoOS/api";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

interface HandleAuthErrorsProps {
  error: ErrorProps;
  onError?: (err: ErrorProps) => void;
  onSuccess?: (accessToken: string) => void;
}

const useAuthErrors = () => {
  const { authTokens, setAuthTokens, setShowLoginModal } = useAuthContext();
  const refresh = useRefresh();

  const handleAuthErrors = useCallback(
    ({ error, onError, onSuccess }: HandleAuthErrorsProps) => {
      if (error?.status === 401) {
        refresh({
          refreshToken: authTokens.refreshToken.value,
          onSuccess,
          onError: (refreshError) => {
            if (refreshError?.status === 401) {
              setAuthTokens({
                accessToken: { value: "", expiry: new Date() },
                refreshToken: { value: "", expiry: new Date() },
              });
              setShowLoginModal(true);
              onError?.(error);
            }
          },
        });
      } else {
        onError?.(error);
      }
    },
    [authTokens.refreshToken.value, refresh, setAuthTokens, setShowLoginModal],
  );

  return useMemo(
    () => ({
      handleAuthErrors,
    }),
    [handleAuthErrors],
  );
};

export { useAuthErrors };
