import type { SessionDescriptor } from "@nextshell/core";

export const getBatchTargetConnectionIds = (
  sessions: SessionDescriptor[]
): string[] => {
  const seen = new Set<string>();
  const targetIds: string[] = [];
  for (const session of sessions) {
    if (seen.has(session.connectionId)) {
      continue;
    }
    seen.add(session.connectionId);
    targetIds.push(session.connectionId);
  }
  return targetIds;
};
