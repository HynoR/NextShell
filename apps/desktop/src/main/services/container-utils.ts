import os from "node:os";
import path from "node:path";
import type { RemoteFileEntry, TerminalEncoding } from "../../../../../packages/core/src/index";
import * as iconv from "iconv-lite";
import { logger } from "../logger";

// ─── Constants ─────────────────────────────────────────────────────────────

export const MONITOR_UPTIME_COMMAND = "cat /proc/uptime 2>/dev/null | awk '{print $1}'";
export const MONITOR_SYSTEM_INFO_OS_RELEASE_COMMAND = "cat /etc/os-release 2>/dev/null";
export const MONITOR_SYSTEM_INFO_HOSTNAME_COMMAND = "hostname 2>/dev/null";
export const MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND = "uname -s 2>/dev/null";
export const MONITOR_SYSTEM_INFO_KERNEL_VERSION_COMMAND = "uname -r 2>/dev/null";
export const MONITOR_SYSTEM_INFO_ARCH_COMMAND = "uname -m 2>/dev/null";
export const MONITOR_SYSTEM_INFO_CPUINFO_COMMAND = "cat /proc/cpuinfo 2>/dev/null";
export const MONITOR_SYSTEM_INFO_MEMINFO_COMMAND = "cat /proc/meminfo 2>/dev/null";
export const MONITOR_SYSTEM_INFO_NET_DEV_COMMAND = "cat /proc/net/dev 2>/dev/null";
export const MONITOR_SYSTEM_INFO_FILESYSTEMS_COMMAND = "export LANG=C LC_ALL=C; (df -kP || df -k || df) 2>/dev/null";
export const MONITOR_NETWORK_INTERVAL_MS = 5000;
export const MONITOR_PROCESS_INTERVAL_MS = 5000;
export const ADHOC_IDLE_TIMEOUT_MS = 30_000;
export const MONITOR_MAX_CONSECUTIVE_FAILURES = 3;
export const MONITOR_COMMAND_TIMEOUT_MS = 10000;
export const SFTP_WARMUP_TIMEOUT_MS = 5000;

// ─── Pure helper functions ─────────────────────────────────────────────────

export const mapEntryType = (permissions: string): RemoteFileEntry["type"] => {
  if (permissions.startsWith("d")) {
    return "directory";
  }

  if (permissions.startsWith("l")) {
    return "link";
  }

  return "file";
};

export const parseLongname = (longname: string): { permissions: string; owner: string; group: string } => {
  const parts = longname.trim().split(/\s+/);

  return {
    permissions: parts[0] ?? "----------",
    owner: parts[2] ?? "unknown",
    group: parts[3] ?? "unknown"
  };
};

export const joinRemotePath = (parent: string, name: string): string => {
  const base = parent === "/" ? "" : parent.replace(/\/$/, "");
  return `${base}/${name}` || "/";
};

export const normalizeError = (error: unknown): string => {
  if (error instanceof Error) {
    return error.message;
  }

  return "Unknown SSH error";
};

export const toAuthRequiredReason = (message: string): string | undefined => {
  const lower = message.toLowerCase();

  if (lower.includes("proxy authentication failed")) {
    return undefined;
  }

  if (lower.includes("ssh username is required")) {
    return "缺少用户名，请输入用户名和认证信息后重试。";
  }

  if (lower.includes("password credential is missing")) {
    return "缺少密码，请输入密码后重试。";
  }

  if (lower.includes("password auth requires password")) {
    return "缺少密码，请输入密码后重试。";
  }

  if (lower.includes("interactive auth requires password")) {
    return "缺少密码，请输入密码后重试。";
  }

  if (lower.includes("private key auth requires")) {
    return "缺少私钥信息，请输入私钥路径或私钥内容后重试。";
  }

  if (
    lower.includes("cannot parse privatekey") ||
    lower.includes("bad decrypt") ||
    lower.includes("passphrase")
  ) {
    return "私钥或口令无效，请重新输入后重试。";
  }

  if (lower.includes("ssh agent auth requires ssh_auth_sock")) {
    return "SSH Agent 不可用，请改用密码或私钥认证。";
  }

  if (
    lower.includes("all configured authentication methods failed") ||
    lower.includes("permission denied") ||
    lower.includes("authentication failed") ||
    lower.includes("unable to authenticate") ||
    lower.includes("userauth failure") ||
    lower.includes("no supported authentication methods available")
  ) {
    return "认证失败，请检查用户名和凭据后重试。";
  }

  return undefined;
};

