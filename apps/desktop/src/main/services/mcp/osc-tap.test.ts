import { describe, expect, test } from "vitest";

import { OSC_TAP_MAX_OUTPUT_BYTES, OscTap, OscTapRegistry, parseOsc7Cwd } from "./osc-tap";

const BEL = "\u0007";
const ESC = "\u001b";
const oscBel = (payload: string): string => `${ESC}]${payload}${BEL}`;
const oscSt = (payload: string): string => `${ESC}]${payload}${ESC}\\`;

const clock = (...values: string[]): (() => number) => {
  const timestamps = values.map((value) => Date.parse(value));
  let index = 0;
  return () => timestamps[Math.min(index++, timestamps.length - 1)]!;
};

describe("OscTap streaming parser", () => {
  test("parses OSC 7 and OSC 133 across every chunk boundary with BEL and ST terminators", () => {
    const tap = new OscTap("session-1", {
      now: clock("2026-08-04T00:00:00.000Z", "2026-08-04T00:00:02.000Z")
    });
    const stream = [
      oscSt("7;file://remote/home/user/my%20project"),
      oscBel("133;C;printf 'hello;world'"),
      "hello\r\n",
      oscSt("133;D;0")
    ].join("");

    for (const character of stream) {
      tap.feed(character);
    }

    expect(tap.getSnapshot()).toEqual({
      sessionId: "session-1",
      cwd: "/home/user/my project",
      lastCommand: "printf 'hello;world'",
      activeCommand: null,
      history: [
        {
          command: "printf 'hello;world'",
          exitCode: 0,
          startedAt: "2026-08-04T00:00:00.000Z",
          endedAt: "2026-08-04T00:00:02.000Z",
          output: "hello\r\n",
          outputBytes: 7,
          truncated: false
        }
      ]
    });
  });

  test("accepts a legacy bare C mark and still captures its output and exit code", () => {
    const tap = new OscTap("legacy", {
      now: clock("2026-08-04T01:00:00.000Z", "2026-08-04T01:00:01.000Z")
    });

    tap.feed(`${oscBel("133;C")}legacy output${oscBel("133;D;17")}`);

    expect(tap.getSnapshot().lastCommand).toBeNull();
    expect(tap.getSnapshot().history).toEqual([
      {
        command: null,
        exitCode: 17,
        startedAt: "2026-08-04T01:00:00.000Z",
        endedAt: "2026-08-04T01:00:01.000Z",
        output: "legacy output",
        outputBytes: 13,
        truncated: false
      }
    ]);
  });

  test("preserves unrelated OSC and ANSI bytes inside command output", () => {
    const tap = new OscTap("raw");
    tap.feed(oscBel("133;C;echo test"));
    tap.feed(`\u001b[31mred${oscBel("0;title")}\u001b[0m`);
    tap.feed(oscBel("133;D;0"));

    expect(tap.getSnapshot().history[0]?.output).toBe(`\u001b[31mred${oscBel("0;title")}\u001b[0m`);
  });
});

describe("OscTap memory bounds and malformed input", () => {
  test("caps retained output by UTF-8 bytes while reporting the full observed size", () => {
    const tap = new OscTap("bounded", { maxOutputBytes: 7 });
    tap.feed(`${oscBel("133;C;emit")}abc😀tail${oscBel("133;D;0")}`);

    const entry = tap.getSnapshot().history[0];
    expect(entry).toMatchObject({
      output: "abc😀",
      outputBytes: Buffer.byteLength("abc😀tail"),
      truncated: true
    });
    expect(Buffer.byteLength(entry?.output ?? "")).toBe(7);
    expect(OSC_TAP_MAX_OUTPUT_BYTES).toBe(512 * 1024);
  });

  test("drops an overlong unterminated OSC from parser state and recovers for the next mark", () => {
    const tap = new OscTap("malformed", { maxPendingBytes: 32 });
    tap.feed(`${ESC}]7;${"x".repeat(80)}`);
    tap.feed(`${BEL}${oscBel("7;file://host/recovered")}`);

    expect(tap.getSnapshot().cwd).toBe("/recovered");
  });

  test("ignores malformed cwd and safely finalizes malformed or orphan exit marks", () => {
    const tap = new OscTap("bad-fields");
    tap.feed(oscBel("7;file://host/%ZZ"));
    tap.feed(oscBel("133;D;0"));
    tap.feed(`${oscBel("133;C;false")}failure${oscBel("133;D;not-a-number")}`);

    expect(tap.getSnapshot().cwd).toBeNull();
    expect(tap.getSnapshot().history).toHaveLength(1);
    expect(tap.getSnapshot().history[0]?.exitCode).toBeNull();
  });

  test("bounds completed command history", () => {
    const tap = new OscTap("history", { maxHistoryEntries: 2 });
    for (const command of ["one", "two", "three"]) {
      tap.feed(`${oscBel(`133;C;${command}`)}${command}${oscBel("133;D;0")}`);
    }
    expect(tap.getSnapshot().history.map((entry) => entry.command)).toEqual(["two", "three"]);
  });
});

