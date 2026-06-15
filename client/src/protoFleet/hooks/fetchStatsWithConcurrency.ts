const STATS_REQUEST_CONCURRENCY = 8;

export async function fetchStatsWithConcurrency<T>(
  ids: bigint[],
  fetchOne: (id: bigint) => Promise<T>,
): Promise<PromiseSettledResult<T>[]> {
  const results = new Array<PromiseSettledResult<T>>(ids.length);
  let nextIndex = 0;

  const workerCount = Math.min(STATS_REQUEST_CONCURRENCY, ids.length);
  const workers = Array.from({ length: workerCount }, async () => {
    for (;;) {
      const index = nextIndex;
      nextIndex += 1;
      if (index >= ids.length) return;
      const id = ids[index];
      if (id === undefined) return;

      try {
        results[index] = { status: "fulfilled", value: await fetchOne(id) };
      } catch (reason) {
        results[index] = { status: "rejected", reason };
      }
    }
  });

  await Promise.all(workers);
  return results;
}
