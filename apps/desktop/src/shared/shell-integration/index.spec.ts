import { describe, expect, test } from "vitest";
import {
  SHELL_INTEGRATION_FAMILIES,
  SHELL_INTEGRATION_PROBE_COMMAND,
  buildInstallCommand,
  buildInstallScript,
  buildManualInstallInstructions,
  buildSourceLine,
  posixSingleQuote,
  resolveShellFamily,
  shellIntegrationScriptName,
  shellIntegrationScriptPath,
  shellIntegrationScriptTempPath,
  shellIntegrationScriptText,
  wrapForPosixShell
} from "./index";

describe("resolveShellFamily", () => {
  test("matches supported shells by basename", () => {
    expect(resolveShellFamily("/bin/bash")).toBe("bash");
    expect(resolveShellFamily("/usr/bin/zsh")).toBe("zsh");
    expect(resolveShellFamily("/bin/sh")).toBe("sh");
    expect(resolveShellFamily("/usr/local/bin/fish")).toBe("fish");
    expect(resolveShellFamily("bash")).toBe("bash");
    expect(resolveShellFamily("  /opt/homebrew/bin/FISH  ")).toBe("fish");
  });

  test("returns undefined for unsupported or empty shells", () => {
    expect(resolveShellFamily("/bin/dash")).toBeUndefined();
    expect(resolveShellFamily("/usr/bin/tcsh")).toBeUndefined();
    expect(resolveShellFamily("")).toBeUndefined();
    expect(resolveShellFamily(undefined)).toBeUndefined();
    expect(resolveShellFamily(null)).toBeUndefined();
  });
});

