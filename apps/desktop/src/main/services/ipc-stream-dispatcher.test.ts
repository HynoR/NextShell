import { Buffer } from "node:buffer";
import { createOrderedBytesDispatcher } from "./ipc-stream-dispatcher";

interface SentRecord {
  channel: string;
  payload: unknown;
}

interface FramePayload {
  sessionId: string;
  data: string;
  deliveryId: number;
  byteLength: number;
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

async function waitUntil(predicate: () => boolean, timeoutMs: number, stepMs = 5): Promise<void> {
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
    }
  };
}

function framePayload(record: SentRecord | undefined): FramePayload {
  return record?.payload as FramePayload;
}

await (async () => {
  // Sliding window: frames go out back-to-back without acks until the window
  // (highWaterBytes) fills, a batched delta ack reopens the window, and the
  // stream drains exactly when the cumulative acked bytes equal the input.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 12,
    lowWaterBytes: 8,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  dispatcher.push({
    streamId: "session-1",
    sender,
    chunk: "abcdefghijklmnopqrstuvwx",
    onPause: () => undefined,
    onResume: () => undefined
  });

  await wait(10);

  assertEqual(sent.length, 3, "frames should stream without acks until the window fills");
  assertEqual(framePayload(sent[0]).data, "abcd", "first frame should preserve enqueue order");
  assertEqual(framePayload(sent[1]).data, "efgh", "second frame should follow without an ack");
  assertEqual(framePayload(sent[2]).data, "ijkl", "third frame should fill the window");

  await wait(10);
  assertEqual(sent.length, 3, "sends must stop at the window until acks arrive");

  // Batched delta ack covering the first two frames reopens 8 bytes of window.
  dispatcher.ack({
    streamId: "session-1",
    deliveryId: framePayload(sent[1]).deliveryId,
    consumedBytes: 8
  });

  await wait(10);

  assertEqual(sent.length, 5, "a batched delta ack should reopen the window for more frames");
  assertEqual(framePayload(sent[3]).data, "mnop", "frames must stay ordered after a batched ack");
  assertEqual(framePayload(sent[4]).data, "qrst", "window should refill up to highWaterBytes");

  let drainedCalls = 0;
  dispatcher.closeWhenDrained("session-1", () => {
    drainedCalls += 1;
  });
  assertEqual(drainedCalls, 0, "drain callback must wait while frames are in flight");

  dispatcher.ack({
    streamId: "session-1",
    deliveryId: framePayload(sent[4]).deliveryId,
    consumedBytes: 12
  });

  await wait(10);
  assertEqual(sent.length, 6, "acking the full window should release the final frame");
  assertEqual(framePayload(sent[5]).data, "uvwx", "final frame should preserve order");
  assertEqual(drainedCalls, 0, "drain callback must wait for the last frame's ack");

  dispatcher.ack({
    streamId: "session-1",
    deliveryId: framePayload(sent[5]).deliveryId,
    consumedBytes: 4
  });

  assertEqual(drainedCalls, 1, "cumulative acks equal to total bytes must drain the stream");

  await wait(20);
  assertEqual(drainedCalls, 1, "drain callback must fire exactly once");
  assertEqual(sent.length, 6, "drained stream should not send further frames");
})();

await (async () => {
  // Backpressure: the source pauses at high water with multiple frames in
  // flight and resumes only after acks bring buffered bytes under low water.
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
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-2",
      sender,
      chunk,
      onPause: () => {
        paused += 1;
      },
      onResume: () => {
        resumed += 1;
      }
    });
  };

  pushChunk("abcd");
  pushChunk("efgh");
  pushChunk("ijkl");

  await waitUntil(() => sent.length >= 2, 100);

  assertEqual(sent.length, 2, "sends must stop once un-acked bytes reach the window");
  assertEqual(paused, 1, "source should pause once when buffered bytes exceed high water");
  assertEqual(resumed, 0, "source must stay paused while the window is full");

  dispatcher.ack({
    streamId: "session-2",
    deliveryId: framePayload(sent[0]).deliveryId,
    consumedBytes: 4
  });
  await waitUntil(() => sent.length >= 3, 100);
  assertEqual(sent.length, 3, "ack progress should release the queued frame");
  assertEqual(resumed, 0, "buffered bytes still above low water must not resume the source");

  dispatcher.ack({
    streamId: "session-2",
    deliveryId: framePayload(sent[2]).deliveryId,
    consumedBytes: 8
  });

  await wait(10);

  assertEqual(resumed, 1, "source should resume once buffered bytes fall below low water");
})();

