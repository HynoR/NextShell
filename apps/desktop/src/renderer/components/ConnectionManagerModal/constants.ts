import { CONNECTION_ZONES } from "@nextshell/shared";
import type { FormTab } from "./types";

export const FIELD_TAB_MAP: Record<string, FormTab> = {
  name: "basic",
  host: "basic",
  port: "basic",
  username: "basic",
  authType: "basic",
  sshKeyId: "basic",
  password: "basic",
  hostFingerprint: "basic",
  strictHostKeyChecking: "basic",
  groupPath: "property",
  tags: "property",
  notes: "property",
  favorite: "property",
  proxyId: "network",
  keepAliveEnabled: "network",
  keepAliveIntervalSec: "network",
  monitorSession: "advanced",
  terminalEncoding: "advanced",
  backspaceMode: "advanced",
  deleteMode: "advanced"
};

export const DEFAULT_VALUES = {
  port: 22,
  authType: "password" as const,
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8" as const,
  backspaceMode: "ascii-backspace" as const,
  deleteMode: "vt220-delete" as const,
  groupPath: "/server",
  groupZone: CONNECTION_ZONES.SERVER as string,
  groupSubPath: "",
  tags: [],
  favorite: false,
  monitorSession: true
};
