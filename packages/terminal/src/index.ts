export interface TerminalPasteOptions {
  bracketedPaste: boolean;
  normalizeCrLf: boolean;
}

const defaultOptions: TerminalPasteOptions = {
  bracketedPaste: true,
  normalizeCrLf: true
};

export const normalizeTerminalInput = (
  raw: string,
  options: Partial<TerminalPasteOptions> = {}
): string => {
  const merged = { ...defaultOptions, ...options };
  const normalized = merged.normalizeCrLf ? raw.replace(/\r?\n/g, "\r") : raw;

  if (!merged.bracketedPaste) {
    return normalized;
  }

  return `\u001b[200~${normalized}\u001b[201~`;
};

interface ParsedShellCommand {
  segments: string[][];
}

const basenameOf = (value: string): string => value.slice(value.lastIndexOf("/") + 1);

/**
 * Tokenizes just enough shell to recognize dangerous commands through quoting,
 * path and wrapper (`sudo`/`env`/`busybox`/`sh -c`) disguises. This is
 * deliberately not a shell parser and proves nothing about safety: it only
 * ever answers "does this match the preset dangerous list".
 */
const parseShellCommand = (command: string): ParsedShellCommand => {
  const segments: string[][] = [];
  let tokens: string[] = [];
  let token = "";
  let tokenStarted = false;
  let quote: "'" | '"' | null = null;
  let escaped = false;

  const finishToken = (): void => {
    if (!tokenStarted) return;
    tokens.push(token);
    token = "";
    tokenStarted = false;
  };
  const finishSegment = (): void => {
    finishToken();
    if (tokens.length > 0) segments.push(tokens);
    tokens = [];
  };

  for (let index = 0; index < command.length; index += 1) {
    const char = command[index];
    if (char === undefined) continue;

    if (escaped) {
      token += char;
      tokenStarted = true;
      escaped = false;
      continue;
    }
    if (char === "\\" && quote !== "'") {
      escaped = true;
      tokenStarted = true;
      continue;
    }
    if (quote) {
      if (char === quote) {
        quote = null;
      } else {
        token += char;
        tokenStarted = true;
      }
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      tokenStarted = true;
      continue;
    }
    if (/\s/u.test(char)) {
      finishToken();
      continue;
    }
    if (char === ">" || char === "<") {
      finishToken();
      continue;
    }
    if (char === ";" || char === "|") {
      finishSegment();
      if (command[index + 1] === char) index += 1;
      continue;
    }
    if (char === "&") {
      if (command[index + 1] === "&") {
        finishSegment();
        index += 1;
      }
      continue;
    }
    token += char;
    tokenStarted = true;
  }

  finishSegment();
  return { segments };
};

const commandTokens = (segment: string[]): { executable?: string; args: string[] } => {
  let index = 0;

  if (basenameOf(segment[index] ?? "") === "sudo") {
    index += 1;
    while (index < segment.length) {
      const option = segment[index] ?? "";
      if (option === "--") {
        index += 1;
        break;
      }
      if (!option.startsWith("-")) break;
      index += 1;
      if (["-C", "-D", "-g", "-h", "-p", "-R", "-r", "-t", "-u", "-U"].includes(option)) {
        index += 1;
      }
    }
  }

  const executable = segment[index];
  return {
    executable: executable ? basenameOf(executable).toLowerCase() : undefined,
    args: segment.slice(index + 1)
  };
};

const hasRecursiveAndForce = (args: string[]): boolean => {
  let recursive = false;
  let force = false;
  for (const arg of args) {
    if (arg === "--recursive") recursive = true;
    if (arg === "--force") force = true;
    if (/^-[^-]*r/iu.test(arg) || /^-[^-]*R/u.test(arg)) recursive = true;
    if (/^-[^-]*f/iu.test(arg)) force = true;
  }
  return recursive && force;
};

const isRootTarget = (arg: string): boolean => /^\/+(?:(?:\.{1,2})\/?|\*)?$/u.test(arg);

