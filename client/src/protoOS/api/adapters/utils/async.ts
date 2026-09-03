export const mapWithConcurrency = async <T, R>(
  values: T[],
  limit: number,
  map: (value: T) => Promise<R>,
): Promise<R[]> => {
  if (!values.length) return [];

  const results = new Array<R>(values.length);
  const boundedLimit = Number.isFinite(limit) ? Math.max(1, Math.floor(limit)) : 1;
  const workerCount = Math.min(boundedLimit, values.length);
  let next = 0;
  const worker = async () => {
    while (next < values.length) {
      const index = next;
      next += 1;
      results[index] = await map(values[index]!);
    }
  };
  await Promise.all(Array.from({ length: workerCount }, worker));
  return results;
};
