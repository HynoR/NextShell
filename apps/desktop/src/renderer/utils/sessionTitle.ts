interface SessionTitleFallback {
  name?: string;
  host?: string;
}

export const claimNextSessionIndex = (
  counters: Map<string, number>,
  connectionId: string
): number => {
  const next = (counters.get(connectionId) ?? 0) + 1;
  counters.set(connectionId, next);
  return next;
};

export const resolveSessionBaseTitle = (
  sessionTitle: string | undefined,
  fallback?: SessionTitleFallback
): string => {
  const title = sessionTitle?.trim();
  if (title) {
    return title;
  }

  if (fallback?.name && fallback.host) {
    return `${fallback.name}@${fallback.host}`;
  }

  return "session";
};

export const formatSessionTitle = (baseTitle: string, index: number): string => {
  const normalizedBaseTitle = baseTitle.trim() || "session";
  return `${normalizedBaseTitle} #${index}`;
};
