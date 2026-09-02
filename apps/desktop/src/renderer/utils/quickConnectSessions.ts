import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";

export interface SessionResultItem {
  session: SessionDescriptor;
  connection: ConnectionProfile | undefined;
  isActive: boolean;
}

const MAX_SESSION_RESULTS = 8;

function sessionSearchable(
  session: SessionDescriptor,
  connection: ConnectionProfile | undefined
): string {
  const connectionBits = connection
    ? `${connection.name} ${connection.host} ${connection.username ?? ""}`
    : "";
  return `${session.title} ${connectionBits}`.toLowerCase();
}

/**
 * Builds the list of open-session results to surface in the quick connect palette.
 * When `keyword` is empty, returns up to MAX_SESSION_RESULTS sessions in MRU order
 * (most recent first), skipping the currently active session.
 * When `keyword` is non-empty, filters sessions by title/connection host/name,
 * without skipping the active session or capping to MRU order.
 */
export function buildQuickConnectSessionResults(params: {
  sessions: SessionDescriptor[];
  sessionMruIds: string[];
  activeSessionId: string | undefined;
  connections: ConnectionProfile[];
  keyword: string;
}): SessionResultItem[] {
  const { sessions, sessionMruIds, activeSessionId, connections, keyword } = params;
  const connectionById = new Map(connections.map((c) => [c.id, c] as const));
  const sessionById = new Map(sessions.map((s) => [s.id, s] as const));
  const lower = keyword.trim().toLowerCase();

  if (!lower) {
    const ordered: SessionDescriptor[] = [];
    const seen = new Set<string>();

    for (const id of sessionMruIds) {
      const session = sessionById.get(id);
      if (!session || seen.has(id) || id === activeSessionId) continue;
      seen.add(id);
      ordered.push(session);
    }

    for (const session of sessions) {
      if (seen.has(session.id) || session.id === activeSessionId) continue;
      seen.add(session.id);
      ordered.push(session);
    }

    return ordered.slice(0, MAX_SESSION_RESULTS).map((session) => ({
      session,
      connection: session.connectionId ? connectionById.get(session.connectionId) : undefined,
      isActive: session.id === activeSessionId
    }));
  }

  return sessions
    .filter((session) => {
      const connection = session.connectionId
        ? connectionById.get(session.connectionId)
        : undefined;
      return sessionSearchable(session, connection).includes(lower);
    })
    .map((session) => ({
      session,
      connection: session.connectionId ? connectionById.get(session.connectionId) : undefined,
      isActive: session.id === activeSessionId
    }));
}
