import { readFileSync, writeFileSync } from "node:fs";

import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import type {
  AgentClientConfigResult,
  AgentClientKind,
  AgentEndpointStatus,
  AgentExportMcpbResult,
  AgentInstallClaudeDesktopResult,
  AgentInstallCursorResult
} from "@nextshell/shared";

import {
  buildCursorDeeplink,
  buildMcpbArchive,
  installClaudeDesktopConfig
} from "./client-install";
import {
  AgentGateway,
  type AgentClientIdentity,
  type AgentGatewayDeps
} from "./agent-gateway";
import { EndpointDiscoveryFile } from "./discovery";
import { McpEndpointServer, type AgentLogger } from "./endpoint-server";
import { registerAgentTools } from "./tools";

export const ENDPOINT_ENV_VAR = "NEXTSHELL_MCP_ENDPOINT";
export const MCP_SERVER_NAME = "nextshell";
const MCP_CLIENT_KEY = "nextshell";
/** Makes the Electron binary behave as a plain Node runtime for the stdio bridge. */
const RUN_AS_NODE_ENV_VAR = "ELECTRON_RUN_AS_NODE";

export interface AgentMcpServiceDeps extends AgentGatewayDeps {
  /** Electron `app.getPath("userData")`; the discovery file lands in `<userData>/mcp`. */
  userDataDir: string;
  appVersion: string;
  /** Override for tests; production uses a short tmpdir path. */
  socketPath?: string;
  /**
   * Absolute path to the bundled stdio bridge entry, or null when it is not
   * shipped. The bridge is not published to npm, so a generated config must
   * point at the copy inside the installation — never at `npx <package>`.
   */
  resolveBridgeEntry?: () => string | null;
  /** Runtime that executes the bridge; defaults to the current executable. */
  bridgeRuntimePath?: string;
  writeClipboard?: (text: string) => void;
  /** Opens a URL with the OS default handler (Cursor deeplink install). */
  openExternal?: (url: string) => Promise<void>;
  /** Native save dialog; resolves null when the user cancels. */
  chooseSavePath?: (options: { title: string; defaultFileName: string }) => Promise<string | null>;
  /** Test override for the Claude Desktop config file location. */
  claudeDesktopConfigPath?: string;
  logger?: AgentLogger;
}

export interface AgentMcpService {
  /** Starts the listener when `preferences.agent.enabled` is true. Idempotent. */
  start: () => Promise<AgentEndpointStatus>;
  /** Stops the listener without touching preferences. Idempotent. */
  stop: () => Promise<AgentEndpointStatus>;
  /** Reconciles the running listener with the current preferences. */
  applyPreferences: () => Promise<AgentEndpointStatus>;
  getStatus: () => AgentEndpointStatus;
  buildClientConfig: (client: AgentClientKind) => AgentClientConfigResult;
  /** Opens the Cursor one-click install deeplink. */
  installCursor: () => Promise<AgentInstallCursorResult>;
  /** Merges the stdio bridge config into claude_desktop_config.json. */
  installClaudeDesktop: () => AgentInstallClaudeDesktopResult;
  /** Exports a `.mcpb` bundle via a save dialog. */
  exportMcpb: () => Promise<AgentExportMcpbResult>;
  /** Global breaker: rejects every tool call without tearing the endpoint down. */
  setHalted: (halted: boolean) => AgentEndpointStatus;
  dispose: () => Promise<void>;
}