export const resolveIconvEncoding = (encoding: TerminalEncoding): string => {
  if (encoding === "gb18030") {
    return "gb18030";
  }

  if (encoding === "gbk") {
    return "gbk";
  }

  if (encoding === "big5") {
    return "big5";
  }

  return "utf8";
};

export const decodeTerminalData = (chunk: Buffer | string, encoding: TerminalEncoding): string => {
  if (typeof chunk === "string") {
    return chunk;
  }

  const codec = resolveIconvEncoding(encoding);
  try {
    return iconv.decode(chunk, codec);
  } catch (error) {
    logger.debug("[TerminalEncoding] decode failed, fallback to utf-8", error);
    return chunk.toString("utf8");
  }
};

export const encodeTerminalData = (data: string, encoding: TerminalEncoding): Buffer => {
  const codec = resolveIconvEncoding(encoding);
  try {
    return iconv.encode(data, codec);
  } catch (error) {
    logger.debug("[TerminalEncoding] encode failed, fallback to utf-8", error);
    return Buffer.from(data, "utf8");
  }
};

export const parseFloatSafe = (value: string | undefined): number => {
  if (!value) {
    return 0;
  }

  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) ? parsed : 0;
};

export const parseIntSafe = (value: string | undefined): number => {
  if (!value) {
    return 0;
  }

  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : 0;
};

type ParsedPrereleaseIdentifier = number | string;

interface ParsedComparableVersion {
  core: number[];
  prerelease: ParsedPrereleaseIdentifier[] | null;
}

export const parseExternalUrl = (rawPath: string): URL | undefined => {
  const trimmed = rawPath.trim();
  if (!trimmed) {
    return undefined;
  }

  try {
    const url = new URL(trimmed);
    const protocol = url.protocol.toLowerCase();
    if (protocol === "http:" || protocol === "https:") {
      return url;
    }
  } catch {
    // ignore invalid URL payloads and continue with local path logic
  }

  return undefined;
};

export const normalizeGithubRepo = (rawRepo: string): string | undefined => {
  const trimmed = rawRepo.trim();
  if (!trimmed) {
    return undefined;
  }

  const withoutPrefix = trimmed
    .replace(/^https?:\/\/github\.com\//i, "")
    .replace(/\.git$/i, "")
    .replace(/^\/+/, "")
    .replace(/\/+$/, "");

  if (/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(withoutPrefix)) {
    return withoutPrefix;
  }
  return undefined;
};

export const parseComparableVersion = (rawVersion: string): ParsedComparableVersion | null => {
  const normalized = rawVersion.trim().replace(/^v/i, "");
  const match = normalized.match(
    /^(\d+(?:\.\d+)*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z.-]+)?$/
  );
  if (!match?.[1]) {
    return null;
  }

  const core = match[1].split(".").map((segment) => Number.parseInt(segment, 10));
  if (core.length === 0 || core.some((segment) => !Number.isSafeInteger(segment) || segment < 0)) {
    return null;
  }

  const prereleasePart = match[2];
  if (!prereleasePart) {
    return { core, prerelease: null };
  }

  const prerelease = prereleasePart.split(".").map((identifier): ParsedPrereleaseIdentifier => {
    if (/^\d+$/.test(identifier)) {
      return Number.parseInt(identifier, 10);
    }
    return identifier.toLowerCase();
  });

  return { core, prerelease };
};

export const compareCoreSegments = (a: number[], b: number[]): number => {
  const maxLength = Math.max(a.length, b.length);
  for (let index = 0; index < maxLength; index += 1) {
    const left = a[index] ?? 0;
    const right = b[index] ?? 0;
    if (left !== right) {
      return left - right;
    }
  }
  return 0;
};

export const comparePrerelease = (
  a: ParsedPrereleaseIdentifier[] | null,
  b: ParsedPrereleaseIdentifier[] | null
): number => {
  if (!a && !b) {
    return 0;
  }
  if (!a) {
    return 1;
  }
  if (!b) {
    return -1;
  }

  const maxLength = Math.max(a.length, b.length);
  for (let index = 0; index < maxLength; index += 1) {
    const left = a[index];
    const right = b[index];

    if (left === undefined) {
      return -1;
    }
    if (right === undefined) {
      return 1;
    }
    if (left === right) {
      continue;
    }

    const leftIsNumber = typeof left === "number";
    const rightIsNumber = typeof right === "number";

    if (leftIsNumber && rightIsNumber) {
      return left - right;
    }
    if (leftIsNumber) {
      return -1;
    }
    if (rightIsNumber) {
      return 1;
    }

    const textCompare = left.localeCompare(right, "en", { sensitivity: "base" });
    if (textCompare !== 0) {
      return textCompare;
    }
  }

  return 0;
};

