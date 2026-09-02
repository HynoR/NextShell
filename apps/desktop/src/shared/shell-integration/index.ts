import integrationBash from "./nextshell-shell-integration.bash?raw";
import integrationZsh from "./nextshell-shell-integration.zsh?raw";
import integrationSh from "./nextshell-shell-integration.sh?raw";
import integrationFish from "./nextshell-shell-integration.fish?raw";

// Shared between the main process (auto-injection) and the renderer (the
// "manual" mode copy-install command) so the remote paths, the heredoc
// delimiter and the source line can never drift apart.

export type ShellIntegrationFamily = "bash" | "zsh" | "sh" | "fish";

export const SHELL_INTEGRATION_FAMILIES: readonly ShellIntegrationFamily[] = [
  "bash",
  "zsh",
  "sh",
  "fish"
];

export const SHELL_INTEGRATION_REMOTE_DIR = "$HOME/.cache/nextshell";

const HEREDOC_DELIMITER = "__NEXTSHELL_INTEGRATION_EOF__";

const SCRIPT_TEXT_BY_FAMILY: Record<ShellIntegrationFamily, string> = {
  bash: integrationBash,
  zsh: integrationZsh,
  sh: integrationSh,
  fish: integrationFish
};

export const resolveShellFamily = (
  shellPath?: string | null
): ShellIntegrationFamily | undefined => {
  const normalized = shellPath?.trim().toLowerCase();
  if (!normalized) {
    return undefined;
  }

  const basename = normalized.slice(normalized.lastIndexOf("/") + 1);
  return SHELL_INTEGRATION_FAMILIES.find((family) => family === basename);
};

/** One script per family: bash/zsh/sh share no parseable syntax subset. */
export const shellIntegrationScriptName = (family: ShellIntegrationFamily): string =>
  `nextshell-shell-integration.${family}`;

export const shellIntegrationScriptPath = (family: ShellIntegrationFamily): string =>
  `${SHELL_INTEGRATION_REMOTE_DIR}/${shellIntegrationScriptName(family)}`;

export const shellIntegrationScriptText = (family: ShellIntegrationFamily): string =>
  SCRIPT_TEXT_BY_FAMILY[family];

/** POSIX single-quoting: close, escape, reopen — valid in sh, bash, zsh and fish. */
export const posixSingleQuote = (value: string): string => `'${value.replaceAll("'", "'\\''")}'`;

/**
 * sshd runs an exec request through the *user's login shell*, and the same
 * applies to anything the user pastes into their own prompt. fish supports
 * neither heredocs nor `${VAR:-}`, so every remote command we generate is
 * handed to `/bin/sh` instead of being written in the caller's dialect.
 */
export const wrapForPosixShell = (script: string): string =>
  `/bin/sh -c ${posixSingleQuote(script)}`;

/** Reads the remote login shell; POSIX-wrapped so it also works under fish. */
export const SHELL_INTEGRATION_PROBE_COMMAND = wrapForPosixShell('printf %s "${SHELL:-}"');

/**
 * The line appended to the user's rc in "manual" mode. fish has no `.` builtin
 * and dash has no `source` builtin, so the two cannot share one spelling.
 */
export const buildSourceLine = (family: ShellIntegrationFamily): string => {
  const quotedPath = `"${shellIntegrationScriptPath(family)}"`;
  return family === "fish" ? `source ${quotedPath}` : `. ${quotedPath}`;
};

// ─── Auto-mode startup bootstrap ─────────────────────────────────────────────
//
// "auto" mode never types anything into the user's shell: the integration is
// activated at shell *startup* (VS Code's mechanism) by launching the login
// shell through an exec+PTY request with a bootstrap file — bash via
// `--init-file`, zsh via a ZDOTDIR of shim rc files, fish via `-C`. Nothing
// lands in history, nothing is echoed, and there is no timing race with
// full-screen programs started from the user's rc. Plain `sh` has no reliable
// startup hook we control (ENV is owned by the user's profile), so it gets no
// auto integration at all — failing quietly beats interfering.

export const SHELL_INTEGRATION_ZDOTDIR = `${SHELL_INTEGRATION_REMOTE_DIR}/zdotdir`;

export const SHELL_INTEGRATION_BASH_INIT_PATH = `${SHELL_INTEGRATION_REMOTE_DIR}/nextshell-bash-init.bash`;

/**
 * bash is started with `--init-file`, which makes it an interactive non-login
 * shell: emulate a login bash's startup files first (exactly the files a real
 * `bash -l` would read — .bash_profile does NOT imply .bashrc; profiles that
 * want it source it themselves), then activate the integration.
 */
