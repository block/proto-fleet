// Cancellation is plumbed end-to-end through the protoOS adapters: an
// `AbortSignal` on `AdapterRequestOptions` reaches `fetch`. These helpers let
// hooks and the fan-out treat aborts as an ordinary, non-error outcome — a
// navigation or unmount cancelling in-flight work is expected, not a failure.

/** True when `error` is the abort produced by an aborted fetch/AbortController. */
export const isAbortError = (error: unknown): boolean => error instanceof DOMException && error.name === "AbortError";
