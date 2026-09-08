import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import skillMarkdown from "../../../../../../nextshell-plugin/skills/nextshell/SKILL.md?raw";
import type { AgentEndpointStatus, AgentOpenRespondInput } from "@nextshell/shared";

import { AgentGateway, type AgentClientIdentity, type AgentGatewayDeps } from "./agent-gateway";
import { McpEndpointServer, type AgentLogger } from "./endpoint-server";
import { registerAgentTools } from "./tools";

export const MCP_SERVER_NAME = "nextshell";

/**
 * Harnesses install skills by copying a directory, so the bundled skill is
 * materialised on disk (rewritten on every start so it tracks the app version)
 * and its path is what the settings page hands out. The plugin directory is
 * the single source of the text.
 */
export const writeAgentSkillFile = (baseDir: string): string | null => {
  const dir = path.join(baseDir, "skills", "nextshell");
  try {
    mkdirSync(dir, { recursive: true });
    const file = path.join(dir, "SKILL.md");
    writeFileSync(file, skillMarkdown, "utf8");
    return file;
  } catch {
    return null;
  }
};

export interface AgentMcpServiceDeps extends AgentGatewayDeps {
  appVersion: string;
  /** Where the bundled SKILL.md was written for harnesses to copy; `null` when the write failed. */
  skillPath?: string | null;
  logger?: AgentLogger;
}

export interface AgentMcpService {
  /** Starts the listener when `preferences.agent.enabled` is true. Idempotent. */
  start: () => Promise<AgentEndpointStatus>;
  /** Stops the listener without touching preferences. Idempotent. */
  stop: () => Promise<AgentEndpointStatus>;
  /** Reconciles the running listener with the current preferences (enabled + port). */
  applyPreferences: () => Promise<AgentEndpointStatus>;
  getStatus: () => AgentEndpointStatus;
  /** Global breaker: rejects every tool call without tearing the endpoint down. */
  setHalted: (halted: boolean) => AgentEndpointStatus;
  /** The user's answer to a `session_open` dialog. */
  respondOpen: (response: AgentOpenRespondInput) => { ok: true };
  dispose: () => Promise<void>;
}

export const createAgentMcpService = (deps: AgentMcpServiceDeps): AgentMcpService => {
  const gateway = new AgentGateway(deps);

  let endpoint: McpEndpointServer | null = null;
  let lastError: string | null = null;
  let pending: Promise<void> = Promise.resolve();

  const preferences = () => deps.getPreferences().agent;

  const createMcpServer = (identity: AgentClientIdentity): McpServer => {
    const server = new McpServer(
      { name: MCP_SERVER_NAME, version: deps.appVersion },
      {
        instructions:
          "NextShell hands the agent the terminal tabs the user has already opened. session_list shows open tabs; host_list finds saved hosts (only the 10 most recent without a query); session_open asks the user to authorize opening one in NextShell — a denied result means stop and ask the user. exec is the only way to run commands; whether it runs in the foreground (typed into the tab, visible) or the background (separate channel, invisible), and whether each command first needs the user's click in NextShell, are the user's settings, not parameters — read `mode` in the response, and on `{ status: \"pending\", requestId }` call exec again with only that requestId; `denied` means stop and ask the user. Commands pass a dangerous-command blacklist, refuse to touch .env files with consent_required until the user agrees (then pass allowSensitive: true), and a foreground exec fails with human_intervention when the user has unsubmitted text on their command line — stop and report when that happens."
      }
    );
    registerAgentTools(server, { gateway, client: identity });
    return server;
  };

  const getStatus = (): AgentEndpointStatus => {
    const agent = preferences();
    return {
      enabled: agent.enabled,
      listening: endpoint?.listening ?? false,
      port: agent.port,
      url: endpoint?.url ?? null,
      clients: endpoint?.getClients() ?? [],
      lastError,
      skillPath: deps.skillPath ?? null,
      halted: gateway.isHalted
    };
  };

  const stopEndpoint = async (): Promise<void> => {
    if (endpoint) {
      await endpoint.stop();
      endpoint = null;
    }
  };

  const startEndpoint = async (port: number): Promise<void> => {
    const server = new McpEndpointServer({ port, createMcpServer, logger: deps.logger });
    try {
      await server.start();
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
      deps.logger?.error?.("MCP endpoint failed to start", { error: lastError });
      await server.stop().catch(() => undefined);
      return;
    }
    endpoint = server;
    lastError = null;
  };

  /** Serialized so overlapping enable/disable calls cannot interleave. */
  const enqueue = async (task: () => Promise<void>): Promise<AgentEndpointStatus> => {
    const run = pending.then(task, task);
    pending = run.then(
      () => undefined,
      () => undefined
    );
    await run;
    return getStatus();
  };

  const reconcile = async (): Promise<void> => {
    const agent = preferences();
    if (!agent.enabled) {
      await stopEndpoint();
      return;
    }
    if (endpoint && endpoint.port !== agent.port) {
      await stopEndpoint();
    }
    if (!endpoint) {
      await startEndpoint(agent.port);
    }
  };

  return {
    start: () => enqueue(reconcile),
    stop: () => enqueue(stopEndpoint),
    applyPreferences: () => enqueue(reconcile),
    getStatus,
    // Deliberately not `enqueue`d: a kill switch that waits behind whatever the
    // endpoint queue is doing is not a kill switch.
    setHalted: (halted) => {
      gateway.setHalted(halted);
      deps.logger?.warn?.(halted ? "Agent access halted by the user" : "Agent access resumed");
      return getStatus();
    },
    respondOpen: (response) => {
      gateway.respondOpen(response);
      return { ok: true };
    },
    dispose: () =>
      enqueue(stopEndpoint).then(() => {
        gateway.dispose();
      })
  };
};

export {
  AgentGateway,
  type AgentClientIdentity,
  type AgentGatewayDeps,
  type AgentSessionInfo,
  type AgentToolError,
  type AgentToolResult
} from "./agent-gateway";
export { buildEndpointUrl, McpEndpointServer } from "./endpoint-server";
