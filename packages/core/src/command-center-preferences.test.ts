import {
  normalizeBatchMaxConcurrency,
  normalizeBatchRetryCount
} from "./index";

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected "${String(expected)}", got "${String(actual)}"`);
  }
};

(() => {
  assertEqual(
    normalizeBatchMaxConcurrency(10, 5),
    10,
    "batch max concurrency should accept in-range values"
  );
  assertEqual(
    normalizeBatchMaxConcurrency(undefined, 5),
    5,
    "batch max concurrency should fallback on undefined"
  );
  assertEqual(
    normalizeBatchMaxConcurrency(0, 5),
    5,
    "batch max concurrency should fallback below min"
  );
  assertEqual(
    normalizeBatchMaxConcurrency(100, 5),
    5,
    "batch max concurrency should fallback above max"
  );
})();

(() => {
  assertEqual(
    normalizeBatchRetryCount(3, 1),
    3,
    "batch retry count should accept in-range values"
  );
  assertEqual(
    normalizeBatchRetryCount(undefined, 1),
    1,
    "batch retry count should fallback on undefined"
  );
  assertEqual(
    normalizeBatchRetryCount(-1, 1),
    1,
    "batch retry count should fallback below min"
  );
  assertEqual(
    normalizeBatchRetryCount(8, 1),
    1,
    "batch retry count should fallback above max"
  );
})();
