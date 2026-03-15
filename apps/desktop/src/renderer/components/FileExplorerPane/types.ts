import type { DataNode } from "antd/es/tree";
import type { ConnectionProfile, RemoteFileEntry } from "@nextshell/core";

export interface FileExplorerPaneProps {
  connection?: ConnectionProfile;
  connected: boolean;
  followSessionId?: string;
  active: boolean;
  onOpenSettings?: () => void;
  onOpenEditorTab?: (connectionId: string, remotePath: string) => Promise<void>;
}

export interface DirTreeNode extends DataNode {
  key: string;
  title: string;
  isLeaf: false;
  children?: DirTreeNode[];
}

export type ClipboardMode = "copy" | "cut";

export interface Clipboard {
  mode: ClipboardMode;
  entries: RemoteFileEntry[];
  sourceConnectionId: string;
}

export interface ContextMenuState {
  x: number;
  y: number;
  entries: RemoteFileEntry[];
}
