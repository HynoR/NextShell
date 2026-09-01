import fs from "node:fs";
import path from "node:path";

/**
 * What the agent wants to do with a path on *this* machine.
 *
 * `read` is the exfiltration direction (`transfer_upload` streams a local file
 * to a remote host). `write` is the tampering direction (`transfer_download`
 * drops remote bytes onto local disk) and is strictly the more dangerous of the
 * two: overwriting `~/.zshrc` or a LaunchAgent is arbitrary code execution on
 * the user's machine, which no amount of remote-side authorization covers.
 */
export type LocalPathIntent = "read" | "write";

export interface LocalPathPolicyContext {
  homeDir: string;
  /** NextShell's own `userData` directory: the SQLite database and backups. */
  appDataDir: string;
  /** When non-empty, the only roots the agent may touch at all. */
  allowedRoots: string[];
  /** Test seam; production passes `fs.realpathSync`. */
  realpath?: (target: string) => string;
}

export interface LocalPathDecision {
  allowed: boolean;
  /** Absolute, `~`-expanded, `..`-collapsed and symlink-resolved. */
  resolved: string;
  /** User-facing sentence shown in the confirmation dialog. */
  reason: string;
}

/**
 * Directories that are off limits in both directions, expressed relative to the
 * home directory. Everything here either *is* a credential store or is one
 * `cat` away from being one.
 */
const DENIED_HOME_DIRECTORIES = [
  ".ssh",
  ".aws",
  ".azure",
  ".gnupg",
  ".kube",
  ".docker",
  ".password-store",
  ".local/share/keyrings",
  ".config/gcloud",
  ".config/gh",
  ".config/google-chrome",
  ".config/chromium",
  ".mozilla",
  "Library/Keychains",
  "Library/Cookies",
  "Library/Application Support/Google/Chrome",
  "Library/Application Support/Chromium",
  "Library/Application Support/BraveSoftware",
  "Library/Application Support/Firefox",
  "Library/Application Support/Microsoft Edge",
  "AppData/Local/Google/Chrome",
  "AppData/Roaming/Mozilla"
] as const;

/** Absolute directories that are off limits regardless of the home directory. */
const DENIED_ABSOLUTE_DIRECTORIES = ["/etc/ssh", "/private/etc/ssh", "/var/db/shadow"] as const;

/**
 * Directories the agent must never *write* into. Reading them is allowed —
 * shipping a copy of `/usr/bin/…` somewhere is not interesting — but writing is
 * how a download turns into persistence or privilege escalation.
 */
const WRITE_DENIED_ABSOLUTE_DIRECTORIES = [
  "/bin",
  "/sbin",
  "/usr",
  "/etc",
  "/private/etc",
  "/Library/LaunchAgents",
  "/Library/LaunchDaemons",
  "/Library/StartupItems",
  "/Applications",
  "/System",
  "/boot",
  "/lib",
  "/lib64"
] as const;

const WRITE_DENIED_HOME_DIRECTORIES = [
  "Library/LaunchAgents",
  "Library/Application Support/com.apple.backgroundtaskmanagementagent",
  ".config/autostart",
  ".config/systemd",
  ".local/share/systemd",
  "bin",
  ".local/bin",
  "AppData/Roaming/Microsoft/Windows/Start Menu/Programs/Startup"
] as const;

const DENIED_BASENAME_PATTERNS: readonly RegExp[] = [
  /^\.env(\..+)?$/i,
  /^id_[a-z0-9]+$/i,
  /^\.?(netrc|npmrc|pypirc|pgpass|git-credentials|htpasswd|dockercfg)$/i,
  /^credentials$/i,
  /^(secrets?|service[-_]?account)\.(json|ya?ml)$/i,
  /\.(pem|key|p12|pfx|jks|keystore|kdbx|ppk|asc|gpg)$/i
];

/**
 * Files whose contents are executed on the next login or shell start, so
 * overwriting one is remote code execution on the user's own machine.
 */
