import type { WebContents } from "electron";
import { Buffer } from "node:buffer";

type SenderLike = Pick<WebContents, "isDestroyed" | "send">;

/**
 * If a delivered frame is not acked within this window the receiver is treated
 * as dead for that stream (e.g. a hung/white-screened renderer whose
 * WebContents is still alive): buffered data is dropped, backpressure is
 * released, and any pending drain callback fires.
 */
const DEFAULT_STALL_TIMEOUT_MS = 30_000;

/**
 * Hard upper bound on how long closeWhenDrained waits for the buffer to drain
 * before force-dropping it and invoking the callback.
 */
const DEFAULT_MAX_DRAIN_WAIT_MS = 60_000;

function unrefTimer(timer: ReturnType<typeof setTimeout>): void {
  (timer as { unref?: () => void }).unref?.();
}

interface OrderedPayloadBuilder {
  streamId: string;
  deliveryId: number;
  chunk: string;
  /**
   * UTF-8 byte length of `chunk`, measured exactly once by the dispatcher.
   * This is the same value used for in-flight/backpressure accounting, so
   * receivers can ack it verbatim and the stream is guaranteed to drain.
   */
  byteLength: number;
}

interface QueuedChunk {
  chunk: string;
  byteLength: number;
}

interface OrderedStreamState<TPayload> {
  sender: SenderLike;
  pendingChunk: string;
  /**
   * Exact UTF-8 byte length of `pendingChunk`, maintained incrementally so
   * pushes/acks never re-scan the pending buffer (Buffer.byteLength is O(n)).
   */
  pendingChunkBytes: number;
  queuedChunks: QueuedChunk[];
  queuedBytes: number;
  inFlight?: {
    deliveryId: number;
    byteLength: number;
  };
  lastAckedDeliveryId: number;
  paused: boolean;
  onPause: () => void;
  onResume: () => void;
  drainTimer?: ReturnType<typeof setTimeout>;
  drainCallback?: () => void;
  stallTimer?: ReturnType<typeof setTimeout>;
  drainDeadlineTimer?: ReturnType<typeof setTimeout>;
}

export interface OrderedBytesDispatcherOptions<TPayload> {
  channel: string;
  flushIntervalMs: number;
  targetChunkBytes: number;
  highWaterBytes: number;
  lowWaterBytes: number;
  buildPayload: (input: OrderedPayloadBuilder) => TPayload;
  /** Overrides DEFAULT_STALL_TIMEOUT_MS (mainly for tests). */
  stallTimeoutMs?: number;
  /** Overrides DEFAULT_MAX_DRAIN_WAIT_MS (mainly for tests). */
  maxDrainWaitMs?: number;
}

export interface OrderedBytesPushInput {
  streamId: string;
  sender: SenderLike;
  chunk: string;
  onPause: () => void;
  onResume: () => void;
}

export interface OrderedBytesAckInput {
  streamId: string;
  deliveryId: number;
  consumedBytes?: number;
}

export interface OrderedBytesDispatcher<TPayload> {
  push: (input: OrderedBytesPushInput) => void;
  ack: (input: OrderedBytesAckInput) => void;
  closeWhenDrained: (streamId: string, onDrained: () => void) => void;
  clear: (streamId: string) => void;
}

function createOrderedState<TPayload>(input: OrderedBytesPushInput): OrderedStreamState<TPayload> {
  return {
    sender: input.sender,
    pendingChunk: "",
    pendingChunkBytes: 0,
    queuedChunks: [],
    queuedBytes: 0,
    lastAckedDeliveryId: 0,
    paused: false,
    onPause: input.onPause,
    onResume: input.onResume,
  };
}

function clearDrainTimer<TPayload>(state: OrderedStreamState<TPayload>): void {
  if (state.drainTimer) {
    clearTimeout(state.drainTimer);
    state.drainTimer = undefined;
  }
}

function clearStallTimer<TPayload>(state: OrderedStreamState<TPayload>): void {
  if (state.stallTimer) {
    clearTimeout(state.stallTimer);
    state.stallTimer = undefined;
  }
}

function clearDrainDeadlineTimer<TPayload>(state: OrderedStreamState<TPayload>): void {
  if (state.drainDeadlineTimer) {
    clearTimeout(state.drainDeadlineTimer);
    state.drainDeadlineTimer = undefined;
  }
}

