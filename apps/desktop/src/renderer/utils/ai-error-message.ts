const pickRawMessage = (error: unknown): string => {
  if (typeof error === "string") return error;
  if (error instanceof Error) return error.message;
  if (error && typeof error === "object") {
    const data = error as Record<string, unknown>;
    if (typeof data.error === "string") return data.error;
    if (typeof data.message === "string") return data.message;
    if (typeof data.reason === "string") return data.reason;
  }
  return "";
};

const normalize = (value: string): string => {
  return value.replace(/^Error:\s*/i, "").replace(/\s+/g, " ").trim();
};

const AI_ERROR_PATTERNS: Array<{ label: string; summary: string; pattern: RegExp }> = [
  { label: "配置错误", summary: "配置错误", pattern: /base url|model.*不能为空|must.*http|provider .*不能为空|\/chat\/completions|\/v1\/messages|:generatecontent/i },
  { label: "鉴权失败", summary: "鉴权失败", pattern: /\b401\b|\b403\b|unauthorized|forbidden|invalid api key|incorrect api key|api key|authentication/i },
  { label: "触发限流", summary: "触发限流", pattern: /\b429\b|rate limit|too many requests|quota/i },
  { label: "请求超时", summary: "请求超时", pattern: /timed out|timeout|请求超时/i },
  { label: "服务异常", summary: "服务异常", pattern: /\b5\d\d\b|bad gateway|service unavailable|upstream|overloaded/i },
];

const classify = (message: string): { label: string; summary: string } | undefined => {
  return AI_ERROR_PATTERNS.find((item) => item.pattern.test(message));
};

export const formatAiErrorMessage = (error: unknown, fallback = "AI 请求失败"): string => {
  const raw = normalize(pickRawMessage(error));
  if (!raw) return fallback;

  const matched = classify(raw);
  if (!matched) return raw;
  return raw.startsWith(`${matched.label}：`) ? raw : `${matched.label}：${raw}`;
};

export const summarizeAiError = (error: unknown, fallback = "AI 请求失败"): string => {
  const raw = normalize(pickRawMessage(error));
  if (!raw) return fallback;
  return classify(raw)?.summary ?? fallback;
};
