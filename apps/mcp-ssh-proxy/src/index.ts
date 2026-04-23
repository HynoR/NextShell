#!/usr/bin/env node

import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  createReadonlyCredentialContext,
  CredentialStoreUnavailableError,
  NextShellDataNotFoundError
} from "@nextshell/runtime";

import { createNextShellMcpServer } from "./server.js";

const writeStderr = (message: string): void => {
  process.stderr.write(`${message}\n`);
};

const normalizeStartupError = (error: unknown): string => {
  if (error instanceof NextShellDataNotFoundError || error instanceof CredentialStoreUnavailableError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim().length > 0) {
    return error.message;
  }
  return "Unknown startup error";
};

const main = async (): Promise<void> => {
  const context = createReadonlyCredentialContext();
  const runtime = createNextShellMcpServer({ context });
  const shutdown = async () => {
    try {
      await runtime.close();
    } catch (error) {
      writeStderr(`Shutdown failed: ${normalizeStartupError(error)}`);
    }
  };

  process.once("SIGINT", () => {
    void shutdown().finally(() => process.exit(0));
  });
  process.once("SIGTERM", () => {
    void shutdown().finally(() => process.exit(0));
  });
  process.once("beforeExit", () => {
    void shutdown();
  });

  const transport = new StdioServerTransport();
  await runtime.server.connect(transport);
};

void main().catch((error) => {
  writeStderr(`Failed to start NextShell MCP SSH proxy: ${normalizeStartupError(error)}`);
  process.exitCode = 1;
});
