import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, Form, Modal } from "antd";
import type { ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import {
  CONNECTION_ZONES,
  ZONE_DISPLAY_NAMES,
  ZONE_ORDER,
  extractZone,
  getSubPath,
  isValidZone
} from "@nextshell/shared";
import type { DragEndEvent, DragStartEvent } from "@dnd-kit/core";
import { SshKeyManagerPanel } from "./SshKeyManagerPanel";
import { ProxyManagerPanel } from "./ProxyManagerPanel";
import { CloudSyncManagerPanel } from "./CloudSyncManagerPanel";
import { ConnectionImportModal } from "./ConnectionImportModal";
import { RecycleBinSection } from "./settings-center";
import { ConnectionSidebar } from "./ConnectionManagerModal/components/ConnectionSidebar";
import { ConnectionFormPanel } from "./ConnectionManagerModal/components/ConnectionFormPanel";
import { DEFAULT_VALUES, FIELD_TAB_MAP, MANAGER_TABS } from "./ConnectionManagerModal/constants";
import { useConnectionExportActions } from "./ConnectionManagerModal/hooks/useConnectionExportActions";
import { useConnectionImportFlow } from "./ConnectionManagerModal/hooks/useConnectionImportFlow";
import { useConnectionPasswordReveal } from "./ConnectionManagerModal/hooks/useConnectionPasswordReveal";
import type {
  ImportPreviewBatch,
  ManagerTab,
  FormTab,
  MgrClipboard,
  MgrContextMenuState,
  MgrGroupNode,
  SortMode
} from "./ConnectionManagerModal/types";
import {
  buildManagerTree,
  collectFlatLeafIds,
  collectGroupLeafIds,
  groupKeyToPath,
  groupPathToSegments,
  sortMgrChildren,
  countMgrLeaves
} from "./ConnectionManagerModal/utils/tree";
import {
  toConnectionPayload,
  toQuickUpsertInput,
  type ConnectionFormValues
} from "./ConnectionManagerModal/utils/connectionForm";
import { formatErrorMessage } from "../utils/errorMessage";
import { promptModal } from "../utils/promptModal";

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
  onOpenLocalTerminal: () => void;
}

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
  onReloadProxies,
  onOpenLocalTerminal
}: ConnectionManagerModalProps) => {
  const { modal, message } = AntdApp.useApp();
  const [activeTab, setActiveTab] = useState<ManagerTab>("connections");
  const [mode, setMode] = useState<"idle" | "new" | "edit">("idle");
  const [keyword, setKeyword] = useState("");
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [primarySelectedId, setPrimarySelectedId] = useState<string>();
  const [selectionAnchorId, setSelectionAnchorId] = useState<string>();
  const [expanded, setExpanded] = useState<Set<string>>(new Set(["root"]));
  const [contextMenu, setContextMenu] = useState<MgrContextMenuState | null>(null);
  const [clipboard, setClipboard] = useState<MgrClipboard | null>(null);
  const [renamingId, setRenamingId] = useState<string>();
  const [emptyFolders, setEmptyFolders] = useState<string[]>([]);
  const [sortMode, setSortMode] = useState<SortMode>("name");
  const [formTab, setFormTab] = useState<FormTab>("basic");
  const [saving, setSaving] = useState(false);
  const [connectingFromForm, setConnectingFromForm] = useState(false);
  const [draggingConnection, setDraggingConnection] = useState<ConnectionProfile | null>(null);
  const [hasCloudWorkspaces, setHasCloudWorkspaces] = useState(false);
  const [form] = Form.useForm<ConnectionUpsertInput>();
  const authType = Form.useWatch("authType", form);
  const keepAliveSetting = Form.useWatch("keepAliveEnabled", form);
  const appliedFocusConnectionIdRef = useRef<string | undefined>(undefined);

  const tree = useMemo(
    () => sortMgrChildren(buildManagerTree(connections, keyword, emptyFolders), sortMode),
    [connections, emptyFolders, keyword, sortMode]
  );
  const hasVisibleConnections = useMemo(() => countMgrLeaves(tree) > 0, [tree]);
  const selectedConnection = useMemo(
    () => connections.find((connection) => connection.id === primarySelectedId),
    [connections, primarySelectedId]
  );
  const cutIds = useMemo(() => {
    if (!clipboard || clipboard.mode !== "cut") return new Set<string>();
    return new Set(clipboard.connectionIds);
  }, [clipboard]);

  const {
    currentImportBatch,
    handleImportBatchImported,
    handleImportFinalShell,
    handleImportNextShell,
    importingPreview,
    importModalOpen,
    importPreviewQueue,
    importQueueIndex,
    resetImportFlow
  } = useConnectionImportFlow({
    modal,
    message,
    onConnectionsImported
  });
  const {
    handleExportAll,
    handleExportSelected
  } = useConnectionExportActions({
    connections,
    selectedIds,
    modal,
    message
  });
  const {
    clearRevealConnectionPassword,
    handleRevealConnectionPassword,
    revealedLoginPassword,
    revealingLoginPassword
  } = useConnectionPasswordReveal({
    activeAuthType: authType,
    modal,
    message,
    primarySelectedId,
    selectedConnection
  });

  useEffect(() => {
    if (!open) return;
    form.resetFields();
    form.setFieldsValue(DEFAULT_VALUES);
    setPrimarySelectedId(undefined);
    setSelectedIds(new Set());
    setSelectionAnchorId(undefined);
    setExpanded(new Set(["root", ...ZONE_ORDER.map((zone) => `mgr-group:${zone}`)]));
    setMode("idle");
    setFormTab("basic");
    setKeyword("");
    setActiveTab("connections");
    setContextMenu(null);
    setClipboard(null);
    setRenamingId(undefined);
    setEmptyFolders([]);
    setSortMode("name");
    resetImportFlow();
    clearRevealConnectionPassword();
  }, [clearRevealConnectionPassword, form, open, resetImportFlow]);

  useEffect(() => {
    if (!open) return;
    window.nextshell.cloudSync.workspaceList().then((list) => {
      setHasCloudWorkspaces(list.length > 0);
    }).catch(() => {
      setHasCloudWorkspaces(false);
    });
  }, [open]);

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

    if (authType === "password" || authType === "interactive") {
      form.setFieldValue("sshKeyId", undefined);
      return;
    }

    form.setFieldValue("password", undefined);
  }, [authType, form, open]);

  useEffect(() => {
    if (selectedIds.size === 0) return;
    const validIds = new Set(connections.map((connection) => connection.id));
    setSelectedIds((prev) => {
      const next = new Set(Array.from(prev).filter((id) => validIds.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [connections, selectedIds.size]);

  const applyConnectionToForm = useCallback((connection: ConnectionProfile) => {
    const connectionZone = extractZone(connection.groupPath);
    const connectionSubPath = getSubPath(connection.groupPath);
    (form as any).setFieldsValue({
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
      keepAliveEnabled: connection.keepAliveEnabled,
      keepAliveIntervalSec: connection.keepAliveIntervalSec,
      terminalEncoding: connection.terminalEncoding,
      backspaceMode: connection.backspaceMode,
      deleteMode: connection.deleteMode,
      groupPath: connection.groupPath,
      groupZone: isValidZone(connectionZone) ? connectionZone : CONNECTION_ZONES.SERVER,
      groupSubPath: connectionSubPath,
      tags: connection.tags,
      notes: connection.notes,
      favorite: connection.favorite,
      monitorSession: connection.monitorSession,
      password: undefined
    });
  }, [form]);

  const handleNew = useCallback((prefillGroupPath?: string) => {
    setPrimarySelectedId(undefined);
    form.resetFields();
    form.setFieldsValue(DEFAULT_VALUES);
    if (prefillGroupPath) {
      const zone = extractZone(prefillGroupPath);
      const subPath = getSubPath(prefillGroupPath);
      (form as any).setFieldsValue({
        groupPath: prefillGroupPath,
        groupZone: isValidZone(zone) ? zone : CONNECTION_ZONES.SERVER,
        groupSubPath: subPath
      });
    }
    setFormTab("basic");
    setMode("new");
  }, [form]);

  const handleSelectSingle = useCallback((connectionId: string) => {
    const connection = connections.find((item) => item.id === connectionId);
    if (!connection) return;
    const expandedKeys = new Set<string>(["root"]);
    const parts = groupPathToSegments(connection.groupPath);
    const segments: string[] = [];
    for (const part of parts) {
      segments.push(part);
      expandedKeys.add(`mgr-group:${segments.join("/")}`);
    }
    setExpanded(expandedKeys);
    setPrimarySelectedId(connectionId);
    setSelectedIds(new Set([connectionId]));
    setSelectionAnchorId(connectionId);
    applyConnectionToForm(connection);
    setMode("edit");
  }, [applyConnectionToForm, connections]);

  const handleMultiSelect = useCallback((connectionId: string, event: React.MouseEvent) => {
    const connection = connections.find((item) => item.id === connectionId);
    if (!connection) return;

    if (event.shiftKey && selectionAnchorId) {
      const flatIds = collectFlatLeafIds(tree, expanded, 0);
      const anchorIndex = flatIds.indexOf(selectionAnchorId);
      const currentIndex = flatIds.indexOf(connectionId);
      if (anchorIndex >= 0 && currentIndex >= 0) {
        const start = Math.min(anchorIndex, currentIndex);
        const end = Math.max(anchorIndex, currentIndex);
        const rangeIds = flatIds.slice(start, end + 1);
        setSelectedIds(new Set(rangeIds));
        setPrimarySelectedId(connectionId);
        applyConnectionToForm(connection);
        setMode("edit");
        return;
      }
    }

    if (event.metaKey || event.ctrlKey) {
      setSelectedIds((prev) => {
        const next = new Set(prev);
        if (next.has(connectionId)) next.delete(connectionId);
        else next.add(connectionId);
        return next;
      });
      setPrimarySelectedId(connectionId);
      setSelectionAnchorId(connectionId);
      applyConnectionToForm(connection);
      setMode("edit");
      return;
    }

    setSelectedIds(new Set([connectionId]));
    setPrimarySelectedId(connectionId);
    setSelectionAnchorId(connectionId);
    applyConnectionToForm(connection);
    setMode("edit");
  }, [applyConnectionToForm, connections, expanded, selectionAnchorId, tree]);

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
    handleSelectSingle(focusConnectionId);
    appliedFocusConnectionIdRef.current = focusConnectionId;
  }, [focusConnectionId, handleSelectSingle, open]);

  const handleReset = useCallback(() => {
    if (selectedConnection) {
      applyConnectionToForm(selectedConnection);
    } else {
      form.resetFields();
      form.setFieldsValue(DEFAULT_VALUES);
    }
  }, [applyConnectionToForm, form, selectedConnection]);

  const handleDelete = useCallback(() => {
    const idsToDelete = selectedIds.size > 0
      ? Array.from(selectedIds)
      : (primarySelectedId ? [primarySelectedId] : []);
    if (idsToDelete.length === 0) return;

    const names = idsToDelete.map((id) => connections.find((connection) => connection.id === id)?.name ?? id);
    const content = idsToDelete.length === 1
      ? `删除「${names[0]}」后会关闭相关会话，是否继续？`
      : `确认删除 ${idsToDelete.length} 个连接？删除后会关闭相关会话。`;

    Modal.confirm({
      title: "确认删除",
      content,
      okText: "删除",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        for (const id of idsToDelete) {
          await onConnectionRemoved(id);
        }
        setPrimarySelectedId(undefined);
        setSelectedIds(new Set());
        form.resetFields();
        form.setFieldsValue(DEFAULT_VALUES);
        setMode("idle");
      }
    });
  }, [connections, form, onConnectionRemoved, primarySelectedId, selectedIds]);

  const handleCloseForm = useCallback(() => {
    setMode("idle");
    setPrimarySelectedId(undefined);
  }, []);

  const saveConnection = useCallback(async (values: ConnectionFormValues): Promise<string | undefined> => {
    const payload = toConnectionPayload(values, {
      selectedConnectionId: primarySelectedId
    });

    if (extractZone(payload.groupPath) === CONNECTION_ZONES.WORKSPACE && !hasCloudWorkspaces) {
      message.warning("请先在连接管理器的云同步中配置云同步工作区");
      return undefined;
    }

    if (values.authType === "privateKey" && !values.sshKeyId) {
      message.error("私钥认证需要选择一个 SSH 密钥。");
      setFormTab("basic");
      return undefined;
    }

    if (payload.strictHostKeyChecking && !payload.hostFingerprint) {
      message.error("启用严格主机校验时必须填写主机指纹。");
      setFormTab("basic");
      return undefined;
    }

    if (!Number.isInteger(payload.port) || payload.port < 1 || payload.port > 65535) {
      message.error("端口必须是 1-65535 的整数。");
      setFormTab("basic");
      return undefined;
    }

    if (!payload.host) {
      message.error("请填写主机地址（在「基本」标签页）。");
      setFormTab("basic");
      return undefined;
    }

    if (
      payload.keepAliveIntervalSec !== undefined &&
      (!Number.isInteger(payload.keepAliveIntervalSec) || payload.keepAliveIntervalSec < 5 || payload.keepAliveIntervalSec > 600)
    ) {
      message.error("Keepalive 间隔需为 5-600 秒的整数。");
      setFormTab("network");
      return undefined;
    }

    setSaving(true);
    try {
      await onConnectionSaved(payload);
      message.success(primarySelectedId ? "连接已更新" : "连接已创建");
      setPrimarySelectedId(payload.id);
      setMode("edit");
      form.setFieldsValue({
        password: undefined
      });
      return payload.id;
    } catch (error) {
      message.error(`保存连接失败：${formatErrorMessage(error, "请检查输入内容")}`);
      return undefined;
    } finally {
      setSaving(false);
    }
  }, [form, hasCloudWorkspaces, message, onConnectionSaved, primarySelectedId]);

  const handleSaveAndConnect = useCallback(async () => {
    if (saving || connectingFromForm) {
      return;
    }

    let values: ConnectionUpsertInput;
    try {
      values = await form.validateFields();
    } catch (errorInfo) {
      const firstField = String(
        (errorInfo as { errorFields?: Array<{ name: Array<string | number> }> })
          ?.errorFields?.[0]?.name?.[0] ?? ""
      );
      const errorTab = FIELD_TAB_MAP[firstField];
      if (errorTab) setFormTab(errorTab);
      return;
    }

    const connectionId = await saveConnection(values as ConnectionFormValues);
    if (!connectionId) {
      return;
    }

    setConnectingFromForm(true);
    try {
      await onConnectConnection(connectionId);
      onClose();
    } finally {
      setConnectingFromForm(false);
    }
  }, [connectingFromForm, form, onClose, onConnectConnection, saveConnection, saving]);

  const handleQuickConnect = useCallback(async (connectionId: string) => {
    await onConnectConnection(connectionId);
    onClose();
  }, [onClose, onConnectConnection]);

  const toggleExpanded = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const connection = (event.active.data.current as { connection: ConnectionProfile } | undefined)?.connection;
    if (connection) setDraggingConnection(connection);
  }, []);

  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    setDraggingConnection(null);
    const overId = event.over?.id as string | undefined;
    if (!overId) return;

    const connection = (event.active.data.current as { connection: ConnectionProfile } | undefined)?.connection;
    if (!connection) return;

    const safePath = groupKeyToPath(overId);

    if (extractZone(safePath) === CONNECTION_ZONES.WORKSPACE && !hasCloudWorkspaces) {
      message.warning("请先在连接管理器的云同步中配置云同步工作区");
      return;
    }

    const connectionsToMove = selectedIds.has(connection.id) && selectedIds.size > 1
      ? connections.filter((item) => selectedIds.has(item.id) && item.groupPath !== safePath)
      : (connection.groupPath !== safePath ? [connection] : []);

    if (connectionsToMove.length === 0) return;

    try {
      for (const item of connectionsToMove) {
        await onConnectionSaved(toQuickUpsertInput(item, { groupPath: safePath }));
      }
      const targetZone = extractZone(safePath);
      const displayName = isValidZone(targetZone) ? ZONE_DISPLAY_NAMES[targetZone] : targetZone;
      message.success(
        connectionsToMove.length === 1
          ? `已移动到 ${displayName}${getSubPath(safePath) || ""}`
          : `已移动 ${connectionsToMove.length} 个连接到 ${displayName}${getSubPath(safePath) || ""}`
      );
      if (primarySelectedId && connectionsToMove.some((item) => item.id === primarySelectedId)) {
        form.setFieldValue("groupPath", safePath);
        (form as any).setFieldValue("groupZone", isValidZone(targetZone) ? targetZone : CONNECTION_ZONES.SERVER);
        (form as any).setFieldValue("groupSubPath", getSubPath(safePath));
      }
    } catch (error) {
      message.error(`移动连接失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [connections, form, hasCloudWorkspaces, message, onConnectionSaved, primarySelectedId, selectedIds]);

  const handleConnectionContextMenu = useCallback((event: React.MouseEvent, connectionId: string) => {
    event.preventDefault();
    event.stopPropagation();
    if (!selectedIds.has(connectionId)) {
      setSelectedIds(new Set([connectionId]));
      setPrimarySelectedId(connectionId);
      setSelectionAnchorId(connectionId);
      const connection = connections.find((item) => item.id === connectionId);
      if (connection) {
        applyConnectionToForm(connection);
        setMode("edit");
      }
    }
    setContextMenu({ x: event.clientX, y: event.clientY, target: { type: "connection", connectionId } });
  }, [applyConnectionToForm, connections, selectedIds]);

  const handleGroupContextMenu = useCallback((event: React.MouseEvent, node: MgrGroupNode) => {
    event.preventDefault();
    event.stopPropagation();
    const groupPath = groupKeyToPath(node.key);
    setContextMenu({ x: event.clientX, y: event.clientY, target: { type: "group", groupKey: node.key, groupPath } });
  }, []);

  const handleEmptyContextMenu = useCallback((event: React.MouseEvent) => {
    if (event.target !== event.currentTarget) return;
    event.preventDefault();
    setContextMenu({ x: event.clientX, y: event.clientY, target: { type: "empty" } });
  }, []);

  const handleGroupCtrlClick = useCallback((node: MgrGroupNode) => {
    const leafIds = collectGroupLeafIds(node);
    setSelectedIds((prev) => {
      const allSelected = leafIds.every((id) => prev.has(id));
      const next = new Set(prev);
      if (allSelected) {
        for (const id of leafIds) next.delete(id);
      } else {
        for (const id of leafIds) next.add(id);
      }
      return next;
    });
  }, []);

  const handleCtxCopy = useCallback(() => {
    const ids = Array.from(selectedIds).filter((id) => {
      const connection = connections.find((item) => item.id === id);
      return connection && connection.originKind !== "cloud";
    });
    if (ids.length === 0) return;
    setClipboard({ mode: "copy", connectionIds: ids });
    message.success(`已复制 ${ids.length} 个连接`);
  }, [connections, message, selectedIds]);

  const handleCtxCut = useCallback(() => {
    const ids = Array.from(selectedIds).filter((id) => {
      const connection = connections.find((item) => item.id === id);
      return connection && connection.originKind !== "cloud";
    });
    if (ids.length === 0) return;
    setClipboard({ mode: "cut", connectionIds: ids });
    message.success(`已剪切 ${ids.length} 个连接`);
  }, [connections, message, selectedIds]);

  const handleCtxPaste = useCallback(async (targetGroupPath: string) => {
    if (!clipboard) return;
    if (extractZone(targetGroupPath) === CONNECTION_ZONES.WORKSPACE && !hasCloudWorkspaces) {
      message.warning("请先在连接管理器的云同步中配置云同步工作区");
      return;
    }
    try {
      if (clipboard.mode === "copy") {
        for (const sourceId of clipboard.connectionIds) {
          await window.nextshell.resourceOps.copyConnection({
            sourceId,
            targetOriginKind: "local",
            targetGroupSubPath: getSubPath(targetGroupPath) || undefined
          });
        }
        message.success(`已粘贴 ${clipboard.connectionIds.length} 个连接`);
        await onConnectionsImported();
      } else {
        for (const connectionId of clipboard.connectionIds) {
          const connection = connections.find((item) => item.id === connectionId);
          if (connection) {
            await onConnectionSaved(toQuickUpsertInput(connection, { groupPath: targetGroupPath }));
          }
        }
        message.success(`已移动 ${clipboard.connectionIds.length} 个连接`);
        setClipboard(null);
      }
    } catch (error) {
      message.error(`粘贴失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [clipboard, connections, hasCloudWorkspaces, message, onConnectionSaved, onConnectionsImported]);

  const handleCtxCopyAddress = useCallback((connectionId: string) => {
    const connection = connections.find((item) => item.id === connectionId);
    if (!connection) return;
    const address = `${connection.host}:${connection.port}`;
    void navigator.clipboard.writeText(address);
    message.success(`已复制地址：${address}`);
  }, [connections, message]);

  const handleCtxNewFolder = useCallback(async (parentGroupPath: string) => {
    const name = await promptModal(modal, "新建文件夹", "请输入文件夹名称");
    if (!name) return;
    if (name.includes("/") || name.includes("\\")) {
      message.error("文件夹名称不能包含 / 或 \\");
      return;
    }
    const folderPath = `${parentGroupPath}/${name}`;
    setEmptyFolders((prev) => [...prev, folderPath]);
    const parentKey = parentGroupPath === "/" ? "root" : `mgr-group:${parentGroupPath.slice(1)}`;
    const folderKey = `mgr-group:${folderPath.slice(1)}`;
    setExpanded((prev) => new Set([...prev, parentKey, folderKey]));
    message.success(`已创建文件夹「${name}」`);
  }, [message, modal]);

  const handleCtxRename = useCallback((connectionId: string) => {
    setRenamingId(connectionId);
  }, []);

  const handleRenameCommit = useCallback(async (connectionId: string, newName: string) => {
    setRenamingId(undefined);
    const connection = connections.find((item) => item.id === connectionId);
    if (!connection || connection.name === newName) return;
    try {
      await onConnectionSaved(toQuickUpsertInput(connection, { name: newName }));
      message.success(`已重命名为「${newName}」`);
    } catch (error) {
      message.error(`重命名失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [connections, message, onConnectionSaved]);

  const handleRenameCancel = useCallback(() => {
    setRenamingId(undefined);
  }, []);

  const selectedExportCount = selectedIds.size;
  const sourceProgress = importPreviewQueue.length > 1
    ? `${importQueueIndex + 1}/${importPreviewQueue.length}`
    : undefined;

  return (
    <>
      <Modal
        open={open}
        onCancel={onClose}
        footer={null}
        width={960}
        style={{ top: 48 }}
        styles={{
          header: { padding: "13px 18px", marginBottom: 0, borderBottom: "1px solid var(--border)" },
          body: { padding: 0, overflow: "hidden" }
        }}
        title={<span className="mgr-modal-title">连接管理器</span>}
        destroyOnHidden
      >
        <div className="mgr-tab-bar">
          {MANAGER_TABS.map((tab) => (
            <button
              key={tab.key}
              type="button"
              className={`mgr-tab${tab.tabClassName ? ` ${tab.tabClassName}` : ""}${activeTab === tab.key ? " mgr-tab--active" : ""}`}
              onClick={() => setActiveTab(tab.key)}
            >
              <i className={tab.icon} aria-hidden="true" />
              <span className={tab.labelClassName}>{tab.label}</span>
            </button>
          ))}
        </div>

        {activeTab === "connections" ? (
          <div className="mgr-connections-layout">
            <ConnectionSidebar
              connections={connections}
              keyword={keyword}
              onKeywordChange={setKeyword}
              onClearKeyword={() => setKeyword("")}
              onOpenLocalTerminal={() => {
                onOpenLocalTerminal();
                onClose();
              }}
              onNewConnection={handleNew}
              tree={tree}
              expanded={expanded}
              toggleExpanded={toggleExpanded}
              primarySelectedId={primarySelectedId}
              selectedIds={selectedIds}
              cutIds={cutIds}
              renamingId={renamingId}
              hasVisibleConnections={hasVisibleConnections}
              draggingConnection={draggingConnection}
              onSelect={handleMultiSelect}
              onQuickConnect={handleQuickConnect}
              onConnectionContextMenu={handleConnectionContextMenu}
              onGroupContextMenu={handleGroupContextMenu}
              onGroupCtrlClick={handleGroupCtrlClick}
              onRenameCommit={handleRenameCommit}
              onRenameCancel={handleRenameCancel}
              onEmptyContextMenu={handleEmptyContextMenu}
              onDragStart={handleDragStart}
              onDragEnd={handleDragEnd}
              contextMenu={contextMenu}
              clipboard={clipboard}
              sortMode={sortMode}
              onCloseContextMenu={() => setContextMenu(null)}
              onEditConnection={handleSelectSingle}
              onRenameConnection={handleCtxRename}
              onCopyConnections={handleCtxCopy}
              onCutConnections={handleCtxCut}
              onPasteConnections={handleCtxPaste}
              onDeleteConnections={handleDelete}
              onCopyAddress={handleCtxCopyAddress}
              onNewFolder={handleCtxNewFolder}
              onSortChange={setSortMode}
              onImportNextShell={handleImportNextShell}
              onImportFinalShell={handleImportFinalShell}
              onExportSelected={handleExportSelected}
              onExportAll={handleExportAll}
              selectedExportCount={selectedExportCount}
              importingPreview={importingPreview}
              onClearClipboard={() => setClipboard(null)}
            />

            <ConnectionFormPanel
              form={form}
              mode={mode}
              selectedConnection={selectedConnection}
              formTab={formTab}
              setFormTab={setFormTab}
              authType={authType}
              keepAliveSetting={keepAliveSetting}
              saving={saving}
              connectingFromForm={connectingFromForm}
              sshKeys={sshKeys}
              proxies={proxies}
              revealedLoginPassword={revealedLoginPassword}
              revealingLoginPassword={revealingLoginPassword}
              onRevealConnectionPassword={() => void handleRevealConnectionPassword()}
              onSave={async (values) => {
                await saveConnection(values);
              }}
              onSaveAndConnect={() => void handleSaveAndConnect()}
              onDelete={handleDelete}
              onReset={handleReset}
              onCloseForm={handleCloseForm}
              onSwitchToIdle={() => setMode("idle")}
              onNewConnection={() => handleNew()}
            />
          </div>
        ) : null}

        {activeTab === "keys" ? (
          <SshKeyManagerPanel sshKeys={sshKeys} onReload={onReloadSshKeys} />
        ) : null}

        {activeTab === "proxies" ? (
          <ProxyManagerPanel proxies={proxies} onReload={onReloadProxies} />
        ) : null}

        {activeTab === "cloudSync" ? (
          <div className="mgr-cloud-sync-panel">
            <CloudSyncManagerPanel />
          </div>
        ) : null}

        {activeTab === "recycleBin" ? (
          <div className="mgr-recycle-bin-panel">
            <RecycleBinSection />
          </div>
        ) : null}
      </Modal>

      <ConnectionImportModal
        open={importModalOpen}
        entries={currentImportBatch?.entries ?? []}
        existingConnections={connections}
        sourceName={currentImportBatch?.fileName}
        sourceProgress={sourceProgress}
        onClose={resetImportFlow}
        onImported={handleImportBatchImported}
      />
    </>
  );
};
