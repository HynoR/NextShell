import type { SessionDescriptor } from "@nextshell/core";
import { getBatchTargetConnectionIds } from "./batchTargets";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

const assertArrayEqual = (actual: string[], expected: string[], message: string): void => {
  assertEqual(actual.length, expected.length, `${message} length`);
  for (let index = 0; index < actual.length; index += 1) {
    assertEqual(actual[index], expected[index], `${message} [${index}]`);
  }
};

const createSession = (
  id: string,
  connectionId: string,
  type: SessionDescriptor["type"]
): SessionDescriptor => ({
  id,
  connectionId,
  title: `${type}:${connectionId}`,
  status: "connected",
  type,
  createdAt: "2026-01-01T00:00:00.000Z",
  reconnectable: true
});

(() => {
  const sessions: SessionDescriptor[] = [
    createSession("s1", "conn-a", "terminal"),
    createSession("s2", "conn-a", "editor"),
    createSession("s3", "conn-b", "processManager"),
    createSession("s4", "conn-c", "networkMonitor"),
    createSession("s5", "conn-b", "terminal")
  ];
  const result = getBatchTargetConnectionIds(sessions);
  assertArrayEqual(
    result,
    ["conn-a", "conn-b", "conn-c"],
    "connection ids should be deduped by first occurrence"
  );
})();

(() => {
  const sessions: SessionDescriptor[] = [];
  const result = getBatchTargetConnectionIds(sessions);
  assertArrayEqual(result, [], "empty sessions should return empty target ids");
})();