await (async () => {
  // Acks below the incarnation floor or beyond the last sent frame are
  // ignored entirely, while an ack whose deliveryId REGRESSES relative to an
  // earlier ack (the receiver's deferred write-callback path and immediate
  // background path flush out of order after a tab switch) must still credit
  // its byte delta — dropping it would inflate inFlightBytes forever.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 16,
    lowWaterBytes: 8,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-3",
      sender,
      chunk,
      onPause: () => undefined,
      onResume: () => undefined
    });
  };

  pushChunk("abcd");
  pushChunk("efgh");
  pushChunk("ijkl");

  await waitUntil(() => sent.length >= 3, 100);
  assertEqual(sent.length, 3, "all three frames fit the window and should be sent without acks");

  const first = framePayload(sent[0]);
  const second = framePayload(sent[1]);
  const third = framePayload(sent[2]);

  // Below anything ever sent, and beyond the last sent frame: both ignored.
  dispatcher.ack({ streamId: "session-3", deliveryId: first.deliveryId - 1, consumedBytes: 4 });
  dispatcher.ack({ streamId: "session-3", deliveryId: third.deliveryId + 5, consumedBytes: 4 });

  let drainedCalls = 0;
  dispatcher.closeWhenDrained("session-3", () => {
    drainedCalls += 1;
  });
  assertEqual(
    drainedCalls,
    0,
    "ignored out-of-range acks must not have credited bytes that are still in flight"
  );

  // The immediate background path flushes the newest frame's ack first...
  dispatcher.ack({ streamId: "session-3", deliveryId: third.deliveryId, consumedBytes: 4 });
  assertEqual(drainedCalls, 0, "older frames are still un-acked after the out-of-order ack");

  // ...then the late write-callback flush arrives with a REGRESSED
  // deliveryId covering the two older frames: its delta must be credited.
  dispatcher.ack({ streamId: "session-3", deliveryId: second.deliveryId, consumedBytes: 8 });
  assertEqual(drainedCalls, 1, "a regressed-deliveryId ack must still credit its byte delta");
})();

