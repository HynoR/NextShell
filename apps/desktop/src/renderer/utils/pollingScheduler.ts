export interface PollingSchedulerDocument {
  visibilityState?: "visible" | "hidden" | "prerender";
  addEventListener?: (type: "visibilitychange", listener: () => void) => void;
  removeEventListener?: (type: "visibilitychange", listener: () => void) => void;
}

export interface PollingSubscriptionOptions {
  enabled: boolean;
  intervalMs: number;
  runImmediately?: boolean;
  task: () => void | Promise<void>;
}

interface PollingTaskEntry {
  task: () => void | Promise<void>;
  running: boolean;
}

interface PollingBucket {
  entries: Set<PollingTaskEntry>;
  timerId?: number;
}

export interface PollingScheduler {
  subscribe: (options: PollingSubscriptionOptions) => () => void;
}

interface PollingSchedulerEnv {
  document?: PollingSchedulerDocument;
  setInterval?: (handler: () => void, timeout: number) => number;
  clearInterval?: (timerId: number) => void;
}

const defaultEnv: Required<PollingSchedulerEnv> = {
  document: typeof document === "undefined" ? {} : document,
  setInterval: (handler, timeout) => window.setInterval(handler, timeout),
  clearInterval: (timerId) => window.clearInterval(timerId)
};

export const createPollingScheduler = (env: PollingSchedulerEnv = {}): PollingScheduler => {
  const resolved = {
    document: env.document ?? defaultEnv.document,
    setInterval: env.setInterval ?? defaultEnv.setInterval,
    clearInterval: env.clearInterval ?? defaultEnv.clearInterval
  };

  const buckets = new Map<number, PollingBucket>();
  let listeningVisibility = false;

  const isVisible = (): boolean => {
    return resolved.document.visibilityState !== "hidden";
  };

  const runEntry = (entry: PollingTaskEntry): void => {
    if (entry.running) {
      return;
    }

    entry.running = true;
    let result: void | Promise<void>;
    try {
      result = entry.task();
    } catch (error) {
      entry.running = false;
      console.error("[pollingScheduler] task failed synchronously", error);
      return;
    }

    if (!(result instanceof Promise)) {
      entry.running = false;
      return;
    }

    void result
      .catch((error) => {
        console.error("[pollingScheduler] task failed", error);
      })
      .finally(() => {
        entry.running = false;
      });
  };

  const stopBucket = (intervalMs: number): void => {
    const bucket = buckets.get(intervalMs);
    if (!bucket?.timerId) {
      return;
    }
    resolved.clearInterval(bucket.timerId);
    bucket.timerId = undefined;
  };

  const startBucket = (intervalMs: number): void => {
    const bucket = buckets.get(intervalMs);
    if (!bucket || bucket.entries.size === 0 || bucket.timerId !== undefined || !isVisible()) {
      return;
    }
    bucket.timerId = resolved.setInterval(() => {
      for (const entry of bucket.entries) {
        runEntry(entry);
      }
    }, intervalMs);
  };

  const ensureVisibilityListener = (): void => {
    if (listeningVisibility) {
      return;
    }
    resolved.document.addEventListener?.("visibilitychange", handleVisibilityChange);
    listeningVisibility = true;
  };

  const teardownVisibilityListener = (): void => {
    if (!listeningVisibility || buckets.size > 0) {
      return;
    }
    resolved.document.removeEventListener?.("visibilitychange", handleVisibilityChange);
    listeningVisibility = false;
  };

  const handleVisibilityChange = (): void => {
    if (isVisible()) {
      for (const intervalMs of buckets.keys()) {
        startBucket(intervalMs);
      }
      return;
    }

    for (const intervalMs of buckets.keys()) {
      stopBucket(intervalMs);
    }
  };

  return {
    subscribe: ({ enabled, intervalMs, runImmediately = false, task }: PollingSubscriptionOptions) => {
      if (!enabled) {
        return () => {};
      }

      const entry: PollingTaskEntry = { task, running: false };
      const bucket = buckets.get(intervalMs) ?? { entries: new Set<PollingTaskEntry>() };
      bucket.entries.add(entry);
      buckets.set(intervalMs, bucket);
      ensureVisibilityListener();
      startBucket(intervalMs);

      if (runImmediately && isVisible()) {
        runEntry(entry);
      }

      return () => {
        const currentBucket = buckets.get(intervalMs);
        if (!currentBucket) {
          return;
        }

        currentBucket.entries.delete(entry);
        if (currentBucket.entries.size > 0) {
          return;
        }

        stopBucket(intervalMs);
        buckets.delete(intervalMs);
        teardownVisibilityListener();
      };
    }
  };
};

export const pollingScheduler = createPollingScheduler();
