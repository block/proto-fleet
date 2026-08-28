import { useCallback } from "react";
import { useAuthErrors } from "./useAuth";
import type { AdapterRequestOptions } from "@/protoOS/api/adapters/types";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

interface AuthRetryOptions<T> {
  request: (params: AdapterRequestOptions) => Promise<T>;
  onSuccess?: (result: T) => void | Promise<void>;
  onError?: (err: ErrorProps) => void;
  shouldRetry?: (error: ErrorProps) => boolean;
}

/**
 * Wraps an authenticated API call with automatic token-refresh retry.
 *
 * Auth headers are the adapter's responsibility now — the adapter decorates its
 * own requests, so callers pass plain params and never build Authorization
 * headers here. On 401 this refreshes the access token (which updates the store)
 * and re-executes the request; the adapter re-decorates with the fresh token on
 * the retry. Callers never manage the retry loop or remember to `return`
 * promises — the chain is always connected.
 */
export const useAuthRetry = () => {
  const { handleAuthErrors } = useAuthErrors();
  const { isFleetHosted } = useMinerHosting();

  return useCallback(
    <T>({ request, onSuccess, onError, shouldRetry }: AuthRetryOptions<T>): Promise<void> => {
      const execute = (isRetry = false): Promise<void> =>
        request({})
          .then((result) => onSuccess?.(result))
          .catch((error) => {
            if (isFleetHosted || isRetry || (shouldRetry && !shouldRetry(error))) {
              onError?.(error);
              return;
            }
            // Refresh updates the store token; the retry's request re-decorates
            // with it via the adapter, so we don't thread the token through here.
            return handleAuthErrors({
              error,
              onError,
              onSuccess: () => execute(true),
            });
          });

      return execute();
    },
    [handleAuthErrors, isFleetHosted],
  );
};