describe("OscTapRegistry", () => {
  test("creates taps lazily and supports feed, get, list and per-session disposal", () => {
    const registry = new OscTapRegistry();
    registry.feed("b", oscBel("7;file://host/b"));
    registry.feed("a", oscBel("7;file://host/a"));

    expect(registry.get("a")?.cwd).toBe("/a");
    expect(registry.list().map((snapshot) => snapshot.sessionId)).toEqual(["b", "a"]);
    expect(registry.dispose("a")).toBe(true);
    expect(registry.dispose("a")).toBe(false);
    expect(registry.get("a")).toBeUndefined();
    expect(registry.list()).toHaveLength(1);
  });

  test("disposeAll releases every session and later feeds create fresh state", () => {
    const registry = new OscTapRegistry();
    registry.feed("one", oscBel("7;file://host/old"));
    registry.feed("two", oscBel("133;C;pending"));
    registry.disposeAll();

    expect(registry.list()).toEqual([]);
    registry.feed("one", "plain");
    expect(registry.get("one")).toMatchObject({ cwd: null, history: [] });
  });

  test("getSummary reads cwd and last command without cloning history", () => {
    const registry = new OscTapRegistry();
    registry.feed("a", `${oscBel("7;file://host/srv")}${oscBel("133;C;uptime")}`);

    expect(registry.getSummary("a")).toEqual({ cwd: "/srv", lastCommand: "uptime" });
    expect(registry.getSummary("missing")).toBeUndefined();
  });
});

describe("parseOsc7Cwd", () => {
  test("accepts only file URLs with absolute decoded paths", () => {
    expect(parseOsc7Cwd("file://host//var///www/")).toBe("/var/www");
    expect(parseOsc7Cwd("file://host/path%00injected")).toBeNull();
    expect(parseOsc7Cwd("https://host/path")).toBeNull();
    expect(parseOsc7Cwd("not a url")).toBeNull();
  });
});

describe("session retention budget", () => {
  test("older entries release their output once the session budget is exceeded", () => {
    const tap = new OscTap("session-budget", {
      maxOutputBytes: 1024,
      maxSessionOutputBytes: 2048
    });

    for (const name of ["first", "second", "third", "fourth"]) {
      tap.feed(oscBel(`133;C;${name}`));
      tap.feed("x".repeat(1024));
      tap.feed(oscBel("133;D;0"));
    }

    const history = tap.getSnapshot().history;
    // Every command survives with its exit code; only the bytes are released.
    expect(history.map((entry) => entry.command)).toEqual(["first", "second", "third", "fourth"]);
    expect(history.map((entry) => entry.output.length)).toEqual([0, 0, 1024, 1024]);
    expect(history.map((entry) => entry.truncated)).toEqual([true, true, false, false]);
    // The reported byte count still reflects what the command actually produced.
    expect(history.map((entry) => entry.outputBytes)).toEqual([1024, 1024, 1024, 1024]);
  });

  test("the newest command always keeps its output, even alone over budget", () => {
    const tap = new OscTap("session-newest", {
      maxOutputBytes: 4096,
      maxSessionOutputBytes: 16
    });

    tap.feed(oscBel("133;C;only"));
    tap.feed("y".repeat(4096));
    tap.feed(oscBel("133;D;0"));

    expect(tap.getSnapshot().history[0]?.output).toHaveLength(4096);
  });
});

describe("waiting on command completion", () => {
  test("resolves with the entry the shell just finished", async () => {
    const tap = new OscTap("session-wait");
    const pending = tap.waitForCommandCompletion(5_000);

    tap.feed(oscBel("133;C;make deploy"));
    tap.feed("building...\r\n");
    tap.feed(oscBel("133;D;0"));

    await expect(pending).resolves.toMatchObject({
      command: "make deploy",
      exitCode: 0,
      output: "building...\r\n",
      truncated: false
    });
  });

  test("a waiter sees the output even though the budget evicts it right after", async () => {
    const tap = new OscTap("session-wait-evict", {
      maxOutputBytes: 64,
      maxSessionOutputBytes: 1
    });
    const pending = tap.waitForCommandCompletion(5_000);

    tap.feed(oscBel("133;C;echo hi"));
    tap.feed("hi\r\n");
    tap.feed(oscBel("133;D;0"));

    await expect(pending).resolves.toMatchObject({ output: "hi\r\n" });
  });

  test("resolves null on timeout rather than inventing an exit code", async () => {
    const tap = new OscTap("session-timeout");
    await expect(tap.waitForCommandCompletion(1)).resolves.toBeNull();
  });

  test("disposal releases every waiter", async () => {
    const tap = new OscTap("session-disposed");
    const pending = tap.waitForCommandCompletion(60_000);
    tap.dispose();
    await expect(pending).resolves.toBeNull();
    // A tap disposed before the wait cannot hang either.
    await expect(tap.waitForCommandCompletion(60_000)).resolves.toBeNull();
  });

  test("every concurrent waiter is resolved by the same mark", async () => {
    const tap = new OscTap("session-multi");
    const first = tap.waitForCommandCompletion(5_000);
    const second = tap.waitForCommandCompletion(5_000);

    tap.feed(oscBel("133;C;uptime"));
    tap.feed(oscBel("133;D;3"));

    const [a, b] = await Promise.all([first, second]);
    expect(a?.exitCode).toBe(3);
    expect(b?.exitCode).toBe(3);
  });

  test("a registry wait on an unseen session resolves null instead of hanging", async () => {
    const registry = new OscTapRegistry();
    await expect(registry.waitForCommandCompletion("nobody", 5_000)).resolves.toBeNull();
  });
});
