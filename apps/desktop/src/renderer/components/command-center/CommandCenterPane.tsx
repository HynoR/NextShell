import { useCallback, useEffect, useMemo, useState } from "react";
import {
  App as AntdApp,
  AutoComplete,
  Button,
  Dropdown,
  Select,
  Space,
  Tag,
  Typography
} from "antd";
import type { ScopedCommandItem, SessionDescriptor } from "@nextshell/core";
import {
  DEFAULT_COMMAND_FOLDER,
  getCommandTabs,
  getCommandsForTab,
  getTabById,
  useCommandStore
} from "../../store/useCommandStore";
import {
  extractPlaceholderKeys,
  getCommandStorageKey,
  loadParamsFromStorage,
  saveParamsToStorage,
  substituteTemplate
} from "../../utils/commandTemplate";
import { promptModal } from "../../utils/promptModal";
import { CommandEditModal } from "./CommandEditModal";

interface CommandCenterPaneProps {
  connected: boolean;
  sessions: SessionDescriptor[];
  activeTerminalSession?: SessionDescriptor;
  onExecuteCommand?: (command: string, appendCr: boolean) => void;
  onExecuteAllCommands?: (command: string, appendCr: boolean) => void;
}

const commandKey = (command: ScopedCommandItem): string =>
  `${command.scope}:${command.workspaceId ?? ""}:${command.id}`;

const isConnectedTerminal = (session: SessionDescriptor): boolean =>
  (!session.type || session.type === "terminal") && session.status === "connected";

