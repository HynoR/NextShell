import { formatErrorMessage } from "../utils/errorMessage";

const AUTH_REQUIRED_PREFIX = "AUTH_REQUIRED::";

export interface NormalizedOpenError {
  reason: string;
  authRequired: boolean;
}

export const extractAuthRequiredReason = (reason: string): string | undefined => {
  const index = reason.indexOf(AUTH_REQUIRED_PREFIX);
  if (index < 0) {
    return undefined;
  }
  return reason.slice(index);
};

export const normalizeOpenError = (
  error: unknown,
  fallback = "打开 SSH 会话失败"
): NormalizedOpenError => {
  const rawReason = formatErrorMessage(error, fallback);
  const authReason = extractAuthRequiredReason(rawReason);
  return {
    reason: authReason ?? rawReason,
    authRequired: authReason !== undefined
  };
};

export const isSessionGenerationCurrent = (
  generationBySession: Map<string, number>,
  cancelledSessionIds: Set<string>,
  sessionId: string,
  generation: number
): boolean => {
  if (cancelledSessionIds.has(sessionId)) {
    return false;
  }
  return (generationBySession.get(sessionId) ?? 0) === generation;
};