export const BASH_INIT_FILE_TEXT = [
  "# NextShell shell-integration bootstrap (bash --init-file). Generated file.",
  "[ -f /etc/profile ] && . /etc/profile",
  'if [ -f "$HOME/.bash_profile" ]; then . "$HOME/.bash_profile"',
  'elif [ -f "$HOME/.bash_login" ]; then . "$HOME/.bash_login"',
  'elif [ -f "$HOME/.profile" ]; then . "$HOME/.profile"',
  "fi",
  `. "${shellIntegrationScriptPath("bash")}"`,
  ""
].join("\n");

export const ZSH_SHIM_FILE_NAMES = [".zshenv", ".zprofile", ".zshrc", ".zlogin"] as const;

export const shellIntegrationZshShimPath = (
  fileName: (typeof ZSH_SHIM_FILE_NAMES)[number]
): string => `${SHELL_INTEGRATION_ZDOTDIR}/${fileName}`;

/**
 * ZDOTDIR shim: zsh reads OUR $fileName, which temporarily restores the user's
 * real ZDOTDIR, sources their counterpart file (so their config sees the
 * ZDOTDIR it expects and may even move it), then flips back so zsh's next
 * startup stage still lands in the shim directory. Only .zshrc additionally
 * activates the integration. NEXTSHELL_USER_ZDOTDIR is exported by the launch
 * command below.
 */
export const buildZshShimText = (fileName: (typeof ZSH_SHIM_FILE_NAMES)[number]): string => {
  const lines = [
    `# NextShell shell-integration ZDOTDIR shim (${fileName}). Generated file.`,
    '_nextshell_user_zdotdir="${NEXTSHELL_USER_ZDOTDIR:-$HOME}"',
    `if [ -f "$_nextshell_user_zdotdir/${fileName}" ]; then`,
    '  _nextshell_shim_zdotdir="$ZDOTDIR"',
    '  ZDOTDIR="$_nextshell_user_zdotdir"',
    `  . "$ZDOTDIR/${fileName}"`,
    "  # The user file may itself relocate ZDOTDIR; keep following it.",
    '  export NEXTSHELL_USER_ZDOTDIR="$ZDOTDIR"',
    '  ZDOTDIR="$_nextshell_shim_zdotdir"',
    "  unset _nextshell_shim_zdotdir",
    "fi",
    "unset _nextshell_user_zdotdir"
  ];
  if (fileName === ".zshrc") {
    lines.push(`. "${shellIntegrationScriptPath("zsh")}"`);
  }
  lines.push("");
  return lines.join("\n");
};

/** Every remote file the auto-mode bootstrap for `family` needs, path → body. */
export const integrationBootstrapFiles = (
  family: ShellIntegrationFamily,
  readScript: (family: ShellIntegrationFamily) => string = shellIntegrationScriptText
): ReadonlyArray<{ path: string; body: string }> => {
  const script = { path: shellIntegrationScriptPath(family), body: readScript(family) };
  switch (family) {
    case "bash":
      return [script, { path: SHELL_INTEGRATION_BASH_INIT_PATH, body: BASH_INIT_FILE_TEXT }];
    case "zsh":
      return [
        script,
        ...ZSH_SHIM_FILE_NAMES.map((name) => ({
          path: shellIntegrationZshShimPath(name),
          body: buildZshShimText(name)
        }))
      ];
    case "fish":
      return [script];
    case "sh":
      return [];
  }
};

/**
 * Remote command that replaces the plain `shell()` request in auto mode: run
 * through an exec+PTY channel, it starts the user's login shell with the
 * integration hooked into its startup files. Every branch double-checks the
 * bootstrap file actually exists and otherwise execs a plain login shell — a
 * missing cache dir must degrade to "no integration", never to a broken
 * terminal. Returns undefined for families without a safe startup hook.
 */
export const buildIntegrationLaunchCommand = (
  family: ShellIntegrationFamily
): string | undefined => {
  const fallback = 'exec "${SHELL:-/bin/sh}" -l';
  switch (family) {
    case "bash":
      return wrapForPosixShell(
        [
          `[ -f "${SHELL_INTEGRATION_BASH_INIT_PATH}" ] || ${fallback}`,
          `exec "\${SHELL:-bash}" --init-file "${SHELL_INTEGRATION_BASH_INIT_PATH}"`
        ].join("\n")
      );
    case "zsh":
      return wrapForPosixShell(
        [
          `[ -f "${shellIntegrationZshShimPath(".zshrc")}" ] || ${fallback}`,
          'export NEXTSHELL_USER_ZDOTDIR="${ZDOTDIR:-$HOME}"',
          `export ZDOTDIR="${SHELL_INTEGRATION_ZDOTDIR}"`,
          'exec "${SHELL:-zsh}" -l'
        ].join("\n")
      );
    case "fish":
      // fish expands $HOME inside double quotes itself; -C runs after the
      // user's config, which is exactly where the integration belongs.
      return wrapForPosixShell(
        [
          `[ -f "${shellIntegrationScriptPath("fish")}" ] || ${fallback}`,
          `exec "\${SHELL:-fish}" -l -C 'source "${shellIntegrationScriptPath("fish")}"'`
        ].join("\n")
      );
    case "sh":
      return undefined;
  }
};

