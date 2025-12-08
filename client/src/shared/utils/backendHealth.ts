import { Code, ConnectError } from "@connectrpc/connect";

/**
 * Determines if an error indicates the backend is completely down.
 *
 * Checks for:
 * - Code.Unknown (2): HTTP 500 errors
 * - Code.Internal: Internal server errors
 * - Code.Unavailable: Backend unreachable/connection failed
 */
export const isBackendDownError = (error: unknown): boolean => {
  return (
    error instanceof ConnectError &&
    (error.code === Code.Unknown || error.code === Code.Internal || error.code === Code.Unavailable)
  );
};
