import { appendWithLimit, createEmptyBuffer, toReplayChunks } from "./sessionOutputBuffer";

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

const utf8Length = (text: string): number => new TextEncoder().encode(text).length;

// Normal path: chunks accumulate with exact byte accounting and replay in order.
{
  const buffer = createEmptyBuffer();
  const result = appendWithLimit(buffer, "hello", 64);
  assert(result === buffer, "appendWithLimit should mutate and return the same buffer");
  appendWithLimit(buffer, " world", 64);

  assertEqual(buffer.chunks.length, 2, "each append should store one chunk");
  assertEqual(buffer.totalBytes, utf8Length("hello world"), "totalBytes should be the byte sum");
  assertEqual(buffer.chunks[0]?.bytes, 5, "stored chunk should carry its own byte count");
  assertEqual(
    toReplayChunks(buffer).join(""),
    "hello world",
    "replay should reproduce appends in order"
  );
}

// Empty text is a no-op; non-positive maxBytes empties the buffer.
{
  const buffer = createEmptyBuffer();
  appendWithLimit(buffer, "abc", 64);
  appendWithLimit(buffer, "", 64);
  assertEqual(buffer.totalBytes, 3, "empty text must not change the buffer");

  appendWithLimit(buffer, "xyz", 0);
  assertEqual(buffer.chunks.length, 0, "maxBytes <= 0 should empty the buffer");
  assertEqual(buffer.totalBytes, 0, "maxBytes <= 0 should reset totalBytes");
}

// Cap eviction: oldest whole chunks are dropped first.
{
  const buffer = createEmptyBuffer();
  appendWithLimit(buffer, "aaaa", 8);
  appendWithLimit(buffer, "bbbb", 8);
  appendWithLimit(buffer, "cccc", 8);

  assertEqual(buffer.totalBytes, 8, "buffer should stay at the byte cap");
  assertEqual(toReplayChunks(buffer).join(""), "bbbbcccc", "oldest chunk should be evicted whole");
}

// Cap eviction: an overflow smaller than the oldest chunk trims it partially.
{
  const buffer = createEmptyBuffer();
  appendWithLimit(buffer, "aaaa", 10);
  appendWithLimit(buffer, "bbbb", 10);
  appendWithLimit(buffer, "cccc", 10);

  assertEqual(buffer.totalBytes, 10, "buffer should stay at the byte cap after a partial trim");
  assertEqual(
    toReplayChunks(buffer).join(""),
    "aabbbbcccc",
    "oldest chunk should lose only the overflow"
  );
}

// A single chunk larger than the cap keeps only its last maxBytes bytes.
{
  const buffer = createEmptyBuffer();
  appendWithLimit(buffer, "abcdefgh", 4);
  assertEqual(buffer.totalBytes, 4, "oversized chunk should be truncated to the cap");
  assertEqual(toReplayChunks(buffer).join(""), "efgh", "truncation should keep the newest bytes");
}

// Multibyte boundary: truncating an oversized chunk on a character boundary
// keeps whole characters ("你好" is 6 UTF-8 bytes; the last 3 are "好").
{
  const buffer = createEmptyBuffer();
  appendWithLimit(buffer, "你好", 3);
  assertEqual(
    toReplayChunks(buffer).join(""),
    "好",
    "boundary truncation should keep whole characters"
  );
  assertEqual(buffer.totalBytes, 3, "boundary truncation should report exact bytes");
}

// Multibyte boundary: a cross-chunk trim landing on a character boundary
// removes exactly the overflow (drop "你", keep "好世界").
{
  const buffer = createEmptyBuffer();
  appendWithLimit(buffer, "你好", 9);
  appendWithLimit(buffer, "世界", 9);
  assertEqual(buffer.totalBytes, 9, "multibyte trim should land exactly on the cap");
  assertEqual(
    toReplayChunks(buffer).join(""),
    "好世界",
    "multibyte trim should drop whole leading characters"
  );
}

// Multibyte boundary: a mid-character cut cannot make progress (replacement
// characters re-inflate the chunk), so the whole oldest chunk is evicted
// instead of looping forever.
{
  const buffer = createEmptyBuffer();
  appendWithLimit(buffer, "你好", 10);
  appendWithLimit(buffer, "世界", 10);
  assertEqual(
    toReplayChunks(buffer).join(""),
    "世界",
    "non-converging mid-character trim should evict the whole chunk"
  );
  assertEqual(buffer.totalBytes, 6, "totalBytes should match the surviving chunks");
}
