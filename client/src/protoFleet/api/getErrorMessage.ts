import { ConnectError } from "@connectrpc/connect";

/**
 * Extracts user-facing error message from a Connect RPC error.
 * Strips protocol-level prefixes like "[internal]" that ConnectError.message includes.
 * If a fallback is provided, it is returned when the raw message is empty.
 */
export function getErrorMessage(err: unknown, fallback?: string): string {
  return ConnectError.from(err).rawMessage || fallback || "";
}
