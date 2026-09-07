import { randomUUID } from "node:crypto";

import type { AgentOpenRequestEvent, AgentOpenRespondInput } from "@nextshell/shared";

/** The user has this long to click 授权 in NextShell before the request is denied. */
export const OPEN_REQUEST_TIMEOUT_MS = 5 * 60_000;

export interface OpenOutcome {
  approved: boolean;
  sessionId?: string;
}

interface PendingOpen {
  outcome: Promise<OpenOutcome>;
  resolve: (outcome: OpenOutcome) => void;
  timer: ReturnType<typeof setTimeout>;
}

export interface AgentOpenBrokerOptions {
  /** Shows the request in the GUI (and raises the window). */
  send: (request: AgentOpenRequestEvent) => void;
  timeoutMs?: number;
  now?: () => number;
}

/**
 * Correlates a `session_open` tool call with the authorization dialog in the
 * renderer. The 5-minute clock hangs on the request, not on any single MCP
 * call: a call waits for a bounded slice via {@link wait} and reports
 * `pending` so the agent can keep waiting without tripping harness timeouts.
 */
export class AgentOpenBroker {
  private readonly options: AgentOpenBrokerOptions;
  private readonly pending = new Map<string, PendingOpen>();

  constructor(options: AgentOpenBrokerOptions) {
    this.options = options;
  }

  create(input: Omit<AgentOpenRequestEvent, "id" | "expiresAt">): string {
    const id = randomUUID();
    const timeoutMs = this.options.timeoutMs ?? OPEN_REQUEST_TIMEOUT_MS;
    const now = this.options.now?.() ?? Date.now();
    let resolve!: (outcome: OpenOutcome) => void;
    const outcome = new Promise<OpenOutcome>((r) => {
      resolve = r;
    });
    const timer = setTimeout(() => resolve({ approved: false }), timeoutMs);
    timer.unref?.();
    this.pending.set(id, { outcome, resolve, timer });
    try {
      this.options.send({ ...input, id, expiresAt: new Date(now + timeoutMs).toISOString() });
    } catch {
      resolve({ approved: false });
    }
    return id;
  }

  /** `null` = unknown or already consumed id; `"pending"` = still waiting. */
  async wait(id: string, waitMs: number): Promise<OpenOutcome | "pending" | null> {
    const entry = this.pending.get(id);
    if (!entry) return null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const slice = new Promise<"pending">((r) => {
      timer = setTimeout(() => r("pending"), waitMs);
    });
    const result = await Promise.race([entry.outcome, slice]);
    if (timer) clearTimeout(timer);
    if (result === "pending") return "pending";
    clearTimeout(entry.timer);
    this.pending.delete(id);
    return result;
  }

  respond(response: AgentOpenRespondInput): boolean {
    const entry = this.pending.get(response.id);
    if (!entry) return false;
    entry.resolve({ approved: response.approved, sessionId: response.sessionId });
    return true;
  }

  dispose(): void {
    for (const entry of this.pending.values()) {
      clearTimeout(entry.timer);
      entry.resolve({ approved: false });
    }
    this.pending.clear();
  }
}
