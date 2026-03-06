import {
  createLatestOnlyDispatcher,
  createOrderedBytesDispatcher,
} from "./ipc-stream-dispatcher";

interface SentRecord {
  channel: string;
  payload: unknown;
}

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(message);
  }
}

function assertEqual<T>(actual: T, expected: T, message: string): void {
  if (actual !== expected) {
    throw new Error(`${message}: expected ${String(expected)}, got ${String(actual)}`);
  }
}

function wait(ms: number): Promise<void> {
  return new Promise<void>((resolve) => setTimeout(resolve, ms));
}

async function waitUntil(
  predicate: () => boolean,
  timeoutMs: number,
  stepMs = 5
): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) {
      return;
    }
    await wait(stepMs);
  }
}

function createSender(records: SentRecord[]): {
  send: (channel: string, payload: unknown) => void;
  isDestroyed: () => boolean;
  destroy: () => void;
} {
  let destroyed = false;
  return {
    send(channel: string, payload: unknown) {
      records.push({ channel, payload });
    },
    isDestroyed() {
      return destroyed;
    },
    destroy() {
      destroyed = true;
    },
  };
}

await (async () => {
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 16,
    lowWaterBytes: 8,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: chunk.length,
    }),
  });

  dispatcher.push({
    streamId: "session-1",
    sender,
    chunk: "abcd",
    onPause: () => undefined,
    onResume: () => undefined,
  });
  dispatcher.push({
    streamId: "session-1",
    sender,
    chunk: "efgh",
    onPause: () => undefined,
    onResume: () => undefined,
  });

  await wait(10);

  assertEqual(sent.length, 1, "ordered dispatcher should only send one in-flight frame");
  const firstPayload = sent[0]?.payload as { data: string; deliveryId: number };
  assertEqual(firstPayload.data, "abcd", "first ordered frame should preserve enqueue order");

  dispatcher.ack({
    streamId: "session-1",
    deliveryId: firstPayload.deliveryId,
    consumedBytes: 4,
  });

  await wait(10);

  assertEqual(sent.length, 2, "ack should unlock the next queued frame");
  const secondPayload = sent[1]?.payload as { data: string };
  assertEqual(secondPayload.data, "efgh", "second ordered frame should send after ack");
})();

await (async () => {
  const sent: SentRecord[] = [];
  const sender = createSender(sent);
  let paused = 0;
  let resumed = 0;

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 8,
    lowWaterBytes: 4,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: chunk.length,
    }),
  });

  dispatcher.push({
    streamId: "session-2",
    sender,
    chunk: "abcd",
    onPause: () => {
      paused += 1;
    },
    onResume: () => {
      resumed += 1;
    },
  });
  dispatcher.push({
    streamId: "session-2",
    sender,
    chunk: "efgh",
    onPause: () => {
      paused += 1;
    },
    onResume: () => {
      resumed += 1;
    },
  });
  dispatcher.push({
    streamId: "session-2",
    sender,
    chunk: "ijkl",
    onPause: () => {
      paused += 1;
    },
    onResume: () => {
      resumed += 1;
    },
  });

  await waitUntil(() => sent.length >= 1, 100);

  assertEqual(paused, 1, "ordered dispatcher should pause once when buffered bytes exceed high water");
  const firstPayload = sent[0]?.payload as { deliveryId: number };
  dispatcher.ack({
    streamId: "session-2",
    deliveryId: firstPayload.deliveryId,
    consumedBytes: 4,
  });
  await waitUntil(() => sent.length >= 2, 100);
  assert(sent.length >= 2, "second ordered frame should be dispatched after first ack");
  const secondPayload = sent[1]?.payload as { deliveryId: number };
  dispatcher.ack({
    streamId: "session-2",
    deliveryId: secondPayload.deliveryId,
    consumedBytes: 4,
  });

  await wait(10);

  assertEqual(resumed, 1, "ordered dispatcher should resume once buffered bytes fall below low water");
})();

await (async () => {
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 16,
    lowWaterBytes: 8,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: chunk.length,
    }),
  });

  dispatcher.push({
    streamId: "session-3",
    sender,
    chunk: "abcd",
    onPause: () => undefined,
    onResume: () => undefined,
  });
  dispatcher.push({
    streamId: "session-3",
    sender,
    chunk: "efgh",
    onPause: () => undefined,
    onResume: () => undefined,
  });

  await wait(10);

  const firstPayload = sent[0]?.payload as { deliveryId: number };
  dispatcher.ack({
    streamId: "session-3",
    deliveryId: firstPayload.deliveryId - 1,
    consumedBytes: 4,
  });
  dispatcher.ack({
    streamId: "session-3",
    deliveryId: firstPayload.deliveryId,
    consumedBytes: 4,
  });
  dispatcher.ack({
    streamId: "session-3",
    deliveryId: firstPayload.deliveryId,
    consumedBytes: 4,
  });

  await wait(10);

  assertEqual(sent.length, 2, "duplicate or stale ack should not cause duplicate sends");
})();

await (async () => {
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 16,
    lowWaterBytes: 8,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: chunk.length,
    }),
  });

  dispatcher.push({
    streamId: "session-4",
    sender,
    chunk: "abc",
    onPause: () => undefined,
    onResume: () => undefined,
  });
  sender.destroy();
  dispatcher.push({
    streamId: "session-4",
    sender,
    chunk: "def",
    onPause: () => undefined,
    onResume: () => undefined,
  });

  await wait(10);

  assertEqual(sent.length, 0, "destroyed sender should prevent dispatch");
})();

await (async () => {
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createLatestOnlyDispatcher({
    channel: "monitor:data",
    buildPayload: ({ deliveryId, payload }) => ({
      deliveryId,
      payload,
    }),
  });

  dispatcher.publish({
    streamId: "connection-1",
    sender,
    payload: { sample: 1 },
  });
  dispatcher.publish({
    streamId: "connection-1",
    sender,
    payload: { sample: 2 },
  });
  dispatcher.publish({
    streamId: "connection-1",
    sender,
    payload: { sample: 3 },
  });

  assertEqual(sent.length, 1, "latest-only dispatcher should keep a single in-flight frame");
  const firstPayload = sent[0]?.payload as { deliveryId: number };
  dispatcher.ack({
    streamId: "connection-1",
    deliveryId: firstPayload.deliveryId,
  });

  assertEqual(sent.length, 2, "latest-only dispatcher should send the latest pending frame after ack");
  const secondPayload = sent[1]?.payload as { payload: { sample: number } };
  assertEqual(secondPayload.payload.sample, 3, "latest-only dispatcher should overwrite older pending snapshots");
})();
