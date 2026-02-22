const ANSI_ESCAPE_REGEX = /\u001b\[[0-9;]*m/g;
const STACK_LINE_REGEX = /^\s*at\s+/;

const ERROR_CODE_MAP: Array<{ pattern: RegExp; label: string }> = [
  { pattern: /\bEACCES\b|\bEPERM\b|permission denied/i, label: "权限不足" },
  { pattern: /\bENOENT\b|no such file or directory/i, label: "目标不存在" },
  { pattern: /\bETIMEDOUT\b|timed out|timeout/i, label: "请求超时" },
  { pattern: /\bECONNREFUSED\b|connection refused/i, label: "连接被拒绝" },
  { pattern: /\bENETUNREACH\b|network is unreachable/i, label: "网络不可达" },
  { pattern: /\bEHOSTUNREACH\b|host is unreachable/i, label: "主机不可达" },
];

const pickRawMessage = (error: unknown): string => {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  if (error && typeof error === "object") {
    const data = error as Record<string, unknown>;
    if (typeof data.reason === "string") return data.reason;
    if (typeof data.error === "string") return data.error;
    if (typeof data.message === "string") return data.message;
  }
  return "";
};

const sanitizeMessage = (value: string): string => {
  if (!value) return "";
  const lines = value
    .replace(ANSI_ESCAPE_REGEX, "")
    .replace(/\r/g, "\n")
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0 && !STACK_LINE_REGEX.test(line));

  const merged = lines.join(" ").replace(/^Error:\s*/i, "").replace(/\s+/g, " ").trim();
  return merged;
};

export const formatErrorMessage = (
  error: unknown,
  fallback = "操作失败",
): string => {
  const sanitized = sanitizeMessage(pickRawMessage(error));
  if (!sanitized) return fallback;

  const mapped = ERROR_CODE_MAP.find((item) => item.pattern.test(sanitized));
  const normalized = mapped ? itemLabel(mapped.label, sanitized) : sanitized;

  return normalized.length > 180 ? `${normalized.slice(0, 180).trim()}...` : normalized;
};

const itemLabel = (label: string, message: string): string => {
  const normalized = message.trim();
  return normalized.toLowerCase() === label.toLowerCase() ? label : `${label}：${normalized}`;
};
