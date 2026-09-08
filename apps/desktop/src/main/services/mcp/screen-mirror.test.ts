import { describe, expect, test } from "vitest";

import { ScreenMirror, ScreenMirrorRegistry } from "./screen-mirror";

const ESC = "";
const BEL = "";

const mirror = (options = {}): ScreenMirror =>
  new ScreenMirror("session-1", { cols: 40, rows: 6, scrollback: 20, ...options });

describe("ScreenMirror rendering", () => {
  test("collapses cursor-addressed rewrites into the frame a human would see", async () => {
    const term = mirror();
    term.write("line1\r\nline2\r\nline3\r\n");
    // What a full-screen program actually sends: move the cursor, overwrite.
    term.write(`${ESC}[2;1HREPLACED`);

    const screen = await term.read({ mode: "screen" });
    expect(screen.content.split("\n")).toEqual(["line1", "REPLACED", "line3"]);
    term.dispose();
  });

  test("a repainting TUI converges on its last frame instead of piling up", async () => {
    const term = mirror();
    for (const load of ["0.10", "0.40", "0.90"]) {
      // Home the cursor, clear, repaint — three frames of a `top`-style refresh.
      term.write(`${ESC}[H${ESC}[2Jload average: ${load}\r\ntasks: 120`);
    }

    const screen = await term.read({ mode: "screen" });
    expect(screen.content).toBe("load average: 0.90\ntasks: 120");
    // Cursor-addressed repaints must not grow the scrollback.
    expect(screen.scrollbackLines).toBe(0);
    term.dispose();
  });

  test("reads reflect every byte written, not a half-parsed frame", async () => {
    const term = mirror();
    // No await between writes: the emulator queues them asynchronously, and a
    // read that did not drain the queue would return an earlier frame.
    for (let index = 0; index < 50; index += 1) {
      term.write(`chunk${index}\r\n`);
    }

    const screen = await term.read({ mode: "screen" });
    expect(screen.content.split("\n").at(-1)).toBe("chunk49");
    term.dispose();
  });
});

describe("ScreenMirror read modes", () => {
  test("screen returns the viewport, scrollback reaches further back", async () => {
    const term = mirror();
    for (let index = 0; index < 30; index += 1) {
      term.write(`row${index}\r\n`);
    }

    const screen = await term.read({ mode: "screen" });
    const scrollback = await term.read({ mode: "scrollback" });

    expect(screen.lines).toBeLessThanOrEqual(6);
    expect(screen.content).toContain("row29");
    expect(screen.content).not.toContain("row10");
    expect(scrollback.content).toContain("row10");
    expect(scrollback.scrollbackLines).toBeGreaterThan(0);
    term.dispose();
  });

  test("lines caps the response from the bottom and reports truncation", async () => {
    const term = mirror();
    for (let index = 0; index < 30; index += 1) {
      term.write(`row${index}\r\n`);
    }

    const limited = await term.read({ mode: "scrollback", lines: 3 });
    expect(limited.lines).toBe(3);
    expect(limited.truncated).toBe(true);
    expect(limited.content.split("\n")).toEqual(["row27", "row28", "row29"]);
    term.dispose();
  });

  test("stripAnsi defaults to on and can be turned off", async () => {
    const term = mirror();
    term.write(`${ESC}[31mred${ESC}[0m\r\n`);

    const plain = await term.read({ mode: "screen" });
    const styled = await term.read({ mode: "screen", stripAnsi: false });

    expect(plain.content).toBe("red");
    expect(plain.content).not.toContain(ESC);
    expect(styled.content).toContain(ESC);
    term.dispose();
  });

  test("OSC sequences never surface as visible screen content", async () => {
    const term = mirror();
    term.write(`${ESC}]7;file://host/var/www${BEL}`);
    term.write(`${ESC}]133;C;du -sh${BEL}`);
    term.write("4.0K\ttotal\r\n");

    const screen = await term.read({ mode: "screen" });
    expect(screen.content).toBe("4.0K    total");
    term.dispose();
  });
});

