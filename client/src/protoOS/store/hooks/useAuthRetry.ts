import { useCallback } from "react";
import useMinerStore from "../useMinerStore";
import { useAuthErrors } from "./useAuth";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { RequestParams } from "@/protoOS/api/generatedApi";

type AuthRequestParams = RequestParams & { headers: { Authorization: string } };

interface AuthRetryOptions<T> {
  request: (params: AuthRequestParams) => Promise<T>;
  onSuccess?: (result: T) => void | Promise<void>;
  onError?: (err: ErrorProps) => void;
  shouldRetry?: (error: ErrorProps) => boolean;
}

/**
 * Wraps an authenticated API call with automatic token-refresh retry.
 *
 * On 401, refreshes the access token and re-executes the request with the
 * fresh token. Callers never need to manage the retry loop, build fresh
 * headers, or remember to `return` promises — the chain is always connected.
 */
export const useAuthRetry = () => {
  const { handleAuthErrors } = useAuthErrors();

  return useCallback(
    <T>({ request, onSuccess, onError, shouldRetry }: AuthRetryOptions<T>): Promise<void> => {
      const authRequestParams: AuthRequestParams = {
        secure: false,
        headers: {
          Authorization: `Bearer ${useMinerStore.getState().auth.authTokens.accessToken?.value || ""}`,
        },
      };

      const execute = (params: AuthRequestParams, isRetry = false): Promise<void> =>
        request(params)
          .then((result) => onSuccess?.(result))
          .catch((error) => {
            if (isRetry || (shouldRetry && !shouldRetry(error))) {
              onError?.(error);
              return;
            }
            return handleAuthErrors({
              error,
              onError,
              onSuccess: (accessToken) =>
                execute(
                  {
                    secure: false,
                    headers: { Authorization: `Bearer ${accessToken}` },
                  },
                  true,
                ),
            });
          });

      return execute(authRequestParams);
    },
    [handleAuthErrors],
  );
};