export const CommandCenterPane = ({
  connected,
  sessions,
  activeTerminalSession,
  onExecuteCommand,
  onExecuteAllCommands
}: CommandCenterPaneProps) => {
  const { message, modal } = AntdApp.useApp();
  const load = useCommandStore((state) => state.load);
  const upsert = useCommandStore((state) => state.upsert);
  const remove = useCommandStore((state) => state.remove);
  const createEphemeralFolder = useCommandStore((state) => state.createEphemeralFolder);
  const renameLocalFolder = useCommandStore((state) => state.renameLocalFolder);
  const removeLocalFolder = useCommandStore((state) => state.removeLocalFolder);
  const allCommands = useCommandStore((state) => state.allCommands);
  const workspaces = useCommandStore((state) => state.workspaces);
  const ephemeralFolders = useCommandStore((state) => state.ephemeralFolders);
  const activeTabId = useCommandStore((state) => state.activeTab);
  const setActiveTab = useCommandStore((state) => state.setActiveTab);
  const loading = useCommandStore((state) => state.loading);

  const tabs = useMemo(
    () => getCommandTabs(allCommands, workspaces, ephemeralFolders),
    [allCommands, ephemeralFolders, workspaces]
  );
  const activeTab = useMemo(() => getTabById(tabs, activeTabId) ?? tabs[0], [activeTabId, tabs]);
  const commands = useMemo(
    () => getCommandsForTab(allCommands, activeTab),
    [activeTab, allCommands]
  );
  const connectedSessions = useMemo(() => sessions.filter(isConnectedTerminal), [sessions]);
  const activeConnected = Boolean(
    connected && activeTerminalSession && isConnectedTerminal(activeTerminalSession)
  );

  const [selectedKey, setSelectedKey] = useState<string>();
  const selectedCommand = commands.find((command) => commandKey(command) === selectedKey);
  const [paramValues, setParamValues] = useState<Record<string, string>>({});
  const [paramHistory, setParamHistory] = useState<Record<string, string[]>>({});
  const [sendTarget, setSendTarget] = useState<"current" | "all">("current");
  const [editOpen, setEditOpen] = useState(false);
  const [editingCommand, setEditingCommand] = useState<ScopedCommandItem | null>(null);

  const placeholderKeys = useMemo(
    () => (selectedCommand ? extractPlaceholderKeys(selectedCommand.command) : []),
    [selectedCommand]
  );
  const storageKey = selectedCommand ? getCommandStorageKey(selectedCommand) : "";

  useEffect(() => {
    void load();
    const unsubApplied = window.nextshell.cloudSync.onApplied(() => void load());
    const unsubStatus = window.nextshell.cloudSync.onStatus(() => void load());
    return () => {
      unsubApplied();
      unsubStatus();
    };
  }, [load]);

  useEffect(() => {
    if (!selectedCommand) {
      setParamValues({});
      setParamHistory({});
      return;
    }
    const history = loadParamsFromStorage(storageKey);
    setParamHistory(history);
    setParamValues(
      Object.fromEntries(placeholderKeys.map((key) => [key, history[key]?.[0] ?? ""]))
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps -- key on identity/command primitives,
    // not selectedCommand's object identity, so a cloud-sync reload doesn't clear params mid-edit.
  }, [
    selectedCommand?.id,
    selectedCommand?.scope,
    selectedCommand?.workspaceId,
    selectedCommand?.command
  ]);

  useEffect(() => {
    if (selectedKey && !commands.some((command) => commandKey(command) === selectedKey)) {
      setSelectedKey(undefined);
    }
  }, [commands, selectedKey]);

  const openEditor = useCallback((command: ScopedCommandItem | null) => {
    setEditingCommand(command);
    setEditOpen(true);
  }, []);

  const handleEditSubmit = useCallback(
    async (values: { name: string; command: string; appendCr: boolean }) => {
      if (!values.name || !values.command) {
        message.warning("名称和命令内容不能为空。");
        return;
      }
      const scope = editingCommand?.scope === "workspace" ? "workspace" : activeTab?.scope;
      const workspaceId =
        editingCommand?.scope === "workspace" ? editingCommand.workspaceId : activeTab?.workspaceId;
      const group = editingCommand
        ? editingCommand.group || DEFAULT_COMMAND_FOLDER
        : activeTab?.group || DEFAULT_COMMAND_FOLDER;
      try {
        await upsert({
          id: editingCommand?.id,
          name: values.name,
          description: editingCommand?.description,
          command: values.command,
          appendCr: values.appendCr,
          group,
          ...(scope === "workspace" && workspaceId ? { workspaceId } : {})
        });
        message.success(editingCommand ? "命令已更新" : "命令已添加");
        setEditOpen(false);
      } catch (error) {
        message.error(`保存命令失败：${String(error)}`);
      }
    },
    [activeTab, editingCommand, message, upsert]
  );

  const handleRemove = useCallback(
    (command: ScopedCommandItem) => {
      modal.confirm({
        title: `删除命令「${command.name}」？`,
        content: "删除后无法恢复。",
        okButtonProps: { danger: true },
        onOk: async () => {
          if (await remove(command)) message.success("命令已删除");
          else message.error("删除命令失败");
        }
      });
    },
    [message, modal, remove]
  );

  const handleRenameFolder = useCallback(
    (name: string) => {
      void promptModal(modal, "重命名文件夹", undefined, name).then((next) => {
        if (!next) return;
        void renameLocalFolder(name, next).then((ok) => {
          if (!ok) message.error("文件夹重命名失败");
        });
      });
    },
    [message, modal, renameLocalFolder]
  );

  const handleCreateFolder = useCallback(() => {
    void promptModal(modal, "新建文件夹", "文件夹名称").then((name) => {
      if (!name) return;
      createEphemeralFolder(name);
    });
  }, [createEphemeralFolder, modal]);

  const handleRemoveFolder = useCallback(
    (name: string) => {
      modal.confirm({
        title: `删除文件夹「${name}」？`,
        content: "文件夹中的命令也会一起删除。",
        okButtonProps: { danger: true },
        onOk: async () => {
          if (await removeLocalFolder(name)) message.success("文件夹已删除");
          else message.error("删除文件夹失败");
        }
      });
    },
    [message, modal, removeLocalFolder]
  );

  const handleSend = useCallback(() => {
    if (!selectedCommand) return;
    const command = substituteTemplate(selectedCommand.command, paramValues);
    if (!command.trim()) return;
    saveParamsToStorage(storageKey, paramValues);
    setParamHistory(loadParamsFromStorage(storageKey));
    const appendCr = selectedCommand.appendCr !== false;
    if (sendTarget === "all") {
      if (connectedSessions.length === 0) return;
      onExecuteAllCommands?.(command, appendCr);
    } else if (activeConnected) {
      onExecuteCommand?.(command, appendCr);
    }
  }, [
    activeConnected,
    connectedSessions.length,
    onExecuteAllCommands,
    onExecuteCommand,
    paramValues,
    selectedCommand,
    sendTarget,
    storageKey
  ]);

  const blankMenu = {
    items: [
      { key: "add", label: "添加命令…" },
      { key: "folder", label: "新建文件夹…", disabled: activeTab?.scope !== "local" }
    ],
    onClick: ({ key }: { key: string }) => {
      if (key === "add") openEditor(null);
      if (key === "folder") handleCreateFolder();
    }
  };

  return (
    <div className="cc-pane">
      <div className="cc-tabs" role="tablist" aria-label="命令文件夹">
        {tabs.map((tab) => {
          const tabButton = (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={tab.id === activeTab?.id}
              className={`cc-tab ${tab.id === activeTab?.id ? "active" : ""}`}
              onClick={() => setActiveTab(tab.id)}
            >
              <i
                className={tab.scope === "local" ? "ri-folder-line" : "ri-cloud-line"}
                aria-hidden="true"
              />
              {tab.label}
            </button>
          );
          return tab.scope === "local" ? (
            <Dropdown
              key={tab.id}
              trigger={["contextMenu"]}
              menu={{
                items: [
                  { key: "rename", label: "重命名" },
                  {
                    key: "delete",
                    label: "删除",
                    danger: true,
                    disabled: tab.group === DEFAULT_COMMAND_FOLDER
                  }
                ],
                onClick: ({ key }) =>
                  key === "rename"
                    ? handleRenameFolder(tab.group ?? DEFAULT_COMMAND_FOLDER)
                    : handleRemoveFolder(tab.group ?? DEFAULT_COMMAND_FOLDER)
              }}
            >
              {tabButton}
            </Dropdown>
          ) : (
            tabButton
          );
        })}
      </div>

      <Dropdown menu={blankMenu} trigger={["contextMenu"]}>
        <div className="cc-chip-area">
          {loading ? <Typography.Text type="secondary">加载中…</Typography.Text> : null}
          {!loading && commands.length === 0 ? (
            <Typography.Text type="secondary">暂无命令，右键添加。</Typography.Text>
          ) : null}
          {commands.map((command) => (
            <Dropdown
              key={commandKey(command)}
              trigger={["contextMenu"]}
              menu={{
                items: [
                  { key: "edit", label: "编辑" },
                  { key: "delete", label: "删除", danger: true }
                ],
                onClick: ({ key }) => (key === "edit" ? openEditor(command) : handleRemove(command))
              }}
            >
              <div
                className={`cc-chip ${selectedKey === commandKey(command) ? "selected" : ""}`}
                role="button"
                tabIndex={0}
                title={command.command}
                onClick={() => setSelectedKey(commandKey(command))}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ")
                    setSelectedKey(commandKey(command));
                }}
              >
                <span className="cc-chip-name">{command.name}</span>
                <button
                  type="button"
                  className="cc-chip-edit"
                  title="编辑命令"
                  aria-label={`编辑 ${command.name}`}
                  onClick={(event) => {
                    event.stopPropagation();
                    openEditor(command);
                  }}
                >
                  <i className="ri-settings-3-line" aria-hidden="true" />
                </button>
              </div>
            </Dropdown>
          ))}
        </div>
      </Dropdown>

      <div className="cc-command-bar">
        {selectedCommand ? (
          <>
            <div className="cc-command-summary">
              <Tag color="blue">{selectedCommand.name}</Tag>
              <Typography.Text
                className="cc-command-preview"
                ellipsis
                title={selectedCommand.command}
              >
                {selectedCommand.command}
              </Typography.Text>
              <Button
                type="text"
                size="small"
                onClick={() => openEditor(selectedCommand)}
                title="编辑命令"
              >
                <i className="ri-edit-line" aria-hidden="true" />
              </Button>
            </div>
            {placeholderKeys.length > 0 ? (
              <div className="cc-params">
                {placeholderKeys.map((key) => (
                  <label key={key} className="cc-param">
                    <span>[#{key}]</span>
                    <AutoComplete
                      value={paramValues[key] ?? ""}
                      options={(paramHistory[key] ?? []).map((value) => ({ value }))}
                      onChange={(value) => setParamValues((prev) => ({ ...prev, [key]: value }))}
                      placeholder={key}
                      filterOption={(input, option) =>
                        String(option?.value ?? "")
                          .toLowerCase()
                          .includes(input.toLowerCase())
                      }
                    />
                  </label>
                ))}
              </div>
            ) : null}
            <div className="cc-send-row">
              <span className="cc-send-label">发送到</span>
              <Select
                size="small"
                value={sendTarget}
                onChange={setSendTarget}
                options={[
                  { value: "current", label: "当前会话", disabled: !activeConnected },
                  {
                    value: "all",
                    label: `全部会话 (${connectedSessions.length})`,
                    disabled: connectedSessions.length === 0
                  }
                ]}
              />
              <Button
                type="primary"
                size="small"
                onClick={handleSend}
                disabled={
                  sendTarget === "current" ? !activeConnected : connectedSessions.length === 0
                }
              >
                发送
              </Button>
            </div>
          </>
        ) : (
          <Typography.Text type="secondary">点击上方命令</Typography.Text>
        )}
      </div>

      <CommandEditModal
        open={editOpen}
        editingCommand={editingCommand}
        onSubmit={(values) => void handleEditSubmit(values)}
        onCancel={() => setEditOpen(false)}
      />
    </div>
  );
};
