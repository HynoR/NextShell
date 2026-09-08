import { Terminal, type IMarker } from "@xterm/headless";
import { SerializeAddon } from "@xterm/addon-serialize";

/** Matches the PTY size sessions are opened with (`session-service.ts`). */
export const SCREEN_MIRROR_DEFAULT_COLS = 140;
export const SCREEN_MIRROR_DEFAULT_ROWS = 40;
/**
 * Measured at roughly 2–4 MB RSS per busy wide terminal at this depth. Kept
 * modest on purpose: the mirror exists so an agent can see the *current* state
 * of a TUI, and `session_history` already answers "what scrolled past".
 */
export const SCREEN_MIRROR_DEFAULT_SCROLLBACK = 1000;
/** Concurrently mirrored sessions; the least recently written one is evicted. */
export const SCREEN_MIRROR_MAX_SESSIONS = 16;
/** Ceiling on a single `session_read` response. */
export const SCREEN_MIRROR_MAX_READ_LINES = 2000;

export type ScreenReadMode = "screen" | "scrollback";

export interface ScreenMirrorOptions {
  cols?: number;
  rows?: number;
  scrollback?: number;
}

export interface ScreenReadOptions {
  mode?: ScreenReadMode;
  /** Lines to return, counted back from the bottom. */
  lines?: number;
  /** Defaults to true: escape sequences are noise for a reader, not signal. */
  stripAnsi?: boolean;
}

export interface ScreenReadResult {
  mode: ScreenReadMode;
  content: string;
  lines: number;
  cols: number;
  rows: number;
  /** Scrollback lines currently held, so a caller can tell it hit the ceiling. */
  scrollbackLines: number;
  truncated: boolean;
}

const clamp = (value: number | undefined, fallback: number, min: number, max: number): number => {
  if (typeof value !== "number" || !Number.isFinite(value)) return fallback;
  return Math.min(max, Math.max(min, Math.trunc(value)));
};

/**
 * A headless terminal fed the same bytes as the user's tab, so the main process
 * can answer "what is on that screen right now".
 *
 * A raw byte log cannot answer that question: a `top` refresh is a stream of
 * cursor-addressing sequences, not a screenful of text. Only an emulator
 * collapses those into the frame a human would see.
 */
export class ScreenMirror {
  readonly sessionId: string;
  private readonly term: Terminal;
  private readonly serializer: SerializeAddon;
  /** Resolves once every byte written so far has been parsed. */
  private flushed: Promise<void> = Promise.resolve();
  private disposed = false;
  lastWriteAt: number;
  /**
   * Where the shell's input line starts: the cursor at the last OSC 133 `B`
   * (prompt end). `null` while a command runs or when the shell has no
   * integration. The marker follows the line through scrollback trimming.
   */
  private promptEnd: { x: number; marker: IMarker } | null = null;
  /** Whether any OSC 133 mark ever arrived — i.e. the shell has integration. */
  private sawPromptMark = false;

  constructor(sessionId: string, options: ScreenMirrorOptions = {}, now: () => number = Date.now) {
    this.sessionId = sessionId;
    this.lastWriteAt = now();
    this.term = new Terminal({
      cols: clamp(options.cols, SCREEN_MIRROR_DEFAULT_COLS, 1, 1000),
      rows: clamp(options.rows, SCREEN_MIRROR_DEFAULT_ROWS, 1, 500),
      scrollback: clamp(options.scrollback, SCREEN_MIRROR_DEFAULT_SCROLLBACK, 0, 100_000),
      // Required in the headless build: `buffer`, `parser`, `unicode` and
      // `markers` are all proposed API there, unlike in the DOM build where
      // only decorations and friends need this. Without it the constructor's
      // first `buffer` access throws.
      allowProposedApi: true
    });
    this.serializer = new SerializeAddon();
    this.term.loadAddon(this.serializer);
    this.term.parser.registerOscHandler(133, (data) => {
      this.sawPromptMark = true;
      this.promptEnd?.marker.dispose();
      this.promptEnd = null;
      if (data === "B" || data.startsWith("B;")) {
        const marker = this.term.registerMarker(0);
        if (marker) this.promptEnd = { x: this.term.buffer.active.cursorX, marker };
      }
      return false;
    });
  }

  get cols(): number {
    return this.term.cols;
  }

  get rows(): number {
    return this.term.rows;
  }

  write(chunk: string, now: () => number = Date.now): void {
    if (this.disposed || chunk.length === 0) return;
    this.lastWriteAt = now();
    // Chained rather than replaced: `Terminal.write` is queued and asynchronous,
    // so a read that only awaited the newest write could still observe a screen
    // assembled from an earlier one.
    this.flushed = this.flushed.then(
      () => new Promise<void>((resolve) => this.term.write(chunk, resolve))
    );
  }

  resize(cols: number, rows: number): void {
    if (this.disposed) return;
    this.term.resize(clamp(cols, this.term.cols, 1, 1000), clamp(rows, this.term.rows, 1, 500));
  }

