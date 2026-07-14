export interface SessionOutputChunk {
  text: string;
  bytes: number;
}

export interface SessionOutputBuffer {
  chunks: SessionOutputChunk[];
  totalBytes: number;
}

export const MAX_SESSION_OUTPUT_BYTES = 2 * 1024 * 1024;

const encoder = new TextEncoder();
const decoder = new TextDecoder();

// UTF-8 continuation bytes are 0b10xxxxxx; a cut landing in front of one is
// mid-sequence, so the decoder will emit replacement characters there.
const isContinuationByte = (byte: number): boolean => {
  return (byte & 0xc0) === 0x80;
};

/**
 * Drop `bytesToTrim` UTF-8 bytes from the front of an encoded chunk and
 * return the remaining text with its exact byte count. When the cut lands on
 * a sequence boundary the byte count is simply the remaining encoded length
 * (TextEncoder output is always valid UTF-8, so the decode round-trips
 * byte-identically); only a mid-sequence cut — which makes the decoder emit
 * replacement characters — requires re-encoding to stay exact.
 */
const trimLeadingEncodedBytes = (encoded: Uint8Array, bytesToTrim: number): SessionOutputChunk => {
  const remaining = encoded.subarray(bytesToTrim);
  const text = decoder.decode(remaining);
  const firstByte = remaining[0];
  const bytes =
    firstByte !== undefined && isContinuationByte(firstByte)
      ? encoder.encode(text).length
      : remaining.length;

  return { text, bytes };
};

export const createEmptyBuffer = (): SessionOutputBuffer => {
  return {
    chunks: [],
    totalBytes: 0
  };
};

/**
 * Append `text`, evicting the oldest content beyond `maxBytes`.
 *
 * The buffer is mutated in place and returned: callers own their buffers
 * privately (TerminalPane keeps them in a ref Map that is never
 * identity-compared by React), and this runs per terminal data frame, so no
 * copies or re-encodes of previously stored chunks happen on the hot path —
 * each incoming chunk is encoded exactly once.
 */
export const appendWithLimit = (
  buffer: SessionOutputBuffer,
  text: string,
  maxBytes: number = MAX_SESSION_OUTPUT_BYTES
): SessionOutputBuffer => {
  if (!text) {
    return buffer;
  }

  if (maxBytes <= 0) {
    buffer.chunks.length = 0;
    buffer.totalBytes = 0;
    return buffer;
  }

  // Encode the incoming chunk once and reuse the bytes for both the
  // oversized-chunk truncation and the stored byte count.
  const encoded = encoder.encode(text);
  const chunk: SessionOutputChunk =
    encoded.length <= maxBytes
      ? { text, bytes: encoded.length }
      : trimLeadingEncodedBytes(encoded, encoded.length - maxBytes);
  if (chunk.bytes <= 0) {
    return buffer;
  }

  const chunks = buffer.chunks;
  chunks.push(chunk);
  let totalBytes = buffer.totalBytes + chunk.bytes;

  let dropCount = 0;
  while (totalBytes > maxBytes && dropCount < chunks.length) {
    const first = chunks[dropCount];
    if (!first) {
      break;
    }

    const overflow = totalBytes - maxBytes;
    if (first.bytes <= overflow) {
      dropCount += 1;
      totalBytes -= first.bytes;
      continue;
    }

    // Partial trim of the oldest chunk; only this single chunk is re-encoded.
    const trimmed = trimLeadingEncodedBytes(encoder.encode(first.text), overflow);
    if (trimmed.bytes >= first.bytes) {
      // A mid-sequence cut can inflate the remainder with replacement
      // characters until it cancels (or exceeds) the bytes removed; without
      // strict progress this loop would never terminate, so evict the whole
      // chunk instead.
      dropCount += 1;
      totalBytes -= first.bytes;
      continue;
    }

    chunks[dropCount] = trimmed;
    totalBytes = totalBytes - first.bytes + trimmed.bytes;
  }

  if (dropCount > 0) {
    chunks.splice(0, dropCount);
  }

  buffer.totalBytes = totalBytes;
  return buffer;
};

export const toReplayChunks = (buffer: SessionOutputBuffer): string[] => {
  return buffer.chunks.map((chunk) => chunk.text);
};
