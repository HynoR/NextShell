import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, Form, Input, InputNumber, Modal, Radio, Select, Switch, Tooltip, message } from "antd";
import type { ConnectionProfile, ConnectionImportEntry, SshKeyProfile, ProxyProfile } from "@nextshell/core";
import { CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX, type ConnectionUpsertInput } from "@nextshell/shared";
import { DndContext, DragOverlay, PointerSensor, useSensors, useSensor, useDraggable, useDroppable } from "@dnd-kit/core";
import type { DragStartEvent, DragEndEvent } from "@dnd-kit/core";
import { SshKeyManagerPanel } from "./SshKeyManagerPanel";
import { ProxyManagerPanel } from "./ProxyManagerPanel";
import { ConnectionImportModal } from "./ConnectionImportModal";
import { formatDateTime, formatRelativeTime } from "../utils/formatTime";
import { formatErrorMessage } from "../utils/errorMessage";

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

const normalizeGroupPath = (value: string | undefined): string => {
  if (!value) return "/server";
  let path = value.trim().replace(/\\/g, "/");
  if (!path.startsWith("/")) path = "/" + path;
  path = path.replace(/\/+/g, "/");
  if (path.length > 1 && path.endsWith("/")) path = path.slice(0, -1);
  return path || "/server";
};

type FormTab = "basic" | "property" | "network" | "advanced";

const groupKeyToPath = (key: string): string => {
  if (key === "root") return "/";
  const prefix = "mgr-group:";
  const raw = key.startsWith(prefix) ? key.slice(prefix.length) : key;
  return "/" + raw;
};

const toQuickUpsertInput = (
  connection: ConnectionProfile,
  patch: Partial<ConnectionUpsertInput>
): ConnectionUpsertInput => ({
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
  portForwards: connection.portForwards,
  terminalEncoding: connection.terminalEncoding,
  backspaceMode: connection.backspaceMode,
  deleteMode: connection.deleteMode,
  groupPath: connection.groupPath,
  tags: connection.tags,
  notes: connection.notes,
  favorite: connection.favorite,
  monitorSession: connection.monitorSession,
  ...patch
});

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

const groupPathToSegments = (groupPath: string): string[] => {
  return groupPath.split("/").filter((s) => s.length > 0);
};

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
    const text = `${connection.name} ${connection.host} ${connection.groupPath} ${connection.tags.join(" ")}`.toLowerCase();
    if (lower && !text.includes(lower)) continue;
    ensureGroup(groupPathToSegments(connection.groupPath)).children.push({ type: "leaf", connection });
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
}) => {
  const { isOver, setNodeRef } = useDroppable({ id: node.key });

  return (
    <button
      ref={setNodeRef}
      type="button"
      className={`mgr-group-row${isOver ? " mgr-group-row--drop-target" : ""}`}
      onClick={onToggle}
    >
      <i
        className={expanded ? "ri-arrow-down-s-line" : "ri-arrow-right-s-line"}
        aria-hidden="true"
      />
      <i className={isOver ? "ri-folder-open-line" : "ri-folder-3-line"} aria-hidden="true" />
      <span className="mgr-group-label">{node.label}</span>
      <span className="mgr-group-count">{countMgrLeaves(node)}</span>
    </button>
  );
};

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
}) => {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: connection.id,
    data: { connection }
  });

  return (
    <div
      ref={setNodeRef}
      className={`mgr-server-row${isSelected ? " selected" : ""}${isDragging ? " mgr-server-row--dragging" : ""}`}
      {...attributes}
      {...listeners}
    >
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
};

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

/* ── Root drop zone ────────────────────────────────── */

const MgrRootDropZone = ({ children }: { children: React.ReactNode }) => {
  const { isOver, setNodeRef } = useDroppable({ id: "root" });
  return (
    <div
      ref={setNodeRef}
      className={`mgr-tree-wrap${isOver ? " mgr-tree-wrap--drop-target" : ""}`}
    >
      {children}
    </div>
  );
};