function chunkByteLength(chunk: string): number {
  return Buffer.byteLength(chunk, "utf8");
}

/**
 * UTF-8 byte size of a single UTF-16 code unit measured in isolation, as
 * Node/Electron's per-char Buffer.byteLength reports it (a lone surrogate
 * encodes as the 3-byte replacement character). Used ONLY to pick split
 * points — matching the previous per-char Buffer.byteLength walk in
 * production — never for byte accounting, which is always measured.
 */
function utf8SizeOfCodeUnit(code: number): number {
  if (code < 0x80) {
    return 1;
  }
  if (code < 0x800) {
    return 2;
  }
  return 3;
}

function isHighSurrogate(code: number): boolean {
  return code >= 0xd800 && code <= 0xdbff;
}

function isLowSurrogate(code: number): boolean {
  return code >= 0xdc00 && code <= 0xdfff;
}

/**
 * Buffer.byteLength delta introduced at the boundary when `left + right` are
 * concatenated: a trailing high surrogate joining a leading low surrogate
 * becomes one 4-byte code point instead of two individually measured lone
 * halves. The delta is measured on just the two boundary code units with the
 * same Buffer.byteLength used everywhere else, so the incremental accounting
 * matches the runtime's own lone-surrogate measurement exactly (Node reports
 * a lone half as 3 bytes, Bun as 2 — hardcoding either would drift).
 */
function surrogateBoundaryDelta(left: string, right: string): number {
  if (left.length === 0 || right.length === 0) {
    return 0;
  }

  if (
    !isHighSurrogate(left.charCodeAt(left.length - 1)) ||
    !isLowSurrogate(right.charCodeAt(0))
  ) {
    return 0;
  }

  const high = left.charAt(left.length - 1);
  const low = right.charAt(0);
  return chunkByteLength(high + low) - chunkByteLength(high) - chunkByteLength(low);
}

/**
 * Exact byte length of `left + right` given each part's own measured byte
 * length, correcting for a surrogate pair joined at the boundary.
 */
function concatenatedByteLength(
  left: string,
  leftBytes: number,
  right: string,
  rightBytes: number
): number {
  return leftBytes + rightBytes + surrogateBoundaryDelta(left, right);
}

/**
 * Exact byte length of `rest` after `chunk` was split off the front of a
 * string measuring `totalBytes` (the inverse of concatenatedByteLength):
 * splitting a surrogate pair leaves two lone halves whose individual
 * measurements no longer sum to the pair's 4 bytes.
 */
function splitRemainderByteLength(
  totalBytes: number,
  chunk: string,
  chunkBytes: number,
  rest: string
): number {
  return totalBytes - chunkBytes - surrogateBoundaryDelta(chunk, rest);
}

/**
 * Split semantics are intentionally identical to the original per-code-unit
 * walk: the boundary may land between the halves of a surrogate pair, in
 * which case each frame carries a lone surrogate measured on its own —
 * receivers ack the reported per-frame byteLength verbatim, so accounting
 * stays consistent as long as frames are measured after the split.
 * `valueBytes` must be the exact byte length of `value` so the caller's
 * incremental accounting can skip re-measuring it.
 */
function splitChunkAtByteLimit(
  value: string,
  valueBytes: number,
  byteLimit: number
): { chunk: string; rest: string } {
  if (valueBytes <= byteLimit) {
    return { chunk: value, rest: "" };
  }

  let index = 0;
  let size = 0;
  while (index < value.length) {
    const nextSize = utf8SizeOfCodeUnit(value.charCodeAt(index));
    if (size > 0 && size + nextSize > byteLimit) {
      break;
    }

    size += nextSize;
    index += 1;
    if (size >= byteLimit) {
      break;
    }
  }

  return {
    chunk: value.slice(0, index),
    rest: value.slice(index)
  };
}

