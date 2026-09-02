import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vitest";

const scriptFile = (family: "bash" | "zsh" | "sh" | "fish"): string =>
  fileURLToPath(
    new URL(`../../shared/shell-integration/nextshell-shell-integration.${family}`, import.meta.url)
  );

const sanitizeOscCommand = (value: string): string => value.replace(/[\x00-\x1f\x7f]/g, "");
const runShell = (command: string, args: string[], environment: Record<string, string> = {}) =>
  spawnSync(command, args, { encoding: "utf8", env: { ...process.env, ...environment } });
const fishAvailable = runShell("fish", ["--version"]).status === 0;

describe("shell integration command text", () => {
  test("bash, zsh and dash parse their dedicated scripts", () => {
    const checks = [
      ["bash", ["--noprofile", "--norc", "-n", scriptFile("bash")]],
      ["zsh", ["-n", scriptFile("zsh")]],
      ["dash", ["-n", scriptFile("sh")]]
    ] as const;
    for (const [command, args] of checks) {
      const result = runShell(command, [...args]);
      expect(result.status, `${command}: ${result.stderr}`).toBe(0);
    }
  });

  test("bash preserves hooks, stays idempotent and sanitizes the command mark", () => {
    const commandText = `printf 'x;y' && echo "$(should-not-run)";\n\u001b]evil\u0007 echo café`;
    const result = runShell(
      "bash",
      [
        "--noprofile",
        "--norc",
        "-c",
        'PROMPT_COMMAND="user_prompt_hook"; PS0="user-ps0"; . "$1"; . "$1"; printf "STATE:%s\\n" "$PROMPT_COMMAND"; printf "PS0:%s\\n" "$PS0"; __nextshell_preexec "$NEXTSHELL_TEST_COMMAND"',
        "--",
        scriptFile("bash")
      ],
      { NEXTSHELL_TEST_COMMAND: commandText }
    );
    expect(result.status, result.stderr).toBe(0);
    expect(result.stdout).toContain("user_prompt_hook");
    expect(result.stdout).toContain(
      "PS0:user-ps0$(command -v __nextshell_preexec >/dev/null 2>&1 && __nextshell_preexec)"
    );
    // The status-capture entry appears once → the hooks were spliced once.
    expect(result.stdout.match(/__nextshell_status=\$\?/g)).toHaveLength(1);
    expect(result.stdout.endsWith(`\u001B]133;C;${sanitizeOscCommand(commandText)}\u0007`)).toBe(
      true
    );
  });

  test("zsh preserves hook arrays, stays idempotent and sanitizes preexec", () => {
    const commandText = `printf 'x;y' && echo "$(should-not-run)";\n\u001b]evil\u0007 echo café`;
    const result = runShell(
      "zsh",
      [
        "-c",
        'precmd_functions=(user_precmd); preexec_functions=(user_preexec); . "$1"; . "$1"; print -r -- "PRECMD:${(j:,:)precmd_functions}"; print -r -- "PREEXEC:${(j:,:)preexec_functions}"; __nextshell_preexec "$NEXTSHELL_TEST_COMMAND"',
        "--",
        scriptFile("zsh")
      ],
      { NEXTSHELL_TEST_COMMAND: commandText }
    );
    expect(result.status, result.stderr).toBe(0);
    expect(result.stdout).toContain("PRECMD:__nextshell_precmd,user_precmd,__nextshell_prompt_end");
    expect(result.stdout).toContain("PREEXEC:user_preexec,__nextshell_preexec");
    expect(result.stdout.endsWith(`\u001B]133;C;${sanitizeOscCommand(commandText)}\u0007`)).toBe(
      true
    );
  });

  test("a child bash inheriting the exported hook strings stays silent", () => {
    // `ssh-agent bash` (or any nested shell) inherits *exported* PROMPT_COMMAND
    // and PS0 strings from the environment, but shell functions never cross
    // that boundary. Before the embedded guards, every prompt in such a child
    // printed `__nextshell_prompt_start: command not found`. Losing the OSC
    // marks there is acceptable; noise and a clobbered `$?` are not.
    const integrated = runShell("bash", [
      "--noprofile",
      "--norc",
      "-c",
      'PROMPT_COMMAND="user_hook"; . "$1"; printf "%s\\036%s" "$PROMPT_COMMAND" "$PS0"',
      "--",
      scriptFile("bash")
    ]);
    expect(integrated.status, integrated.stderr).toBe(0);
    const [promptCommand = "", ps0 = ""] = integrated.stdout.split("\u001e");

    const orphan = runShell(
      "bash",
      [
        "--noprofile",
        "--norc",
        "-c",
        // One simulated prompt cycle after a failing command: the user's own
        // hook (and the shell afterwards) must still observe exit code 7.
        [
          'user_hook() { printf "user_hook=%s;" "$?"; }',
          '(exit 7); eval "$PROMPT_COMMAND"; printf "after=%s;" "$?"',
          'printf "ps0=[%s]" "$(eval "printf %s \\"${PS0}\\"")"'
        ].join("\n")
      ],
      { PROMPT_COMMAND: promptCommand, PS0: ps0 }
    );
    expect(orphan.status, orphan.stderr).toBe(0);
    expect(orphan.stderr).toBe("");
    expect(orphan.stdout).toBe("user_hook=7;after=7;ps0=[]");
  });

  test("dash sources its fallback twice without inventing command marks", () => {
    const result = runShell("dash", [
      "-c",
      'PS1="user> "; . "$1"; . "$1"; printf "%s" "$PS1"',
      "--",
      scriptFile("sh")
    ]);
    expect(result.status, result.stderr).toBe(0);
    expect(result.stdout).toBe(
      "user> $(command -v __nextshell_emit_cwd >/dev/null 2>&1 && __nextshell_emit_cwd)"
    );
    expect(result.stdout).not.toContain("133;");
  });

  test.skipIf(!fishAvailable)("fish stays idempotent and sanitizes preexec", () => {
    const commandText = `printf 'x;y' && echo "$(should-not-run)";\n\u001b]evil\u0007 echo café`;
    const result = runShell(
      "fish",
      [
        "-c",
        'source "$argv[1]"; source "$argv[1]"; __nextshell_preexec "$NEXTSHELL_TEST_COMMAND"',
        scriptFile("fish")
      ],
      { NEXTSHELL_TEST_COMMAND: commandText }
    );
    expect(result.status, result.stderr).toBe(0);
    expect(result.stdout).toBe(`\u001B]133;C;${sanitizeOscCommand(commandText)}\u0007`);
  });
});