/* ── Constants ──────────────────────────────────────────── */

/** Maps form field names to their containing tab, used to auto-switch on validation error */
const FIELD_TAB_MAP: Record<string, FormTab> = {
  name: "basic",
  host: "basic",
  port: "basic",
  username: "basic",
  authType: "basic",
  sshKeyId: "basic",
  password: "basic",
  hostFingerprint: "basic",
  strictHostKeyChecking: "basic",
  groupPath: "property",
  tags: "property",
  notes: "property",
  favorite: "property",
  proxyId: "network",
  monitorSession: "advanced",
  terminalEncoding: "advanced",
  backspaceMode: "advanced",
  deleteMode: "advanced"
};

const DEFAULT_VALUES = {
  port: 22,
  authType: "password" as const,
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8" as const,
  backspaceMode: "ascii-backspace" as const,
  deleteMode: "vt220-delete" as const,
  groupPath: "/server",
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
  const { modal } = AntdApp.useApp();
  const [activeTab, setActiveTab] = useState<ManagerTab>("connections");
  const [mode, setMode] = useState<"idle" | "new" | "edit">("idle");
  const [keyword, setKeyword] = useState("");
  const [selectedConnectionId, setSelectedConnectionId] = useState<string>();
  const [selectedExportIds, setSelectedExportIds] = useState<Set<string>>(new Set());
  const [expanded, setExpanded] = useState<Set<string>>(new Set(["root"]));
  const [formTab, setFormTab] = useState<FormTab>("basic");
  const [saving, setSaving] = useState(false);
  const [connectingFromForm, setConnectingFromForm] = useState(false);
  const [importingPreview, setImportingPreview] = useState(false);
  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importPreviewQueue, setImportPreviewQueue] = useState<ImportPreviewBatch[]>([]);
  const [importQueueIndex, setImportQueueIndex] = useState(0);
  const [revealedLoginPassword, setRevealedLoginPassword] = useState<string>();
  const [revealingLoginPassword, setRevealingLoginPassword] = useState(false);
  const revealPasswordTimeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
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
    setFormTab("basic");
    setKeyword("");
    setActiveTab("connections");
    setImportingPreview(false);
    setImportModalOpen(false);
    setImportPreviewQueue([]);
    setImportQueueIndex(0);
    setRevealedLoginPassword(undefined);
    if (revealPasswordTimeoutRef.current) {
      clearTimeout(revealPasswordTimeoutRef.current);
      revealPasswordTimeoutRef.current = undefined;
    }
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

    if (authType === "password" || authType === "interactive") {
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

  useEffect(() => {
    setRevealedLoginPassword(undefined);
    if (revealPasswordTimeoutRef.current) {
      clearTimeout(revealPasswordTimeoutRef.current);
      revealPasswordTimeoutRef.current = undefined;
    }
  }, [authType, selectedConnectionId]);

  useEffect(() => {
    return () => {
      if (revealPasswordTimeoutRef.current) {
        clearTimeout(revealPasswordTimeoutRef.current);
      }
    };
  }, []);

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
    setFormTab("basic");
    setMode("new");
  }, [form]);

  const handleSelect = useCallback((connectionId: string) => {
    const connection = connections.find((c) => c.id === connectionId);
    if (!connection) return;
    const expandedKeys = new Set<string>(["root"]);
    const parts = groupPathToSegments(connection.groupPath);
    const segments: string[] = [];
    for (const part of parts) {
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
    const groupPath = normalizeGroupPath(values.groupPath);
    const tags = sanitizeTextArray(values.tags);
    const notes = sanitizeOptionalText(values.notes);
    // InputNumber may return null when cleared; coerce safely
    const rawPort = values.port as unknown as number | null | undefined;
    const port = rawPort == null ? NaN : Number(rawPort);
    const host = values.host.trim();
    const name = sanitizeOptionalText(values.name) ?? `${host}:${port}`;
    const terminalEncoding = values.terminalEncoding ?? "utf-8";
    const backspaceMode = values.backspaceMode ?? "ascii-backspace";
    const deleteMode = values.deleteMode ?? "vt220-delete";

    if (values.authType === "privateKey" && !values.sshKeyId) {
      message.error("私钥认证需要选择一个 SSH 密钥。");
      setFormTab("basic");
      return undefined;
    }

    if (values.strictHostKeyChecking && !hostFingerprint) {
      message.error("启用严格主机校验时必须填写主机指纹。");
      setFormTab("basic");
      return undefined;
    }

    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      message.error("端口必须是 1-65535 的整数。");
      setFormTab("basic");
      return undefined;
    }

    if (!host) {
      message.error("请填写主机地址（在「基本」标签页）。");
      setFormTab("basic");
      return undefined;
    }
    const username = (values.username ?? "").trim();

    setSaving(true);
    try {
      const payload: ConnectionUpsertInput = {
        id: values.id ?? selectedConnectionId ?? crypto.randomUUID(),
        name,
        host,
        port,
        username,
        authType: values.authType,
        password,
        sshKeyId: values.authType === "privateKey" ? values.sshKeyId : undefined,
        hostFingerprint,
        strictHostKeyChecking: values.strictHostKeyChecking ?? false,
        proxyId: values.proxyId,
        portForwards: selectedConnection?.portForwards ?? [],
        terminalEncoding,
        backspaceMode,
        deleteMode,
        tags,
        groupPath,
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
      message.error(`保存连接失败：${formatErrorMessage(error, "请检查输入内容")}`);
      return undefined;
    } finally {
      setSaving(false);
    }
  }, [form, onConnectionSaved, selectedConnection, selectedConnectionId, setFormTab]);

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
      const errTab = FIELD_TAB_MAP[firstField];
      if (errTab) setFormTab(errTab);
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
  }, [connectingFromForm, form, onConnectConnection, saveConnection, saving, setFormTab]);

  const toggleExpanded = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  /* ── Drag-and-drop (dnd-kit) ─────────────────────── */
  const [draggingConnection, setDraggingConnection] = useState<ConnectionProfile | null>(null);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } })
  );

  const handleDragStart = useCallback((event: DragStartEvent) => {
    const conn = (event.active.data.current as { connection: ConnectionProfile } | undefined)?.connection;
    if (conn) setDraggingConnection(conn);
  }, []);

  const handleDragEnd = useCallback(async (event: DragEndEvent) => {
    setDraggingConnection(null);
    const overId = event.over?.id as string | undefined;
    if (!overId) return;

    const conn = (event.active.data.current as { connection: ConnectionProfile } | undefined)?.connection;
    if (!conn) return;

    const targetPath = groupKeyToPath(overId);
    if (targetPath === conn.groupPath) return;

    try {
      await onConnectionSaved(toQuickUpsertInput(conn, { groupPath: targetPath }));
      message.success(`已移动到 ${targetPath}`);
      if (selectedConnectionId === conn.id) {
        form.setFieldValue("groupPath", targetPath);
      }
    } catch (error) {
      message.error(`移动连接失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [form, onConnectionSaved, selectedConnectionId]);

  const getCachedMasterPassword = useCallback(async (): Promise<string> => {
    try {
      const result = await window.nextshell.masterPassword.getCached();
      return result.password ?? "";
    } catch {
      return "";
    }
  }, []);

  const promptExportMode = useCallback((): Promise<"plain" | "encrypted" | null> => {
    return new Promise((resolve) => {
      let mode: "plain" | "encrypted" = "plain";
      let settled = false;
      const settle = (value: "plain" | "encrypted" | null): void => {
        if (settled) return;
        settled = true;
        resolve(value);
      };

      modal.confirm({
        title: "导出选项",
        okText: "继续",
        cancelText: "取消",
        content: (
          <Radio.Group
            defaultValue="plain"
            onChange={(event) => {
              mode = event.target.value;
            }}
          >
            <Radio value="plain">普通导出（JSON）</Radio>
            <Radio value="encrypted">加密导出（AES + b64##）</Radio>
          </Radio.Group>
        ),
        onOk: () => settle(mode),
        onCancel: () => settle(null)
      });
    });
  }, [modal]);

  const promptExportEncryptionPassword = useCallback((defaultPassword?: string): Promise<string | null> => {
    return new Promise((resolve) => {
      let password = defaultPassword ?? "";
      let confirmPassword = defaultPassword ?? "";
      let settled = false;
      const settle = (value: string | null): void => {
        if (settled) return;
        settled = true;
        resolve(value);
      };

      modal.confirm({
        title: "输入导出加密密码",
        okText: "确认",
        cancelText: "取消",
        content: (
          <div style={{ display: "grid", gap: 8 }}>
            {defaultPassword ? (
              <div style={{ fontSize: 12, color: "var(--t3)" }}>
                已自动填充主密码，可按需修改。
              </div>
            ) : null}
            <Input.Password
              placeholder="请输入密码（至少 6 位）"
              defaultValue={defaultPassword}
              onChange={(event) => {
                password = event.target.value;
              }}
            />
            <Input.Password
              placeholder="请再次输入密码"
              defaultValue={defaultPassword}
              onChange={(event) => {
                confirmPassword = event.target.value;
              }}
            />
          </div>
        ),
        onOk: async () => {
          const trimmedPassword = password.trim();
          const trimmedConfirm = confirmPassword.trim();
          if (trimmedPassword.length < 6) {
            message.warning("导出加密密码至少需要 6 个字符。");
            throw new Error("invalid-export-password-length");
          }
          if (trimmedPassword !== trimmedConfirm) {
            message.warning("两次输入的密码不一致。");
            throw new Error("invalid-export-password-confirm");
          }
          settle(trimmedPassword);
        },
        onCancel: () => settle(null)
      });
    });
  }, [modal]);

  const runSingleExport = useCallback(
    async (exportIds: string[]): Promise<void> => {
      if (exportIds.length === 0) return;

      const mode = await promptExportMode();
      if (!mode) return;

      let encryptionPassword: string | undefined;
      if (mode === "encrypted") {
        const defaultPassword = await getCachedMasterPassword();
        const password = await promptExportEncryptionPassword(defaultPassword);
        if (!password) return;
        encryptionPassword = password;
      }

      try {
        const result = await window.nextshell.connection.exportToFile({
          connectionIds: exportIds,
          encryptionPassword
        });
        if (result.ok) {
          if (mode === "encrypted") {
            message.success(`已加密导出 ${exportIds.length} 个连接`);
          } else {
            message.success(`已导出 ${exportIds.length} 个连接`);
          }
        }
      } catch (error) {
        message.error(`导出失败：${formatErrorMessage(error, "请稍后重试")}`);
      }
    },
    [getCachedMasterPassword, promptExportEncryptionPassword, promptExportMode]
  );

  const handleExportAll = useCallback(async () => {
    if (connections.length === 0) return;
    await runSingleExport(connections.map((connection) => connection.id));
  }, [connections, runSingleExport]);

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

    const mode = await promptExportMode();
    if (!mode) return;

    let encryptionPassword: string | undefined;
    if (mode === "encrypted") {
      const defaultPassword = await getCachedMasterPassword();
      const password = await promptExportEncryptionPassword(defaultPassword);
      if (!password) return;
      encryptionPassword = password;
    }

    const directory = await window.nextshell.dialog.openDirectory({
      title: "选择导出目录"
    });
    if (directory.canceled || !directory.filePath) {
      return;
    }

    try {
      const result = await window.nextshell.connection.exportBatch({
        connectionIds: exportIds,
        directoryPath: directory.filePath,
        encryptionPassword
      });

      if (result.failed === 0) {
        if (mode === "encrypted") {
          message.success(`已加密导出 ${result.exported} 个连接到目录：${result.directoryPath}`);
        } else {
          message.success(`已导出 ${result.exported} 个连接到目录：${result.directoryPath}`);
        }
        return;
      }

      if (result.exported > 0) {
        message.warning(`已导出 ${result.exported}/${result.total}，失败 ${result.failed}`);
      } else {
        message.error(`导出失败：共 ${result.failed} 个连接导出失败`);
      }

      const maxWarnings = 5;
      result.errors.slice(0, maxWarnings).forEach((errorText) => {
        message.warning(formatErrorMessage(errorText, "导出失败"));
      });
      if (result.errors.length > maxWarnings) {
        message.warning(`其余 ${result.errors.length - maxWarnings} 项导出失败`);
      }
    } catch (error) {
      message.error(`导出失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [connections, getCachedMasterPassword, promptExportEncryptionPassword, promptExportMode, selectedExportIds]);

  const promptMasterPasswordForReveal = useCallback((defaultPassword?: string): Promise<string | null> => {
    return new Promise((resolve) => {
      let password = defaultPassword ?? "";
      let settled = false;
      const settle = (value: string | null): void => {
        if (settled) return;
        settled = true;
        resolve(value);
      };

      modal.confirm({
        title: "输入主密码查看登录密码",
        okText: "查看",
        cancelText: "取消",
        content: (
          <div style={{ display: "grid", gap: 8 }}>
            {defaultPassword ? (
              <div style={{ fontSize: 12, color: "var(--t3)" }}>
                已自动填充主密码，可按需修改。
              </div>
            ) : null}
            <Input.Password
              placeholder="请输入主密码"
              defaultValue={defaultPassword}
              onChange={(event) => {
                password = event.target.value;
              }}
            />
          </div>
        ),
        onOk: async () => {
          const trimmed = password.trim();
          if (!trimmed) {
            message.warning("请输入主密码。");
            throw new Error("empty-master-password");
          }
          settle(trimmed);
        },
        onCancel: () => settle(null)
      });
    });
  }, [modal]);

  const handleRevealConnectionPassword = useCallback(async () => {
    if (!selectedConnection || !selectedConnectionId) {
      return;
    }
    if (selectedConnection.authType !== "password" && selectedConnection.authType !== "interactive") {
      message.warning("仅密码/交互式认证连接支持查看登录密码。");
      return;
    }

    const defaultMasterPassword = await getCachedMasterPassword();
    const inputPassword = await promptMasterPasswordForReveal(defaultMasterPassword);
    if (!inputPassword) {
      return;
    }

    try {
      setRevealingLoginPassword(true);
      const result = await window.nextshell.connection.revealPassword({
        connectionId: selectedConnectionId,
        masterPassword: inputPassword
      });
      setRevealedLoginPassword(result.password);
      if (revealPasswordTimeoutRef.current) {
        clearTimeout(revealPasswordTimeoutRef.current);
      }
      revealPasswordTimeoutRef.current = setTimeout(() => {
        setRevealedLoginPassword(undefined);
        revealPasswordTimeoutRef.current = undefined;
      }, 30_000);
      message.success("已显示登录密码，30 秒后自动隐藏。");
    } catch (error) {
      message.error(`查看登录密码失败：${formatErrorMessage(error, "请检查主密码")}`);
    } finally {
      setRevealingLoginPassword(false);
    }
  }, [
    getCachedMasterPassword,
    promptMasterPasswordForReveal,
    selectedConnection,
    selectedConnectionId
  ]);

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

  const promptImportDecryptionPassword = useCallback(
    (fileName: string, promptText: string): Promise<string | null> => {
      return new Promise((resolve) => {
        let password = "";
        let settled = false;
        const settle = (value: string | null): void => {
          if (settled) return;
          settled = true;
          resolve(value);
        };

        modal.confirm({
          title: `${fileName} 需要解密密码`,
          okText: "解密",
          cancelText: "跳过该文件",
          content: (
            <div style={{ display: "grid", gap: 8 }}>
              <div style={{ fontSize: 12, color: "var(--t3)" }}>{promptText}</div>
              <Input.Password
                placeholder="请输入导入密码"
                onChange={(event) => {
                  password = event.target.value;
                }}
              />
            </div>
          ),
          onOk: async () => {
            const trimmed = password.trim();
            if (!trimmed) {
              message.warning("请输入解密密码。");
              throw new Error("empty-import-password");
            }
            settle(trimmed);
          },
          onCancel: () => settle(null)
        });
      });
    },
    [modal]
  );

  const loadImportPreviewQueue = useCallback(async (source: "nextshell" | "finalshell") => {
    if (importingPreview) return;
    try {
      setImportingPreview(true);
      const dialogResult = await window.nextshell.dialog.openFiles({
        title: source === "nextshell" ? "选择 NextShell 导入文件" : "选择 FinalShell 配置文件",
        multi: true
      });
      if (dialogResult.canceled || dialogResult.filePaths.length === 0) return;

      const queue: ImportPreviewBatch[] = [];
      const warnings: string[] = [];

      for (const filePath of dialogResult.filePaths) {
        const fileName = getFileName(filePath);
        if (source === "nextshell") {
          let decryptionPassword: string | undefined;
          let handled = false;

          while (!handled) {
            try {
              const entries = await window.nextshell.connection.importPreview({
                filePath,
                decryptionPassword
              });
              if (entries.length === 0) {
                warnings.push(`${fileName}：文件中没有可导入的连接`);
              } else {
                queue.push({ fileName, entries });
              }
              handled = true;
            } catch (error) {
              const reason = formatErrorMessage(error, "导入预览失败");
              if (reason.startsWith(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX)) {
                const promptText =
                  reason.slice(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX.length).trim()
                  || "该导入文件已加密，请输入密码";
                const inputPassword = await promptImportDecryptionPassword(fileName, promptText);
                if (!inputPassword) {
                  warnings.push(`${fileName}：用户取消解密，已跳过该文件`);
                  handled = true;
                  continue;
                }
                decryptionPassword = inputPassword;
                continue;
              }

              warnings.push(`${fileName}：${formatErrorMessage(reason, "导入预览失败")}`);
              handled = true;
            }
          }
          continue;
        }

        try {
          const entries = await window.nextshell.connection.importFinalShellPreview({
            filePath
          });
          if (entries.length === 0) {
            warnings.push(`${fileName}：文件中没有可导入的连接`);
          } else {
            queue.push({ fileName, entries });
          }
        } catch (error) {
          warnings.push(`${fileName}：${formatErrorMessage(error, "导入预览失败")}`);
        }
      }

      if (warnings.length > 0) {
        warnings.forEach((item) => {
          message.warning(formatErrorMessage(item, "部分文件导入失败"));
        });
      }

      if (queue.length === 0) {
        message.warning(
          source === "nextshell"
            ? "未找到可导入的 NextShell 连接文件"
            : "未找到可导入的 FinalShell 连接文件"
        );
        return;
      }

      setImportPreviewQueue(queue);
      setImportQueueIndex(0);
      setImportModalOpen(true);
      if (queue.length > 1) {
        message.info(`已加载 ${queue.length} 个文件，将按文件逐个导入`);
      }
    } catch (error) {
      message.error(`导入预览失败：${formatErrorMessage(error, "请检查文件格式")}`);
    } finally {
      setImportingPreview(false);
    }
  }, [getFileName, importingPreview, promptImportDecryptionPassword]);

  const handleImportNextShell = useCallback(async () => {
    await loadImportPreviewQueue("nextshell");
  }, [loadImportPreviewQueue]);

  const handleImportFinalShell = useCallback(async () => {
    await loadImportPreviewQueue("finalshell");
  }, [loadImportPreviewQueue]);

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
          <DndContext
            sensors={sensors}
            onDragStart={handleDragStart}
            onDragEnd={(e) => void handleDragEnd(e)}
          >
          <MgrRootDropZone>
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
          </MgrRootDropZone>
          <DragOverlay>
            {draggingConnection ? (
              <div className="mgr-drag-overlay">
                <i className="ri-server-line" aria-hidden="true" />
                <span>{draggingConnection.name}</span>
              </div>
            ) : null}
          </DragOverlay>
          </DndContext>

          {/* Footer */}
          <div className="mgr-sidebar-footer">
            <span className="mgr-count">
              {connections.length} 个连接
              {selectedExportCount > 0 ? ` · 已选 ${selectedExportCount}` : ""}
            </span>
            <div className="mgr-sidebar-footer-actions">
              <Tooltip title="导入 NextShell 文件">
                <button type="button" className="mgr-action-btn" onClick={handleImportNextShell} disabled={importingPreview}>
                  <i className={importingPreview ? "ri-loader-4-line ri-spin" : "ri-upload-2-line"} />
                </button>
              </Tooltip>
              <Tooltip title="导入 FinalShell 文件">
                <button
                  type="button"
                  className="mgr-action-btn"
                  onClick={handleImportFinalShell}
                  disabled={importingPreview}
                >
                  <i className="ri-file-upload-line" />
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
                  <>
                    <div className="mgr-form-subtitle">
                      {selectedConnection.username.trim()
                        ? `${selectedConnection.username}@${selectedConnection.host}:${selectedConnection.port}`
                        : `${selectedConnection.host}:${selectedConnection.port}`}
                    </div>
                    <div className="mgr-form-meta">
                      <span
                        className="mgr-form-meta-item"
                        title={`修改时间：${formatDateTime(selectedConnection.updatedAt)}`}
                      >
                        <i className="ri-edit-2-line" aria-hidden="true" />
                        {formatRelativeTime(selectedConnection.updatedAt)}
                      </span>
                      <span className="mgr-form-meta-sep">·</span>
                      <span
                        className="mgr-form-meta-item"
                        title={selectedConnection.lastConnectedAt
                          ? `上次连接：${formatDateTime(selectedConnection.lastConnectedAt)}`
                          : "从未连接"}
                      >
                        <i className="ri-plug-line" aria-hidden="true" />
                        {selectedConnection.lastConnectedAt
                          ? formatRelativeTime(selectedConnection.lastConnectedAt)
                          : "从未连接"}
                      </span>
                    </div>
                  </>
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
                      <i className="ri-delete-bin-line" aria-hidden="true" />
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
              onFinishFailed={({ errorFields }) => {
                const firstField = String(errorFields[0]?.name?.[0] ?? "");
                const errTab = FIELD_TAB_MAP[firstField];
                if (errTab) setFormTab(errTab);
              }}
            >
              {/* ── Form tab bar ──── */}
              <div className="mgr-form-tab-bar">
                <button
                  type="button"
                  title="基本信息"
                  className={`mgr-form-tab${formTab === "basic" ? " mgr-form-tab--active" : ""}`}
                  onClick={() => setFormTab("basic")}
                >
                  <i className="ri-server-line" aria-hidden="true" />
                  基本
                </button>
                <button
                  type="button"
                  title="属性信息"
                  className={`mgr-form-tab${formTab === "property" ? " mgr-form-tab--active" : ""}`}
                  onClick={() => setFormTab("property")}
                >
                  <i className="ri-price-tag-3-line" aria-hidden="true" />
                  属性
                </button>
                <button
                  type="button"
                  title="网络代理"
                  className={`mgr-form-tab${formTab === "network" ? " mgr-form-tab--active" : ""}`}
                  onClick={() => setFormTab("network")}
                >
                  <i className="ri-shield-line" aria-hidden="true" />
                  网络
                </button>
                <button
                  type="button"
                  title="高级设置"
                  className={`mgr-form-tab${formTab === "advanced" ? " mgr-form-tab--active" : ""}`}
                  onClick={() => setFormTab("advanced")}
                >
                  <i className="ri-settings-3-line" aria-hidden="true" />
                  高级
                </button>
              </div>

              <div className="mgr-form-tab-body">
                {/* ── Tab: 基本 ──── */}
                <div style={{ display: formTab === "basic" ? "" : "none" }}>
                    <Form.Item label="名称" name="name">
                      <Input placeholder="我的服务器（可选，留空将使用 host:port）" />
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

                    <div className="flex gap-3 items-start">
                      <Form.Item
                        label="用户名"
                        name="username"
                        className="flex-1"
                      >
                        <Input placeholder="root（可选，首次连接时输入）" />
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
                            { label: "交互式登录", value: "interactive" },
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

                    {authType === "password" || authType === "interactive" ? (
                      <>
                        <Form.Item
                          label="密码"
                          name="password"
                          preserve={false}
                        >
                          <Input.Password placeholder="输入密码（留空则不更新）" />
                        </Form.Item>
                        {mode === "edit" && (
                          selectedConnection?.authType === "password" ||
                          selectedConnection?.authType === "interactive"
                        ) ? (
                          <Form.Item label="已保存的登录密码" preserve={false}>
                            <div style={{ display: "grid", gap: 8 }}>
                              <button
                                type="button"
                                className="mgr-action-btn"
                                onClick={() => void handleRevealConnectionPassword()}
                                disabled={revealingLoginPassword}
                                style={{ justifySelf: "start" }}
                              >
                                <i
                                  className={revealingLoginPassword ? "ri-loader-4-line" : "ri-eye-line"}
                                  aria-hidden="true"
                                />
                                {revealingLoginPassword ? "验证中..." : "输入主密码查看"}
                              </button>
                              {revealedLoginPassword ? (
                                <Input.Password
                                  value={revealedLoginPassword}
                                  readOnly
                                  visibilityToggle
                                />
                              ) : (
                                <div style={{ fontSize: 12, color: "var(--t3)" }}>
                                  仅在输入主密码后显示，30 秒自动隐藏。
                                </div>
                              )}
                            </div>
                          </Form.Item>
                        ) : null}
                      </>
                    ) : null}

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
                </div>

                {/* ── Tab: 属性 ──── */}
                <div style={{ display: formTab === "property" ? "" : "none" }}>
                    <Form.Item label="分组路径" name="groupPath">
                      <Input
                        placeholder="/server/production"
                        prefix={<i className="ri-folder-3-line" style={{ color: "var(--t3)", fontSize: 13 }} />}
                        style={{ fontFamily: "var(--mono)" }}
                      />
                    </Form.Item>

                    <div className="flex gap-3 items-start">
                      <Form.Item label="标签" name="tags" className="flex-1">
                        <Select
                          mode="tags"
                          tokenSeparators={[","]}
                          placeholder="web, linux, prod"
                        />
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

                    <Form.Item label="备注" name="notes" className="!mb-0">
                      <Input.TextArea rows={2} placeholder="可选备注信息..." className="mgr-textarea" />
                    </Form.Item>
                </div>

                {/* ── Tab: 网络 ──── */}
                <div style={{ display: formTab === "network" ? "" : "none" }}>
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
                </div>

                {/* ── Tab: 高级 ──── */}
                <div style={{ display: formTab === "advanced" ? "" : "none" }}>
                    <div className="flex gap-3 items-start">
                      <Form.Item
                        label="监控会话"
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

                    <div className="mgr-section-label">按键序列</div>

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
                </div>
              </div>
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
