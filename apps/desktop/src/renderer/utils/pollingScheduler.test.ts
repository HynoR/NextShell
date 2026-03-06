import { createPollingScheduler } from "./pollingScheduler";

type VisibilityListener = () => void;

interface FakeDocument {
  visibilityState: "visible" | "hidden";
  addEventListener: (type: string, listener: VisibilityListener) => void;
  removeEventListener: (type: string, listener: VisibilityListener) => void;
}

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const createFakeDocument = (): FakeDocument & { dispatchVisibilityChange: () => void } => {
  const listeners = new Set<VisibilityListener>();

  return {
    visibilityState: "visible",
    addEventListener(type, listener) {
      if (type === "visibilitychange") {
        listeners.add(listener);
      }
    },
    removeEventListener(type, listener) {
      if (type === "visibilitychange") {
        listeners.delete(listener);
      }
    },
    dispatchVisibilityChange() {
      for (const listener of listeners) {
        listener();
      }
    }
  };
};

const createIntervalHarness = () => {
  let nextId = 1;
  const intervals = new Map<number, { fn: () => void; intervalMs: number }>();

  return {
    setInterval(fn: () => void, intervalMs: number) {
      const id = nextId++;
      intervals.set(id, { fn, intervalMs });
      return id;
    },
    clearInterval(id: number) {
      intervals.delete(id);
    },
    getActiveIntervalCount() {
      return intervals.size;
    },
    tick(intervalMs: number) {
      for (const entry of intervals.values()) {
        if (entry.intervalMs === intervalMs) {
          entry.fn();
        }
      }
    }
  };
};

(() => {
  const fakeDocument = createFakeDocument();
  const harness = createIntervalHarness();
  const scheduler = createPollingScheduler({
    document: fakeDocument,
    setInterval: harness.setInterval,
    clearInterval: harness.clearInterval
  });

  let firstRuns = 0;
  let secondRuns = 0;

  const stopFirst = scheduler.subscribe({
    enabled: true,
    intervalMs: 1000,
    runImmediately: false,
    task: () => {
      firstRuns += 1;
    }
  });
  const stopSecond = scheduler.subscribe({
    enabled: true,
    intervalMs: 1000,
    runImmediately: false,
    task: () => {
      secondRuns += 1;
    }
  });

  assertEqual(harness.getActiveIntervalCount(), 1, "same interval should share one native timer");
  harness.tick(1000);
  assertEqual(firstRuns, 1, "first task should run on tick");
  assertEqual(secondRuns, 1, "second task should run on tick");

  stopFirst();
  assertEqual(
    harness.getActiveIntervalCount(),
    1,
    "removing one subscriber should keep shared timer alive"
  );

  stopSecond();
  assertEqual(harness.getActiveIntervalCount(), 0, "last unsubscribe should clear the timer");
})();

(() => {
  const fakeDocument = createFakeDocument();
  const harness = createIntervalHarness();
  const scheduler = createPollingScheduler({
    document: fakeDocument,
    setInterval: harness.setInterval,
    clearInterval: harness.clearInterval
  });

  const stop = scheduler.subscribe({
    enabled: false,
    intervalMs: 2000,
    runImmediately: true,
    task: () => {}
  });

  assertEqual(harness.getActiveIntervalCount(), 0, "disabled subscription should not allocate timers");
  stop();
})();

(() => {
  const fakeDocument = createFakeDocument();
  const harness = createIntervalHarness();
  const scheduler = createPollingScheduler({
    document: fakeDocument,
    setInterval: harness.setInterval,
    clearInterval: harness.clearInterval
  });

  let runs = 0;
  const stop = scheduler.subscribe({
    enabled: true,
    intervalMs: 5000,
    runImmediately: false,
    task: () => {
      runs += 1;
    }
  });

  assertEqual(harness.getActiveIntervalCount(), 1, "visible document should start polling");

  fakeDocument.visibilityState = "hidden";
  fakeDocument.dispatchVisibilityChange();
  assertEqual(harness.getActiveIntervalCount(), 0, "hidden document should pause polling");

  fakeDocument.visibilityState = "visible";
  fakeDocument.dispatchVisibilityChange();
  assertEqual(harness.getActiveIntervalCount(), 1, "visible document should resume polling");

  harness.tick(5000);
  assertEqual(runs, 1, "resumed scheduler should continue with normal cadence");
  stop();
})();

(() => {
  const fakeDocument = createFakeDocument();
  const harness = createIntervalHarness();
  const scheduler = createPollingScheduler({
    document: fakeDocument,
    setInterval: harness.setInterval,
    clearInterval: harness.clearInterval
  });

  let healthyRuns = 0;
  const stopBroken = scheduler.subscribe({
    enabled: true,
    intervalMs: 1000,
    runImmediately: false,
    task: () => {
      throw new Error("boom");
    }
  });
  const stopHealthy = scheduler.subscribe({
    enabled: true,
    intervalMs: 1000,
    runImmediately: false,
    task: () => {
      healthyRuns += 1;
    }
  });

  harness.tick(1000);
  harness.tick(1000);
  assertEqual(healthyRuns, 2, "sync task failure should not block sibling pollers or future ticks");

  stopBroken();
  stopHealthy();
})();

await (async () => {
  const fakeDocument = createFakeDocument();
  const harness = createIntervalHarness();
  const scheduler = createPollingScheduler({
    document: fakeDocument,
    setInterval: harness.setInterval,
    clearInterval: harness.clearInterval
  });

  let runningResolve: (() => void) | undefined;
  let runs = 0;

  const stop = scheduler.subscribe({
    enabled: true,
    intervalMs: 1000,
    runImmediately: false,
    task: async () => {
      runs += 1;
      await new Promise<void>((resolve) => {
        runningResolve = resolve;
      });
    }
  });

  harness.tick(1000);
  harness.tick(1000);
  assertEqual(runs, 1, "scheduler should skip overlapping async executions");

  const resolve = runningResolve;
  if (!resolve) {
    throw new Error("async task should expose its completion handle");
  }
  resolve();
  await new Promise<void>((done) => {
    setTimeout(done, 0);
  });

  harness.tick(1000);
  assertEqual(runs, 2, "scheduler should allow the next tick after async completion");

  stop();
})();