const shellQuote = (value: string): string =>
  /^[A-Za-z0-9_@%+=:,./-]+$/.test(value) ? value : `'${value.replace(/'/g, `'\\''`)}'`;

export const createAgentMcpService = (deps: AgentMcpServiceDeps): AgentMcpService => {
  const gateway = new AgentGateway(deps);
  const discovery = new EndpointDiscoveryFile({
    userDataDir: deps.userDataDir,
    appVersion: deps.appVersion
  });

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
      socketPath: endpoint?.socketPath ?? null,
      endpointFilePath: discovery.primaryPath,
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
    await discovery.remove();
  };

  const startEndpoint = async (): Promise<void> => {
    const server = new McpEndpointServer({
      socketPath: deps.socketPath,
      createMcpServer,
      logger: deps.logger
    });

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

    try {
      await discovery.write({ socketPath: server.socketPath });
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
      deps.logger?.warn?.("Failed to write the MCP endpoint discovery file", { error: lastError });
    }
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

  /**
   * The stdio bridge config every command-based client shares. Throws when the
   * bundled bridge is missing — every caller must fail before showing dialogs.
   */
  const buildStdioServerConfig = (): {
    bridgeEntry: string;
    runtime: string;
    env: Record<string, string>;
    serverConfig: Record<string, unknown>;
  } => {
    const bridgeEntry = deps.resolveBridgeEntry?.() ?? null;
    if (!bridgeEntry) {
      throw new Error("未找到随应用分发的 MCP 桥接程序，无法生成接入配置；请重新安装应用");
    }
    const runtime = deps.bridgeRuntimePath ?? process.execPath;
    const env = {
      [RUN_AS_NODE_ENV_VAR]: "1",
      [ENDPOINT_ENV_VAR]: discovery.primaryPath
    };
    return { bridgeEntry, runtime, env, serverConfig: { command: runtime, args: [bridgeEntry], env } };
  };

  const reconcile = async (): Promise<void> => {
    const agent = preferences();
    if (!agent.enabled) {
      await stopEndpoint();
      return;
    }
    if (!endpoint) {
      await startEndpoint();
    }
  };

  return {
    start: () => enqueue(reconcile),
    stop: () => enqueue(stopEndpoint),
    applyPreferences: () => enqueue(reconcile),
    getStatus,
    buildClientConfig: (client) => {
      const status = getStatus();
      const stdio = buildStdioServerConfig();
      const command = `claude mcp add ${MCP_CLIENT_KEY} --env ${RUN_AS_NODE_ENV_VAR}=1 --env ${ENDPOINT_ENV_VAR}=${shellQuote(
        status.endpointFilePath
      )} -- ${shellQuote(stdio.runtime)} ${shellQuote(stdio.bridgeEntry)}`;

      const json = JSON.stringify({ mcpServers: { [MCP_CLIENT_KEY]: stdio.serverConfig } }, null, 2);
      deps.writeClipboard?.(client === "claude-code" ? command : json);
      return { ok: true, command, json };
    },
    installCursor: async () => {
      const deeplink = buildCursorDeeplink(MCP_CLIENT_KEY, buildStdioServerConfig().serverConfig);
      await deps.openExternal?.(deeplink);
      return { ok: true, deeplink };
    },
    installClaudeDesktop: () => {
      const { serverConfig } = buildStdioServerConfig();
      const { configPath } = installClaudeDesktopConfig(MCP_CLIENT_KEY, serverConfig, {
        configPath: deps.claudeDesktopConfigPath
      });
      return { ok: true, configPath };
    },
    exportMcpb: async () => {
      // Resolve the bridge before the dialog: a save prompt that can only end
      // in an error is worse than failing immediately.
      const { bridgeEntry } = buildStdioServerConfig();
      if (!deps.chooseSavePath) {
        throw new Error("当前环境不支持保存对话框，无法导出 .mcpb");
      }
      const savePath = await deps.chooseSavePath({
        title: "导出 NextShell .mcpb 安装包",
        defaultFileName: "nextshell.mcpb"
      });
      if (!savePath) return { ok: false, canceled: true };
      const archive = buildMcpbArchive({
        appVersion: deps.appVersion,
        endpointFilePath: discovery.primaryPath,
        bridgeCode: readFileSync(bridgeEntry)
      });
      writeFileSync(savePath, archive);
      return { ok: true, filePath: savePath };
    },
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
export { McpEndpointServer, resolveDefaultSocketPath } from "./endpoint-server";
export { EndpointDiscoveryFile, type EndpointDiscoveryRecord } from "./discovery";
