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

const measureBytes = (text: string): number => {
  return encoder.encode(text).length;
};

const truncateToLastBytes = (text: string, maxBytes: number): string => {
  if (maxBytes <= 0) {
    return "";
  }

  const encoded = encoder.encode(text);
  if (encoded.length <= maxBytes) {
    return text;
  }

  return decoder.decode(encoded.slice(encoded.length - maxBytes));
};

const trimLeadingBytes = (text: string, bytesToTrim: number): string => {
  if (bytesToTrim <= 0) {
    return text;
  }

  const encoded = encoder.encode(text);
  if (bytesToTrim >= encoded.length) {
    return "";
  }

  return decoder.decode(encoded.slice(bytesToTrim));
};

export const createEmptyBuffer = (): SessionOutputBuffer => {
  return {
    chunks: [],
    totalBytes: 0
  };
};

export const appendWithLimit = (
  buffer: SessionOutputBuffer,
  text: string,
  maxBytes: number = MAX_SESSION_OUTPUT_BYTES
): SessionOutputBuffer => {
  if (!text) {
    return buffer;
  }

  if (maxBytes <= 0) {
    return createEmptyBuffer();
  }

  const normalizedText = truncateToLastBytes(text, maxBytes);
  const chunkBytes = measureBytes(normalizedText);
  if (chunkBytes <= 0) {
    return buffer;
  }

  const chunks = [...buffer.chunks, { text: normalizedText, bytes: chunkBytes }];
  let totalBytes = buffer.totalBytes + chunkBytes;

  while (totalBytes > maxBytes && chunks.length > 0) {
    const overflow = totalBytes - maxBytes;
    const first = chunks[0];
    if (!first) {
      break;
    }

    if (first.bytes <= overflow) {
      chunks.shift();
      totalBytes -= first.bytes;
      continue;
    }

    const trimmedText = trimLeadingBytes(first.text, overflow);
    const trimmedBytes = measureBytes(trimmedText);
    chunks[0] = { text: trimmedText, bytes: trimmedBytes };
    totalBytes = totalBytes - first.bytes + trimmedBytes;
  }

  return { chunks, totalBytes };
};

export const toReplayChunks = (buffer: SessionOutputBuffer): string[] => {
  return buffer.chunks.map((chunk) => chunk.text);
};
