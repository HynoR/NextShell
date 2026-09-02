import { create } from "zustand";
import type { CloudSyncWorkspaceProfile, ScopedCommandItem } from "@nextshell/core";
import { clearParamsFromStorage, getCommandStorageKey } from "../utils/commandTemplate";
import { formatErrorMessage } from "../utils/errorMessage";

export const DEFAULT_COMMAND_FOLDER = "默认";
export type CommandTabId = `local:${string}` | `workspace:${string}`;

export interface CommandTab {
  id: CommandTabId;
  label: string;
  scope: "local" | "workspace";
  group?: string;
  workspaceId?: string;
}

interface CommandStoreState {
  allCommands: ScopedCommandItem[];
  workspaces: CloudSyncWorkspaceProfile[];
  ephemeralFolders: string[];
  loading: boolean;
  activeTab: CommandTabId;

  load: () => Promise<void>;
  upsert: (params: {
    id?: string;
    name: string;
    description?: string;
    group: string;
    command: string;
    appendCr?: boolean;
    workspaceId?: string;
  }) => Promise<boolean>;
  remove: (cmd: ScopedCommandItem) => Promise<boolean>;
  createEphemeralFolder: (desiredName?: string) => string;
  renameLocalFolder: (oldName: string, newName: string) => Promise<boolean>;
  removeLocalFolder: (name: string) => Promise<boolean>;
  setActiveTab: (tab: CommandTabId) => void;
}

export const folderOf = (command: ScopedCommandItem): string =>
  command.group.trim() || DEFAULT_COMMAND_FOLDER;

const localTab = (group: string): CommandTab => ({
  id: `local:${group}`,
  label: group,
  scope: "local",
  group
});

export const workspaceTab = (workspace: CloudSyncWorkspaceProfile): CommandTab => ({
  id: `workspace:${workspace.id}`,
  label: workspace.displayName || workspace.workspaceName,
  scope: "workspace",
  workspaceId: workspace.id
});

export function getCommandTabs(
  commands: ScopedCommandItem[],
  workspaces: CloudSyncWorkspaceProfile[],
  ephemeralFolders: string[]
): CommandTab[] {
  const groups = new Set([DEFAULT_COMMAND_FOLDER, ...ephemeralFolders]);
  for (const command of commands) {
    if (command.scope === "local") groups.add(folderOf(command));
  }
  return [
    ...Array.from(groups)
      .sort((a, b) => a.localeCompare(b))
      .map(localTab),
    ...workspaces.map(workspaceTab)
  ];
}

export function getCommandsForTab(
  commands: ScopedCommandItem[],
  tab: CommandTab | undefined
): ScopedCommandItem[] {
  if (!tab) return [];
  const scoped = commands.filter((command) =>
    tab.scope === "local"
      ? command.scope === "local" && folderOf(command) === tab.group
      : command.scope === "workspace" && command.workspaceId === tab.workspaceId
  );
  return scoped.sort((a, b) =>
    tab.scope === "workspace"
      ? `${a.group}\u0000${a.name}`.localeCompare(`${b.group}\u0000${b.name}`)
      : a.name.localeCompare(b.name)
  );
}

const sameCommand = (left: ScopedCommandItem, right: ScopedCommandItem): boolean =>
  left.id === right.id && left.scope === right.scope && left.workspaceId === right.workspaceId;

export const useCommandStore = create<CommandStoreState>((set, get) => ({
  allCommands: [],
  workspaces: [],
  ephemeralFolders: [],
  loading: false,
  activeTab: `local:${DEFAULT_COMMAND_FOLDER}`,

  load: async () => {
    set({ loading: true });
    try {
      const [allCommands, workspaces] = await Promise.all([
        window.nextshell.savedCommand.listScoped(),
        window.nextshell.cloudSync.workspaceList()
      ]);
      const tabs = getCommandTabs(allCommands, workspaces, get().ephemeralFolders);
      const currentTab = get().activeTab;
      const activeTab = tabs.some((tab) => tab.id === currentTab)
        ? currentTab
        : (tabs[0]?.id ?? `local:${DEFAULT_COMMAND_FOLDER}`);
      set({ allCommands, workspaces, activeTab, loading: false });
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
    const previous = get().allCommands;
    set({ allCommands: previous.filter((item) => !sameCommand(item, cmd)) });
    clearParamsFromStorage(getCommandStorageKey(cmd));
    try {
      await window.nextshell.savedCommand.remove({
        id: cmd.id,
        workspaceId: cmd.scope === "workspace" ? cmd.workspaceId : undefined
      });
      return true;
    } catch {
      set({ allCommands: previous });
      return false;
    }
  },

  createEphemeralFolder: (desiredName) => {
    const folders = get().ephemeralFolders;
    const base = desiredName?.trim() || "新文件夹";
    let index = 1;
    let name = base;
    while (folders.includes(name)) name = `${base} ${++index}`;
    set({ ephemeralFolders: [...folders, name], activeTab: `local:${name}` });
    return name;
  },

  renameLocalFolder: async (oldName, rawNewName) => {
    const newName = rawNewName.trim();
    if (!newName || oldName === newName) return Boolean(newName);
    const commands = get().allCommands.filter(
      (command) => command.scope === "local" && folderOf(command) === oldName
    );
    try {
      await Promise.all(
        commands.map((command) =>
          window.nextshell.savedCommand.upsert({
            id: command.id,
            name: command.name,
            description: command.description,
            group: newName,
            command: command.command,
            appendCr: command.appendCr
          })
        )
      );
      set((state) => ({
        ephemeralFolders: state.ephemeralFolders
          .map((folder) => (folder === oldName ? newName : folder))
          .filter((folder, index, all) => all.indexOf(folder) === index),
        activeTab: state.activeTab === `local:${oldName}` ? `local:${newName}` : state.activeTab
      }));
      await get().load();
      return true;
    } catch {
      await get().load();
      return false;
    }
  },

  removeLocalFolder: async (name) => {
    const commands = get().allCommands.filter(
      (command) => command.scope === "local" && folderOf(command) === name
    );
    try {
      await Promise.all(
        commands.map((command) => window.nextshell.savedCommand.remove({ id: command.id }))
      );
      set((state) => ({
        ephemeralFolders: state.ephemeralFolders.filter((folder) => folder !== name),
        activeTab:
          state.activeTab === `local:${name}` ? `local:${DEFAULT_COMMAND_FOLDER}` : state.activeTab
      }));
      await get().load();
      return true;
    } catch {
      await get().load();
      return false;
    }
  },

  setActiveTab: (activeTab) => set({ activeTab })
}));

export const getTabById = (tabs: CommandTab[], id: CommandTabId): CommandTab | undefined =>
  tabs.find((tab) => tab.id === id);