describe("ScreenMirror pending input", () => {
  const prompt = `${ESC}]133;A${BEL}user@host $ ${ESC}]133;B${BEL}`;

  test("is null without shell integration and empty right after the prompt", async () => {
    const term = mirror();
    term.write("$ ");
    expect(await term.pendingInput()).toBeNull();
    term.write(prompt);
    expect(await term.pendingInput()).toBe("");
    term.dispose();
  });

  test("reports what the user typed until the command starts", async () => {
    const term = mirror();
    term.write(prompt);
    term.write("vim foo.txt");
    expect(await term.pendingInput()).toBe("vim foo.txt");
    // While the command runs there is no prompt to type over.
    term.write(`\r\n${ESC}]133;C;vim foo.txt${BEL}`);
    expect(await term.pendingInput()).toBe("");
    term.write(`${ESC}]133;D;0${BEL}\r\n${prompt}`);
    expect(await term.pendingInput()).toBe("");
    term.dispose();
  });

  test("ignores a right-side prompt and follows wrapped input", async () => {
    const term = mirror();
    // zsh draws RPROMPT to the right of the cursor and moves back.
    term.write(`${prompt}${ESC}[s${ESC}[1;30H12:00${ESC}[u`);
    expect(await term.pendingInput()).toBe("");
    term.write("echo " + "x".repeat(40));
    expect(await term.pendingInput()).toBe("echo " + "x".repeat(40));
    term.dispose();
  });
});

describe("ScreenMirrorRegistry", () => {
  test("creates mirrors lazily and disposes them per session", async () => {
    const registry = new ScreenMirrorRegistry({ cols: 20, rows: 4, scrollback: 10 });
    registry.write("a", "hello\r\n");
    registry.write("b", "world\r\n");

    expect(registry.size).toBe(2);
    expect((await registry.get("a")?.read())?.content).toBe("hello");
    expect(registry.dispose("a")).toBe(true);
    expect(registry.dispose("a")).toBe(false);
    expect(registry.get("a")).toBeUndefined();

    registry.disposeAll();
    expect(registry.size).toBe(0);
  });

  test("evicts the least recently written session once at capacity", () => {
    let clock = 0;
    const registry = new ScreenMirrorRegistry({
      maxSessions: 2,
      cols: 20,
      rows: 4,
      now: () => (clock += 1)
    });

    registry.write("old", "a");
    registry.write("recent", "b");
    registry.write("recent", "c");
    registry.write("new", "d");

    expect(registry.get("old")).toBeUndefined();
    expect(registry.get("recent")).toBeDefined();
    expect(registry.get("new")).toBeDefined();
  });

  test("a resize follows the user's terminal so wrapping matches", async () => {
    const registry = new ScreenMirrorRegistry({ cols: 10, rows: 4, scrollback: 10 });
    registry.write("a", "x");
    registry.resize("a", 80, 24);

    const screen = await registry.get("a")?.read();
    expect(screen?.cols).toBe(80);
    expect(screen?.rows).toBe(24);
    registry.disposeAll();
  });

  test("resizing an unmirrored session is a no-op rather than an error", () => {
    const registry = new ScreenMirrorRegistry();
    expect(() => registry.resize("missing", 80, 24)).not.toThrow();
  });
});

describe("resource footprint", () => {
  /**
   * Phase 3.4: confirm the per-session cost under a realistic wide-terminal
   * load before this ships as a resident main-process cost. The threshold is
   * deliberately loose — it is a regression guard against an order-of-magnitude
   * change, not a benchmark.
   */
  test("a full scrollback of a wide terminal stays within a few MB per session", async () => {
    const instances: ScreenMirror[] = [];
    const count = 4;
    const payload = `${ESC}[32m${"lorem ipsum dolor sit amet ".repeat(5)}${ESC}[0m\r\n`;

    global.gc?.();
    const before = process.memoryUsage().heapUsed;
    for (let index = 0; index < count; index += 1) {
      const instance = new ScreenMirror(`session-${index}`, {
        cols: 140,
        rows: 40,
        scrollback: 1000
      });
      // Enough to push past the scrollback ceiling, which is the worst case.
      for (let line = 0; line < 1100; line += 1) {
        instance.write(payload);
      }
      const screen = await instance.read({ mode: "screen" });
      expect(screen.scrollbackLines).toBe(1000);
      instances.push(instance);
    }
    const perInstanceMb = (process.memoryUsage().heapUsed - before) / count / 1024 / 1024;

    expect(perInstanceMb).toBeLessThan(8);
    for (const instance of instances) instance.dispose();
  }, 60_000);
});