await (async () => {
  // Acks carrying deliveryIds from a PREVIOUS incarnation of the same
  // streamId (state recreated after a stall drop while the source kept
  // producing) must be rejected: crediting them would over-open the new
  // incarnation's window and fire drains while its frames are truly un-acked.
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
    stallTimeoutMs: 120,
    maxDrainWaitMs: 10_000,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-reincarnated",
      sender,
      chunk,
      onPause: () => undefined,
      onResume: () => {
        resumed += 1;
      }
    });
  };

  // First incarnation: fill the window, never ack, and let the stall timer
  // drop the state (releasing the paused source).
  pushChunk("abcd");
  pushChunk("efgh");
  pushChunk("ijkl");
  await waitUntil(() => sent.length >= 2, 100);
  assertEqual(sent.length, 2, "the first incarnation should hold a full window of un-acked frames");

  await waitUntil(() => resumed >= 1, 1000);
  assertEqual(resumed, 1, "the stall drop should release the paused source");

  // The source keeps producing: the same streamId is recreated and fills a
  // fresh window with its own frames.
  pushChunk("mnop");
  pushChunk("qrst");
  await waitUntil(() => sent.length >= 4, 100);
  assertEqual(sent.length, 4, "the recreated stream should send its own frames");

  // The hung receiver wakes up and flushes an ack for frames the PREVIOUS
  // incarnation sent: it must fall below the new incarnation's floor.
  dispatcher.ack({
    streamId: "session-reincarnated",
    deliveryId: framePayload(sent[1]).deliveryId,
    consumedBytes: 8
  });

  dispatcher.closeWhenDrained("session-reincarnated", () => {
    drainedCalls += 1;
  });
  assertEqual(drainedCalls, 0, "a prior-incarnation ack must not fire the drain early");

  pushChunk("uvwx");
  await wait(15);
  assertEqual(sent.length, 4, "a prior-incarnation ack must not open the send window");

  // Only the new incarnation's own acks make real progress.
  dispatcher.ack({
    streamId: "session-reincarnated",
    deliveryId: framePayload(sent[3]).deliveryId,
    consumedBytes: 8
  });
  await waitUntil(() => sent.length >= 5, 100);
  assertEqual(sent.length, 5, "real ack progress should release the queued frame");
  assertEqual(framePayload(sent[4]).data, "uvwx", "ordering must survive the rejected stale ack");

  dispatcher.ack({
    streamId: "session-reincarnated",
    deliveryId: framePayload(sent[4]).deliveryId,
    consumedBytes: 4
  });
  assertEqual(drainedCalls, 1, "only the new incarnation's own acks may drain it");
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
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  dispatcher.push({
    streamId: "session-4",
    sender,
    chunk: "abc",
    onPause: () => undefined,
    onResume: () => undefined
  });
  sender.destroy();
  dispatcher.push({
    streamId: "session-4",
    sender,
    chunk: "def",
    onPause: () => undefined,
    onResume: () => undefined
  });

  await wait(10);

  assertEqual(sent.length, 0, "destroyed sender should prevent dispatch");
})();

await (async () => {
  // Stalled receiver: a whole window of frames is in flight and never acked,
  // so the stall timeout must drop the stream, release backpressure, and fire
  // closeWhenDrained exactly once.
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
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-5",
      sender,
      chunk,
      onPause: () => undefined,
      onResume: () => {
        resumed += 1;
      }
    });
  };

  pushChunk("abcd");
  pushChunk("efgh");
  pushChunk("ijkl");

  await waitUntil(() => sent.length >= 2, 100);
  assertEqual(sent.length, 2, "a stalled stream should hold a full window of un-acked frames");

  dispatcher.closeWhenDrained("session-5", () => {
    drainedCalls += 1;
  });
  assertEqual(drainedCalls, 0, "drain callback should not fire while data is still buffered");

  await waitUntil(() => drainedCalls > 0, 500);

  assertEqual(drainedCalls, 1, "stall timeout should force the drain callback exactly once");
  assertEqual(resumed, 1, "stall timeout should release the paused source");
  assertEqual(sent.length, 2, "no further frames should be sent after the stream is dropped");

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
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  dispatcher.push({
    streamId: "session-6",
    sender,
    chunk: "abcd",
    onPause: () => undefined,
    onResume: () => undefined
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
  // Healthy receiver: a single cumulative ack covering every in-flight frame
  // drains the stream, and neither the stall timeout nor the drain deadline
  // fires a second time.
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
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  dispatcher.push({
    streamId: "session-7",
    sender,
    chunk: "abcd",
    onPause: () => undefined,
    onResume: () => undefined
  });
  dispatcher.push({
    streamId: "session-7",
    sender,
    chunk: "efgh",
    onPause: () => undefined,
    onResume: () => undefined
  });

  await waitUntil(() => sent.length >= 2, 100);
  assertEqual(sent.length, 2, "both frames should be in flight without acks");

  dispatcher.closeWhenDrained("session-7", () => {
    drainedCalls += 1;
  });

  dispatcher.ack({
    streamId: "session-7",
    deliveryId: framePayload(sent[1]).deliveryId,
    consumedBytes: 8
  });

  await waitUntil(() => drainedCalls > 0, 100);
  assertEqual(drainedCalls, 1, "one cumulative ack covering all frames should drain the stream");

  await wait(100);
  assertEqual(drainedCalls, 1, "stall/deadline timers must not re-fire the drain callback");
  assertEqual(sent.length, 2, "drained stream should not send further frames");
})();