export const resolveLocalPath = (rawPath: string): string => {
  const trimmed = rawPath.trim();
  if (!trimmed) {
    return trimmed;
  }

  if (trimmed === "~") {
    return os.homedir();
  }

  if (trimmed.startsWith("~/")) {
    return path.join(os.homedir(), trimmed.slice(2));
  }

  return path.resolve(trimmed);
};

export const parseUptimeSeconds = (raw: string): number => {
  const firstLine = raw.split(/\r?\n/).find((line) => line.trim().length > 0)?.trim() ?? "";
  if (!firstLine) {
    return 0;
  }

  const direct = parseFloatSafe(firstLine.split(/\s+/)[0]);
  if (direct > 0) {
    return direct;
  }

  const lower = firstLine.toLowerCase();
  let seconds = 0;

  const dayMatch = lower.match(/(\d+)\s+day/);
  if (dayMatch?.[1]) {
    seconds += parseIntSafe(dayMatch[1]) * 24 * 3600;
  }

  const hourMinuteMatch = lower.match(/(\d+):(\d+)/);
  if (hourMinuteMatch?.[1] && hourMinuteMatch[2]) {
    seconds += parseIntSafe(hourMinuteMatch[1]) * 3600 + parseIntSafe(hourMinuteMatch[2]) * 60;
  } else {
    const hourMatch = lower.match(/(\d+)\s+hr/);
    if (hourMatch?.[1]) {
      seconds += parseIntSafe(hourMatch[1]) * 3600;
    }
    const minuteMatch = lower.match(/(\d+)\s+min/);
    if (minuteMatch?.[1]) {
      seconds += parseIntSafe(minuteMatch[1]) * 60;
    }
  }

  return seconds;
};

/** Parse a compound probe output back into named sections. */
export const parseCompoundOutput = (stdout: string): Map<string, string> => {
  const sections = new Map<string, string>();
  const lines = stdout.split("\n");
  let currentSection = "";
  let currentContent: string[] = [];

  for (const line of lines) {
    const match = line.match(/^---NS_(\w+)---\r?$/);
    if (match?.[1]) {
      if (currentSection) {
        sections.set(currentSection, currentContent.join("\n"));
      }
      currentSection = match[1];
      currentContent = [];
    } else {
      currentContent.push(line);
    }
  }
  if (currentSection) {
    sections.set(currentSection, currentContent.join("\n"));
  }

  return sections;
};

/**
 * Build a single compound shell command to collect all system info
 * (hostname, kernel, CPU, memory, filesystems, etc.) in one exec.
 */
export const buildSystemInfoCommand = (): string => {
  return [
    "echo '---NS_UPTIME---'",
    MONITOR_UPTIME_COMMAND,
    "echo '---NS_OSRELEASE---'",
    MONITOR_SYSTEM_INFO_OS_RELEASE_COMMAND,
    "echo '---NS_HOSTNAME---'",
    MONITOR_SYSTEM_INFO_HOSTNAME_COMMAND,
    "echo '---NS_KERNELNAME---'",
    MONITOR_SYSTEM_INFO_KERNEL_NAME_COMMAND,
    "echo '---NS_KERNELVER---'",
    MONITOR_SYSTEM_INFO_KERNEL_VERSION_COMMAND,
    "echo '---NS_ARCH---'",
    MONITOR_SYSTEM_INFO_ARCH_COMMAND,
    "echo '---NS_CPUINFO---'",
    MONITOR_SYSTEM_INFO_CPUINFO_COMMAND,
    "echo '---NS_MEMINFO---'",
    MONITOR_SYSTEM_INFO_MEMINFO_COMMAND,
    "echo '---NS_NETDEV---'",
    MONITOR_SYSTEM_INFO_NET_DEV_COMMAND,
    "echo '---NS_FILESYSTEMS---'",
    MONITOR_SYSTEM_INFO_FILESYSTEMS_COMMAND,
    "echo '---NS_SYSINFO_END---'"
  ].join("; ");
};
