export const mapWithConcurrency = async <T, R>(
  values: T[],
  limit: number,
  map: (value: T) => Promise<R>,
  signal?: AbortSignal,
): Promise<R[]> => {
  if (!values.length) return [];

  const results = new Array<R>(values.length);
  const boundedLimit = Number.isFinite(limit) ? Math.max(1, Math.floor(limit)) : 1;
  const workerCount = Math.min(boundedLimit, values.length);
  let next = 0;
  const worker = async () => {
    while (next < values.length) {
      // Stop launching new work once aborted. On a large CT1 fan-out (up to 864
      // boards) this is what lets a navigation cancel the remaining waves instead
      // of running them all to completion; the individual in-flight fetches abort
      // via the same signal threaded through each request.
      if (signal?.aborted) throw new DOMException("The operation was aborted.", "AbortError");
      const index = next;
      next += 1;
      results[index] = await map(values[index]!);
    }
  };
  await Promise.all(Array.from({ length: workerCount }, worker));
  return results;
};
