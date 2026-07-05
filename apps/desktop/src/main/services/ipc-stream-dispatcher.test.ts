import { Buffer } from "node:buffer";
import { createOrderedBytesDispatcher } from "./ipc-stream-dispatcher";

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
  // Stalled receiver: in-flight frame is never acked, so the stall timeout
  // must drop the stream, release backpressure, and fire closeWhenDrained.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);
  let resumed = 0;
  let drainedCalls = 0;

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 8,
    lowWaterBytes: 4,
    stallTimeoutMs: 20,
    maxDrainWaitMs: 10_000,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: chunk.length,
    }),
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-5",
      sender,
      chunk,
      onPause: () => undefined,
      onResume: () => {
        resumed += 1;
      },
    });
  };

  pushChunk("abcd");
  pushChunk("efgh");
  pushChunk("ijkl");

  await waitUntil(() => sent.length >= 1, 100);
  assertEqual(sent.length, 1, "stalled stream should keep a single un-acked frame in flight");

  dispatcher.closeWhenDrained("session-5", () => {
    drainedCalls += 1;
  });
  assertEqual(drainedCalls, 0, "drain callback should not fire while data is still buffered");

  await waitUntil(() => drainedCalls > 0, 500);

  assertEqual(drainedCalls, 1, "stall timeout should force the drain callback exactly once");
  assertEqual(resumed, 1, "stall timeout should release the paused source");
  assertEqual(sent.length, 1, "no further frames should be sent after the stream is dropped");

  await wait(50);
  assertEqual(drainedCalls, 1, "drain callback must not fire again after the stall drop");
})();

await (async () => {
  // Drain deadline hard cap: acks so slow the buffer never drains must not
  // hold the drain callback past maxDrainWaitMs.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);
  let drainedCalls = 0;

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 16,
    lowWaterBytes: 8,
    stallTimeoutMs: 10_000,
    maxDrainWaitMs: 25,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: chunk.length,
    }),
  });

  dispatcher.push({
    streamId: "session-6",
    sender,
    chunk: "abcd",
    onPause: () => undefined,
    onResume: () => undefined,
  });

  await waitUntil(() => sent.length >= 1, 100);

  dispatcher.closeWhenDrained("session-6", () => {
    drainedCalls += 1;
  });
  assertEqual(drainedCalls, 0, "drain callback should wait while a frame is un-acked");

  await waitUntil(() => drainedCalls > 0, 500);
  assertEqual(drainedCalls, 1, "drain deadline should force the callback exactly once");
})();

await (async () => {
  // Healthy receiver: acks keep flowing, so the stream drains normally and
  // neither the stall timeout nor the drain deadline fires a second time.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);
  let drainedCalls = 0;

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 16,
    lowWaterBytes: 8,
    stallTimeoutMs: 30,
    maxDrainWaitMs: 60,
    buildPayload: ({ streamId, deliveryId, chunk }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength: chunk.length,
    }),
  });

  dispatcher.push({
    streamId: "session-7",
    sender,
    chunk: "abcd",
    onPause: () => undefined,
    onResume: () => undefined,
  });
  dispatcher.push({
    streamId: "session-7",
    sender,
    chunk: "efgh",
    onPause: () => undefined,
    onResume: () => undefined,
  });

  await waitUntil(() => sent.length >= 1, 100);

  dispatcher.closeWhenDrained("session-7", () => {
    drainedCalls += 1;
  });

  const firstPayload = sent[0]?.payload as { deliveryId: number };
  dispatcher.ack({
    streamId: "session-7",
    deliveryId: firstPayload.deliveryId,
    consumedBytes: 4,
  });

  await waitUntil(() => sent.length >= 2, 100);
  const secondPayload = sent[1]?.payload as { deliveryId: number };
  dispatcher.ack({
    streamId: "session-7",
    deliveryId: secondPayload.deliveryId,
    consumedBytes: 4,
  });

  await waitUntil(() => drainedCalls > 0, 100);
  assertEqual(drainedCalls, 1, "acked stream should drain and fire the callback once");

  await wait(100);
  assertEqual(drainedCalls, 1, "stall/deadline timers must not re-fire the drain callback");
  assertEqual(sent.length, 2, "drained stream should not send further frames");
})();

