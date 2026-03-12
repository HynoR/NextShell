import type { CloudSyncConflictItem, CloudSyncConfigureInput } from "@nextshell/shared";

export type SettingsSection =
  | "window"
  | "transfer"
  | "editor"
  | "command"
  | "terminal"
  | "network"
  | "cloudSync"
  | "backup"
  | "security"
  | "about";

export type LocalShellMode = "preset" | "custom";
export type LocalShellPreset = "system" | "powershell" | "cmd" | "zsh" | "sh" | "bash";
export type LocalShellPreference = {
  mode: LocalShellMode;
  preset: LocalShellPreset;
  customPath: string;
};

export type CloudSyncRuntimeState = "disabled" | "idle" | "syncing" | "error" | string;

export type CloudSyncStatusView = {
  enabled: boolean;
  state: CloudSyncRuntimeState;
  apiBaseUrl: string;
  workspaceName: string;
  pullIntervalSec: number;
  ignoreTlsErrors: boolean;
  lastSyncAt: string | null;
  lastError: string | null;
  keytarAvailable: boolean | null;
  pendingCount: number;
  conflictCount: number;
};

export type CloudSyncApi = {
  configure?: (input: CloudSyncConfigureInput) => Promise<unknown>;
  disable?: () => Promise<unknown>;
  status?: () => Promise<unknown>;
  syncNow?: () => Promise<unknown>;
  listConflicts?: () => Promise<unknown>;
  resolveConflict?: (input: {
    resourceType: "connection" | "sshKey" | "proxy";
    resourceId: string;
    strategy: "overwrite_local" | "keep_local";
  }) => Promise<unknown>;
  onStatus?: (listener: (event: unknown) => void) => (() => void) | void;
  onApplied?: (listener: (event: unknown) => void) => (() => void) | void;
};

export type SaveFn = (patch: Record<string, unknown>) => void;
