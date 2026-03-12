export interface RemoteOsc7BootstrapPlan {
  enabled: boolean;
  env: Record<string, string>;
  shellBootstrap?: string;
  startMarker?: string;
  endMarker?: string;
}

// OSC 133 sequences — terminals don't render these, reliable for sentinel detection
export const OSC7_BS_START = "\x1b]133;NS_BS_S\x07";
export const OSC7_BS_END = "\x1b]133;NS_BS_E\x07";

export type Osc7ShellFamily = "bash" | "zsh";

const OSC7_BASH_BOOTSTRAP = [
  "__nextshell_osc7_emit() { printf '\\033]7;file://%s%s\\007' \"${NEXTSHELL_OSC7_HOST:-localhost}\" \"$PWD\"; };",
  "case \";${PROMPT_COMMAND:-};\" in",
  "*\";__nextshell_osc7_emit;\"*) ;;",
  "*) PROMPT_COMMAND=\"__nextshell_osc7_emit${PROMPT_COMMAND:+;$PROMPT_COMMAND}\" ;;",
  "esac;",
  "__nextshell_osc7_emit"
].join(" ");

const OSC7_ZSH_BOOTSTRAP = [
  "emulate -L zsh;",
  "autoload -Uz add-zsh-hook >/dev/null 2>&1 || true;",
  "if ! typeset -f __nextshell_osc7_emit >/dev/null 2>&1; then",
  "__nextshell_osc7_emit() { printf '\\033]7;file://%s%s\\007' \"${NEXTSHELL_OSC7_HOST:-localhost}\" \"$PWD\"; };",
  "fi;",
  "if [ -z \"${NEXTSHELL_OSC7_INSTALLED-}\" ]; then",
  "typeset -g NEXTSHELL_OSC7_INSTALLED=1;",
  "add-zsh-hook precmd __nextshell_osc7_emit >/dev/null 2>&1 || true;",
  "add-zsh-hook chpwd __nextshell_osc7_emit >/dev/null 2>&1 || true;",
  "fi;",
  "__nextshell_osc7_emit"
].join(" ");

const wrapBootstrapWithSentinels = (bootstrap: string): string => {
  const startEsc = OSC7_BS_START.replace(/\x1b/g, "\\033").replace(/\x07/g, "\\007");
  const endEsc = OSC7_BS_END.replace(/\x1b/g, "\\033").replace(/\x07/g, "\\007");
  return `printf '${startEsc}'; ${bootstrap}; printf '${endEsc}\\r\\033[K'`;
};

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
  shellFamily?: Osc7ShellFamily
): RemoteOsc7BootstrapPlan => {
  if (!enabled) {
    return {
      enabled: false,
      env: {}
    };
  }

  const rawBootstrap =
    shellFamily === "bash"
      ? OSC7_BASH_BOOTSTRAP
      : shellFamily === "zsh"
        ? OSC7_ZSH_BOOTSTRAP
        : undefined;

  return {
    enabled: true,
    env: {
      NEXTSHELL_OSC7_HOST: osc7Host
    },
    shellBootstrap: rawBootstrap ? wrapBootstrapWithSentinels(rawBootstrap) : undefined,
    startMarker: rawBootstrap ? OSC7_BS_START : undefined,
    endMarker: rawBootstrap ? OSC7_BS_END : undefined
  };
};
