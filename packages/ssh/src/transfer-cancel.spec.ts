import { describe, expect, test } from "bun:test";
import { TransferCancelledError, isTransferCancelledError, runWithAbort } from "./index";

const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 5));

describe("isTransferCancelledError", () => {
  test("detects the TransferCancelledError class", () => {
    expect(isTransferCancelledError(new TransferCancelledError())).toBe(true);
  });

  test("detects duck-typed cancelled errors (crossing the IPC boundary)", () => {
    expect(isTransferCancelledError({ cancelled: true })).toBe(true);
  });

  test("rejects ordinary errors and non-objects", () => {
    expect(isTransferCancelledError(new Error("boom"))).toBe(false);
    expect(isTransferCancelledError(undefined)).toBe(false);
    expect(isTransferCancelledError("nope")).toBe(false);
  });
});

describe("runWithAbort", () => {
  test("runs the op directly when no signal is given", async () => {
    const result = await runWithAbort(undefined, async () => 42, () => {
      throw new Error("onAbort should not run");
    });
    expect(result).toBe(42);
  });

  test("returns the op result when it completes before any abort", async () => {
    const controller = new AbortController();
    let aborted = false;
    const result = await runWithAbort(controller.signal, async () => "done", () => {
      aborted = true;
    });
    expect(result).toBe("done");
    expect(aborted).toBe(false);
  });

  test("throws immediately and calls onAbort when already aborted", async () => {
    const controller = new AbortController();
    controller.abort();
    let aborted = false;
    let caught: unknown;
    try {
      await runWithAbort(controller.signal, async () => "x", () => {
        aborted = true;
      });
    } catch (error) {
      caught = error;
    }
    expect(aborted).toBe(true);
    expect(isTransferCancelledError(caught)).toBe(true);
  });

  test("aborts an in-flight op: rejects cancelled and invokes onAbort", async () => {
    const controller = new AbortController();
    let aborted = false;
    const promise = runWithAbort(
      controller.signal,
      () => new Promise<string>((resolve) => setTimeout(() => resolve("late"), 1000)),
      () => {
        aborted = true;
      }
    );
    controller.abort();
    let caught: unknown;
    try {
      await promise;
    } catch (error) {
      caught = error;
    }
    expect(aborted).toBe(true);
    expect(isTransferCancelledError(caught)).toBe(true);
  });

  test("propagates a real op error unchanged", async () => {
    const controller = new AbortController();
    let caught: unknown;
    try {
      await runWithAbort(controller.signal, async () => {
        throw new Error("real failure");
      }, () => undefined);
    } catch (error) {
      caught = error;
    }
    await tick();
    expect(isTransferCancelledError(caught)).toBe(false);
    expect((caught as Error).message).toBe("real failure");
  });
});
