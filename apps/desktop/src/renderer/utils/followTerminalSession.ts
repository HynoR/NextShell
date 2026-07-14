import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";

const isConnectedRemoteTerminal = (session: SessionDescriptor, connectionId: string): boolean =>
  session.target === "remote" &&
  session.type === "terminal" &&
  session.status === "connected" &&
  session.connectionId === connectionId;

export const resolveFollowTerminalSessionId = ({
  activeConnectionId,
  activeSessionId,
  connections,
  sessions,
  lastActiveRemoteTerminalByConnection
}: {
  activeConnectionId?: string;
  activeSessionId?: string;
  connections: ConnectionProfile[];
  sessions: SessionDescriptor[];
  lastActiveRemoteTerminalByConnection: Record<string, string | undefined>;
}): string | undefined => {
  if (!activeConnectionId) {
    return undefined;
  }

  const activeConnection = connections.find((connection) => connection.id === activeConnectionId);
  if (!activeConnection?.monitorSession) {
    return undefined;
  }

  const activeSession = activeSessionId
    ? sessions.find((session) => session.id === activeSessionId)
    : undefined;
  if (activeSession && isConnectedRemoteTerminal(activeSession, activeConnectionId)) {
    return activeSession.id;
  }

  const rememberedSessionId = lastActiveRemoteTerminalByConnection[activeConnectionId];
  if (rememberedSessionId) {
    const rememberedSession = sessions.find((session) => session.id === rememberedSessionId);
    if (rememberedSession && isConnectedRemoteTerminal(rememberedSession, activeConnectionId)) {
      return rememberedSession.id;
    }
  }

  return sessions.find((session) => isConnectedRemoteTerminal(session, activeConnectionId))?.id;
};
