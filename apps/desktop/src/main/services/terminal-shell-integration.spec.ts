import { beforeEach, describe, expect, test } from "vitest";
import type { ExecResult } from "@nextshell/ssh";
import {
  forgetShellIntegrationInstalls,
  prepareShellIntegrationLaunch,
  type ShellIntegrationExecLike
} from "./terminal-shell-integration";
import {
  SHELL_INTEGRATION_PROBE_COMMAND,
  buildIntegrationLaunchCommand
} from "../../shared/shell-integration";

const ok = (stdout: string): ExecResult => ({ stdout, stderr: "", exitCode: 0 });

interface FakeConnection extends ShellIntegrationExecLike {
  commands: string[];
}

/**
 * First exec is always the $SHELL probe, later ones the bootstrap install.
 * `shellPath` drives the probe; `installExitCode`/`failProbe` drive failures.
 */
const createFakeConnection = (options: {
  shellPath?: string;
  installExitCode?: number;
  failProbe?: boolean;
  /** Resolves execs manually; used to overlap concurrent callers. */
  gate?: Array<() => void>;
}): FakeConnection => {
  const commands: string[] = [];
  return {
    commands,
    async exec(command) {
      commands.push(command);
      if (options.gate) {
        await new Promise<void>((resolve) => options.gate!.push(resolve));
      }
      if (command === SHELL_INTEGRATION_PROBE_COMMAND) {
        if (options.failProbe) {
          throw new Error("probe channel died");
        }
        return ok(options.shellPath ?? "/bin/bash");
      }
      return { stdout: "", stderr: "", exitCode: options.installExitCode ?? 0 };
    }
  };
};

beforeEach(() => {
  forgetShellIntegrationInstalls();
});

describe("prepareShellIntegrationLaunch", () => {
  test("probes the login shell, installs the bootstrap and returns the launch command", async () => {
    const connection = createFakeConnection({ shellPath: "/usr/bin/zsh" });

    const launch = await prepareShellIntegrationLaunch({ connection, connectionId: "c1" });

    expect(launch).toBe(buildIntegrationLaunchCommand("zsh"));
    expect(connection.commands[0]).toBe(SHELL_INTEGRATION_PROBE_COMMAND);
    expect(connection.commands).toHaveLength(2);
    expect(connection.commands[1]).toContain("zdotdir");
  });

  test("concurrent tabs on one connection share a single probe + install", async () => {
    const gate: Array<() => void> = [];
    const connection = createFakeConnection({ shellPath: "/bin/bash", gate });

    const first = prepareShellIntegrationLaunch({ connection, connectionId: "c1" });
    const second = prepareShellIntegrationLaunch({ connection, connectionId: "c1" });
    // Release the probe, then the install.
    while (gate.length > 0 || connection.commands.length < 2) {
      gate.shift()?.();
      await Promise.resolve();
    }

    expect(await first).toBe(buildIntegrationLaunchCommand("bash"));
    expect(await second).toBe(await first);
    expect(connection.commands).toHaveLength(2);
  });

  test("a probe failure resolves undefined and is retried by the next session", async () => {
    const failing = createFakeConnection({ failProbe: true });
    expect(
      await prepareShellIntegrationLaunch({ connection: failing, connectionId: "c1" })
    ).toBeUndefined();

    // Failure was not cached: a healthy connection under the same id retries.
    const healthy = createFakeConnection({ shellPath: "/bin/bash" });
    expect(await prepareShellIntegrationLaunch({ connection: healthy, connectionId: "c1" })).toBe(
      buildIntegrationLaunchCommand("bash")
    );
  });

  test("a non-zero install exit resolves undefined without caching", async () => {
    const connection = createFakeConnection({ shellPath: "/bin/bash", installExitCode: 1 });

    expect(await prepareShellIntegrationLaunch({ connection, connectionId: "c1" })).toBeUndefined();
    expect(
      await prepareShellIntegrationLaunch({
        connection: createFakeConnection({ shellPath: "/bin/bash" }),
        connectionId: "c1"
      })
    ).toBe(buildIntegrationLaunchCommand("bash"));
  });

  test("shells without a safe startup hook get no integration and no install", async () => {
    for (const shellPath of ["/bin/sh", "/bin/dash", "/opt/weird/xonsh", ""]) {
      const connection = createFakeConnection({ shellPath });
      expect(
        await prepareShellIntegrationLaunch({ connection, connectionId: `c-${shellPath}` })
      ).toBeUndefined();
      // The probe ran; nothing was installed for sh/unknown shells.
      expect(connection.commands).toHaveLength(1);
    }
  });

  test("a cached success is reused until the connection is forgotten", async () => {
    const connection = createFakeConnection({ shellPath: "/bin/bash" });
    await prepareShellIntegrationLaunch({ connection, connectionId: "c1" });
    await prepareShellIntegrationLaunch({ connection, connectionId: "c1" });
    expect(connection.commands).toHaveLength(2);

    forgetShellIntegrationInstalls("c1");
    await prepareShellIntegrationLaunch({ connection, connectionId: "c1" });
    expect(connection.commands).toHaveLength(4);
  });

  test("falls back to connection identity when no connectionId is given", async () => {
    const connection = createFakeConnection({ shellPath: "/bin/bash" });
    await prepareShellIntegrationLaunch({ connection });
    await prepareShellIntegrationLaunch({ connection });
    expect(connection.commands).toHaveLength(2);
  });
});
