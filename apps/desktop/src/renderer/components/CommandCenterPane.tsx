import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type KeyboardEvent as ReactKeyboardEvent
} from "react";
import {
  Button,
  Drawer,
  Input,
  Modal,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
  message
} from "antd";
import type {
  BatchCommandExecutionResult,
  ConnectionProfile,
  SavedCommand,
  SessionDescriptor
} from "@nextshell/core";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { formatErrorMessage } from "../utils/errorMessage";
import { getBatchTargetConnectionIds } from "../utils/batchTargets";

const CMD_PARAMS_STORAGE_PREFIX = "nextshell:cmdParams:";

const TEMPLATE_PLACEHOLDER_REGEX = /\[#(\w+)\]/g;

type TemplateExecutionMode = "single" | "batch";

function extractPlaceholderKeys(command: string): string[] {
  const keys: string[] = [];
  let match: RegExpExecArray | null;
  TEMPLATE_PLACEHOLDER_REGEX.lastIndex = 0;
  while ((match = TEMPLATE_PLACEHOLDER_REGEX.exec(command)) !== null) {
    keys.push(match[1] ?? "");
  }
  return [...new Set(keys)];
}

function substituteTemplate(command: string, params: Record<string, string>): string {
  return command.replace(TEMPLATE_PLACEHOLDER_REGEX, (_, key: string) =>
    params[key] !== undefined && params[key] !== "" ? params[key] : ""
  );
}

function loadParamsFromStorage(commandId: string): Record<string, string> {
  try {
    const raw = localStorage.getItem(CMD_PARAMS_STORAGE_PREFIX + commandId);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return Object.fromEntries(
        Object.entries(parsed).filter(
          (entry): entry is [string, string] =>
            typeof entry[0] === "string" && typeof entry[1] === "string"
        )
      );
    }
  } catch {
    // ignore
  }
  return {};
}

function saveParamsToStorage(commandId: string, params: Record<string, string>): void {
  try {
    localStorage.setItem(CMD_PARAMS_STORAGE_PREFIX + commandId, JSON.stringify(params));
  } catch {
    // ignore
  }
}

function clearParamsFromStorage(commandId: string): void {
  try {
    localStorage.removeItem(CMD_PARAMS_STORAGE_PREFIX + commandId);
  } catch {
    // ignore
  }
}

interface CommandCenterPaneProps {
  connection?: ConnectionProfile;
  connected: boolean;
  connections: ConnectionProfile[];
  sessions: SessionDescriptor[];
  onExecuteCommand?: (command: string) => void;
}