await (async () => {
  // Over-ack: a consumedBytes delta larger than the in-flight total must
  // clamp to zero instead of corrupting the window, and the stream must keep
  // working afterwards.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);
  let drainedCalls = 0;

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 8,
    lowWaterBytes: 4,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-overack",
      sender,
      chunk,
      onPause: () => undefined,
      onResume: () => undefined
    });
  };

  pushChunk("abcd");
  await waitUntil(() => sent.length >= 1, 100);

  dispatcher.ack({
    streamId: "session-overack",
    deliveryId: framePayload(sent[0]).deliveryId,
    consumedBytes: 999
  });

  pushChunk("efgh");
  await waitUntil(() => sent.length >= 2, 100);
  assertEqual(sent.length, 2, "the stream must keep sending after a clamped over-ack");
  assertEqual(framePayload(sent[1]).data, "efgh", "ordering must survive a clamped over-ack");

  dispatcher.closeWhenDrained("session-overack", () => {
    drainedCalls += 1;
  });
  assertEqual(drainedCalls, 0, "the second frame is still un-acked and must block the drain");

  dispatcher.ack({
    streamId: "session-overack",
    deliveryId: framePayload(sent[1]).deliveryId,
    consumedBytes: 4
  });
  assertEqual(drainedCalls, 1, "a clamped over-ack must not break subsequent drain accounting");
})();

await (async () => {
  // Acks for unknown streams are ignored and do not disturb other streams.
  const sent: SentRecord[] = [];
  const sender = createSender(sent);
  let drainedCalls = 0;

  const dispatcher = createOrderedBytesDispatcher({
    channel: "session:data",
    flushIntervalMs: 2,
    targetChunkBytes: 4,
    highWaterBytes: 16,
    lowWaterBytes: 8,
    buildPayload: ({ streamId, deliveryId, chunk, byteLength }) => ({
      sessionId: streamId,
      data: chunk,
      deliveryId,
      byteLength
    })
  });

  dispatcher.ack({ streamId: "never-existed", deliveryId: 1, consumedBytes: 4 });

  dispatcher.push({
    streamId: "session-known",
    sender,
    chunk: "abcd",
    onPause: () => undefined,
    onResume: () => undefined
  });
  dispatcher.ack({ streamId: "still-unknown", deliveryId: 1, consumedBytes: 4 });

  await waitUntil(() => sent.length >= 1, 100);
  assertEqual(sent.length, 1, "unknown-stream acks must not affect dispatch");

  dispatcher.closeWhenDrained("session-known", () => {
    drainedCalls += 1;
  });
  assertEqual(drainedCalls, 0, "unknown-stream acks must not have drained the known stream");

  dispatcher.ack({
    streamId: "session-known",
    deliveryId: framePayload(sent[0]).deliveryId,
    consumedBytes: 4
  });
  assertEqual(drainedCalls, 1, "the known stream must drain from its own ack only");
})();

