import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Drawer,
  Input,
  InputNumber,
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
  SavedCommand
} from "@nextshell/core";
import { usePreferencesStore } from "../store/usePreferencesStore";

const CMD_PARAMS_STORAGE_PREFIX = "nextshell:cmdParams:";

const TEMPLATE_PLACEHOLDER_REGEX = /\[#(\w+)\]/g;

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
  onExecuteCommand?: (command: string) => void;
}

const toGroupPath = (connection: ConnectionProfile): string => {
  if (connection.groupPath.length === 0) {
    return "/";
  }
  return connection.groupPath.join("/");
};

export const CommandCenterPane = ({
  connection,
  connected,
  connections,
  onExecuteCommand
}: CommandCenterPaneProps) => {
  const rememberTemplateParams = usePreferencesStore(
    (state) => state.preferences.commandCenter.rememberTemplateParams
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
  const [batchCommand, setBatchCommand] = useState("");
  const [batchGroup, setBatchGroup] = useState<string | undefined>(undefined);
  const [batchConnectionIds, setBatchConnectionIds] = useState<string[]>([]);
  const [batchRetryCount, setBatchRetryCount] = useState(1);
  const [batchConcurrency, setBatchConcurrency] = useState(5);
  const [batchResult, setBatchResult] = useState<BatchCommandExecutionResult | undefined>(
    undefined
  );
  const [batchRunning, setBatchRunning] = useState(false);

  const groupOptions = useMemo(() => {
    const set = new Set<string>();
    for (const item of connections) {
      set.add(toGroupPath(item));
    }
    return Array.from(set)
      .sort((a, b) => a.localeCompare(b))
      .map((item) => ({ label: item, value: item }));
  }, [connections]);

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
      // Load all commands without filters
      const list = await window.nextshell.savedCommand.list({});
      setAllCommands(list);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "加载命令库失败";
      message.error(reason);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadSavedCommands();
  }, [loadSavedCommands]);

  useEffect(() => {
    if (connection) setBatchGroup(toGroupPath(connection));
  }, [connection]);

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
      // Refresh all commands from DB to get new/updated item
      void loadSavedCommands();
    } catch (error) {
      const reason = error instanceof Error ? error.message : "保存失败";
      message.error(reason);
    }
  };

  const removeSavedCommand = async (cmd: SavedCommand) => {
    // Optimistic: remove from UI immediately
    const prev = [...allCommands];
    setAllCommands((commands) => commands.filter((c) => c.id !== cmd.id));
    try {
      await window.nextshell.savedCommand.remove({ id: cmd.id });
      message.success("已删除");
    } catch (error) {
      const reason = error instanceof Error ? error.message : "删除失败";
      message.error(reason);
      // Rollback
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

  const openTemplateDrawer = (cmd: SavedCommand) => {
    const keys = extractPlaceholderKeys(cmd.command);
    const initial = rememberTemplateParams ? loadParamsFromStorage(cmd.id) : {};
    const params: Record<string, string> = {};
    for (const key of keys) {
      params[key] = initial[key] ?? "";
    }
    setTemplateCommand(cmd);
    setTemplateParams(params);
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
    runCommand(resolved);
  };

  const runSavedCommand = (cmd: SavedCommand) => {
    if (cmd.isTemplate) {
      openTemplateDrawer(cmd);
      return;
    }
    runCommand(cmd.command);
  };

  const runBatch = async (): Promise<void> => {
    const command = batchCommand.trim();
    if (!command) {
      message.warning("请输入批量命令。");
      return;
    }
    const targetIds =
      batchConnectionIds.length > 0
        ? batchConnectionIds
        : batchGroup
          ? connections.filter((c) => toGroupPath(c) === batchGroup).map((c) => c.id)
          : connection
            ? [connection.id]
            : [];
    if (targetIds.length === 0) {
      message.warning("请选择至少一个目标连接或分组。");
      return;
    }
    try {
      setBatchRunning(true);
      const result = await window.nextshell.command.execBatch({
        command,
        connectionIds: targetIds,
        maxConcurrency: batchConcurrency,
        retryCount: batchRetryCount
      });
      setBatchResult(result);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "批量执行失败";
      message.error(reason);
    } finally {
      setBatchRunning(false);
    }
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
    library: false,
    batch: true
  });

  const toggleSection = (key: string) => {
    setSectionCollapsed((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  return (
    <div className="cc-pane">
      {/* ── Section 1: Command Library ── */}
      <div className="cc-section">
        <button
          type="button"
          className="cc-section-header"
          onClick={() => toggleSection("library")}
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
        </button>

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

      {/* ── Section 2: Batch Execution ── */}
      <div className="cc-section">
        <button
          type="button"
          className="cc-section-header"
          onClick={() => toggleSection("batch")}
        >
          <i
            className={sectionCollapsed.batch ? "ri-arrow-right-s-line" : "ri-arrow-down-s-line"}
            aria-hidden="true"
          />
          <i className="ri-stack-line cc-section-icon" aria-hidden="true" />
          <span className="cc-section-title">批量执行</span>
          {batchRunning && <Tag color="processing" style={{ margin: 0, lineHeight: "18px", fontSize: 10 }}>执行中</Tag>}
        </button>

        {!sectionCollapsed.batch && (
          <div className="cc-section-body cc-batch-body">
            <div className="cc-batch-row">
              <Input
                value={batchCommand}
                onChange={(e) => setBatchCommand(e.target.value)}
                placeholder="输入批量执行的命令"
                onPressEnter={() => void runBatch()}
                size="small"
              />
            </div>
            <div className="cc-batch-row cc-batch-options">
              <Select
                allowClear
                value={batchGroup}
                onChange={setBatchGroup}
                placeholder="选择分组"
                options={groupOptions}
                size="small"
                style={{ flex: 1, minWidth: 120 }}
              />
              <Select
                mode="multiple"
                allowClear
                value={batchConnectionIds}
                onChange={setBatchConnectionIds}
                placeholder="或选择连接"
                options={connections.map((c) => ({ label: `${c.name} (${c.host})`, value: c.id }))}
                size="small"
                style={{ flex: 2, minWidth: 160 }}
              />
            </div>
            <div className="cc-batch-row cc-batch-footer">
              <div className="cc-batch-params">
                <span className="cc-batch-param">
                  并发
                  <InputNumber
                    min={1} max={50}
                    value={batchConcurrency}
                    onChange={(v) => setBatchConcurrency(Number(v) || 1)}
                    size="small"
                    style={{ width: 56 }}
                  />
                </span>
                <span className="cc-batch-param">
                  重试
                  <InputNumber
                    min={0} max={5}
                    value={batchRetryCount}
                    onChange={(v) => setBatchRetryCount(Number(v) || 0)}
                    size="small"
                    style={{ width: 56 }}
                  />
                </span>
              </div>
              <Button type="primary" size="small" loading={batchRunning} onClick={() => void runBatch()}>
                执行
              </Button>
            </div>

            {batchResult ? (
              <div className="cc-batch-result">
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
            ) : null}
          </div>
        )}
      </div>

      <Modal
        title={editingCommand ? "编辑命令" : "新建命令"}
        open={editModalOpen}
        onOk={() => void submitEdit()}
        onCancel={() => setEditModalOpen(false)}
        destroyOnClose
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
        }}
        size={400}
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
                  填写参数后执行，
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
    </div>
  );
};