export const CommandCenterPane = ({
  connection,
  connected,
  connections,
  sessions,
  onExecuteCommand
}: CommandCenterPaneProps) => {
  const rememberTemplateParams = usePreferencesStore(
    (state) => state.preferences.commandCenter.rememberTemplateParams
  );
  const batchMaxConcurrency = usePreferencesStore(
    (state) => state.preferences.commandCenter.batchMaxConcurrency
  );
  const batchRetryCount = usePreferencesStore(
    (state) => state.preferences.commandCenter.batchRetryCount
  );

  const [allCommands, setAllCommands] = useState<SavedCommand[]>([]);
  const [keyword, setKeyword] = useState("");
  const [groupFilter, setGroupFilter] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [editModalOpen, setEditModalOpen] = useState(false);
  const [editingCommand, setEditingCommand] = useState<SavedCommand | null>(null);
  const [formName, setFormName] = useState("");
  const [formDescription, setFormDescription] = useState("");
  const [formGroup, setFormGroup] = useState("默认");
  const [formCommand, setFormCommand] = useState("");
  const [formIsTemplate, setFormIsTemplate] = useState(false);
  const [templateDrawerOpen, setTemplateDrawerOpen] = useState(false);
  const [templateCommand, setTemplateCommand] = useState<SavedCommand | null>(null);
  const [templateParams, setTemplateParams] = useState<Record<string, string>>({});
  const [templateExecutionMode, setTemplateExecutionMode] = useState<TemplateExecutionMode>("single");
  const [batchResult, setBatchResult] = useState<BatchCommandExecutionResult | undefined>(
    undefined
  );
  const [batchResultDrawerOpen, setBatchResultDrawerOpen] = useState(false);
  const [batchRunning, setBatchRunning] = useState(false);

  const targetConnectionIds = useMemo(() => getBatchTargetConnectionIds(sessions), [sessions]);

  // Client-side filtering — avoids IPC on every keystroke
  const savedCommands = useMemo(() => {
    let filtered = allCommands;
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
  }, [allCommands, keyword, groupFilter]);

  const savedCommandGroups = useMemo(() => {
    const groups = new Map<string, SavedCommand[]>();
    for (const cmd of savedCommands) {
      const g = cmd.group || "默认";
      if (!groups.has(g)) groups.set(g, []);
      groups.get(g)!.push(cmd);
    }
    return Array.from(groups.entries()).sort((a, b) => a[0].localeCompare(b[0]));
  }, [savedCommands]);

  const loadSavedCommands = useCallback(async () => {
    setLoading(true);
    try {
      const list = await window.nextshell.savedCommand.list({});
      setAllCommands(list);
    } catch (error) {
      message.error(`加载命令库失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadSavedCommands();
  }, [loadSavedCommands]);

  const openCreateModal = () => {
    setEditingCommand(null);
    setFormName("");
    setFormDescription("");
    setFormGroup("默认");
    setFormCommand("");
    setFormIsTemplate(false);
    setEditModalOpen(true);
  };

  const openEditModal = (cmd: SavedCommand) => {
    setEditingCommand(cmd);
    setFormName(cmd.name);
    setFormDescription(cmd.description ?? "");
    setFormGroup(cmd.group || "默认");
    setFormCommand(cmd.command);
    setFormIsTemplate(cmd.isTemplate);
    setEditModalOpen(true);
  };

  const submitEdit = async () => {
    const name = formName.trim();
    const command = formCommand.trim();
    if (!name || !command) {
      message.warning("名称和命令内容不能为空。");
      return;
    }
    try {
      await window.nextshell.savedCommand.upsert({
        id: editingCommand?.id,
        name,
        description: formDescription.trim() || undefined,
        group: formGroup.trim() || "默认",
        command,
        isTemplate: formIsTemplate
      });
      message.success(editingCommand ? "命令已更新" : "命令已添加");
      setEditModalOpen(false);
      void loadSavedCommands();
    } catch (error) {
      message.error(`保存命令失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  };

  const removeSavedCommand = async (cmd: SavedCommand) => {
    const prev = [...allCommands];
    setAllCommands((commands) => commands.filter((c) => c.id !== cmd.id));
    try {
      await window.nextshell.savedCommand.remove({ id: cmd.id });
      message.success("已删除");
    } catch (error) {
      message.error(`删除命令失败：${formatErrorMessage(error, "请稍后重试")}`);
      setAllCommands(prev);
    }
  };

  const runCommand = (command: string): void => {
    if (!connection) {
      message.warning("请先选择连接。");
      return;
    }
    if (!connected) {
      message.warning("当前连接未建立会话，请双击左侧服务器建立 SSH 连接。");
      return;
    }
    const normalized = command.trim();
    if (!normalized) return;
    onExecuteCommand?.(normalized);
  };

  const runBatchCommand = useCallback(
    async (command: string): Promise<void> => {
      const normalized = command.trim();
      if (!normalized) {
        return;
      }
      if (targetConnectionIds.length === 0) {
        message.warning("当前没有打开标签页，无法批量执行。");
        return;
      }
      try {
        setBatchRunning(true);
        const result = await window.nextshell.command.execBatch({
          command: normalized,
          connectionIds: targetConnectionIds,
          maxConcurrency: batchMaxConcurrency,
          retryCount: batchRetryCount
        });
        setBatchResult(result);
        setBatchResultDrawerOpen(true);
      } catch (error) {
        message.error(`批量执行失败：${formatErrorMessage(error, "请检查连接状态")}`);
      } finally {
        setBatchRunning(false);
      }
    },
    [batchMaxConcurrency, batchRetryCount, targetConnectionIds]
  );

  const openTemplateDrawer = (cmd: SavedCommand, mode: TemplateExecutionMode) => {
    const keys = extractPlaceholderKeys(cmd.command);
    const initial = rememberTemplateParams ? loadParamsFromStorage(cmd.id) : {};
    const params: Record<string, string> = {};
    for (const key of keys) {
      params[key] = initial[key] ?? "";
    }
    setTemplateCommand(cmd);
    setTemplateParams(params);
    setTemplateExecutionMode(mode);
    setTemplateDrawerOpen(true);
  };

  const runTemplateFromDrawer = () => {
    if (!templateCommand) return;
    const resolved = substituteTemplate(templateCommand.command, templateParams);
    if (rememberTemplateParams) {
      saveParamsToStorage(templateCommand.id, templateParams);
    } else {
      clearParamsFromStorage(templateCommand.id);
    }
    setTemplateDrawerOpen(false);
    setTemplateCommand(null);
    if (templateExecutionMode === "batch") {
      void runBatchCommand(resolved);
      return;
    }
    runCommand(resolved);
  };

  const runSavedCommand = (cmd: SavedCommand) => {
    if (cmd.isTemplate) {
      openTemplateDrawer(cmd, "single");
      return;
    }
    runCommand(cmd.command);
  };

  const runBatchForSavedCommand = async (cmd: SavedCommand): Promise<void> => {
    if (cmd.isTemplate) {
      openTemplateDrawer(cmd, "batch");
      return;
    }
    await runBatchCommand(cmd.command);
  };

  const groupOptionsForFilter = useMemo(() => {
    const set = new Set<string>();
    for (const cmd of savedCommands) {
      set.add(cmd.group || "默认");
    }
    return [{ label: "全部", value: undefined }, ...Array.from(set).sort((a, b) => a.localeCompare(b)).map((g) => ({ label: g, value: g }))];
  }, [savedCommands]);

  const templateKeys = templateCommand ? extractPlaceholderKeys(templateCommand.command) : [];

  const [sectionCollapsed, setSectionCollapsed] = useState<Record<string, boolean>>({
    library: false
  });

  const toggleSection = (key: string) => {
    setSectionCollapsed((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const onSectionHeaderKeyDown = (
    event: ReactKeyboardEvent<HTMLDivElement>,
    key: string
  ) => {
    if (event.key !== "Enter" && event.key !== " ") {
      return;
    }
    event.preventDefault();
    toggleSection(key);
  };

  return (
    <div className="cc-pane">
      <div className="cc-section">
        <div
          role="button"
          tabIndex={0}
          aria-expanded={!sectionCollapsed.library}
          className="cc-section-header"
          onClick={() => toggleSection("library")}
          onKeyDown={(event) => onSectionHeaderKeyDown(event, "library")}
        >
          <i
            className={sectionCollapsed.library ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"}
            aria-hidden="true"
          />
          <i className="ri-code-box-line cc-section-icon" aria-hidden="true" />
          <span className="cc-section-title">命令库</span>
          <span className="cc-section-count">{savedCommands.length}</span>
          <span className="cc-section-actions" onClick={(e) => e.stopPropagation()}>
            <Select
              allowClear
              placeholder="分组"
              value={groupFilter}
              onChange={setGroupFilter}
              options={groupOptionsForFilter}
              size="small"
              style={{ width: 110 }}
              onClick={(e) => e.stopPropagation()}
            />
            <Input
              allowClear
              placeholder="搜索"
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              size="small"
              style={{ width: 120 }}
              prefix={<i className="ri-search-line" style={{ fontSize: 12, color: "var(--t3)" }} />}
              onClick={(e) => e.stopPropagation()}
            />
            <button
              className="cc-add-btn"
              onClick={(e) => {
                e.stopPropagation();
                openCreateModal();
              }}
              title="新建命令"
            >
              <i className="ri-add-line" aria-hidden="true" />
            </button>
          </span>
        </div>

        {!sectionCollapsed.library && (
          <div className="cc-section-body">
            {loading ? (
              <div className="cc-empty-hint">加载中…</div>
            ) : savedCommandGroups.length === 0 ? (
              <div className="cc-empty-hint">
                <i className="ri-inbox-2-line" aria-hidden="true" />
                <span>暂无命令，点击 + 添加</span>
              </div>
            ) : (
              <div className="cc-library-list">
                {savedCommandGroups.map(([groupName, cmds]) => (
                  <div key={groupName} className="cc-cmd-group">
                    <div className="cc-cmd-group-label">
                      <i className="ri-folder-3-line" aria-hidden="true" />
                      {groupName}
                    </div>
                    <div className="cc-cmd-items">
                      {cmds.map((cmd) => (
                        <div key={cmd.id} className="cc-cmd-card">
                          <div className="cc-cmd-card-head">
                            <span className="cc-cmd-name">{cmd.name}</span>
                            {cmd.isTemplate && <Tag color="blue" style={{ margin: 0, lineHeight: "18px", fontSize: 10 }}>模板</Tag>}
                          </div>
                          <code className="cc-cmd-preview">
                            {cmd.command.length > 60 ? `${cmd.command.slice(0, 60)}…` : cmd.command}
                          </code>
                          <div className="cc-cmd-card-actions">
                            <button
                              className="cc-action-btn primary"
                              disabled={!connection || !connected}
                              onClick={() => runSavedCommand(cmd)}
                              title="执行"
                            >
                              <i className="ri-play-line" aria-hidden="true" />
                            </button>
                            <button
                              className="cc-action-btn secondary"
                              disabled={batchRunning}
                              onClick={() => void runBatchForSavedCommand(cmd)}
                              title="批量执行"
                            >
                              <i className="ri-stack-line" aria-hidden="true" />
                            </button>
                            <button
                              className="cc-action-btn"
                              onClick={() => openEditModal(cmd)}
                              title="编辑"
                            >
                              <i className="ri-edit-line" aria-hidden="true" />
                            </button>
                            <button
                              className="cc-action-btn danger"
                              onClick={() => void removeSavedCommand(cmd)}
                              title="删除"
                            >
                              <i className="ri-delete-bin-line" aria-hidden="true" />
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      <Modal
        title={editingCommand ? "编辑命令" : "新建命令"}
        open={editModalOpen}
        onOk={() => void submitEdit()}
        onCancel={() => setEditModalOpen(false)}
        destroyOnHidden
        width={520}
      >
        <Space direction="vertical" style={{ width: "100%" }} size="middle">
          <div>
            <Typography.Text type="secondary">名称</Typography.Text>
            <Input
              value={formName}
              onChange={(e) => setFormName(e.target.value)}
              placeholder="命令名称"
            />
          </div>
          <div>
            <Typography.Text type="secondary">描述（可选）</Typography.Text>
            <Input
              value={formDescription}
              onChange={(e) => setFormDescription(e.target.value)}
              placeholder="简要说明"
            />
          </div>
          <div>
            <Typography.Text type="secondary">分组</Typography.Text>
            <Input
              value={formGroup}
              onChange={(e) => setFormGroup(e.target.value)}
              placeholder="默认"
            />
          </div>
          <div>
            <Typography.Text type="secondary">命令内容（模板可用 [#key] 占位符）</Typography.Text>
            <Input.TextArea
              value={formCommand}
              onChange={(e) => setFormCommand(e.target.value)}
              placeholder="例如: uname -a 或 tail -n [#lines] /var/log/syslog"
              rows={3}
            />
          </div>
          <div>
            <Space>
              <Typography.Text type="secondary">模板命令（含 [#占位符] 时勾选）</Typography.Text>
              <Switch checked={formIsTemplate} onChange={setFormIsTemplate} />
            </Space>
          </div>
        </Space>
      </Modal>

      <Drawer
        title={templateCommand ? `执行：${templateCommand.name}` : "参数"}
        open={templateDrawerOpen}
        onClose={() => {
          setTemplateDrawerOpen(false);
          setTemplateCommand(null);
          setTemplateExecutionMode("single");
        }}
        width={400}
        footer={
          <Button type="primary" onClick={() => runTemplateFromDrawer()}>
            执行
          </Button>
        }
      >
        {templateCommand && (
          <Space direction="vertical" style={{ width: "100%" }}>
            {templateKeys.length > 0 ? (
              <>
                <Typography.Text type="secondary">
                  填写参数后{templateExecutionMode === "batch" ? "将对当前打开标签页服务器批量执行，" : "执行，"}
                  {rememberTemplateParams ? "将自动记住本次输入。" : "本次输入不会被记住。"}
                </Typography.Text>
                {templateKeys.map((key) => (
                  <div key={key}>
                    <Typography.Text strong>[#{key}]</Typography.Text>
                    <Input
                      value={templateParams[key] ?? ""}
                      onChange={(e) =>
                        setTemplateParams((prev) => ({ ...prev, [key]: e.target.value }))
                      }
                      placeholder={key}
                    />
                  </div>
                ))}
              </>
            ) : (
              <Typography.Text type="secondary">无需参数，点击「执行」直接运行。</Typography.Text>
            )}
          </Space>
        )}
      </Drawer>

      <Drawer
        title={batchResult ? `批量执行结果：${batchResult.command}` : "批量执行结果"}
        open={batchResultDrawerOpen}
        onClose={() => setBatchResultDrawerOpen(false)}
        width={560}
      >
        {batchResult ? (
          <div className="cc-batch-result cc-batch-drawer">
            <div className="cc-batch-summary">
              <span>总计 <strong>{batchResult.total}</strong></span>
              <span className="cc-batch-ok">成功 {batchResult.successCount}</span>
              <span className="cc-batch-fail">失败 {batchResult.failedCount}</span>
              <span>{batchResult.durationMs}ms</span>
            </div>
            <div className="cc-batch-items">
              {batchResult.results.map((item) => {
                const target = connections.find((c) => c.id === item.connectionId);
                return (
                  <div key={`${item.connectionId}-${item.executedAt}`} className="cc-result-item">
                    <div className="cc-result-item-head">
                      <span>{target?.name ?? item.connectionId}</span>
                      <Tag color={item.success ? "green" : "red"} style={{ margin: 0, lineHeight: "18px", fontSize: 10 }}>
                        {item.success ? "成功" : "失败"} / {item.attempts}次
                      </Tag>
                    </div>
                    <pre className="cc-output">{item.stdout || "(empty)"}</pre>
                    {(item.stderr || item.error) ? (
                      <pre className="cc-output error">{item.stderr || item.error}</pre>
                    ) : null}
                  </div>
                );
              })}
            </div>
          </div>
        ) : (
          <Typography.Text type="secondary">暂无批量执行结果。</Typography.Text>
        )}
      </Drawer>
    </div>
  );
};
