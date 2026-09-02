import type { ExecResult } from "@nextshell/ssh";
import {
  SHELL_INTEGRATION_PROBE_COMMAND,
  buildBootstrapInstallCommand,
  buildIntegrationLaunchCommand,
  resolveShellFamily,
  type ShellIntegrationFamily
} from "../../shared/shell-integration";

// Shell-integration bootstrap ("auto" preference mode): the integration is
// activated at shell *startup* — the terminal channel is opened as an exec+PTY
// request that launches the login shell with a bootstrap file (bash
// `--init-file`, zsh ZDOTDIR shims, fish `-C`), after installing the files
// under $HOME/.cache/nextshell through a plain exec channel. Nothing is ever
// typed into the user's shell: no history pollution, no echoed source line,
// and no timing race with full-screen programs or prompts. Every failure
// (probe, unsupported family, install error) degrades to a plain `shell()`
// request with no integration — failing quietly beats interfering.

// Re-exported so session-service has a single import for the whole feature.
export {
  SHELL_INTEGRATION_PROBE_COMMAND,
  resolveShellFamily,
  type ShellIntegrationFamily
} from "../../shared/shell-integration";

export interface ShellIntegrationExecLike {
  exec(command: string): Promise<ExecResult>;
}

/**
 * Per-connection launch-command bookkeeping. Several tabs to one server open
 * within milliseconds of each other; only the first runs the probe + install
 * round trips, the rest await the same promise. A resolved `undefined` means
 * "integration unavailable here" and is deliberately NOT cached: transient
 * failures (dropped exec channel, full disk) should not disable integration
 * for the rest of the connection's lifetime.
 */
const launchCommandByConnectionId = new Map<string, Promise<string | undefined>>();
/** Weak, so connections without an id take their bookkeeping with them. */
let launchCommandByConnection = new WeakMap<
  ShellIntegrationExecLike,
  Promise<string | undefined>
>();

/**
 * Drops the cached launch command for one connection (or all of them). Call it
 * when a connection is torn down so a reconnect — possibly to a host whose
 * cache dir was wiped meanwhile — probes and installs again.
 */
export const forgetShellIntegrationInstalls = (connectionId?: string): void => {
  if (connectionId === undefined) {
    launchCommandByConnectionId.clear();
    launchCommandByConnection = new WeakMap();
    return;
  }
  launchCommandByConnectionId.delete(connectionId);
};

export interface ShellIntegrationLaunchOptions {
  connection: ShellIntegrationExecLike;
  /** Dedupe key; falls back to the `connection` object's identity. */
  connectionId?: string;
  /** Script body override for tests; defaults to the bundled per-family script. */
  scriptText?: string;
  log?: (message: string, metadata?: Record<string, unknown>) => void;
}

const probeAndInstall = async (
  options: ShellIntegrationLaunchOptions
): Promise<string | undefined> => {
  const { connection, scriptText, log = () => undefined } = options;

  let family: ShellIntegrationFamily | undefined;
  try {
    const probe = await connection.exec(SHELL_INTEGRATION_PROBE_COMMAND);
    family = resolveShellFamily(probe.stdout.trim() || undefined);
  } catch (error) {
    log("[ShellIntegration] login-shell probe failed; opening plain shell", {
      reason: error instanceof Error ? error.message : String(error)
    });
    return undefined;
  }

  if (!family) {
    log("[ShellIntegration] unknown login shell; opening plain shell");
    return undefined;
  }

  const launchCommand = buildIntegrationLaunchCommand(family);
  if (!launchCommand) {
    log("[ShellIntegration] no startup hook for this shell; opening plain shell", { family });
    return undefined;
  }

  try {
    const result = await connection.exec(
      buildBootstrapInstallCommand(family, scriptText ? () => scriptText : undefined)
    );
    if (result.exitCode !== 0) {
      log("[ShellIntegration] bootstrap install failed; opening plain shell", {
        family,
        exitCode: result.exitCode
      });
      return undefined;
    }
  } catch (error) {
    log("[ShellIntegration] bootstrap install failed; opening plain shell", {
      family,
      reason: error instanceof Error ? error.message : String(error)
    });
    return undefined;
  }

  return launchCommand;
};

/**
 * Resolves the exec+PTY command that opens an integration-enabled login shell
 * on this connection, or undefined when the terminal should fall back to a
 * plain `shell()` request. Never rejects. Concurrent callers on the same
 * connection share one probe + install.
 */
export const prepareShellIntegrationLaunch = (
  options: ShellIntegrationLaunchOptions
): Promise<string | undefined> => {
  const { connection, connectionId } = options;
  const cached =
    connectionId !== undefined
      ? launchCommandByConnectionId.get(connectionId)
      : launchCommandByConnection.get(connection);
  if (cached) {
    return cached;
  }

  const pending = probeAndInstall(options)
    .catch(() => undefined)
    .then((launchCommand) => {
      if (launchCommand === undefined) {
        // Keep failures out of the cache so the next session tries again.
        if (connectionId !== undefined) {
          if (launchCommandByConnectionId.get(connectionId) === pending) {
            launchCommandByConnectionId.delete(connectionId);
          }
        } else if (launchCommandByConnection.get(connection) === pending) {
          launchCommandByConnection.delete(connection);
        }
      }
      return launchCommand;
    });

  if (connectionId !== undefined) {
    launchCommandByConnectionId.set(connectionId, pending);
  } else {
    launchCommandByConnection.set(connection, pending);
  }
  return pending;
};
