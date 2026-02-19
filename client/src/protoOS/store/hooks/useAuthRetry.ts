import { useCallback } from "react";
import { useAuthErrors, useAuthHeader } from "./useAuth";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";

type AuthHeader = { headers: { Authorization: string } };

interface AuthRetryOptions<T> {
  request: (header: AuthHeader) => Promise<T>;
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
  const authHeader = useAuthHeader();
  const { handleAuthErrors } = useAuthErrors();

  return useCallback(
    <T>({ request, onSuccess, onError, shouldRetry }: AuthRetryOptions<T>): Promise<void> => {
      const execute = (header: AuthHeader, isRetry = false): Promise<void> =>
        request(header)
          .then((result) => onSuccess?.(result))
          .catch((error) => {
            if (isRetry || (shouldRetry && !shouldRetry(error))) {
              onError?.(error);
              return;
            }
            return handleAuthErrors({
              error,
              onError,
              onSuccess: (accessToken) => execute({ headers: { Authorization: `Bearer ${accessToken}` } }, true),
            });
          });

      return execute(authHeader);
    },
    [authHeader, handleAuthErrors],
  );
};
