export interface RemoteOsc7BootstrapPlan {
  enabled: boolean;
  launchCommand?: string;
}

export type Osc7ShellFamily = "bash" | "zsh";

const quotePosix = (value: string): string => `'${value.replace(/'/g, `'\"'\"'`)}'`;

const BASH_INIT_FILE = [
  "[ -f ~/.bashrc ] && . ~/.bashrc",
  "__nextshell_osc7_emit() {",
  "  printf '\\033]7;file://%s%s\\007' \"${NEXTSHELL_OSC7_HOST}\" \"$PWD\"",
  "}",
  "case \";${PROMPT_COMMAND:-};\" in",
  "  *\";__nextshell_osc7_emit;\"*) ;;",
  "  *) PROMPT_COMMAND=\"__nextshell_osc7_emit${PROMPT_COMMAND:+;$PROMPT_COMMAND}\" ;;",
  "esac",
  "__nextshell_osc7_cleanup() {",
  "  [ -n \"${NEXTSHELL_OSC7_RCFILE:-}\" ] && rm -f -- \"$NEXTSHELL_OSC7_RCFILE\"",
  "}",
  "trap __nextshell_osc7_cleanup EXIT",
  "__nextshell_osc7_emit"
].join("\n");

const ZSH_ENV_FILE = ["[ -f ~/.zshenv ] && . ~/.zshenv"].join("\n");

const ZSH_RC_FILE = [
  "[ -f ~/.zshrc ] && . ~/.zshrc",
  "autoload -Uz add-zsh-hook >/dev/null 2>&1 || true",
  "if ! typeset -f __nextshell_osc7_emit >/dev/null 2>&1; then",
  "  __nextshell_osc7_emit() {",
  "    printf '\\033]7;file://%s%s\\007' \"${NEXTSHELL_OSC7_HOST}\" \"$PWD\"",
  "  }",
  "fi",
  "if [[ -z \"${NEXTSHELL_OSC7_INSTALLED:-}\" ]]; then",
  "  typeset -g NEXTSHELL_OSC7_INSTALLED=1",
  "  add-zsh-hook precmd __nextshell_osc7_emit >/dev/null 2>&1 || true",
  "  add-zsh-hook chpwd __nextshell_osc7_emit >/dev/null 2>&1 || true",
  "  __nextshell_osc7_cleanup() {",
  "    [[ -n \"${NEXTSHELL_OSC7_ZDOTDIR:-}\" ]] && rm -rf -- \"$NEXTSHELL_OSC7_ZDOTDIR\"",
  "  }",
  "  add-zsh-hook zshexit __nextshell_osc7_cleanup >/dev/null 2>&1 || true",
  "fi",
  "__nextshell_osc7_emit"
].join("\n");

const buildBashLaunchCommand = (shellPath: string, osc7Host: string): string =>
  [
    "__ns_osc7_rc=$(mktemp \"${TMPDIR:-/tmp}/nextshell-osc7-bash.XXXXXX\") || exit 1",
    "cat > \"$__ns_osc7_rc\" <<'__NEXTSHELL_OSC7_BASH__'",
    BASH_INIT_FILE,
    "__NEXTSHELL_OSC7_BASH__",
    `NEXTSHELL_OSC7_HOST=${quotePosix(osc7Host)} NEXTSHELL_OSC7_RCFILE="$__ns_osc7_rc" exec ${quotePosix(shellPath)} --init-file "$__ns_osc7_rc" -i`
  ].join("\n");

const buildZshLaunchCommand = (shellPath: string, osc7Host: string): string =>
  [
    "__ns_osc7_zdotdir=$(mktemp -d \"${TMPDIR:-/tmp}/nextshell-osc7-zsh.XXXXXX\") || exit 1",
    "cat > \"$__ns_osc7_zdotdir/.zshenv\" <<'__NEXTSHELL_OSC7_ZSHENV__'",
    ZSH_ENV_FILE,
    "__NEXTSHELL_OSC7_ZSHENV__",
    "cat > \"$__ns_osc7_zdotdir/.zshrc\" <<'__NEXTSHELL_OSC7_ZSHRC__'",
    ZSH_RC_FILE,
    "__NEXTSHELL_OSC7_ZSHRC__",
    `ZDOTDIR="$__ns_osc7_zdotdir" NEXTSHELL_OSC7_HOST=${quotePosix(osc7Host)} NEXTSHELL_OSC7_ZDOTDIR="$__ns_osc7_zdotdir" exec ${quotePosix(shellPath)} -i`
  ].join("\n");

export const resolveOsc7ShellFamily = (
  shellPath?: string | null
): Osc7ShellFamily | undefined => {
  const normalized = shellPath?.trim().toLowerCase();
  if (!normalized) {
    return undefined;
  }

  if (normalized.endsWith("/bash") || normalized === "bash") {
    return "bash";
  }

  if (normalized.endsWith("/zsh") || normalized === "zsh") {
    return "zsh";
  }

  return undefined;
};

export const createRemoteOsc7BootstrapPlan = (
  enabled: boolean,
  osc7Host = "localhost",
  shellFamily?: Osc7ShellFamily,
  shellPath?: string | null
): RemoteOsc7BootstrapPlan => {
  if (!enabled) {
    return {
      enabled: false
    };
  }

  const normalizedShellPath = shellPath?.trim();
  const launchShell = normalizedShellPath || shellFamily;
  const launchCommand =
    shellFamily === "bash"
      ? launchShell
        ? buildBashLaunchCommand(launchShell, osc7Host)
        : undefined
      : shellFamily === "zsh"
        ? launchShell
          ? buildZshLaunchCommand(launchShell, osc7Host)
          : undefined
        : undefined;

  return {
    enabled: Boolean(launchCommand),
    launchCommand
  };
};
