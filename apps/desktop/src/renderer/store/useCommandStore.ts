import { create } from "zustand";
import type { CloudSyncWorkspaceProfile, ScopedCommandItem } from "@nextshell/core";
import { clearParamsFromStorage, getCommandStorageKey } from "../utils/commandTemplate";
import { formatErrorMessage } from "../utils/errorMessage";

type CommandScopeId = "local" | string;

interface CommandGroup {
  name: string;
  commands: ScopedCommandItem[];
}

interface CommandStoreState {
  allCommands: ScopedCommandItem[];
  workspaces: CloudSyncWorkspaceProfile[];
  loading: boolean;
  activeScope: CommandScopeId;
  keyword: string;
  groupFilter: string | undefined;

  load: () => Promise<void>;
  upsert: (params: {
    id?: string;
    name: string;
    description?: string;
    group: string;
    command: string;
    isTemplate: boolean;
    workspaceId?: string;
  }) => Promise<boolean>;
  remove: (cmd: ScopedCommandItem) => Promise<boolean>;
  setActiveScope: (scope: CommandScopeId) => void;
  setKeyword: (keyword: string) => void;
  setGroupFilter: (group: string | undefined) => void;
}

export function filterCommands(
  allCommands: ScopedCommandItem[],
  activeScope: CommandScopeId,
  keyword: string,
  groupFilter: string | undefined
): ScopedCommandItem[] {
  let filtered = allCommands.filter((cmd) =>
    activeScope === "local"
      ? cmd.scope === "local"
      : cmd.scope === "workspace" && cmd.workspaceId === activeScope
  );

  const kw = keyword.trim().toLowerCase();
  if (kw) {
    filtered = filtered.filter(
      (cmd) =>
        cmd.name.toLowerCase().includes(kw) ||
        cmd.command.toLowerCase().includes(kw) ||
        (cmd.description ?? "").toLowerCase().includes(kw)
    );
  }

  if (groupFilter) {
    filtered = filtered.filter((cmd) => (cmd.group || "默认") === groupFilter);
  }

  return filtered;
}

export function groupCommands(commands: ScopedCommandItem[]): CommandGroup[] {
  const groups = new Map<string, ScopedCommandItem[]>();
  for (const cmd of commands) {
    const g = cmd.group || "默认";
    if (!groups.has(g)) groups.set(g, []);
    groups.get(g)!.push(cmd);
  }
  return Array.from(groups.entries())
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([name, commands]) => ({ name, commands }));
}

export const useCommandStore = create<CommandStoreState>((set, get) => ({
  allCommands: [],
  workspaces: [],
  loading: false,
  activeScope: "local",
  keyword: "",
  groupFilter: undefined,

  load: async () => {
    set({ loading: true });
    try {
      const [list, workspaceList] = await Promise.all([
        window.nextshell.savedCommand.listScoped(),
        window.nextshell.cloudSync.workspaceList()
      ]);
      const state = get();
      const scopeStillValid =
        state.activeScope === "local" || workspaceList.some((w) => w.id === state.activeScope);
      set({
        allCommands: list,
        workspaces: workspaceList,
        loading: false,
        ...(scopeStillValid ? {} : { activeScope: "local" as CommandScopeId })
      });
    } catch {
      set({ loading: false });
    }
  },

  upsert: async (params) => {
    try {
      await window.nextshell.savedCommand.upsert(params);
      await get().load();
      return true;
    } catch (error) {
      throw new Error(formatErrorMessage(error, "请稍后重试"));
    }
  },

  remove: async (cmd) => {
    const prev = [...get().allCommands];
    set({
      allCommands: prev.filter(
        (item) =>
          !(item.id === cmd.id && item.scope === cmd.scope && item.workspaceId === cmd.workspaceId)
      )
    });
    clearParamsFromStorage(getCommandStorageKey(cmd));
    try {
      await window.nextshell.savedCommand.remove({
        id: cmd.id,
        workspaceId: cmd.scope === "workspace" ? cmd.workspaceId : undefined
      });
      return true;
    } catch {
      set({ allCommands: prev });
      return false;
    }
  },

  setActiveScope: (scope) => set({ activeScope: scope, keyword: "", groupFilter: undefined }),
  setKeyword: (keyword) => set({ keyword }),
  setGroupFilter: (groupFilter) => set({ groupFilter })
}));

// ── Pure helpers (use in useMemo, not as Zustand selectors) ──────────

export function getActiveScopeLabel(
  activeScope: CommandScopeId,
  workspaces: CloudSyncWorkspaceProfile[]
): string {
  if (activeScope === "local") return "本地";
  const ws = workspaces.find((w) => w.id === activeScope);
  return ws ? ws.displayName || ws.workspaceName : "本地";
}

export function buildGroupOptions(
  allCommands: ScopedCommandItem[],
  activeScope: CommandScopeId
): Array<{ label: string; value: string | undefined }> {
  const scoped = filterCommands(allCommands, activeScope, "", undefined);
  const groups = new Set<string>();
  for (const cmd of scoped) groups.add(cmd.group || "默认");
  return [
    { label: "全部", value: undefined },
    ...Array.from(groups)
      .sort((a, b) => a.localeCompare(b))
      .map((g) => ({ label: g, value: g }))
  ];
}
