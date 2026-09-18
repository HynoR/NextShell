import { execFileSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, test } from "vitest";
import {
  SHELL_INTEGRATION_FAMILIES,
  buildBootstrapInstallCommand,
  buildInstallCommand,
  buildIntegrationLaunchCommand,
  buildSourceLine,
  shellIntegrationScriptText
} from "../../shared/shell-integration";

const canRunZsh = (() => {
  if (process.platform === "win32") return false;
  try {
    execFileSync("/usr/bin/env", ["zsh", "-c", "exit 0"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

// Escaping a shell script that is itself full of quotes into a single `sh -c`
// argument is the most fragile part of the injection path, and a mistake there
// silently corrupts the installed file rather than failing loudly. So run the
// generated command for real against a throwaway HOME and diff the result.
const canRunPosixShell = process.platform !== "win32" && existsSync("/bin/sh");
const normalizeLineEndings = (text: string): string => text.replace(/\r\n?/g, "\n");

describe.skipIf(!canRunPosixShell)("generated install command", () => {
  const home = mkdtempSync(path.join(tmpdir(), "nextshell-shell-integration-"));

  afterAll(() => {
    rmSync(home, { recursive: true, force: true });
  });

  const runInHome = (command: string): string =>
    execFileSync("/bin/sh", ["-c", command], {
      env: { ...process.env, HOME: home }
    }).toString();

  test("installs every script with normalized line endings", () => {
    runInHome(buildInstallCommand(SHELL_INTEGRATION_FAMILIES));

    for (const family of SHELL_INTEGRATION_FAMILIES) {
      const installed = readFileSync(
        path.join(home, ".cache/nextshell", `nextshell-shell-integration.${family}`),
        "utf-8"
      );
      expect(installed).toBe(normalizeLineEndings(shellIntegrationScriptText(family)));
      expect(installed).not.toContain("\r");
    }
  });

  test("is re-runnable — reinstalling overwrites instead of appending", () => {
    runInHome(buildInstallCommand(["sh"]));
    runInHome(buildInstallCommand(["sh"]));

    const installed = readFileSync(
      path.join(home, ".cache/nextshell", "nextshell-shell-integration.sh"),
      "utf-8"
    );
    expect(installed).toBe(normalizeLineEndings(shellIntegrationScriptText("sh")));
  });

  test("leaves no staging files behind in the cache dir", () => {
    runInHome(buildInstallCommand(SHELL_INTEGRATION_FAMILIES));

    const entries = readdirSync(path.join(home, ".cache/nextshell"));
    expect(entries.filter((entry) => entry.includes(".tmp"))).toEqual([]);
  });

  test("a script stays sourceable while another session reinstalls it", () => {
    // The bug: `cat > final` truncates in place, so a second tab installing
    // while this tab's shell is mid-`source` reads an empty or half-written
    // file — parse errors, or OSC 7/133 silently never arriving.
    runInHome(buildInstallCommand(["sh"]));

    const sourceLine = buildSourceLine("sh");
    // The writer keeps reinstalling until the reader is done, so the two are
    // guaranteed to overlap instead of racing to finish first.
    const output = runInHome(
      [
        'lock="$HOME/.nextshell-install-race"',
        ': > "$lock"',
        `w=0; while [ -f "$lock" ] && [ $w -lt 500 ]; do ${buildInstallCommand(["sh"])}; w=$((w+1)); done &`,
        "writer=$!",
        "bad=0; reads=0",
        "while [ $reads -lt 30 ]; do",
        `  out=$(/bin/sh -c '${sourceLine}; __nextshell_emit_cwd' 2>&1) || bad=$((bad+1))`,
        '  case "$out" in *"]7;file://"*) ;; *) bad=$((bad+1)) ;; esac',
        "  reads=$((reads+1))",
        "done",
        'rm -f "$lock"',
        "wait $writer",
        'printf "reads=%s bad=%s" "$reads" "$bad"'
      ].join("\n")
    );

    expect(output).toBe("reads=30 bad=0");
  });

  test("a write that cannot be published leaves the previous script intact", () => {
    const scriptPath = path.join(home, ".cache/nextshell", "nextshell-shell-integration.sh");
    runInHome(buildInstallCommand(["sh"]));

    chmodSync(path.dirname(scriptPath), 0o500);
    try {
      expect(() => runInHome(buildInstallCommand(["sh"]))).toThrow();
    } finally {
      chmodSync(path.dirname(scriptPath), 0o700);
    }

    expect(readFileSync(scriptPath, "utf-8")).toBe(
      normalizeLineEndings(shellIntegrationScriptText("sh"))
    );
    expect(readdirSync(path.dirname(scriptPath)).filter((e) => e.includes(".tmp"))).toEqual([]);
  });

  test("the POSIX source line loads the script and emits OSC 7", () => {
    runInHome(buildInstallCommand(["sh"]));

    const output = runInHome(`PS1='$ '; ${buildSourceLine("sh")}; __nextshell_emit_cwd`);

    expect(output).toContain("]7;file://");
  });

  test("sourcing twice is a no-op thanks to the sentinel", () => {
    runInHome(buildInstallCommand(["sh"]));

    const sourceLine = buildSourceLine("sh");
    const output = runInHome(
      `PS1='$ '; ${sourceLine}; ${sourceLine}; printf '%s' "$PS1" | tr -dc 'a-z_' `
    );

    // A second source must not append the cwd hook to PS1 again. One appended
    // hook mentions the function twice: the orphan-shell guard plus the call.
    expect(output.match(/__nextshell_emit_cwd/g)).toHaveLength(2);
  });
});

// bash re-parses PROMPT_COMMAND as a script on *every* prompt, so a splice that
// merely looks right is not enough — it has to still parse next to whatever the
// user already had there.
const canRunBash = process.platform !== "win32" && existsSync("/bin/bash");

describe.skipIf(!canRunBash)("bash PROMPT_COMMAND splicing", () => {
  const home = mkdtempSync(path.join(tmpdir(), "nextshell-bash-prompt-"));

  afterAll(() => {
    rmSync(home, { recursive: true, force: true });
  });

  beforeAll(() => {
    execFileSync("/bin/sh", ["-c", buildInstallCommand(["bash"])], {
      env: { ...process.env, HOME: home }
    });
  });

  // Env vars arrive in the child as ordinary shell variables, so this is the
  // closest stand-in for a user whose rc file already set PROMPT_COMMAND.
  const spliceInto = (existing: string): string =>
    execFileSync(
      "/bin/bash",
      ["--norc", "--noprofile", "-c", `${buildSourceLine("bash")}; printf %s "$PROMPT_COMMAND"`],
      {
        env: { ...process.env, HOME: home, PROMPT_COMMAND: existing }
      }
    ).toString();

  const expectParses = (command: string): void => {
    expect(() =>
      execFileSync("/bin/bash", ["-n"], { input: command, stdio: "pipe" })
    ).not.toThrow();
  };

  test("a PROMPT_COMMAND ending in a separator stays parseable", () => {
    // `PROMPT_COMMAND='history -a; '` is a widespread snippet. Joining with
    // `;` yielded `history -a; ;__nextshell_prompt_end` — an empty command
    // between two separators, i.e. a syntax error on every single prompt.
    const spliced = spliceInto("history -a; ");

    expect(spliced).toContain("history -a");
    expect(spliced).toContain("__nextshell_prompt_start");
    expect(spliced).toContain("__nextshell_prompt_end");
    expectParses(spliced);
  });

  test("empty and plain PROMPT_COMMAND values stay parseable", () => {
    for (const existing of ["", "history -a", "printf ''"]) {
      expectParses(spliceInto(existing));
    }
  });

  test("re-sourcing in a fresh shell does not stack the hooks twice", () => {
    const once = spliceInto("history -a; ");
    expect(spliceInto(once)).toBe(once);
  });
});

// The startup bootstrap replaces stdin injection entirely: nothing may ever be
// typed into the user's shell, so the only correctness surface left is "the
// right files land on the remote and the startup chain sources them". Run the
// generated install commands for real against a throwaway HOME.
describe.skipIf(!canRunPosixShell)("bootstrap install command", () => {
  let home!: string;

  beforeEach(() => {
    home = mkdtempSync(path.join(tmpdir(), "nextshell-bootstrap-"));
  });

  afterEach(() => {
    rmSync(home, { recursive: true, force: true });
  });

  const runInHome = (command: string): string =>
    execFileSync("/bin/sh", ["-c", command], {
      env: { ...process.env, HOME: home }
    }).toString();

  const cacheEntries = (): string[] => readdirSync(path.join(home, ".cache/nextshell"));

  test("bash installs the integration script plus the --init-file bootstrap", () => {
    runInHome(buildBootstrapInstallCommand("bash"));

    expect(cacheEntries().sort()).toEqual([
      "nextshell-bash-init.bash",
      "nextshell-shell-integration.bash"
    ]);
  });

  test("zsh installs the integration script plus all four ZDOTDIR shims", () => {
    runInHome(buildBootstrapInstallCommand("zsh"));

    expect(cacheEntries()).toEqual(["nextshell-shell-integration.zsh", "zdotdir"]);
    expect(readdirSync(path.join(home, ".cache/nextshell/zdotdir")).sort()).toEqual(
      [".zshenv", ".zprofile", ".zshrc", ".zlogin"].sort()
    );
  });

  test("fish installs only the integration script and leaves no staging files", () => {
    runInHome(buildBootstrapInstallCommand("fish"));

    expect(cacheEntries()).toEqual(["nextshell-shell-integration.fish"]);
    expect(cacheEntries().filter((entry) => entry.includes(".tmp"))).toEqual([]);
  });

  test.skipIf(!canRunBash)(
    "the bash init file emulates a login shell, then activates the integration",
    () => {
      // A marker script keeps the assertion about OUR plumbing, not about the
      // bundled integration script's internals.
      runInHome(buildBootstrapInstallCommand("bash", () => "NEXTSHELL_TEST_MARKER=integration\n"));
      writeFileSync(path.join(home, ".bash_profile"), "NEXTSHELL_TEST_PROFILE=login\n");

      const output = execFileSync(
        "/bin/bash",
        [
          "--norc",
          "--noprofile",
          "-c",
          '. "$HOME/.cache/nextshell/nextshell-bash-init.bash"; printf %s "$NEXTSHELL_TEST_PROFILE:$NEXTSHELL_TEST_MARKER"'
        ],
        { env: { HOME: home, PATH: process.env.PATH ?? "" } }
      ).toString();

      // The profile ran BEFORE the integration (login emulation first).
      expect(output).toBe("login:integration");
    }
  );

  test.skipIf(!canRunZsh)(
    "the ZDOTDIR shims source the user's zshrc, then activate the integration",
    () => {
      runInHome(buildBootstrapInstallCommand("zsh", () => "NEXTSHELL_TEST_MARKER=integration\n"));
      writeFileSync(path.join(home, ".zshrc"), "NEXTSHELL_TEST_RC=user\n");

      const output = execFileSync(
        "/usr/bin/env",
        ["zsh", "-i", "-c", 'printf %s "$NEXTSHELL_TEST_RC:$NEXTSHELL_TEST_MARKER"'],
        {
          env: {
            HOME: home,
            PATH: process.env.PATH ?? "",
            NEXTSHELL_USER_ZDOTDIR: home,
            ZDOTDIR: path.join(home, ".cache/nextshell/zdotdir")
          }
        }
      ).toString();

      expect(output).toBe("user:integration");
    }
  );

  test("launch commands guard against a missing bootstrap and fall back to a plain shell", () => {
    for (const family of ["bash", "zsh", "fish"] as const) {
      const launch = buildIntegrationLaunchCommand(family);
      expect(launch).toBeDefined();
      // Belt and braces: a wiped cache dir must yield a login shell, never a
      // broken terminal.
      expect(launch).toContain('|| exec "${SHELL:-/bin/sh}" -l');
    }
    expect(buildIntegrationLaunchCommand("sh")).toBeUndefined();
  });
});
