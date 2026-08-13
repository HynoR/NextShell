import { CONNECTION_ZONES } from "@nextshell/shared";
import type { FormTab, ManagerTab } from "./types";

export const MANAGER_TABS: Array<{
  key: ManagerTab;
  label: string;
  icon: string;
  tabClassName?: string;
  labelClassName?: string;
}> = [
  { key: "connections", label: "连接", icon: "ri-server-line" },
  { key: "keys", label: "密钥", icon: "ri-key-2-line" },
  { key: "proxies", label: "代理", icon: "ri-shield-line" },
  { key: "import", label: "导入", icon: "ri-upload-2-line" }
];

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
  workspaceId: "property",
  tags: "property",
  notes: "property",
  favorite: "property",
  agentAccess: "property",
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
  workspaceId: undefined,
  tags: [],
  favorite: false,
  monitorSession: true,
  // Hosts stay invisible to agents until the user grants access explicitly.
  agentAccess: "off" as const
};
