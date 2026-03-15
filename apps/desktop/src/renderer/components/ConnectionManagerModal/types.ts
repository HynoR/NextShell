import type { ConnectionImportEntry, ConnectionProfile } from "@nextshell/core";
import type { ConnectionZone } from "@nextshell/shared";

export type ManagerTab = "connections" | "keys" | "proxies" | "cloudSync" | "recycleBin";
export type ManagerMode = "idle" | "new" | "edit";
export type FormTab = "basic" | "property" | "network" | "advanced";
export type SortMode = "name" | "host" | "createdAt";

export interface ImportPreviewBatch {
  fileName: string;
  entries: ConnectionImportEntry[];
}

export interface MgrGroupNode {
  type: "group";
  key: string;
  label: string;
  children: MgrTreeNode[];
  zone?: ConnectionZone;
  icon?: string;
}

export interface MgrLeafNode {
  type: "leaf";
  connection: ConnectionProfile;
}

export type MgrTreeNode = MgrGroupNode | MgrLeafNode;

export type MgrContextTarget =
  | { type: "connection"; connectionId: string }
  | { type: "group"; groupKey: string; groupPath: string }
  | { type: "empty" };

export interface MgrContextMenuState {
  x: number;
  y: number;
  target: MgrContextTarget;
}

export interface MgrClipboard {
  mode: "copy" | "cut";
  connectionIds: string[];
}
