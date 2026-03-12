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

export const formatDynamicSessionTitle = (
  baseTitle: string,
  remoteTitle: string
): string => {
  const normalizedBaseTitle = baseTitle.trim() || "session";
  const normalizedRemoteTitle = remoteTitle.trim();
  if (!normalizedRemoteTitle) {
    return normalizedBaseTitle;
  }
  return `${normalizedBaseTitle} — ${normalizedRemoteTitle}`;
};

export const getDynamicSessionTitleParts = (
  sessionTitle: string,
  dynamicBaseTitle?: string
): { baseTitle: string; remoteTitle?: string } => {
  const normalizedSessionTitle = sessionTitle.trim() || "session";
  const normalizedDynamicBaseTitle = dynamicBaseTitle?.trim();
  if (!normalizedDynamicBaseTitle) {
    return { baseTitle: normalizedSessionTitle };
  }

  if (normalizedSessionTitle === normalizedDynamicBaseTitle) {
    return { baseTitle: normalizedDynamicBaseTitle };
  }

  const prefix = `${normalizedDynamicBaseTitle} — `;
  if (!normalizedSessionTitle.startsWith(prefix)) {
    return { baseTitle: normalizedDynamicBaseTitle };
  }

  const remoteTitle = normalizedSessionTitle.slice(prefix.length).trim();
  return remoteTitle
    ? { baseTitle: normalizedDynamicBaseTitle, remoteTitle }
    : { baseTitle: normalizedDynamicBaseTitle };
};
