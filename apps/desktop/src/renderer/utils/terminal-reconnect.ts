import type { SessionStatus } from "@nextshell/core";

export const isEnterInput = (data: string): boolean => data === "\r" || data === "\n";

export const shouldReconnectOnInput = (status: SessionStatus | undefined, data: string): boolean =>
  status === "disconnected" && isEnterInput(data);