await (async () => {
  // Multibyte end-to-end: CJK + emoji must reassemble to exactly the input,
  // every frame's reported byteLength must equal the UTF-8 length of its
  // data, and CUMULATIVE delta acks (batches covering several frames, with
  // deliveryId = highest processed) must fully drain the byte accounting.
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
      byteLength
    })
  });

  dispatcher.push({
    streamId: "session-multibyte",
    sender,
    chunk: input,
    onPause: () => undefined,
    onResume: () => undefined
  });

  let drained = false;
  dispatcher.closeWhenDrained("session-multibyte", () => {
    drained = true;
  });

  // The window (1KB) dwarfs the input, so every frame is already out.
  assert(sent.length >= 2, "multibyte input should be carved into multiple frames");
  assert(!drained, "stream must not drain before any acks");

  const frames = sent.map((record) => framePayload(record));
  let batchBytes = 0;
  let batchDeliveryId = 0;
  frames.forEach((frame, index) => {
    assertEqual(
      frame.byteLength,
      Buffer.byteLength(frame.data, "utf8"),
      "frame byteLength must equal the UTF-8 byte length of its data"
    );
    batchBytes += frame.byteLength;
    batchDeliveryId = frame.deliveryId;

    const isBatchBoundary = (index + 1) % 3 === 0 || index === frames.length - 1;
    if (isBatchBoundary) {
      dispatcher.ack({
        streamId: "session-multibyte",
        deliveryId: batchDeliveryId,
        consumedBytes: batchBytes
      });
      batchBytes = 0;
    }
  });

  await waitUntil(() => drained, 200);
  assert(drained, "cumulative delta acks summing every frame's byteLength must drain exactly");
  assertEqual(sent.length, frames.length, "batched acks must not trigger duplicate sends");
  const reassembled = sent.map((record) => framePayload(record).data).join("");
  assertEqual(reassembled, input, "reassembled multibyte frames must equal the original input");
})();

await (async () => {
  // A surrogate pair may be split across two frames (per-code-unit carving);
  // each lone half must be reported as the runtime's own measurement — the
  // same value used for in-flight accounting — so delta acks built from the
  // reported byteLengths still drain, and joining the frames must restore the
  // original pair.
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
      byteLength
    })
  });

  dispatcher.push({
    streamId: "session-surrogate",
    sender,
    chunk: "😀",
    onPause: () => undefined,
    onResume: () => undefined
  });

  let drained = false;
  dispatcher.closeWhenDrained("session-surrogate", () => {
    drained = true;
  });

  await waitUntil(() => sent.length >= 1, 100);
  const first = framePayload(sent[0]);
  assertEqual(first.data, "\ud83d", "first frame should carry the lone high surrogate");
  assertEqual(
    first.byteLength,
    Buffer.byteLength("\ud83d", "utf8"),
    "a lone high surrogate must be reported with the runtime's own measurement"
  );
  dispatcher.ack({
    streamId: "session-surrogate",
    deliveryId: first.deliveryId,
    consumedBytes: first.byteLength
  });

  await waitUntil(() => sent.length >= 2, 100);
  const second = framePayload(sent[1]);
  assertEqual(second.data, "\ude00", "second frame should carry the lone low surrogate");
  assertEqual(
    second.byteLength,
    Buffer.byteLength("\ude00", "utf8"),
    "a lone low surrogate must be reported with the runtime's own measurement"
  );
  dispatcher.ack({
    streamId: "session-surrogate",
    deliveryId: second.deliveryId,
    consumedBytes: second.byteLength
  });

  await waitUntil(() => drained, 100);
  assert(drained, "surrogate-split stream must drain after both halves are acked");
  assertEqual(
    first.data + second.data,
    "😀",
    "joining the split frames must restore the surrogate pair"
  );
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
      byteLength
    })
  });

  const pushChunk = (chunk: string) => {
    dispatcher.push({
      streamId: "session-joined",
      sender,
      chunk,
      onPause: () => undefined,
      onResume: () => undefined
    });
  };

  pushChunk("\ud83d");
  pushChunk("\ude00");

  await waitUntil(() => sent.length >= 1, 100);
  const payload = framePayload(sent[0]);
  assertEqual(payload.data, "😀", "halves pushed separately should be flushed as the joined pair");
  assertEqual(payload.byteLength, 4, "a joined surrogate pair must be measured as 4 UTF-8 bytes");

  let drained = false;
  dispatcher.closeWhenDrained("session-joined", () => {
    drained = true;
  });
  dispatcher.ack({
    streamId: "session-joined",
    deliveryId: payload.deliveryId,
    consumedBytes: payload.byteLength
  });

  await waitUntil(() => drained, 100);
  assert(drained, "joined-pair stream must drain after a verbatim ack");
})();