await (async () => {
  // Multibyte end-to-end: CJK + emoji must reassemble to exactly the input,
  // every frame's reported byteLength must equal the UTF-8 length of its
  // data, and acking each frame verbatim (as the renderer does) must fully
  // drain the byte accounting.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);
  const input = "开始😀你好世界🚀漢字テスト🎉终点end";

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 5,
    highWaterBytes: 1024,
    lowWaterBytes: 512,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength,
    }),
  });

  dispatcher.push({
    streamId: "session-multibyte",
    sender,
    chunk: input,
    onPause: () => undefined,
    onResume: () => undefined,
  });

  let drained = false;
  dispatcher.closeWhenDrained("session-multibyte", () => {
    drained = true;
  });

  let ackedCount = 0;
  const deadline = Date.now() + 2_000;
  while (!drained && Date.now() < deadline) {
    if (sent.length > ackedCount) {
      const payload = sent[ackedCount]?.payload as {
        data: string;
        deliveryId: number;
        byteLength: number;
      };
      assertEqual(
        payload.byteLength,
        Buffer.byteLength(payload.data, "utf8"),
        "frame byteLength must equal the UTF-8 byte length of its data"
      );
      ackedCount += 1;
      dispatcher.ack({
        streamId: "session-multibyte",
        deliveryId: payload.deliveryId,
        consumedBytes: payload.byteLength,
      });
    } else {
      await wait(2);
    }
  }

  assert(drained, "multibyte stream must fully drain when frames are acked with their reported byteLength");
  assert(sent.length >= 2, "multibyte input should be carved into multiple frames");
  const reassembled = sent
    .map((record) => (record.payload as { data: string }).data)
    .join("");
  assertEqual(reassembled, input, "reassembled multibyte frames must equal the original input");
})();

await (async () => {
  // A surrogate pair may be split across two frames (per-code-unit carving);
  // each lone half must be reported as 3 UTF-8 bytes — the same value used
  // for in-flight accounting — so verbatim acks still drain, and joining the
  // frames must restore the original pair.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 3,
    highWaterBytes: 1024,
    lowWaterBytes: 512,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength,
    }),
  });

  dispatcher.push({
    streamId: "session-surrogate",
    sender,
    chunk: "😀",
    onPause: () => undefined,
    onResume: () => undefined,
  });

  let drained = false;
  dispatcher.closeWhenDrained("session-surrogate", () => {
    drained = true;
  });

  await waitUntil(() => sent.length >= 1, 100);
  const first = sent[0]?.payload as { data: string; deliveryId: number; byteLength: number };
  assertEqual(first.data, "\ud83d", "first frame should carry the lone high surrogate");
  assertEqual(
    first.byteLength,
    Buffer.byteLength("\ud83d", "utf8"),
    "a lone high surrogate must be reported with the runtime's own measurement"
  );
  dispatcher.ack({
    streamId: "session-surrogate",
    deliveryId: first.deliveryId,
    consumedBytes: first.byteLength,
  });

  await waitUntil(() => sent.length >= 2, 100);
  const second = sent[1]?.payload as { data: string; deliveryId: number; byteLength: number };
  assertEqual(second.data, "\ude00", "second frame should carry the lone low surrogate");
  assertEqual(
    second.byteLength,
    Buffer.byteLength("\ude00", "utf8"),
    "a lone low surrogate must be reported with the runtime's own measurement"
  );
  dispatcher.ack({
    streamId: "session-surrogate",
    deliveryId: second.deliveryId,
    consumedBytes: second.byteLength,
  });

  await waitUntil(() => drained, 100);
  assert(drained, "surrogate-split stream must drain after both halves are acked");
  assertEqual(first.data + second.data, "😀", "joining the split frames must restore the surrogate pair");
})();

await (async () => {
  // Surrogate halves arriving in separate pushes join into one pair inside
  // the pending buffer; the incremental byte accounting must measure the
  // joined pair as 4 bytes, not 3 + 3.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 64,
    highWaterBytes: 1024,
    lowWaterBytes: 512,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength,
    }),
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-joined",
      sender,
      chunk,
      onPause: () => undefined,
      onResume: () => undefined,
    });
  };

  pushChunk("\ud83d");
  pushChunk("\ude00");

  await waitUntil(() => sent.length >= 1, 100);
  const payload = sent[0]?.payload as { data: string; deliveryId: number; byteLength: number };
  assertEqual(payload.data, "😀", "halves pushed separately should be flushed as the joined pair");
  assertEqual(payload.byteLength, 4, "a joined surrogate pair must be measured as 4 UTF-8 bytes");

  let drained = false;
  dispatcher.closeWhenDrained("session-joined", () => {
    drained = true;
  });
  dispatcher.ack({
    streamId: "session-joined",
    deliveryId: payload.deliveryId,
    consumedBytes: payload.byteLength,
  });

  await waitUntil(() => drained, 100);
  assert(drained, "joined-pair stream must drain after a verbatim ack");
})();