const WRITE_DENIED_BASENAME_PATTERNS: readonly RegExp[] = [
  /^\.(bashrc|bash_profile|bash_login|bash_logout|profile|zshrc|zprofile|zshenv|zlogin|kshrc|inputrc)$/i,
  /^config\.fish$/i,
  /^(crontab|authorized_keys|authorized_keys2|sudoers)$/i,
  /\.(desktop|plist)$/i
];

/**
 * macOS firmlinks `/Users`, `/home`, `/private` and friends onto the data
 * volume, so `realpath("/Users/x/.ssh")` comes back as
 * `/System/Volumes/Data/Users/x/.ssh`. That prefix is a mount detail, not a
 * location: left in place it both hides every `~/…` rule (the path no longer
 * starts with the home directory) and falsely trips the `/System` write rule
 * for every ordinary file in the user's home.
 */
const DATA_VOLUME_PREFIX = "/System/Volumes/Data";

const stripDataVolume = (target: string): string => {
  if (target === DATA_VOLUME_PREFIX) {
    return "/";
  }
  return target.startsWith(`${DATA_VOLUME_PREFIX}/`)
    ? target.slice(DATA_VOLUME_PREFIX.length)
    : target;
};

const normalizeSeparators = (target: string): string => target.split(path.sep).join("/");

/** True when `candidate` is `root` itself or lives underneath it. */
const isWithin = (candidate: string, root: string): boolean => {
  if (root.length === 0) {
    return false;
  }
  const normalizedRoot = normalizeSeparators(root).replace(/\/+$/, "");
  const normalizedCandidate = normalizeSeparators(candidate);
  if (normalizedRoot === "") {
    return true;
  }
  return (
    normalizedCandidate === normalizedRoot || normalizedCandidate.startsWith(`${normalizedRoot}/`)
  );
};

export const expandLocalPath = (input: string, homeDir: string): string => {
  const trimmed = input.trim();
  if (trimmed === "~") {
    return path.resolve(homeDir);
  }
  if (trimmed.startsWith("~/") || trimmed.startsWith("~\\")) {
    return path.resolve(homeDir, trimmed.slice(2));
  }
  return path.resolve(trimmed);
};

/**
 * True when `input` is absolute once `~` is accounted for. A relative path would
 * be resolved against the app's own working directory, which the agent cannot
 * see and the user would never expect — the same reason remote paths must be
 * absolute.
 */
const isAbsoluteInput = (input: string): boolean =>
  input.startsWith("~") || path.isAbsolute(input);

/**
 * Resolves symlinks as far as the path actually exists, then re-appends the
 * missing tail. Without this, `ln -s ~/.ssh /tmp/keys` would defeat every rule
 * below — and for a download the target file legitimately does not exist yet,
 * so plain `realpathSync` is not an option either.
 */
const resolveThroughSymlinks = (
  target: string,
  realpath: (candidate: string) => string
): string => {
  const segments: string[] = [];
  let current = target;

  for (let depth = 0; depth < 64; depth += 1) {
    try {
      const real = realpath(current);
      return segments.length > 0 ? path.join(real, ...segments) : real;
    } catch {
      const parent = path.dirname(current);
      if (parent === current) {
        return target;
      }
      segments.unshift(path.basename(current));
      current = parent;
    }
  }
  return target;
};

/**
 * The single gate every agent-supplied local path passes through. Denials win
 * over `allowedRoots`: a user who allows `~` must not thereby hand over
 * `~/.ssh`.
 */