describe("script assets", () => {
  test("every family maps to its own script file", () => {
    for (const family of SHELL_INTEGRATION_FAMILIES) {
      expect(shellIntegrationScriptName(family)).toBe(`nextshell-shell-integration.${family}`);
      expect(shellIntegrationScriptPath(family)).toBe(
        `$HOME/.cache/nextshell/nextshell-shell-integration.${family}`
      );
      expect(shellIntegrationScriptText(family).length).toBeGreaterThan(0);
    }
  });

  test("the four scripts are distinct — no shared syntax subset exists", () => {
    const texts = SHELL_INTEGRATION_FAMILIES.map((family) => shellIntegrationScriptText(family));
    expect(new Set(texts).size).toBe(texts.length);
  });

  test("bash, zsh and fish emit command marks, while POSIX sh explicitly degrades to OSC 7", () => {
    for (const family of ["bash", "zsh", "fish"] as const) {
      const text = shellIntegrationScriptText(family);
      expect(text).toContain("133;A");
      expect(text).toContain("133;C");
      expect(text).toContain("133;D");
      expect(text).toContain("]7;file://");
    }

    for (const family of ["bash", "zsh"] as const) {
      expect(shellIntegrationScriptText(family)).toContain("133;B");
    }

    const posix = shellIntegrationScriptText("sh");
    expect(posix).toContain("]7;file://");
    expect(posix).not.toContain("133;");
  });

  test("command-bearing shells keep raw text but sanitize OSC control bytes", () => {
    const bash = shellIntegrationScriptText("bash");
    const zsh = shellIntegrationScriptText("zsh");
    const fish = shellIntegrationScriptText("fish");

    expect(bash).toContain("__nextshell_sanitize_command");
    expect(zsh).toContain("__nextshell_sanitize_command");
    expect(fish).toContain("string replace --all --regex '[[:cntrl:]]'");
    for (const text of [bash, zsh, fish]) {
      expect(text).toContain("133;C;");
    }
  });

  test("POSIX sh script stays free of bash/zsh-only syntax", () => {
    // dash parses the whole file before running any of it, so a single array
    // literal or bash-only test anywhere would take the entire integration
    // down — including inside branches that would never execute.
    const code = shellIntegrationScriptText("sh")
      .split("\n")
      .filter((line) => !line.trimStart().startsWith("#"))
      .join("\n");

    expect(code).not.toMatch(/\+=\(/);
    expect(code).not.toMatch(/\[\[/);
    expect(code).not.toMatch(/\blocal\b/);
    expect(code).not.toMatch(/\bfunction\b/);
  });
});

describe("posixSingleQuote", () => {
  test("wraps plain values", () => {
    expect(posixSingleQuote("echo hi")).toBe("'echo hi'");
  });

  test("escapes embedded single quotes by close-escape-reopen", () => {
    expect(posixSingleQuote("printf 'x'")).toBe(`'printf '\\''x'\\'''`);
  });

  test("leaves expansion characters inert", () => {
    const quoted = posixSingleQuote('cat "$HOME/f" `id` $(id)');
    expect(quoted).toBe(`'cat "$HOME/f" \`id\` $(id)'`);
  });
});

describe("wrapForPosixShell", () => {
  test("hands the script to /bin/sh so the login shell dialect never matters", () => {
    expect(wrapForPosixShell("printf hi")).toBe("/bin/sh -c 'printf hi'");
  });

  test("probe command reads $SHELL through /bin/sh", () => {
    // fish supports neither `${VAR:-}` nor heredocs, so an unwrapped probe
    // silently fails on every fish remote.
    expect(SHELL_INTEGRATION_PROBE_COMMAND).toBe(`/bin/sh -c 'printf %s "\${SHELL:-}"'`);
  });
});

describe("buildSourceLine", () => {
  test("uses `.` everywhere except fish", () => {
    // dash has no `source` builtin; fish has no `.` builtin.
    expect(buildSourceLine("bash")).toBe(
      '. "$HOME/.cache/nextshell/nextshell-shell-integration.bash"'
    );
    expect(buildSourceLine("zsh")).toBe(
      '. "$HOME/.cache/nextshell/nextshell-shell-integration.zsh"'
    );
    expect(buildSourceLine("sh")).toBe('. "$HOME/.cache/nextshell/nextshell-shell-integration.sh"');
    expect(buildSourceLine("fish")).toBe(
      'source "$HOME/.cache/nextshell/nextshell-shell-integration.fish"'
    );
  });
});

describe("buildInstallScript", () => {
  test("creates the cache dir and writes each script through a quoted heredoc", () => {
    const script = buildInstallScript(["zsh"], () => "# script\necho hi\n");

    expect(script).toContain('mkdir -p "$HOME/.cache/nextshell"');
    expect(script).toContain(
      "cat > \"$HOME/.cache/nextshell/nextshell-shell-integration.zsh.$$.tmp\" <<'__NEXTSHELL_INTEGRATION_EOF__'"
    );
    expect(script).toContain("# script\necho hi\n__NEXTSHELL_INTEGRATION_EOF__");
    expect(script.endsWith("__NEXTSHELL_INTEGRATION_EOF__")).toBe(true);
  });

  test("never truncates the live script — it stages a temp file and renames it", () => {
    // Two tabs on one server install milliseconds apart. `cat > final` would
    // hand the other tab's sourcing shell an empty/half file; a same-directory
    // rename is atomic, and `$$` keeps the two writers off each other's temp.
    const script = buildInstallScript(["bash"], () => "# script\n");
    const final = "$HOME/.cache/nextshell/nextshell-shell-integration.bash";
    const temp = `${final}.$$.tmp`;

    expect(shellIntegrationScriptTempPath("bash")).toBe(temp);
    expect(script).not.toContain(`cat > "${final}"`);
    expect(script).toContain(`cat > "${temp}"`);
    expect(script).toContain(`mv -f "${temp}" "${final}"`);
    // The publish must be conditional on the write, and a failed write must
    // clean up after itself instead of leaving a stale temp file behind.
    expect(script).toContain(`<<'__NEXTSHELL_INTEGRATION_EOF__' && mv -f`);
    expect(script).toContain(`|| { rm -f "${temp}"; exit 1; }`);
  });

  test("appends the missing trailing newline so the delimiter sits alone", () => {
    const script = buildInstallScript(["fish"], () => "# fish script");
    expect(script).toContain("# fish script\n__NEXTSHELL_INTEGRATION_EOF__");
  });

  test("writes one block per requested family", () => {
    const script = buildInstallScript(["bash", "fish"], (family) => `# ${family}\n`);
    expect(script).toContain("nextshell-shell-integration.bash");
    expect(script).toContain("nextshell-shell-integration.fish");
    expect(script.match(/__NEXTSHELL_INTEGRATION_EOF__/g)).toHaveLength(4);
  });
});

describe("buildInstallCommand", () => {
  test("is a single /bin/sh invocation so fish remotes can run it", () => {
    const command = buildInstallCommand(["bash"], () => "# script\n");
    expect(command.startsWith("/bin/sh -c '")).toBe(true);
    expect(command.endsWith("'")).toBe(true);
  });

  test("escapes single quotes coming from the script body", () => {
    const command = buildInstallCommand(["bash"], () => "printf 'x'\n");
    expect(command).toContain(`'\\''x'\\''`);
  });
});

describe("buildManualInstallInstructions", () => {
  test("installs every family and shows the matching activation line", () => {
    const instructions = buildManualInstallInstructions();

    expect(instructions).toContain("/bin/sh -c '");
    for (const family of SHELL_INTEGRATION_FAMILIES) {
      expect(instructions).toContain(`nextshell-shell-integration.${family}`);
    }
    expect(instructions).toContain("~/.bashrc");
    expect(instructions).toContain("~/.zshrc");
    expect(instructions).toContain("~/.profile");
    expect(instructions).toContain("~/.config/fish/config.fish");
  });
});
