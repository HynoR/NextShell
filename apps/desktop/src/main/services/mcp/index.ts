import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type { AgentEndpointStatus } from "@nextshell/shared";

import { AgentGateway, type AgentClientIdentity, type AgentGatewayDeps } from "./agent-gateway";
import { McpEndpointServer, type AgentLogger } from "./endpoint-server";
import { registerAgentTools } from "./tools";

export const MCP_SERVER_NAME = "nextshell";

export interface AgentMcpServiceDeps extends AgentGatewayDeps {
  appVersion: string;
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
          "NextShell hands the agent the terminal tabs the user has already opened. session_list is the only discovery entry; exec and session_send_keys borrow a live session, pass a dangerous-command blacklist, and fail with human_intervention when the user typed into the tab after the agent's last operation — stop and report when that happens."
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
    dispose: () => enqueue(stopEndpoint).then(() => undefined)
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
