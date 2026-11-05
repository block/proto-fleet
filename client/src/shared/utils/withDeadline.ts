// Bound the caller's wait even when a transport or authentication refresh does
// not settle on abort. The same signal also prevents a late auth retry from
// issuing another command after the outcome has become unknown.
export const withDeadline = async <T>(request: () => Promise<T>, controller: AbortController, timeoutMs: number) => {
  const { signal } = controller;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let onAbort: () => void = () => {};
  try {
    return await new Promise<T>((resolve, reject) => {
      onAbort = () => reject(signal.reason);
      if (signal.aborted) {
        onAbort();
        return;
      }
      signal.addEventListener("abort", onAbort, { once: true });
      timer = setTimeout(() => controller.abort(new DOMException("Request timed out", "TimeoutError")), timeoutMs);
      request().then(resolve, reject);
    });
  } finally {
    clearTimeout(timer);
    signal.removeEventListener("abort", onAbort);
  }
};