export function createOrderedBytesDispatcher<TPayload>(
  options: OrderedBytesDispatcherOptions<TPayload>
): OrderedBytesDispatcher<TPayload> {
  const streams = new Map<string, OrderedStreamState<TPayload>>();
  const stallTimeoutMs = options.stallTimeoutMs ?? DEFAULT_STALL_TIMEOUT_MS;
  const maxDrainWaitMs = options.maxDrainWaitMs ?? DEFAULT_MAX_DRAIN_WAIT_MS;
  let nextDeliveryId = 1;

  function totalBufferedBytes(state: OrderedStreamState<TPayload>): number {
    return state.pendingChunkBytes
      + state.queuedBytes
      + (state.inFlight?.byteLength ?? 0);
  }

  function maybeResume(streamId: string, state: OrderedStreamState<TPayload>): void {
    if (!state.paused || totalBufferedBytes(state) > options.lowWaterBytes) {
      return;
    }

    state.paused = false;
    state.onResume();
    if (!streams.has(streamId)) {
      return;
    }
  }

  function clearState(streamId: string, notifyDrain = false): void {
    const state = streams.get(streamId);
    if (!state) {
      return;
    }

    const drainCallback = notifyDrain ? state.drainCallback : undefined;
    // Null the field so a stale reference to this state (e.g. in ack()) can
    // never re-fire the callback.
    state.drainCallback = undefined;
    clearDrainTimer(state);
    clearStallTimer(state);
    clearDrainDeadlineTimer(state);
    if (state.paused) {
      state.paused = false;
      state.onResume();
    }
    streams.delete(streamId);
    drainCallback?.();
  }

  function maybeNotifyDrain(streamId: string, state: OrderedStreamState<TPayload>): void {
    if (!state.drainCallback) {
      return;
    }

    if (state.pendingChunk.length > 0 || state.queuedBytes > 0 || state.inFlight) {
      return;
    }

    const callback = state.drainCallback;
    state.drainCallback = undefined;
    clearState(streamId);
    callback();
  }

  function dropStalledStream(streamId: string): void {
    const state = streams.get(streamId);
    if (!state) {
      return;
    }

    // The receiver stopped acking (hung renderer with a live WebContents).
    // Drop everything buffered so the source is released and any pending
    // closeWhenDrained callback still fires; clearState resumes a paused
    // source and clears all timers.
    state.pendingChunk = "";
    state.pendingChunkBytes = 0;
    state.queuedChunks = [];
    state.queuedBytes = 0;
    state.inFlight = undefined;
    clearState(streamId, true);
  }

  function armStallTimer(streamId: string, state: OrderedStreamState<TPayload>): void {
    clearStallTimer(state);
    state.stallTimer = setTimeout(() => {
      dropStalledStream(streamId);
    }, stallTimeoutMs);
    unrefTimer(state.stallTimer);
  }

  function sendNext(streamId: string): void {
    const state = streams.get(streamId);
    if (!state) {
      return;
    }

    if (state.sender.isDestroyed()) {
      clearState(streamId, true);
      return;
    }

    if (state.inFlight || state.queuedChunks.length === 0) {
      maybeNotifyDrain(streamId, state);
      return;
    }

    const next = state.queuedChunks.shift();
    if (!next) {
      maybeNotifyDrain(streamId, state);
      return;
    }

    // Reuse the byte length measured when the frame was carved: the same
    // value drives queued/in-flight accounting AND the payload's byteLength,
    // so a verbatim ack from the receiver always balances the books.
    state.queuedBytes -= next.byteLength;
    const deliveryId = nextDeliveryId;
    nextDeliveryId += 1;
    state.inFlight = {
      deliveryId,
      byteLength: next.byteLength
    };
    state.sender.send(
      options.channel,
      options.buildPayload({
        streamId,
        deliveryId,
        chunk: next.chunk,
        byteLength: next.byteLength,
      })
    );
    armStallTimer(streamId, state);
  }

  function flushPending(streamId: string): void {
    const state = streams.get(streamId);
    if (!state) {
      return;
    }

    clearDrainTimer(state);
    if (state.pendingChunk.length === 0) {
      sendNext(streamId);
      return;
    }

    // The explicit length check makes the carve loop structurally unable to
    // spin even if the incremental byte counter ever disagreed with the
    // actual pending string.
    while (state.pendingChunk.length > 0 && state.pendingChunkBytes > options.targetChunkBytes) {
      const { chunk, rest } = splitChunkAtByteLimit(
        state.pendingChunk,
        state.pendingChunkBytes,
        options.targetChunkBytes
      );
      // Measure the carved frame exactly once; splitRemainderByteLength keeps
      // the pending counter exact even when the split lands inside a
      // surrogate pair (lone halves measure differently than the joined pair).
      const chunkBytes = chunkByteLength(chunk);
      state.pendingChunkBytes = splitRemainderByteLength(
        state.pendingChunkBytes,
        chunk,
        chunkBytes,
        rest
      );
      state.pendingChunk = rest;
      state.queuedChunks.push({ chunk, byteLength: chunkBytes });
      state.queuedBytes += chunkBytes;
    }

    if (state.pendingChunk.length === 0) {
      state.pendingChunkBytes = 0;
    }

    if (state.pendingChunk.length > 0 && state.queuedChunks.length === 0 && !state.inFlight) {
      state.queuedChunks.push({ chunk: state.pendingChunk, byteLength: state.pendingChunkBytes });
      state.queuedBytes += state.pendingChunkBytes;
      state.pendingChunk = "";
      state.pendingChunkBytes = 0;
    } else if (
      state.pendingChunk.length > 0 &&
      state.pendingChunkBytes >= options.targetChunkBytes
    ) {
      state.queuedChunks.push({ chunk: state.pendingChunk, byteLength: state.pendingChunkBytes });
      state.queuedBytes += state.pendingChunkBytes;
      state.pendingChunk = "";
      state.pendingChunkBytes = 0;
    }

    sendNext(streamId);
  }

  function ensureFlushTimer(streamId: string, state: OrderedStreamState<TPayload>): void {
    if (state.drainTimer) {
      return;
    }

    state.drainTimer = setTimeout(() => {
      flushPending(streamId);
    }, options.flushIntervalMs);
  }

  return {
    push(input) {
      if (!input.chunk) {
        return;
      }

      let state = streams.get(input.streamId);
      if (!state) {
        state = createOrderedState<TPayload>(input);
        streams.set(input.streamId, state);
      } else {
        state.sender = input.sender;
        state.onPause = input.onPause;
        state.onResume = input.onResume;
      }

      if (state.sender.isDestroyed()) {
        clearState(input.streamId);
        return;
      }

      // Measure the incoming chunk exactly once; the pending counter is then
      // maintained incrementally (with a boundary correction in case the
      // append joins two surrogate halves into one pair).
      state.pendingChunkBytes = concatenatedByteLength(
        state.pendingChunk,
        state.pendingChunkBytes,
        input.chunk,
        chunkByteLength(input.chunk)
      );
      state.pendingChunk += input.chunk;
      if (state.pendingChunkBytes >= options.targetChunkBytes) {
        flushPending(input.streamId);
      } else {
        ensureFlushTimer(input.streamId, state);
      }

      if (!state.paused && totalBufferedBytes(state) > options.highWaterBytes) {
        state.paused = true;
        state.onPause();
      }
    },
    ack(input) {
      const state = streams.get(input.streamId);
      if (!state?.inFlight) {
        return;
      }

      if (input.deliveryId <= state.lastAckedDeliveryId || input.deliveryId !== state.inFlight.deliveryId) {
        return;
      }

      const consumedBytes = input.consumedBytes ?? state.inFlight.byteLength;
      if (consumedBytes < state.inFlight.byteLength) {
        state.inFlight.byteLength -= consumedBytes;
        // Partial ack proves the receiver is alive: reset the stall window.
        armStallTimer(input.streamId, state);
        maybeResume(input.streamId, state);
        return;
      }

      state.lastAckedDeliveryId = input.deliveryId;
      state.inFlight = undefined;
      clearStallTimer(state);
      maybeResume(input.streamId, state);
      flushPending(input.streamId);
      maybeNotifyDrain(input.streamId, state);
    },
    closeWhenDrained(streamId, onDrained) {
      const state = streams.get(streamId);
      if (!state) {
        onDrained();
        return;
      }

      state.drainCallback = onDrained;
      flushPending(streamId);
      maybeNotifyDrain(streamId, state);

      // Hard cap: if the buffer has not drained by then (receiver stalled or
      // acking too slowly), force-drop it so the callback always fires.
      const remaining = streams.get(streamId);
      if (remaining && !remaining.drainDeadlineTimer) {
        remaining.drainDeadlineTimer = setTimeout(() => {
          dropStalledStream(streamId);
        }, maxDrainWaitMs);
        unrefTimer(remaining.drainDeadlineTimer);
      }
    },
    clear(streamId) {
      clearState(streamId);
    },
  };
}
