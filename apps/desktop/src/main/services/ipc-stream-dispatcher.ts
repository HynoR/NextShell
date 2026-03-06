import type { WebContents } from "electron";
import { Buffer } from "node:buffer";

type SenderLike = Pick<WebContents, "isDestroyed" | "send">;

interface OrderedPayloadBuilder {
  streamId: string;
  deliveryId: number;
  chunk: string;
}

interface LatestPayloadBuilder<TValue> {
  streamId: string;
  deliveryId: number;
  payload: TValue;
}

interface OrderedStreamState<TPayload> {
  sender: SenderLike;
  pendingChunk: string;
  queuedChunks: string[];
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
}

interface LatestStreamState<TPayload, TValue> {
  sender: SenderLike;
  inFlight?: {
    deliveryId: number;
  };
  pendingPayload?: TValue;
  lastAckedDeliveryId: number;
}

export interface OrderedBytesDispatcherOptions<TPayload> {
  channel: string;
  flushIntervalMs: number;
  targetChunkBytes: number;
  highWaterBytes: number;
  lowWaterBytes: number;
  buildPayload: (input: OrderedPayloadBuilder) => TPayload;
}

export interface LatestOnlyDispatcherOptions<TPayload, TValue> {
  channel: string;
  buildPayload: (input: LatestPayloadBuilder<TValue>) => TPayload;
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

export interface LatestOnlyPublishInput<TValue> {
  streamId: string;
  sender: SenderLike;
  payload: TValue;
}

export interface LatestOnlyAckInput {
  streamId: string;
  deliveryId: number;
}

export interface OrderedBytesDispatcher<TPayload> {
  push: (input: OrderedBytesPushInput) => void;
  ack: (input: OrderedBytesAckInput) => void;
  closeWhenDrained: (streamId: string, onDrained: () => void) => void;
  clear: (streamId: string) => void;
}

export interface LatestOnlyDispatcher<TValue> {
  publish: (input: LatestOnlyPublishInput<TValue>) => void;
  ack: (input: LatestOnlyAckInput) => void;
  clear: (streamId: string) => void;
}

function createOrderedState<TPayload>(input: OrderedBytesPushInput): OrderedStreamState<TPayload> {
  return {
    sender: input.sender,
    pendingChunk: "",
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

function chunkByteLength(chunk: string): number {
  return Buffer.byteLength(chunk, "utf8");
}

function splitChunkAtByteLimit(value: string, byteLimit: number): { chunk: string; rest: string } {
  if (chunkByteLength(value) <= byteLimit) {
    return { chunk: value, rest: "" };
  }

  let index = 0;
  let size = 0;
  while (index < value.length) {
    const nextChar = value[index];
    if (typeof nextChar !== "string") {
      break;
    }

    const nextSize = chunkByteLength(nextChar);
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
  let nextDeliveryId = 1;

  function totalBufferedBytes(state: OrderedStreamState<TPayload>): number {
    return chunkByteLength(state.pendingChunk)
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
    clearDrainTimer(state);
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
    clearState(streamId);
    callback();
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

    const chunk = state.queuedChunks.shift();
    if (!chunk) {
      maybeNotifyDrain(streamId, state);
      return;
    }

    state.queuedBytes -= chunkByteLength(chunk);
    const deliveryId = nextDeliveryId;
    nextDeliveryId += 1;
    state.inFlight = {
      deliveryId,
      byteLength: chunkByteLength(chunk)
    };
    state.sender.send(
      options.channel,
      options.buildPayload({
        streamId,
        deliveryId,
        chunk,
      })
    );
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

    while (chunkByteLength(state.pendingChunk) > options.targetChunkBytes) {
      const { chunk, rest } = splitChunkAtByteLimit(state.pendingChunk, options.targetChunkBytes);
      state.pendingChunk = rest;
      state.queuedChunks.push(chunk);
      state.queuedBytes += chunkByteLength(chunk);
    }

    if (state.pendingChunk.length > 0 && state.queuedChunks.length === 0 && !state.inFlight) {
      state.queuedChunks.push(state.pendingChunk);
      state.queuedBytes += chunkByteLength(state.pendingChunk);
      state.pendingChunk = "";
    } else if (
      state.pendingChunk.length > 0 &&
      chunkByteLength(state.pendingChunk) >= options.targetChunkBytes
    ) {
      state.queuedChunks.push(state.pendingChunk);
      state.queuedBytes += chunkByteLength(state.pendingChunk);
      state.pendingChunk = "";
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

      state.pendingChunk += input.chunk;
      if (chunkByteLength(state.pendingChunk) >= options.targetChunkBytes) {
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
        maybeResume(input.streamId, state);
        return;
      }

      state.lastAckedDeliveryId = input.deliveryId;
      state.inFlight = undefined;
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
    },
    clear(streamId) {
      clearState(streamId);
    },
  };
}

export function createLatestOnlyDispatcher<TPayload, TValue>(
  options: LatestOnlyDispatcherOptions<TPayload, TValue>
): LatestOnlyDispatcher<TValue> {
  const streams = new Map<string, LatestStreamState<TPayload, TValue>>();
  let nextDeliveryId = 1;

  function clear(streamId: string): void {
    streams.delete(streamId);
  }

  function send(streamId: string, state: LatestStreamState<TPayload, TValue>, payload: TValue): void {
    if (state.sender.isDestroyed()) {
      clear(streamId);
      return;
    }

    const deliveryId = nextDeliveryId;
    nextDeliveryId += 1;
    state.inFlight = { deliveryId };
    state.sender.send(
      options.channel,
      options.buildPayload({
        streamId,
        deliveryId,
        payload,
      })
    );
  }

  return {
    publish(input) {
      const current = streams.get(input.streamId);
      const state: LatestStreamState<TPayload, TValue> = current ?? {
        sender: input.sender,
        lastAckedDeliveryId: 0,
      };

      state.sender = input.sender;
      streams.set(input.streamId, state);

      if (state.sender.isDestroyed()) {
        clear(input.streamId);
        return;
      }

      if (state.inFlight) {
        state.pendingPayload = input.payload;
        return;
      }

      send(input.streamId, state, input.payload);
    },
    ack(input) {
      const state = streams.get(input.streamId);
      if (!state?.inFlight) {
        return;
      }

      if (input.deliveryId <= state.lastAckedDeliveryId || input.deliveryId !== state.inFlight.deliveryId) {
        return;
      }

      state.lastAckedDeliveryId = input.deliveryId;
      state.inFlight = undefined;

      if (state.pendingPayload === undefined) {
        return;
      }

      const nextPayload = state.pendingPayload;
      state.pendingPayload = undefined;
      send(input.streamId, state, nextPayload);
    },
    clear,
  };
}
