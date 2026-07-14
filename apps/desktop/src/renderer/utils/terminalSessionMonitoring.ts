import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";

export const shouldTrackTerminalSessionMetadata = (
  session?: SessionDescriptor,
  connection?: ConnectionProfile
): boolean => {
  return Boolean(
    session &&
    session.target === "remote" &&
    session.type === "terminal" &&
    session.connectionId &&
    connection?.id === session.connectionId &&
    connection.monitorSession
  );
};
