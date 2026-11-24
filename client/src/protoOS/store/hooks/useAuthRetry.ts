import { useCallback } from "react";
import { useAuthErrors } from "./useAuth";
import type { AdapterRequestOptions } from "@/protoOS/api/adapters/types";
import { ErrorProps } from "@/protoOS/api/apiResponseTypes";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { isAbortError } from "@/shared/utils/abort";

interface AuthRetryOptions<T> {
  request: (params: AdapterRequestOptions) => Promise<T>;
  onSuccess?: (result: T) => void | Promise<void>;
  onError?: (err: ErrorProps) => void;
  shouldRetry?: (error: ErrorProps) => boolean;
  /**
   * Cancels the request (and its retry) when aborted. An aborted request
   * resolves silently — `onError` does not fire — since a cancel from
   * navigation/unmount is teardown, not a failure the UI should surface.
   */
  signal?: AbortSignal;
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
    <T>({ request, onSuccess, onError, shouldRetry, signal }: AuthRetryOptions<T>): Promise<void> => {
      const execute = (isRetry = false): Promise<void> =>
        request({ signal })
          .then((result) => onSuccess?.(result))
          .catch((error) => {
            // A cancelled request is teardown, not an error: skip retry and
            // onError so a navigated-away view doesn't surface a stale message.
            // Guard on the signal too, not just the error type: an abort landing
            // mid body-parse can surface as a resolved {data:null} that the
            // adapter's normalizer then rethrows as a non-abort error. If our own
            // signal fired, treat whatever bubbles up as teardown.
            if (isAbortError(error) || signal?.aborted) return;
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