/**
 * Same directory as the final script (so the rename stays on one filesystem and
 * is therefore atomic) plus the writing shell's PID, which is unique among all
 * processes alive at that moment — i.e. among exactly the concurrent installers
 * we are racing against.
 */
export const shellIntegrationScriptTempPath = (family: ShellIntegrationFamily): string =>
  `${shellIntegrationScriptPath(family)}.$$.tmp`;

/**
 * POSIX script that creates the cache dir and writes the requested scripts.
 *
 * The write is staged through a per-process temp file and published with `mv`:
 * opening the final path with `>` truncates it in place, so a second tab
 * installing while a first tab's shell is mid-`source` would hand that shell an
 * empty or half-written file (parse errors, or OSC 133/7 silently never
 * arriving). `mv` within one directory is a rename(2) — a sourcing shell sees
 * either the whole old file or the whole new one.
 */
export const buildInstallScript = (
  families: readonly ShellIntegrationFamily[],
  readScript: (family: ShellIntegrationFamily) => string = shellIntegrationScriptText
): string =>
  buildFileInstallScript(
    families.map((family) => ({
      path: shellIntegrationScriptPath(family),
      body: readScript(family)
    }))
  );

/** Atomic-write install script for an arbitrary list of remote files. */
export const buildFileInstallScript = (
  files: ReadonlyArray<{ path: string; body: string }>
): string => {
  const dirs = Array.from(new Set(files.map(({ path }) => path.slice(0, path.lastIndexOf("/")))));
  const blocks = files.map(({ path, body: text }) => {
    // The heredoc delimiter must sit alone on its line, so guarantee a
    // trailing newline after the script body.
    const body = text.endsWith("\n") ? text : `${text}\n`;
    const tempPath = `${path}.$$.tmp`;
    // The heredoc body starts after the *whole* command line, so the `&&`/`||`
    // tail is allowed to sit next to the `<<` redirection. A failed write must
    // never be published, and must not leave the temp file behind either.
    const header =
      `cat > "${tempPath}" <<'${HEREDOC_DELIMITER}' &&` +
      ` mv -f "${tempPath}" "${path}"` +
      ` || { rm -f "${tempPath}"; exit 1; }`;
    return `${header}\n${body}${HEREDOC_DELIMITER}`;
  });

  return [...dirs.map((dir) => `mkdir -p "${dir}"`), ...blocks].join("\n");
};

/** Ready-to-exec remote command that installs the given integration scripts. */
export const buildInstallCommand = (
  families: readonly ShellIntegrationFamily[],
  readScript?: (family: ShellIntegrationFamily) => string
): string => wrapForPosixShell(buildInstallScript(families, readScript));

/**
 * Ready-to-exec remote command that installs everything `family`'s startup
 * bootstrap needs (integration script + init/shim files).
 */
export const buildBootstrapInstallCommand = (
  family: ShellIntegrationFamily,
  readScript?: (family: ShellIntegrationFamily) => string
): string =>
  wrapForPosixShell(buildFileInstallScript(integrationBootstrapFiles(family, readScript)));

const MANUAL_RC_FILE_BY_FAMILY: Record<ShellIntegrationFamily, string> = {
  bash: "~/.bashrc",
  zsh: "~/.zshrc",
  sh: "~/.profile",
  fish: "~/.config/fish/config.fish"
};

/**
 * "Manual" mode payload: one command that installs every family's script,
 * followed by the per-shell activation line. Everything runs through
 * `/bin/sh -c`, so pasting it into bash, zsh or fish all behave the same.
 */
export const buildManualInstallInstructions = (): string =>
  [
    "# NextShell 终端集成：把下面整段粘贴到远端 shell 执行",
    buildInstallCommand(SHELL_INTEGRATION_FAMILIES),
    "",
    "# 然后把与你的 shell 对应的一行追加到登录配置文件中：",
    ...SHELL_INTEGRATION_FAMILIES.map(
      (family) =>
        `#   ${family.padEnd(4)} → echo '${buildSourceLine(family)}' >> ${MANUAL_RC_FILE_BY_FAMILY[family]}`
    )
  ].join("\n");