export const evaluateLocalPath = (
  input: string,
  intent: LocalPathIntent,
  context: LocalPathPolicyContext
): LocalPathDecision => {
  const raw = input.trim();
  if (raw.length === 0) {
    return { allowed: false, resolved: "", reason: "本地路径不能为空" };
  }
  if (raw.includes("\0")) {
    return { allowed: false, resolved: "", reason: "本地路径不能包含 NUL 字节" };
  }
  if (!isAbsoluteInput(raw)) {
    return {
      allowed: false,
      resolved: "",
      reason: "本地路径必须是绝对路径（或以 ~ 开头）；相对路径会落到应用自己的工作目录"
    };
  }

  const realpath = context.realpath ?? fs.realpathSync;
  const resolveReal = (target: string): string =>
    stripDataVolume(resolveThroughSymlinks(target, realpath));
  const expanded = expandLocalPath(raw, context.homeDir);
  const resolved = resolveReal(expanded);

  /**
   * Both the literal and the symlink-resolved form are checked against both
   * forms of every rule base. Checking only the resolved path lets an aliased
   * base slip past; checking only the literal path lets a symlink walk straight
   * into a denied directory. Neither form alone is sufficient.
   */
  const forms = expanded === resolved ? [resolved] : [expanded, resolved];
  const within = (base: string): boolean => {
    const absolute = path.resolve(base);
    const real = resolveReal(absolute);
    const bases = absolute === real ? [absolute] : [absolute, real];
    return forms.some((form) => bases.some((candidate) => isWithin(form, candidate)));
  };
  const basenames = [...new Set(forms.map((form) => path.basename(form)))];
  const matchesName = (patterns: readonly RegExp[]): string | null => {
    for (const name of basenames) {
      if (patterns.some((pattern) => pattern.test(name))) return name;
    }
    return null;
  };

  const home = context.homeDir ? path.resolve(context.homeDir) : "";
  const deny = (reason: string): LocalPathDecision => ({ allowed: false, resolved, reason });

  // NextShell's own data directory holds the credential database and its
  // backups; handing it to an agent would undo the whole "credentials never
  // leave the app" property in one call.
  if (context.appDataDir && within(context.appDataDir)) {
    return deny("NextShell 自身的数据目录（含凭据数据库与备份）不对 Agent 开放");
  }

  for (const directory of DENIED_ABSOLUTE_DIRECTORIES) {
    if (within(directory)) {
      return deny(`系统敏感目录 ${directory} 不对 Agent 开放`);
    }
  }

  if (home) {
    for (const relative of DENIED_HOME_DIRECTORIES) {
      if (within(path.join(home, relative))) {
        return deny(`敏感目录 ~/${relative} 不对 Agent 开放`);
      }
    }
  }

  const deniedName = matchesName(DENIED_BASENAME_PATTERNS);
  if (deniedName) {
    return deny(`文件名 ${deniedName} 命中凭据文件模式，不对 Agent 开放`);
  }

  if (intent === "write") {
    for (const directory of WRITE_DENIED_ABSOLUTE_DIRECTORIES) {
      if (within(directory)) {
        return deny(`Agent 不能向系统目录 ${directory} 写入`);
      }
    }
    if (home) {
      for (const relative of WRITE_DENIED_HOME_DIRECTORIES) {
        if (within(path.join(home, relative))) {
          return deny(`Agent 不能向自启动 / 可执行目录 ~/${relative} 写入`);
        }
      }
    }
    const executedName = matchesName(WRITE_DENIED_BASENAME_PATTERNS);
    if (executedName) {
      return deny(`Agent 不能写入会被自动执行的文件 ${executedName}`);
    }
  }

  const roots = context.allowedRoots
    .map((root) => root.trim())
    .filter((root) => root.length > 0);
  // Every form must land inside an allowed root: a symlink that points out of
  // the sandbox is exactly what this check exists to stop.
  if (roots.length > 0) {
    const rootForms = roots.flatMap((root) => {
      const absolute = expandLocalPath(root, context.homeDir);
      const real = resolveReal(absolute);
      return absolute === real ? [absolute] : [absolute, real];
    });
    const contained = forms.every((form) => rootForms.some((root) => isWithin(form, root)));
    if (!contained) {
      return deny(
        `本地路径不在允许的根目录内（当前允许：${roots.join("、")}）；请在设置 → Agent 接入里调整`
      );
    }
  }

  return {
    allowed: true,
    resolved,
    reason: roots.length > 0 ? "位于允许的本地根目录内" : "未命中本地路径拒绝清单"
  };
};
