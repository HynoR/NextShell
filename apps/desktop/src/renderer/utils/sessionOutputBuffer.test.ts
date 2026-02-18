import {
  appendWithLimit,
  createEmptyBuffer,
  toReplayChunks
} from "./sessionOutputBuffer";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  let buffer = createEmptyBuffer();
  buffer = appendWithLimit(buffer, "abc", 5);
  buffer = appendWithLimit(buffer, "de", 5);
  buffer = appendWithLimit(buffer, "f", 5);

  assertEqual(toReplayChunks(buffer).join(""), "bcdef", "fifo trim keeps newest content");
  assertEqual(buffer.totalBytes, 5, "total bytes respects max");
})();

(() => {
  let sessionABuffer = createEmptyBuffer();
  let sessionBBuffer = createEmptyBuffer();

  sessionABuffer = appendWithLimit(sessionABuffer, "A1", 8);
  sessionBBuffer = appendWithLimit(sessionBBuffer, "B1", 8);
  sessionABuffer = appendWithLimit(sessionABuffer, "A2", 8);
  sessionBBuffer = appendWithLimit(sessionBBuffer, "B2", 8);

  assertEqual(toReplayChunks(sessionABuffer).join(""), "A1A2", "session A isolated");
  assertEqual(toReplayChunks(sessionBBuffer).join(""), "B1B2", "session B isolated");
})();

(() => {
  let buffer = createEmptyBuffer();
  buffer = appendWithLimit(buffer, "\r\n[session connecting]\r\n", 256);
  buffer = appendWithLimit(buffer, "root@host:~# ls\r\n", 256);
  buffer = appendWithLimit(buffer, "file-1\r\n", 256);

  assertEqual(
    toReplayChunks(buffer).join(""),
    "\r\n[session connecting]\r\nroot@host:~# ls\r\nfile-1\r\n",
    "status and data chunks preserve order"
  );
})();

(() => {
  let buffer = createEmptyBuffer();
  buffer = appendWithLimit(buffer, "", 16);
  assertEqual(buffer.totalBytes, 0, "empty append ignored");

  const long = "x".repeat(100);
  buffer = appendWithLimit(buffer, long, 16);
  assertEqual(toReplayChunks(buffer).join(""), "x".repeat(16), "single oversize chunk is truncated");
  assertEqual(buffer.totalBytes, 16, "oversize chunk keeps max bytes");
})();
