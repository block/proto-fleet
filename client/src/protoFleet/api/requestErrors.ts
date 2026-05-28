import { Code, ConnectError } from "@connectrpc/connect";

import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";

export function toError(error: unknown, fallbackMessage: string): Error {
  const message = getErrorMessage(error);
  if (message) {
    return new Error(message);
  }

  if (error instanceof Error && error.message) {
    return error;
  }

  return new Error(fallbackMessage);
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
