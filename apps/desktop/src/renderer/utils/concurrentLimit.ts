/**
 * Execute an array of async tasks with a concurrency limit.
 *
 * Similar to `p-map` but zero dependencies.
 *
 * @param items - The items to process.
 * @param fn - Async function to apply to each item.
 * @param concurrency - Maximum number of concurrent tasks (default 5).
 * @returns Results in the same order as `items`.
 */
export async function pMap<T, R>(
  items: T[],
  fn: (item: T, index: number) => Promise<R>,
  concurrency = 5
): Promise<R[]> {
  const results: R[] = new Array(items.length);
  let nextIndex = 0;

  const worker = async (): Promise<void> => {
    while (nextIndex < items.length) {
      const i = nextIndex;
      nextIndex += 1;
      results[i] = await fn(items[i]!, i);
    }
  };

  const workers: Promise<void>[] = [];
  for (let w = 0; w < Math.min(concurrency, items.length); w += 1) {
    workers.push(worker());
  }

  await Promise.all(workers);
  return results;
}

/**
 * Like pMap but collects both successes and failures.
 */
export async function pMapSettled<T, R>(
  items: T[],
  fn: (item: T, index: number) => Promise<R>,
  concurrency = 5
): Promise<Array<{ status: "fulfilled"; value: R } | { status: "rejected"; reason: unknown }>> {
  return pMap(
    items,
    async (item, index) => {
      try {
        const value = await fn(item, index);
        return { status: "fulfilled" as const, value };
      } catch (reason) {
        return { status: "rejected" as const, reason };
      }
    },
    concurrency
  );
}
