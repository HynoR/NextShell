import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Form, Input, InputNumber, Modal, Select, Switch, Tooltip, message } from "antd";
import type { ConnectionProfile, ConnectionImportEntry, SshKeyProfile, ProxyProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { SshKeyManagerPanel } from "./SshKeyManagerPanel";
import { ProxyManagerPanel } from "./ProxyManagerPanel";
import { ConnectionImportModal } from "./ConnectionImportModal";

type ManagerTab = "connections" | "keys" | "proxies";

interface ImportPreviewBatch {
  fileName: string;
  entries: ConnectionImportEntry[];
}

interface ConnectionManagerModalProps {
  open: boolean;
  focusConnectionId?: string;
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  proxies: ProxyProfile[];
  onClose: () => void;
  onConnectionSaved: (payload: ConnectionUpsertInput) => Promise<void>;
  onConnectConnection: (connectionId: string) => Promise<void>;
  onConnectionRemoved: (connectionId: string) => Promise<void>;
  onConnectionsImported: () => Promise<void>;
  onReloadSshKeys: () => Promise<void>;
  onReloadProxies: () => Promise<void>;
}

const sanitizeOptionalText = (value: string | undefined): string | undefined => {
  if (typeof value !== "string") {
    return undefined;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
};

const sanitizeTextArray = (values: string[] | undefined): string[] => {
  return (values ?? [])
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
};

/* ── Custom tree types ──────────────────────────────────── */

interface MgrGroupNode {
  type: "group";
  key: string;
  label: string;
  children: MgrTreeNode[];
}

interface MgrLeafNode {
  type: "leaf";
  connection: ConnectionProfile;
}

type MgrTreeNode = MgrGroupNode | MgrLeafNode;

const buildManagerTree = (connections: ConnectionProfile[], keyword: string): MgrGroupNode => {
  const lower = keyword.toLowerCase().trim();
  const root: MgrGroupNode = { type: "group", key: "root", label: "全部连接", children: [] };

  const ensureGroup = (path: string[]): MgrGroupNode => {
    let pointer = root;
    const segments: string[] = [];
    for (const part of path) {
      segments.push(part);
      const key = `mgr-group:${segments.join("/")}`;
      let next = pointer.children.find(
        (n): n is MgrGroupNode => n.type === "group" && n.key === key
      );
      if (!next) {
        next = { type: "group", key, label: part, children: [] };
        pointer.children.push(next);
      }
      pointer = next;
    }
    return pointer;
  };

  for (const connection of connections) {
    const text = `${connection.name} ${connection.host} ${connection.groupPath.join("/")} ${connection.tags.join(" ")}`.toLowerCase();
    if (lower && !text.includes(lower)) continue;
    ensureGroup(connection.groupPath).children.push({ type: "leaf", connection });
  }

  return root;
};

const countMgrLeaves = (node: MgrGroupNode): number => {
  let count = 0;
  for (const child of node.children) {
    if (child.type === "leaf") count += 1;
    else count += countMgrLeaves(child);
  }
  return count;
};

/* ── Custom tree sub-components ─────────────────────────── */

const MgrGroupRow = ({
  node,
  expanded,
  onToggle
}: {
  node: MgrGroupNode;
  expanded: boolean;
  onToggle: () => void;
}) => (
  <button type="button" className="mgr-group-row" onClick={onToggle}>
    <i
      className={expanded ? "ri-arrow-down-s-line" : "ri-arrow-right-s-line"}
      aria-hidden="true"
    />
    <i className="ri-folder-3-line" aria-hidden="true" />
    <span className="mgr-group-label">{node.label}</span>
    <span className="mgr-group-count">{countMgrLeaves(node)}</span>
  </button>
);

const MgrServerRow = ({
  connection,
  isSelected,
  isExportSelected,
  onSelect,
  onToggleExportSelect
}: {
  connection: ConnectionProfile;
  isSelected: boolean;
  isExportSelected: boolean;
  onSelect: () => void;
  onToggleExportSelect: () => void;
}) => (
  <div className={`mgr-server-row${isSelected ? " selected" : ""}`}>
    <button
      type="button"
      className={`mgr-server-check-btn${isExportSelected ? " checked" : ""}`}
      onClick={onToggleExportSelect}
      title={isExportSelected ? "取消导出选择" : "加入导出选择"}
      aria-label={isExportSelected ? "取消导出选择" : "加入导出选择"}
    >
      <i
        className={isExportSelected ? "ri-checkbox-circle-fill" : "ri-checkbox-blank-circle-line"}
        aria-hidden="true"
      />
    </button>
    <button
      type="button"
      className="mgr-server-select-btn"
      onClick={onSelect}
      title={`${connection.name} (${connection.host}:${connection.port})`}
    >
      <span className="mgr-server-status" />
      {connection.favorite ? (
        <i className="ri-star-fill mgr-server-star" aria-hidden="true" />
      ) : null}
      <span className="mgr-server-name">{connection.name}</span>
      <span className="mgr-server-host">{connection.host}</span>
    </button>
  </div>
);

const MgrTreeGroup = ({
  node,
  depth,
  expanded,
  toggleExpanded,
  selectedConnectionId,
  selectedExportIds,
  onSelect,
  onToggleExportSelect
}: {
  node: MgrGroupNode;
  depth: number;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
  selectedConnectionId: string | undefined;
  selectedExportIds: Set<string>;
  onSelect: (id: string) => void;
  onToggleExportSelect: (id: string) => void;
}) => {
  const isExpanded = expanded.has(node.key);
  return (
    <div className="mgr-group">
      {depth > 0 && (
        <MgrGroupRow
          node={node}
          expanded={isExpanded}
          onToggle={() => toggleExpanded(node.key)}
        />
      )}
      {(depth === 0 || isExpanded) && (
        <div className={`mgr-group-children${depth > 0 ? " mgr-group-children--indented" : ""}`}>
          {node.children.map((child) =>
            child.type === "group" ? (
              <MgrTreeGroup
                key={child.key}
                node={child}
                depth={depth + 1}
                expanded={expanded}
                toggleExpanded={toggleExpanded}
                selectedConnectionId={selectedConnectionId}
                selectedExportIds={selectedExportIds}
                onSelect={onSelect}
                onToggleExportSelect={onToggleExportSelect}
              />
            ) : (
              <MgrServerRow
                key={child.connection.id}
                connection={child.connection}
                isSelected={child.connection.id === selectedConnectionId}
                isExportSelected={selectedExportIds.has(child.connection.id)}
                onSelect={() => onSelect(child.connection.id)}
                onToggleExportSelect={() => onToggleExportSelect(child.connection.id)}
              />
            )
          )}
        </div>
      )}
    </div>
  );
};

/* ── Constants ──────────────────────────────────────────── */

const DEFAULT_VALUES = {
  port: 22,
  authType: "password" as const,
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8" as const,
  backspaceMode: "ascii-backspace" as const,
  deleteMode: "vt220-delete" as const,
  groupPath: ["server"],
  tags: [],
  favorite: false,
  monitorSession: true
};

/* ── Main component ─────────────────────────────────────── */

export const ConnectionManagerModal = ({
  open,
  focusConnectionId,
  connections,
  sshKeys,
  proxies,
  onClose,
  onConnectionSaved,
  onConnectConnection,
  onConnectionRemoved,
  onConnectionsImported,
  onReloadSshKeys,
  onReloadProxies
}: ConnectionManagerModalProps) => {
  const [activeTab, setActiveTab] = useState<ManagerTab>("connections");
  const [mode, setMode] = useState<"idle" | "new" | "edit">("idle");
  const [keyword, setKeyword] = useState("");
  const [selectedConnectionId, setSelectedConnectionId] = useState<string>();
  const [selectedExportIds, setSelectedExportIds] = useState<Set<string>>(new Set());
  const [expanded, setExpanded] = useState<Set<string>>(new Set(["root"]));
  const [saving, setSaving] = useState(false);
  const [connectingFromForm, setConnectingFromForm] = useState(false);
  const [importingPreview, setImportingPreview] = useState(false);
  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importPreviewQueue, setImportPreviewQueue] = useState<ImportPreviewBatch[]>([]);
  const [importQueueIndex, setImportQueueIndex] = useState(0);
  const [form] = Form.useForm<ConnectionUpsertInput>();
  const authType = Form.useWatch("authType", form);
  const appliedFocusConnectionIdRef = useRef<string | undefined>(undefined);

  const tree = useMemo(
    () => buildManagerTree(connections, keyword),
    [connections, keyword]
  );

  useEffect(() => {
    if (!open) return;
    form.resetFields();
    form.setFieldsValue(DEFAULT_VALUES);
    setSelectedConnectionId(undefined);
    setSelectedExportIds(new Set());
    setExpanded(new Set(["root"]));
    setMode("idle");
    setKeyword("");
    setActiveTab("connections");
    setImportingPreview(false);
    setImportModalOpen(false);
    setImportPreviewQueue([]);
    setImportQueueIndex(0);
  }, [form, open]);

  // Auto-expand all groups when keyword is set
  useMemo(() => {
    if (keyword.trim()) {
      const keys = new Set<string>(["root"]);
      const walk = (node: MgrGroupNode) => {
        keys.add(node.key);
        for (const child of node.children) {
          if (child.type === "group") walk(child);
        }
      };
      walk(tree);
      setExpanded(keys);
    }
  }, [keyword, tree]);

  useEffect(() => {
    if (!open || !authType) return;

    if (authType === "agent") {
      form.setFieldsValue({
        password: undefined,
        sshKeyId: undefined
      });
      return;
    }

    if (authType === "password") {
      form.setFieldValue("sshKeyId", undefined);
      return;
    }

    // privateKey — clear password
    form.setFieldValue("password", undefined);
  }, [authType, form, open]);

  const selectedConnection = useMemo(
    () => connections.find((c) => c.id === selectedConnectionId),
    [connections, selectedConnectionId]
  );

  const selectedExportCount = selectedExportIds.size;
  const currentImportBatch = importPreviewQueue[importQueueIndex];

  useEffect(() => {
    if (selectedExportIds.size === 0) return;
    const validIds = new Set(connections.map((connection) => connection.id));
    setSelectedExportIds((prev) => {
      const next = new Set(Array.from(prev).filter((id) => validIds.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [connections, selectedExportIds.size]);

  const applyConnectionToForm = useCallback((connection: ConnectionProfile) => {
    form.setFieldsValue({
      id: connection.id,
      name: connection.name,
      host: connection.host,
      port: connection.port,
      username: connection.username,
      authType: connection.authType,
      sshKeyId: connection.sshKeyId,
      hostFingerprint: connection.hostFingerprint,
      strictHostKeyChecking: connection.strictHostKeyChecking,
      proxyId: connection.proxyId,
      terminalEncoding: connection.terminalEncoding,
      backspaceMode: connection.backspaceMode,
      deleteMode: connection.deleteMode,
      groupPath: connection.groupPath,
      tags: connection.tags,
      notes: connection.notes,
      favorite: connection.favorite,
      monitorSession: connection.monitorSession,
      password: undefined
    });
  }, [form]);

  const handleNew = useCallback(() => {
    setSelectedConnectionId(undefined);
    form.resetFields();
    form.setFieldsValue(DEFAULT_VALUES);
    setMode("new");
  }, [form]);

  const handleSelect = useCallback((connectionId: string) => {
    const connection = connections.find((c) => c.id === connectionId);
    if (!connection) return;
    const expandedKeys = new Set<string>(["root"]);
    const segments: string[] = [];
    for (const part of connection.groupPath) {
      segments.push(part);
      expandedKeys.add(`mgr-group:${segments.join("/")}`);
    }
    setExpanded(expandedKeys);
    setSelectedConnectionId(connectionId);
    applyConnectionToForm(connection);
    setMode("edit");
  }, [connections, applyConnectionToForm]);

  useEffect(() => {
    if (!open) {
      appliedFocusConnectionIdRef.current = undefined;
      return;
    }

    if (!focusConnectionId || appliedFocusConnectionIdRef.current === focusConnectionId) {
      return;
    }

    setActiveTab("connections");
    setKeyword("");
    handleSelect(focusConnectionId);
    appliedFocusConnectionIdRef.current = focusConnectionId;
  }, [focusConnectionId, handleSelect, open]);

  const handleReset = useCallback(() => {
    if (selectedConnection) {
      applyConnectionToForm(selectedConnection);
    } else {
      form.resetFields();
      form.setFieldsValue(DEFAULT_VALUES);
    }
  }, [applyConnectionToForm, form, selectedConnection]);

  const handleDelete = useCallback(() => {
    if (!selectedConnectionId) return;
    Modal.confirm({
      title: "确认删除",
      content: `删除「${selectedConnection?.name ?? ""}」后会关闭相关会话，是否继续？`,
      okText: "删除",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        await onConnectionRemoved(selectedConnectionId);
        setSelectedConnectionId(undefined);
        form.resetFields();
        form.setFieldsValue(DEFAULT_VALUES);
        setMode("idle");
      }
    });
  }, [form, onConnectionRemoved, selectedConnection, selectedConnectionId]);

  const handleCloseForm = useCallback(() => {
    setMode("idle");
    setSelectedConnectionId(undefined);
  }, []);

  const saveConnection = useCallback(async (values: ConnectionUpsertInput): Promise<string | undefined> => {
    const password = sanitizeOptionalText(values.password);
    const hostFingerprint = sanitizeOptionalText(values.hostFingerprint);
    const groupPath = sanitizeTextArray(values.groupPath);
    const tags = sanitizeTextArray(values.tags);
    const notes = sanitizeOptionalText(values.notes);
    const port = Number(values.port);
    const terminalEncoding = values.terminalEncoding ?? "utf-8";
    const backspaceMode = values.backspaceMode ?? "ascii-backspace";
    const deleteMode = values.deleteMode ?? "vt220-delete";

    if (values.authType === "privateKey" && !values.sshKeyId) {
      message.error("私钥认证需要选择一个 SSH 密钥。");
      return undefined;
    }

    if (values.strictHostKeyChecking && !hostFingerprint) {
      message.error("启用严格主机校验时必须填写主机指纹。");
      return undefined;
    }

    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      message.error("端口必须是 1-65535 的整数。");
      return undefined;
    }

    setSaving(true);
    try {
      const payload: ConnectionUpsertInput = {
        id: values.id ?? selectedConnectionId ?? crypto.randomUUID(),
        name: values.name.trim(),
        host: values.host.trim(),
        port,
        username: (values.username ?? "").trim(),
        authType: values.authType,
        password,
        sshKeyId: values.authType === "privateKey" ? values.sshKeyId : undefined,
        hostFingerprint,
        strictHostKeyChecking: values.strictHostKeyChecking ?? false,
        proxyId: values.proxyId,
        terminalEncoding,
        backspaceMode,
        deleteMode,
        tags,
        groupPath: groupPath.length > 0 ? groupPath : ["server"],
        notes,
        favorite: values.favorite ?? false,
        monitorSession: values.monitorSession ?? false
      };
      await onConnectionSaved(payload);
      message.success(selectedConnectionId ? "连接已更新" : "连接已创建");
      setSelectedConnectionId(payload.id);
      setMode("edit");
      form.setFieldsValue({
        password: undefined
      });
      return payload.id;
    } catch (error) {
      const reason = error instanceof Error ? error.message : "保存失败";
      message.error(reason);
      return undefined;
    } finally {
      setSaving(false);
    }
  }, [form, onConnectionSaved, selectedConnectionId]);

  const handleSaveAndConnect = useCallback(async () => {
    if (saving || connectingFromForm) {
      return;
    }

    let values: ConnectionUpsertInput;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }

    const connectionId = await saveConnection(values);
    if (!connectionId) {
      return;
    }

    setConnectingFromForm(true);
    try {
      await onConnectConnection(connectionId);
    } finally {
      setConnectingFromForm(false);
    }
  }, [connectingFromForm, form, onConnectConnection, saveConnection, saving]);

  const toggleExpanded = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const handleExportAll = useCallback(async () => {
    if (connections.length === 0) return;
    const exportIds = connections.map((connection) => connection.id);
    try {
      const result = await window.nextshell.connection.exportToFile({
        connectionIds: exportIds
      });
      if (result.ok) {
        message.success(`已导出 ${exportIds.length} 个连接`);
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : "未知错误";
      message.error(`导出失败：${reason}`);
    }
  }, [connections]);

  const handleToggleExportSelect = useCallback((connectionId: string) => {
    setSelectedExportIds((prev) => {
      const next = new Set(prev);
      if (next.has(connectionId)) {
        next.delete(connectionId);
      } else {
        next.add(connectionId);
      }
      return next;
    });
  }, []);

  const handleExportSelected = useCallback(async () => {
    if (selectedExportIds.size === 0) return;
    const exportIds = connections
      .map((connection) => connection.id)
      .filter((id) => selectedExportIds.has(id));
    if (exportIds.length === 0) return;

    try {
      const result = await window.nextshell.connection.exportToFile({
        connectionIds: exportIds
      });
      if (result.ok) {
        message.success(`已导出 ${exportIds.length} 个连接`);
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : "未知错误";
      message.error(`导出失败：${reason}`);
    }
  }, [connections, selectedExportIds]);

  const resetImportFlow = useCallback(() => {
    setImportModalOpen(false);
    setImportPreviewQueue([]);
    setImportQueueIndex(0);
  }, []);

  const getFileName = useCallback((filePath: string): string => {
    const normalized = filePath.replace(/\\/g, "/");
    const splitIndex = normalized.lastIndexOf("/");
    if (splitIndex < 0) {
      return normalized;
    }
    return normalized.slice(splitIndex + 1);
  }, []);

  const handleImport = useCallback(async () => {
    if (importingPreview) return;
    try {
      setImportingPreview(true);
      const dialogResult = await window.nextshell.dialog.openFiles({
        title: "选择导入文件",
        multi: true
      });
      if (dialogResult.canceled || dialogResult.filePaths.length === 0) return;

      const previewResults = await Promise.all(dialogResult.filePaths.map(async (filePath) => {
        const fileName = getFileName(filePath);
        try {
          const entries = await window.nextshell.connection.importPreview({ filePath });
          if (entries.length === 0) {
            return { fileName, warning: `${fileName}：文件中没有可导入的连接` as string };
          }
          return { fileName, entries } as { fileName: string; entries: ConnectionImportEntry[] };
        } catch (error) {
          const reason = error instanceof Error ? error.message : "未知错误";
          return { fileName, warning: `${fileName}：${reason}` as string };
        }
      }));

      const queue: ImportPreviewBatch[] = [];
      const warnings: string[] = [];
      previewResults.forEach((item) => {
        if ("warning" in item) {
          warnings.push(item.warning);
          return;
        }
        queue.push({ fileName: item.fileName, entries: item.entries });
      });

      if (warnings.length > 0) {
        warnings.forEach((item) => {
          message.warning(item);
        });
      }

      if (queue.length === 0) {
        message.warning("未找到可导入的连接文件");
        return;
      }

      setImportPreviewQueue(queue);
      setImportQueueIndex(0);
      setImportModalOpen(true);
      if (queue.length > 1) {
        message.info(`已加载 ${queue.length} 个 JSON，将按文件逐个导入`);
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : "未知错误";
      message.error(`导入预览失败：${reason}`);
    } finally {
      setImportingPreview(false);
    }
  }, [getFileName, importingPreview]);

  const handleImportBatchImported = useCallback(async () => {
    await onConnectionsImported();
    const nextIndex = importQueueIndex + 1;
    if (nextIndex < importPreviewQueue.length) {
      setImportQueueIndex(nextIndex);
      const nextBatch = importPreviewQueue[nextIndex];
      message.info(`继续导入 ${nextBatch?.fileName ?? "下一个文件"} (${nextIndex + 1}/${importPreviewQueue.length})`);
      return;
    }

    resetImportFlow();
  }, [importPreviewQueue, importQueueIndex, onConnectionsImported, resetImportFlow]);

  return (
    <>
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={900}
      style={{ top: 48 }}
      styles={{
        header: { padding: "13px 18px", marginBottom: 0, borderBottom: "1px solid var(--border)" },
        body: { padding: 0, overflow: "hidden" },
      }}
      title={<span className="mgr-modal-title">连接管理器</span>}
      destroyOnHidden
    >
      {/* ── Tab bar ───────────────────────────── */}
      <div className="mgr-tab-bar">
        <button
          type="button"
          className={`mgr-tab${activeTab === "connections" ? " mgr-tab--active" : ""}`}
          onClick={() => setActiveTab("connections")}
        >
          <i className="ri-server-line" aria-hidden="true" />
          连接
        </button>
        <button
          type="button"
          className={`mgr-tab${activeTab === "keys" ? " mgr-tab--active" : ""}`}
          onClick={() => setActiveTab("keys")}
        >
          <i className="ri-key-2-line" aria-hidden="true" />
          密钥
        </button>
        <button
          type="button"
          className={`mgr-tab${activeTab === "proxies" ? " mgr-tab--active" : ""}`}
          onClick={() => setActiveTab("proxies")}
        >
          <i className="ri-shield-line" aria-hidden="true" />
          代理
        </button>
      </div>

      {/* ── Connections tab ───────────────────── */}
      {activeTab === "connections" && (
      <div className={mode !== "idle" ? "grid grid-cols-[230px_1fr] h-[540px] overflow-hidden" : "h-[540px] overflow-hidden"}>

        {/* ── Sidebar ─────────────────────────── */}
        <div className={`flex flex-col bg-[var(--bg-elevated)] overflow-hidden${mode !== "idle" ? " border-r border-[var(--border)]" : ""}`}>

          {/* Sidebar header */}
          <div className="mgr-sidebar-head">
            <div className="mgr-sidebar-title-row">
              <span className="mgr-sidebar-title">全部连接</span>
              {connections.length > 0 && (
                <span className="mgr-count-badge">{connections.length}</span>
              )}
            </div>
            <button
              className="mgr-new-btn"
              onClick={handleNew}
              title="新建连接"
            >
              <i className="ri-add-line" aria-hidden="true" />
            </button>
          </div>

          {/* Search */}
          <div className="mgr-search-row">
            <i className="ri-search-line mgr-search-icon" aria-hidden="true" />
            <input
              className="mgr-search"
              placeholder="搜索连接..."
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
            />
            {keyword && (
              <button
                className="mgr-search-clear"
                onClick={() => setKeyword("")}
                title="清除"
              >
                <i className="ri-close-line" aria-hidden="true" />
              </button>
            )}
          </div>

          {/* Tree */}
          <div className="mgr-tree-wrap">
            {tree.children.length === 0 ? (
              <div className="mgr-tree-empty">
                {keyword ? (
                  <>
                    <i className="ri-search-line" aria-hidden="true" />
                    <span>未找到匹配连接</span>
                  </>
                ) : (
                  <>
                    <i className="ri-server-line" aria-hidden="true" />
                    <span>暂无连接</span>
                  </>
                )}
              </div>
            ) : (
              <MgrTreeGroup
                node={tree}
                depth={0}
                expanded={expanded}
                toggleExpanded={toggleExpanded}
                selectedConnectionId={selectedConnectionId}
                selectedExportIds={selectedExportIds}
                onSelect={handleSelect}
                onToggleExportSelect={handleToggleExportSelect}
              />
            )}
          </div>

          {/* Footer */}
          <div className="mgr-sidebar-footer">
            <span className="mgr-count">
              {connections.length} 个连接
              {selectedExportCount > 0 ? ` · 已选 ${selectedExportCount}` : ""}
            </span>
            <div className="mgr-sidebar-footer-actions">
              <Tooltip title="导入连接">
                <button type="button" className="mgr-action-btn" onClick={handleImport} disabled={importingPreview}>
                  <i className={importingPreview ? "ri-loader-4-line ri-spin" : "ri-upload-2-line"} />
                </button>
              </Tooltip>
              <Tooltip title="导出选中连接">
                <button
                  type="button"
                  className="mgr-action-btn"
                  onClick={handleExportSelected}
                  disabled={selectedExportCount === 0}
                >
                  <i className="ri-download-cloud-2-line" />
                </button>
              </Tooltip>
              <Tooltip title="导出所有连接">
                <button type="button" className="mgr-action-btn" onClick={handleExportAll}
                  disabled={connections.length === 0}>
                  <i className="ri-download-2-line" />
                </button>
              </Tooltip>
            </div>
          </div>
        </div>

        {/* ── Right panel ─────────────────────── */}
        {mode !== "idle" && (
          <div className="flex flex-col overflow-hidden">

            {/* Form header */}
            <div className="mgr-form-header">
              <div>
                <div className="mgr-form-title">
                  {mode === "new" ? "新建连接" : (selectedConnection?.name ?? "编辑连接")}
                </div>
                {mode === "edit" && selectedConnection ? (
                  <div className="mgr-form-subtitle">
                    {selectedConnection.username.trim()
                      ? `${selectedConnection.username}@${selectedConnection.host}:${selectedConnection.port}`
                      : `${selectedConnection.host}:${selectedConnection.port}`}
                  </div>
                ) : (
                  <div className="mgr-form-subtitle">填写以下信息后点击保存</div>
                )}
              </div>
              <div className="mgr-form-header-right">
                <span className="mgr-ssh-badge">SSH</span>
                <button
                  type="button"
                  className="mgr-connect-btn"
                  onClick={() => void handleSaveAndConnect()}
                  disabled={saving || connectingFromForm}
                  title="保存并连接"
                >
                  {connectingFromForm ? (
                    <i className="ri-loader-4-line mgr-form-header-icon-spin" aria-hidden="true" />
                  ) : (
                    <i className="ri-terminal-box-line" aria-hidden="true" />
                  )}
                  连接
                </button>
                {mode === "edit" ? (
                  <Tooltip title="删除连接">
                    <button
                      type="button"
                      className="mgr-form-header-icon-btn mgr-form-header-icon-btn--danger"
                      onClick={handleDelete}
                      aria-label="删除连接"
                    >
                      <i className="ri-trash-line" aria-hidden="true" />
                    </button>
                  </Tooltip>
                ) : (
                  <Tooltip title="取消">
                    <button
                      type="button"
                      className="mgr-form-header-icon-btn"
                      onClick={() => setMode("idle")}
                      aria-label="取消"
                    >
                      <i className="ri-arrow-left-line" aria-hidden="true" />
                    </button>
                  </Tooltip>
                )}
                <Tooltip title="重置">
                  <button
                    type="button"
                    className="mgr-form-header-icon-btn"
                    onClick={handleReset}
                    aria-label="重置"
                  >
                    <i className="ri-refresh-line" aria-hidden="true" />
                  </button>
                </Tooltip>
                <Tooltip title="保存连接">
                  <button
                    type="button"
                    className="mgr-form-header-icon-btn mgr-form-header-icon-btn--primary"
                    onClick={() => form.submit()}
                    disabled={saving || connectingFromForm}
                    aria-label="保存连接"
                  >
                    {saving ? (
                      <i className="ri-loader-4-line mgr-form-header-icon-spin" aria-hidden="true" />
                    ) : (
                      <i className="ri-save-line" aria-hidden="true" />
                    )}
                  </button>
                </Tooltip>
                <Tooltip title="收起表单">
                  <button
                    type="button"
                    className="mgr-form-close-btn"
                    onClick={handleCloseForm}
                    aria-label="收起表单"
                  >
                    <i className="ri-close-line" aria-hidden="true" />
                  </button>
                </Tooltip>
              </div>
            </div>

            <Form
              form={form}
              layout="vertical"
              requiredMark={false}
              className="mgr-form"
              initialValues={DEFAULT_VALUES}
              onFinish={async (values) => {
                await saveConnection(values);
              }}
            >
              {/* Section: 连接信息 */}
              <div className="mgr-section-label">连接信息</div>

              <Form.Item label="名称" name="name" rules={[{ required: true, message: "请输入连接名称" }]}>
                <Input placeholder="我的服务器" />
              </Form.Item>

              <div className="flex gap-3 items-start">
                <Form.Item
                  label="Host / IP"
                  name="host"
                  rules={[{ required: true, message: "请输入主机地址" }]}
                  style={{ flex: 1 }}
                >
                  <Input placeholder="192.168.1.1 或 example.com" style={{ fontFamily: "var(--mono)" }} />
                </Form.Item>
                <Form.Item
                  label="端口"
                  name="port"
                  rules={[{ required: true, message: "请输入端口" }]}
                  className="w-[90px] shrink-0"
                >
                  <InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} />
                </Form.Item>
              </div>

              {/* Section: 认证 */}
              <div className="mgr-section-label mgr-section-gap">认证</div>

              <div className="flex gap-3 items-start">
                <Form.Item
                  label="用户名"
                  name="username"
                  className="flex-1"
                >
                  <Input placeholder="root" />
                </Form.Item>
                <Form.Item
                  label="认证方式"
                  name="authType"
                  rules={[{ required: true }]}
                  className="w-[150px] shrink-0"
                >
                  <Select
                    options={[
                      { label: "密码", value: "password" },
                      { label: "私钥文件", value: "privateKey" },
                      { label: "SSH Agent", value: "agent" }
                    ]}
                  />
                </Form.Item>
              </div>

              {authType === "privateKey" ? (
                <Form.Item
                  label="SSH 密钥"
                  name="sshKeyId"
                  rules={[{ required: true, message: "请选择一个 SSH 密钥" }]}
                >
                  <Select
                    placeholder="选择密钥..."
                    allowClear
                    options={sshKeys.map((k) => ({ label: k.name, value: k.id }))}
                    notFoundContent={
                      <div style={{ textAlign: "center", padding: "8px 0", color: "var(--text-muted)" }}>
                        暂无密钥，请先在「密钥管理」中添加
                      </div>
                    }
                  />
                </Form.Item>
              ) : null}

              {authType === "password" ? (
                <Form.Item
                  label="密码"
                  name="password"
                  preserve={false}
                >
                  <Input.Password placeholder="输入密码（留空则不更新）" />
                </Form.Item>
              ) : null}

              <div className="mgr-section-label mgr-section-gap">网络代理</div>

              <Form.Item
                label="代理"
                name="proxyId"
              >
                <Select
                  placeholder="直连（不使用代理）"
                  allowClear
                  options={proxies.map((p) => ({
                    label: `${p.name} (${p.proxyType.toUpperCase()} ${p.host}:${p.port})`,
                    value: p.id
                  }))}
                  notFoundContent={
                    <div style={{ textAlign: "center", padding: "8px 0", color: "var(--text-muted)" }}>
                      暂无代理，请先在「代理管理」中添加
                    </div>
                  }
                />
              </Form.Item>

              {/* Section: 分组 & 标签 */}
              <div className="mgr-section-label mgr-section-gap">分组 & 标签</div>

              <div className="flex gap-3 items-start">
                <Form.Item label="分组路径" name="groupPath" className="flex-1">
                  <Select
                    mode="tags"
                    tokenSeparators={[","]}
                    placeholder="server / production"
                  />
                </Form.Item>
                <Form.Item label="标签" name="tags" className="flex-1">
                  <Select
                    mode="tags"
                    tokenSeparators={[","]}
                    placeholder="web, linux, prod"
                  />
                </Form.Item>
              </div>

              <div className="flex gap-3 items-start">
                <Form.Item label="备注" name="notes" className="flex-1 !mb-0">
                  <Input.TextArea rows={2} placeholder="可选备注信息..." className="mgr-textarea" />
                </Form.Item>
                <Form.Item
                  label="收藏"
                  name="favorite"
                  valuePropName="checked"
                  className="shrink-0 !mb-0"
                >
                  <Switch size="small" />
                </Form.Item>
              </div>

              <div className="mgr-section-label mgr-section-gap">安全</div>

              <div className="flex gap-3 items-start">
                <Form.Item
                  label="主机指纹（SHA256:... / md5:aa:bb... / hex）"
                  name="hostFingerprint"
                  className="flex-1"
                >
                  <Input placeholder="SHA256:xxxxxxxxxxxxxxxxxxxx" className="mgr-mono-input" />
                </Form.Item>
                <Form.Item
                  label="严格主机校验"
                  name="strictHostKeyChecking"
                  valuePropName="checked"
                  className="shrink-0 !mb-0"
                >
                  <Switch size="small" />
                </Form.Item>
              </div>

              <div className="mgr-section-label mgr-section-gap">高级</div>

              <div className="flex gap-3 items-start">
                <Form.Item
                  label="Monitor Session"
                  name="monitorSession"
                  valuePropName="checked"
                  className="shrink-0 !mb-0"
                >
                  <Switch size="small" />
                </Form.Item>
                <div className="mgr-monitor-hint">
                  启用后支持进程管理器和网络监控
                </div>
              </div>

              <Form.Item
                label="字符编码"
                name="terminalEncoding"
              >
                <Select
                  options={[
                    { label: "UTF-8", value: "utf-8" },
                    { label: "GB18030", value: "gb18030" },
                    { label: "GBK", value: "gbk" },
                    { label: "Big5", value: "big5" }
                  ]}
                />
              </Form.Item>

              <div className="mgr-section-label">按键序列（解决退格/删除键异常）</div>

              <div className="flex gap-3 items-start">
                <Form.Item
                  label="Backspace 退格键"
                  name="backspaceMode"
                  className="flex-1"
                >
                  <Select
                    options={[
                      { label: "ASCII - Backspace", value: "ascii-backspace" },
                      { label: "ASCII - Delete", value: "ascii-delete" }
                    ]}
                  />
                </Form.Item>
                <Form.Item
                  label="Delete 删除键"
                  name="deleteMode"
                  className="flex-1"
                >
                  <Select
                    options={[
                      { label: "VT220 - Delete", value: "vt220-delete" },
                      { label: "ASCII - Delete", value: "ascii-delete" },
                      { label: "ASCII - Backspace", value: "ascii-backspace" }
                    ]}
                  />
                </Form.Item>
              </div>

              <div className="mgr-form-subtitle">终端高级配置保存后需重连会话生效。</div>
            </Form>
          </div>
        )}
      </div>
      )}

      {/* ── SSH Keys tab ─────────────────────── */}
      {activeTab === "keys" && (
        <SshKeyManagerPanel sshKeys={sshKeys} onReload={onReloadSshKeys} />
      )}

      {/* ── Proxies tab ──────────────────────── */}
      {activeTab === "proxies" && (
        <ProxyManagerPanel proxies={proxies} onReload={onReloadProxies} />
      )}
    </Modal>

    <ConnectionImportModal
      open={importModalOpen}
      entries={currentImportBatch?.entries ?? []}
      existingConnections={connections}
      sourceName={currentImportBatch?.fileName}
      sourceProgress={importPreviewQueue.length > 1 ? `${importQueueIndex + 1}/${importPreviewQueue.length}` : undefined}
      onClose={resetImportFlow}
      onImported={handleImportBatchImported}
    />
    </>
  );
};