  /** Awaits the write queue first — reading early returns a stale frame. */
  async read(options: ScreenReadOptions = {}): Promise<ScreenReadResult> {
    await this.flushed;
    const mode: ScreenReadMode = options.mode === "scrollback" ? "scrollback" : "screen";
    const stripAnsi = options.stripAnsi !== false;
    const buffer = this.term.buffer.active;
    const scrollbackLines = buffer.baseY;

    const top = mode === "screen" ? buffer.baseY : 0;
    // The emulator always has `rows` lines; the blank ones below the cursor are
    // padding, not content. Trimming them first is what makes `lines: 3` return
    // three lines the caller can use rather than one line and two blanks.
    let end = buffer.baseY + this.term.rows;
    while (end > top && this.lineAt(end - 1).length === 0) {
      end -= 1;
    }

    const available = end - top;
    const requested = clamp(options.lines, available, 1, SCREEN_MIRROR_MAX_READ_LINES);
    const count = Math.min(requested, available);
    const start = end - count;

    return {
      mode,
      content: stripAnsi ? this.readPlain(start, end) : this.readSerialized(mode, count),
      lines: count,
      cols: this.term.cols,
      rows: this.term.rows,
      scrollbackLines,
      truncated: start > top
    };
  }

  /**
   * Text the user has typed at the prompt but not submitted: everything between
   * the last OSC 133 `B` mark and the cursor. `""` means nothing is pending —
   * a clean line, or a command running with no prompt to type over; `null`
   * means the shell never sent a prompt mark, so there is nothing to judge by.
   * Measured to the cursor, not the end of the line, so a right-side prompt
   * (zsh RPROMPT) does not count as input.
   */
  async pendingInput(): Promise<string | null> {
    await this.flushed;
    if (!this.sawPromptMark) return null;
    const mark = this.promptEnd;
    if (!mark || mark.marker.isDisposed || mark.marker.line < 0) return "";
    const buffer = this.term.buffer.active;
    const cursorRow = buffer.baseY + buffer.cursorY;
    if (cursorRow < mark.marker.line) return "";
    const parts: string[] = [];
    for (let row = mark.marker.line; row <= cursorRow; row += 1) {
      const line = buffer.getLine(row)?.translateToString(true) ?? "";
      const from = row === mark.marker.line ? mark.x : 0;
      const to = row === cursorRow ? buffer.cursorX : line.length;
      parts.push(line.slice(from, to));
    }
    // ponytail: text left of a cursor moved back to the prompt (Home key) is missed; accept.
    return parts.join("").trim();
  }

  /**
   * `translateToString(true)` trims the trailing whitespace an emulator pads
   * every line with, which would otherwise be the bulk of the response for a
   * mostly-empty screen.
   */
  private lineAt(index: number): string {
    return this.term.buffer.active.getLine(index)?.translateToString(true) ?? "";
  }

  private readPlain(start: number, end: number): string {
    const lines: string[] = [];
    for (let index = start; index < end; index += 1) {
      lines.push(this.lineAt(index));
    }
    return lines.join("\n");
  }

  private readSerialized(mode: ScreenReadMode, count: number): string {
    const scrollback = mode === "screen" ? 0 : Math.max(0, count - this.term.rows);
    const lines = this.serializer.serialize({ scrollback }).split("\n");
    while (lines.length > 0 && lines[lines.length - 1] === "") {
      lines.pop();
    }
    return lines.length > count ? lines.slice(lines.length - count).join("\n") : lines.join("\n");
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.serializer.dispose();
    this.term.dispose();
  }
}

export interface ScreenMirrorRegistryOptions extends ScreenMirrorOptions {
  maxSessions?: number;
  now?: () => number;
}

/**
 * Holds one mirror per agent-visible session, with a hard ceiling on how many
 * exist at once. Sessions on hosts the agent cannot reach are never mirrored,
 * so an unauthorized host costs nothing.
 */
export class ScreenMirrorRegistry {
  private readonly mirrors = new Map<string, ScreenMirror>();
  private readonly options: ScreenMirrorRegistryOptions;
  private readonly maxSessions: number;
  private readonly now: () => number;

  constructor(options: ScreenMirrorRegistryOptions = {}) {
    this.options = options;
    this.maxSessions = Math.max(1, options.maxSessions ?? SCREEN_MIRROR_MAX_SESSIONS);
    this.now = options.now ?? Date.now;
  }

  get size(): number {
    return this.mirrors.size;
  }

  /** Feeds a session's bytes, creating its mirror on first sight. */
  write(sessionId: string, chunk: string): void {
    let mirror = this.mirrors.get(sessionId);
    if (!mirror) {
      this.evictIfFull();
      mirror = new ScreenMirror(sessionId, this.options, this.now);
      this.mirrors.set(sessionId, mirror);
    }
    mirror.write(chunk, this.now);
  }

  get(sessionId: string): ScreenMirror | undefined {
    return this.mirrors.get(sessionId);
  }

  resize(sessionId: string, cols: number, rows: number): void {
    this.mirrors.get(sessionId)?.resize(cols, rows);
  }

  dispose(sessionId: string): boolean {
    const mirror = this.mirrors.get(sessionId);
    if (!mirror) return false;
    mirror.dispose();
    this.mirrors.delete(sessionId);
    return true;
  }

  disposeAll(): void {
    for (const mirror of this.mirrors.values()) {
      mirror.dispose();
    }
    this.mirrors.clear();
  }

  /**
   * Drops the least recently written mirror. Recency beats age here: the tab a
   * user left idle an hour ago is the one an agent is least likely to ask about,
   * and it is also the one whose bytes are cheapest to lose.
   */
  private evictIfFull(): void {
    if (this.mirrors.size < this.maxSessions) return;
    let oldest: ScreenMirror | undefined;
    for (const mirror of this.mirrors.values()) {
      if (!oldest || mirror.lastWriteAt < oldest.lastWriteAt) oldest = mirror;
    }
    if (oldest) this.dispose(oldest.sessionId);
  }
}
