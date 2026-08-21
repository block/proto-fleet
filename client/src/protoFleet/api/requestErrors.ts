import { Code, ConnectError } from "@connectrpc/connect";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

const authOrPermissionErrorCodes = new Set<Code>([Code.Unauthenticated, Code.PermissionDenied]);

interface ErrorWithCause extends Error {
  cause?: unknown;
}

function createErrorWithCause(message: string, cause: unknown): Error {
  const error = new Error(message) as ErrorWithCause;
  error.cause = cause;
  return error;
}

export function getErrorCause(error: unknown): unknown {
  return error instanceof Error ? (error as ErrorWithCause).cause : undefined;
}

export function toError(error: unknown, fallbackMessage: string): Error {
  const message = getErrorMessage(error);
  if (message) {
    return createErrorWithCause(message, error);
  }

  if (error instanceof Error && error.message) {
    return error;
  }

  return createErrorWithCause(fallbackMessage, error);
}

export function assertNotAborted(signal?: AbortSignal): void {
  if (signal?.aborted) {
    throw new DOMException("The operation was aborted.", "AbortError");
  }
}

export function isAbortError(error: unknown, signal?: AbortSignal): boolean {
  if (error instanceof DOMException && error.name === "AbortError") {
    return true;
  }

  return error instanceof ConnectError && error.code === Code.Canceled && Boolean(signal?.aborted);
}

export function isAuthOrPermissionError(error: unknown): boolean {
  return error instanceof ConnectError && authOrPermissionErrorCodes.has(error.code);
}

export function isPermissionDeniedError(error: unknown): boolean {
  return error instanceof ConnectError && error.code === Code.PermissionDenied;
}
