import { useCallback, useEffect, useMemo, useState } from "react";
import { App as AntdApp, Input, Select, Space, Tag, TreeSelect } from "antd";
import type {
  BatchCommandExecutionResult,
  ConnectionProfile,
  ScopedCommandItem,
  SessionDescriptor,
} from "@nextshell/core";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import {
  useCommandStore,
  filterCommands,
  groupCommands,
  buildGroupOptions,
  getActiveScopeLabel,
} from "../../store/useCommandStore";
import {
  buildBatchTargetTree,
  getBatchTargetConnectionIds,
} from "../../utils/batchTargets";
import { formatErrorMessage } from "../../utils/errorMessage";
import { CommandList } from "./CommandList";
import { CommandEditModal } from "./CommandEditModal";
import {
  TemplateParamDrawer,
  type TemplateExecutionMode,
} from "./TemplateParamDrawer";
import { BatchResultDrawer } from "./BatchResultDrawer";

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
  onExecuteCommand,
}: CommandCenterPaneProps) => {
  const { message } = AntdApp.useApp();
  const batchMaxConcurrency = usePreferencesStore(
    (s) => s.preferences.commandCenter.batchMaxConcurrency
  );
  const batchRetryCount = usePreferencesStore(
    (s) => s.preferences.commandCenter.batchRetryCount
  );

  const load = useCommandStore((s) => s.load);
  const upsert = useCommandStore((s) => s.upsert);
  const remove = useCommandStore((s) => s.remove);
  const loading = useCommandStore((s) => s.loading);
  const allCommands = useCommandStore((s) => s.allCommands);
  const activeScope = useCommandStore((s) => s.activeScope);
  const setActiveScope = useCommandStore((s) => s.setActiveScope);
  const keyword = useCommandStore((s) => s.keyword);
  const setKeyword = useCommandStore((s) => s.setKeyword);
  const groupFilter = useCommandStore((s) => s.groupFilter);
  const setGroupFilter = useCommandStore((s) => s.setGroupFilter);
  const workspaces = useCommandStore((s) => s.workspaces);

  const scopeLabel = useMemo(
    () => getActiveScopeLabel(activeScope, workspaces),
    [activeScope, workspaces]
  );
  const filteredCommands = useMemo(
    () => filterCommands(allCommands, activeScope, keyword, groupFilter),
    [allCommands, activeScope, keyword, groupFilter]
  );
  const commandGroups = useMemo(
    () => groupCommands(filteredCommands),
    [filteredCommands]
  );
  const groupOptions = useMemo(
    () => buildGroupOptions(allCommands, activeScope),
    [allCommands, activeScope]
  );

  // ── Edit modal ─────────────────────────────────────────────
  const [editOpen, setEditOpen] = useState(false);
  const [editingCmd, setEditingCmd] = useState<ScopedCommandItem | null>(null);

  // ── Template drawer ────────────────────────────────────────
  const [templateOpen, setTemplateOpen] = useState(false);
  const [templateCmd, setTemplateCmd] = useState<ScopedCommandItem | null>(null);
  const [templateMode, setTemplateMode] = useState<TemplateExecutionMode>("single");

  // ── Batch state ────────────────────────────────────────────
  const [batchRunning, setBatchRunning] = useState(false);
  const [batchResult, setBatchResult] = useState<BatchCommandExecutionResult>();
  const [batchResultOpen, setBatchResultOpen] = useState(false);

  // ── Batch targets ──────────────────────────────────────────
  const openTabTargetIds = useMemo(
    () => getBatchTargetConnectionIds(sessions),
    [sessions]
  );
  const [targetSelection, setTargetSelection] = useState<string[] | null>(null);
  const targetConnectionIds = useMemo(
    () => targetSelection ?? openTabTargetIds,
    [targetSelection, openTabTargetIds]
  );
  const targetTree = useMemo(
    () => buildBatchTargetTree(connections),
    [connections]
  );

  // ── Data loading ───────────────────────────────────────────
  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    const unsub1 = window.nextshell.cloudSync.onApplied(() => void load());
    const unsub2 = window.nextshell.cloudSync.onStatus(() => void load());
    return () => {
      unsub1();
      unsub2();
    };
  }, [load]);

  // ── Execution ──────────────────────────────────────────────
  const runSingle = useCallback(
    (command: string) => {
      if (!connection) {
        message.warning("请先选择连接。");
        return;
      }
      if (!connected) {
        message.warning("当前连接未建立会话，请先连接 SSH 终端。");
        return;
      }
      const normalized = command.trim();
      if (normalized) onExecuteCommand?.(normalized);
    },
    [connection, connected, onExecuteCommand]
  );

  const runBatch = useCallback(
    async (command: string) => {
      const normalized = command.trim();
      if (!normalized) return;
      if (targetConnectionIds.length === 0) {
        message.warning("请先选择批量执行的目标连接。");
        return;
      }
      try {
        setBatchRunning(true);
        const result = await window.nextshell.command.execBatch({
          command: normalized,
          connectionIds: targetConnectionIds,
          maxConcurrency: batchMaxConcurrency,
          retryCount: batchRetryCount,
        });
        setBatchResult(result);
        setBatchResultOpen(true);
      } catch (error) {
        message.error(
          `批量执行失败：${formatErrorMessage(error, "请检查连接状态")}`
        );
      } finally {
        setBatchRunning(false);
      }
    },
    [batchMaxConcurrency, batchRetryCount, targetConnectionIds]
  );

  // ── Handlers ───────────────────────────────────────────────
  const handleRun = useCallback(
    (cmd: ScopedCommandItem) => {
      if (cmd.isTemplate) {
        setTemplateCmd(cmd);
        setTemplateMode("single");
        setTemplateOpen(true);
      } else {
        runSingle(cmd.command);
      }
    },
    [runSingle]
  );

  const handleRunBatch = useCallback(
    (cmd: ScopedCommandItem) => {
      if (cmd.isTemplate) {
        setTemplateCmd(cmd);
        setTemplateMode("batch");
        setTemplateOpen(true);
      } else {
        void runBatch(cmd.command);
      }
    },
    [runBatch]
  );

  const handleTemplateExecute = useCallback(
    (resolved: string, mode: TemplateExecutionMode) => {
      setTemplateOpen(false);
      setTemplateCmd(null);
      if (mode === "batch") {
        void runBatch(resolved);
      } else {
        runSingle(resolved);
      }
    },
    [runBatch, runSingle]
  );

  const handleEdit = useCallback((cmd: ScopedCommandItem) => {
    setEditingCmd(cmd);
    setEditOpen(true);
  }, []);

  const handleCreate = useCallback(() => {
    setEditingCmd(null);
    setEditOpen(true);
  }, []);

  const handleEditSubmit = useCallback(
    async (values: {
      name: string;
      description: string;
      group: string;
      command: string;
      isTemplate: boolean;
    }) => {
      if (!values.name || !values.command) {
        message.warning("名称和命令内容不能为空。");
        return;
      }
      try {
        await upsert({
          id: editingCmd?.id,
          name: values.name,
          description: values.description || undefined,
          group: values.group,
          command: values.command,
          isTemplate: values.isTemplate,
          workspaceId:
            editingCmd?.scope === "workspace"
              ? editingCmd.workspaceId
              : activeScope === "local"
                ? undefined
                : activeScope,
        });
        message.success(editingCmd ? "命令已更新" : "命令已添加");
        setEditOpen(false);
      } catch (error) {
        message.error(`保存命令失败：${String(error)}`);
      }
    },
    [activeScope, editingCmd, upsert]
  );

  const handleRemove = useCallback(
    async (cmd: ScopedCommandItem) => {
      const ok = await remove(cmd);
      if (ok) {
        message.success("已删除");
      } else {
        message.error("删除命令失败");
      }
    },
    [remove]
  );

  return (
    <div className="cc-pane">
      {/* ── Scope tabs ────────────────────────────────── */}
      <div className="cc-header">
        <div className="cc-scope-bar">
          <button
            type="button"
            className={`cc-scope-tab ${activeScope === "local" ? "active" : ""}`}
            onClick={() => setActiveScope("local")}
          >
            本 地
          </button>
          {workspaces.map((ws) => (
            <button
              key={ws.id}
              type="button"
              className={`cc-scope-tab ${activeScope === ws.id ? "active" : ""}`}
              onClick={() => setActiveScope(ws.id)}
            >
              {ws.displayName || ws.workspaceName}
            </button>
          ))}
        </div>

        {/* ── Batch targets ─────────────────────────── */}
        <div className="cc-batch-bar">
          <span className="cc-batch-label">
            <i className="ri-stack-line" aria-hidden="true" />
            批量目标
            <Tag style={{ margin: 0 }}>{targetConnectionIds.length}</Tag>
          </span>
          <TreeSelect
            treeData={targetTree}
            value={targetConnectionIds}
            onChange={(v) => setTargetSelection(v as string[])}
            treeCheckable
            showCheckedStrategy={TreeSelect.SHOW_CHILD}
            treeNodeFilterProp="title"
            showSearch
            allowClear
            maxTagCount="responsive"
            size="small"
            style={{ flex: 1, minWidth: 200 }}
            placeholder="选择批量执行的目标连接（可按分组勾选，未连接的会自动连接）"
          />
          <button
            type="button"
            className="cc-batch-reset"
            onClick={() => setTargetSelection(null)}
            title="重置为当前打开的标签页"
          >
            打开的标签页
          </button>
        </div>

        {/* ── Toolbar ───────────────────────────────── */}
        <div className="cc-toolbar">
          <span className="cc-toolbar-title">
            <i className="ri-code-box-line" aria-hidden="true" />
            命令库 · {scopeLabel}
            <span className="cc-toolbar-count">{filteredCommands.length}</span>
          </span>
          <Space size={6} className="cc-toolbar-actions">
            <Select
              allowClear
              placeholder="分组"
              value={groupFilter}
              onChange={setGroupFilter}
              options={groupOptions}
              size="small"
              style={{ width: 110 }}
            />
            <Input
              allowClear
              placeholder="搜索"
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              size="small"
              style={{ width: 130 }}
              prefix={
                <i
                  className="ri-search-line"
                  style={{ fontSize: 12, color: "var(--t3)" }}
                />
              }
            />
            <button
              type="button"
              className="cc-add-btn"
              onClick={handleCreate}
              title="新建命令"
            >
              <i className="ri-add-line" aria-hidden="true" />
            </button>
          </Space>
        </div>
      </div>

      {/* ── Command list ──────────────────────────────── */}
      <div className="cc-body">
        <CommandList
          groups={commandGroups}
          loading={loading}
          emptyLabel={scopeLabel}
          canExecuteSingle={!!connection && connected}
          batchRunning={batchRunning}
          onRun={handleRun}
          onRunBatch={handleRunBatch}
          onEdit={handleEdit}
          onRemove={(cmd) => void handleRemove(cmd)}
        />
      </div>

      {/* ── Overlays ──────────────────────────────────── */}
      <CommandEditModal
        open={editOpen}
        scopeLabel={scopeLabel}
        editingCommand={editingCmd}
        onSubmit={(v) => void handleEditSubmit(v)}
        onCancel={() => setEditOpen(false)}
      />

      <TemplateParamDrawer
        command={templateCmd}
        mode={templateMode}
        batchTargetCount={targetConnectionIds.length}
        open={templateOpen}
        onExecute={handleTemplateExecute}
        onClose={() => {
          setTemplateOpen(false);
          setTemplateCmd(null);
        }}
      />

      <BatchResultDrawer
        result={batchResult}
        connections={connections}
        open={batchResultOpen}
        onClose={() => setBatchResultOpen(false)}
      />
    </div>
  );
};
