import { describe, expect, test, vi } from "vitest";
import type { ConnectionProfile } from "@nextshell/core";
import type { SshConnection } from "@nextshell/ssh";
import type { CachedConnectionRepository } from "@nextshell/storage";

import { CommandService } from "./command-service";

const profile = { id: "11111111-1111-1111-1111-111111111111" } as ConnectionProfile;

describe("CommandService agent execution options", () => {
  test("runs in the requested cwd, passes cancellation and returns the shell's actual cwd", async () => {
    const exec = vi.fn(async (_command: string, _options: { signal: AbortSignal }) => ({
      stdout: "ok\n",
      stderr: "before\u001eNEXTSHELL_CWD=/srv/real\u001fafter",
      exitCode: 0
    }));
    const service = new CommandService({
      connections: {} as CachedConnectionRepository,
      getConnectionOrThrow: () => profile,
      ensureConnection: async () => ({ exec } as unknown as SshConnection),
      listWorkspaces: () => [],
      markWorkspaceCommandsDirty: () => undefined
    });
    const controller = new AbortController();

    const result = await service.execCommand(profile.id, "pwd", {
      cwd: "/srv/user's app",
      signal: controller.signal
    });

    expect(exec).toHaveBeenCalledOnce();
    const [remoteCommand, options] = exec.mock.calls[0] ?? [];
    expect(remoteCommand).toContain(`cd '/srv/user'\\''s app' || exit $?;`);
    expect(remoteCommand).toContain("NEXTSHELL_CWD");
    expect(options).toEqual({ signal: controller.signal });
    expect(result.cwd).toBe("/srv/real");
    expect(result.stderr).toBe("beforeafter");
    expect(result.command).toBe("pwd");
  });
});