const unwrapCommand = (
  executable: string,
  args: string[]
): { executable: string; args: string[] } => {
  let currentExecutable = executable;
  let currentArgs = args;

  for (let depth = 0; depth < 4; depth += 1) {
    if (["command", "nohup"].includes(currentExecutable)) {
      const nextIndex = currentArgs.findIndex((arg) => !arg.startsWith("-"));
      if (nextIndex < 0) break;
      currentExecutable = basenameOf(currentArgs[nextIndex] ?? "").toLowerCase();
      currentArgs = currentArgs.slice(nextIndex + 1);
      continue;
    }
    if (currentExecutable === "env") {
      let nextIndex = 0;
      while (nextIndex < currentArgs.length) {
        const arg = currentArgs[nextIndex] ?? "";
        if (arg === "-u" || arg === "--unset") {
          nextIndex += 2;
          continue;
        }
        if (arg.startsWith("-") || /^[A-Za-z_][A-Za-z0-9_]*=/u.test(arg)) {
          nextIndex += 1;
          continue;
        }
        break;
      }
      const next = currentArgs[nextIndex];
      if (!next) break;
      currentExecutable = basenameOf(next).toLowerCase();
      currentArgs = currentArgs.slice(nextIndex + 1);
      continue;
    }
    if (currentExecutable === "busybox") {
      const next = currentArgs[0];
      if (!next) break;
      currentExecutable = basenameOf(next).toLowerCase();
      currentArgs = currentArgs.slice(1);
      continue;
    }
    break;
  }

  return { executable: currentExecutable, args: currentArgs };
};

const dangerousReason = (
  command: string,
  parsed: ParsedShellCommand,
  recursionDepth = 0
): string | null => {
  const dequoted = command.replace(/["']/gu, "").replace(/\\(?=\S)/gu, "");

  if (/(:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:)/u.test(dequoted)) {
    return "Fork bomb pattern is forbidden";
  }
  const deviceRedirect = dequoted.match(/(?:^|[^>])>{1,2}\s*(\/dev\/[^\s;&|]+)/iu)?.[1];
  if (deviceRedirect && !/^\/dev\/(?:null|stdout|stderr|fd(?:\/|$))/iu.test(deviceRedirect)) {
    return "Output redirection to a device file is dangerous";
  }

  for (const segment of parsed.segments) {
    const resolved = commandTokens(segment);
    if (!resolved.executable) continue;
    const unwrapped = unwrapCommand(resolved.executable, resolved.args);
    const executable = unwrapped.executable;
    const args = unwrapped.args;

    if (recursionDepth < 2 && ["bash", "dash", "fish", "ksh", "sh", "zsh"].includes(executable)) {
      const commandIndex = args.findIndex((arg) => arg === "-c" || arg === "--command");
      const nested = commandIndex >= 0 ? args[commandIndex + 1] : undefined;
      if (nested) {
        const nestedReason = dangerousReason(nested, parseShellCommand(nested), recursionDepth + 1);
        if (nestedReason) return nestedReason;
      }
    }

    if (executable === "rm" && hasRecursiveAndForce(args) && args.some(isRootTarget)) {
      return "Recursive forced removal of the filesystem root is dangerous";
    }
    if (executable === "mkfs" || executable.startsWith("mkfs.")) {
      return "Filesystem formatting commands are dangerous";
    }
    if (executable === "dd" && args.some((arg) => /^of=\/dev\//iu.test(arg))) {
      return "Writing dd output to a device is dangerous";
    }
    if (executable === "shutdown" || executable === "reboot") {
      return "Host shutdown and reboot commands are dangerous";
    }
    if (
      executable === "chmod" &&
      args.some((arg) => arg === "-R" || arg === "--recursive") &&
      args.includes("777") &&
      args.some(isRootTarget)
    ) {
      return "Recursively making the filesystem root world-writable is dangerous";
    }
    if (
      executable === "kill" &&
      args.some((arg) => arg === "-9" || arg.toUpperCase() === "-KILL") &&
      args.includes("-1")
    ) {
      return "Sending SIGKILL to every permitted process is dangerous";
    }
  }
  return null;
};

/**
 * The preset dangerous-command blacklist for the Agent gateway. Returns the
 * human-readable reason when the command matches, `null` otherwise. There is
 * no safe/unsafe classification here by design: anything not on the list is
 * simply allowed through.
 */
export const matchDangerousCommand = (command: string): string | null =>
  dangerousReason(command, parseShellCommand(command));
