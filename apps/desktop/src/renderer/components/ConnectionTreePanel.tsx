import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { App as AntdApp, Form, Input, Modal, Select, message } from "antd";
import type { ConnectionProfile, SessionDescriptor, SshKeyProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { promptModal } from "../utils/promptModal";
import { formatDateTime, formatRelativeTime } from "../utils/formatTime";

interface ConnectionTreePanelProps {
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  sessions: SessionDescriptor[];
  activeConnectionId?: string;
  onSelect: (connectionId: string) => void;
  onConnectByDoubleClick: (connectionId: string) => void;
  onConnect: (connectionId: string) => void;
  onQuickSave: (payload: ConnectionUpsertInput) => Promise<void>;
  onOpenManagerForConnection: (connectionId: string) => void;
}

interface CredentialEditFormValues {
  username?: string;
  authType: ConnectionProfile["authType"];
  password?: string;
  sshKeyId?: string;
}

interface ConnectionContextMenuState {
  x: number;
  y: number;
  connectionId: string;
}

interface GroupNode {
  type: "group";
  key: string;
  label: string;
  children: TreeNode[];
}

interface LeafNode {
  type: "leaf";
  connection: ConnectionProfile;
  isConnected: boolean;
}

type TreeNode = GroupNode | LeafNode;

const sanitizeOptionalText = (value: string | undefined): string | undefined => {
  if (typeof value !== "string") {
    return undefined;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
};

const toConnectionUpsertInput = (
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

const groupPathToSegments = (groupPath: string): string[] => {
  return groupPath.split("/").filter((s) => s.length > 0);
};

const buildTree = (
  connections: ConnectionProfile[],
  sessions: SessionDescriptor[],
  keyword: string
): GroupNode => {
  const lower = keyword.trim().toLowerCase();
  const root: GroupNode = {
    type: "group",
    key: "root",
    label: "连接中心",
    children: []
  };

  const connectedConnectionIds = new Set(
    sessions
      .filter((s) => s.status === "connected" && s.type === "terminal")
      .map((s) => s.connectionId)
  );

  const ensureGroup = (path: string[]): GroupNode => {
    let current = root;
    const visited: string[] = [];

    for (const segment of path) {
      visited.push(segment);
      const key = `group:${visited.join("/")}`;
      let next = current.children.find(
        (node): node is GroupNode => node.type === "group" && node.key === key
      );
      if (!next) {
        next = { type: "group", key, label: segment, children: [] };
        current.children.push(next);
      }
      current = next;
    }

    return current;
  };

  for (const connection of connections) {
    const searchable = `${connection.name} ${connection.host} ${connection.tags.join(" ")} ${connection.groupPath} ${connection.notes ?? ""}`.toLowerCase();
    if (lower && !searchable.includes(lower)) {
      continue;
    }

    const group = ensureGroup(groupPathToSegments(connection.groupPath));
    const isConnected = connectedConnectionIds.has(connection.id);
    group.children.push({
      type: "leaf",
      connection,
      isConnected
    });
  }

  return root;
};

/* ── Sub-components ── */

const GroupRow = ({
  node,
  expanded,
  onToggle
}: {
  node: GroupNode;
  expanded: boolean;
  onToggle: () => void;
}) => (
  <button type="button" className="ct-group-row" onClick={onToggle}>
    <i
      className={expanded ? "ri-arrow-down-s-line" : "ri-arrow-right-s-line"}
      aria-hidden="true"
    />
    <i className="ri-folder-3-line ct-group-icon" aria-hidden="true" />
    <span className="ct-group-label">{node.label}</span>
    <span className="ct-group-count">{countLeaves(node)}</span>
  </button>
);

const ServerRow = ({
  node,
  isActive,
  onSelect,
  onDoubleClick,
  onConnect,
  onContextMenu
}: {
  node: LeafNode;
  isActive: boolean;
  onSelect: () => void;
  onDoubleClick: () => void;
  onConnect: () => void;
  onContextMenu: (event: React.MouseEvent<HTMLButtonElement>) => void;
}) => {
  const c = node.connection;
  return (
    <button
      type="button"
      className={`ct-server-row${isActive ? " active" : ""}${node.isConnected ? " connected" : ""}`}
      onClick={onSelect}
      onDoubleClick={(e) => {
        e.stopPropagation();
        onDoubleClick();
      }}
      onContextMenu={onContextMenu}
      title={`${c.name} (${c.host}:${c.port})`}
    >
      <span className={`ct-status-dot${node.isConnected ? " online" : ""}`} />
      {c.favorite ? (
        <i className="ri-star-fill ct-star" aria-hidden="true" />
      ) : null}
      <span className="ct-server-name">{c.name}</span>
      <span className="ct-server-host">{c.host}</span>
      <span
        className="ct-connect-btn"
        title="新建终端连接"
        role="button"
        tabIndex={0}
        onClick={(e) => {
          e.stopPropagation();
          onConnect();
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            e.stopPropagation();
            onConnect();
          }
        }}
      >
        <i className="ri-terminal-box-line" aria-hidden="true" />
      </span>
    </button>
  );
};

function countLeaves(node: GroupNode): number {
  let count = 0;
  for (const child of node.children) {
    if (child.type === "leaf") count += 1;
    else count += countLeaves(child);
  }
  return count;
}

const TreeGroup = ({
  node,
  depth,
  expanded,
  toggleExpanded,
  activeConnectionId,
  onSelect,
  onDoubleClick,
  onConnect,
  onContextMenu
}: {
  node: GroupNode;
  depth: number;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
  activeConnectionId?: string;
  onSelect: (id: string) => void;
  onDoubleClick: (id: string) => void;
  onConnect: (id: string) => void;
  onContextMenu: (event: React.MouseEvent<HTMLButtonElement>, id: string) => void;
}) => {
  const isExpanded = expanded.has(node.key);
  return (
    <div className="ct-group" style={{ "--depth": depth } as React.CSSProperties}>
      {depth > 0 && (
        <GroupRow
          node={node}
          expanded={isExpanded}
          onToggle={() => toggleExpanded(node.key)}
        />
      )}
      {(depth === 0 || isExpanded) && (
        <div className="ct-group-children">
          {node.children.map((child) =>
            child.type === "group" ? (
              <TreeGroup
                key={child.key}
                node={child}
                depth={depth + 1}
                expanded={expanded}
                toggleExpanded={toggleExpanded}
                activeConnectionId={activeConnectionId}
                onSelect={onSelect}
                onDoubleClick={onDoubleClick}
                onConnect={onConnect}
                onContextMenu={onContextMenu}
              />
            ) : (
              <ServerRow
                key={child.connection.id}
                node={child}
                isActive={child.connection.id === activeConnectionId}
                onSelect={() => onSelect(child.connection.id)}
                onDoubleClick={() => onDoubleClick(child.connection.id)}
                onConnect={() => onConnect(child.connection.id)}
                onContextMenu={(event) => onContextMenu(event, child.connection.id)}
              />
            )
          )}
        </div>
      )}
    </div>
  );
};

const ConnectionContextMenu = ({
  state,
  connection,
  busy,
  onClose,
  onEditName,
  onEditCredentials,
  onEditServer
}: {
  state: ConnectionContextMenuState;
  connection: ConnectionProfile;
  busy: boolean;
  onClose: () => void;
  onEditName: (connection: ConnectionProfile) => void;
  onEditCredentials: (connection: ConnectionProfile) => void;
  onEditServer: (connectionId: string) => void;
}) => {
  const menuRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({
    left: state.x,
    top: state.y
  });
  const [visible, setVisible] = useState(false);

  useLayoutEffect(() => {
    const element = menuRef.current;
    if (!element) {
      return;
    }

    const { offsetWidth: width, offsetHeight: height } = element;
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const gap = 4;

    let top = state.y - height - gap;
    if (top < gap) {
      top = state.y + gap;
    }
    if (top + height > viewportHeight - gap) {
      top = viewportHeight - height - gap;
    }

    let left = state.x;
    if (left + width > viewportWidth - gap) {
      left = state.x - width;
    }
    if (left < gap) {
      left = gap;
    }

    setPos({ left, top });
    setVisible(true);
  }, [state.x, state.y]);

  useEffect(() => {
    const handleWindowMouseDown = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        onClose();
      }
    };
    window.addEventListener("mousedown", handleWindowMouseDown);
    return () => window.removeEventListener("mousedown", handleWindowMouseDown);
  }, [onClose]);

  const run = (fn: () => void) => {
    if (busy) {
      return;
    }
    fn();
    onClose();
  };

  return (
    <div
      ref={menuRef}
      className="ct-ctx-menu"
      style={{ left: pos.left, top: pos.top, visibility: visible ? "visible" : "hidden" }}
      onContextMenu={(event) => event.preventDefault()}
    >
      <button className="ct-ctx-item" disabled={busy} onClick={() => run(() => onEditName(connection))}>
        <span className="ct-ctx-icon">
          <i className="ri-edit-line" aria-hidden="true" />
        </span>
        编辑名称
      </button>
      <button className="ct-ctx-item" disabled={busy} onClick={() => run(() => onEditCredentials(connection))}>
        <span className="ct-ctx-icon">
          <i className="ri-key-2-line" aria-hidden="true" />
        </span>
        编辑凭据
      </button>
      <div className="ct-ctx-divider" />
      <button className="ct-ctx-item" disabled={busy} onClick={() => run(() => onEditServer(connection.id))}>
        <span className="ct-ctx-icon">
          <i className="ri-settings-3-line" aria-hidden="true" />
        </span>
        修改服务器
      </button>
      <div className="ct-ctx-connection-hint">
        {connection.host}:{connection.port}
      </div>
      <div className="ct-ctx-time-row">
        <span
          className="ct-ctx-time-item"
          title={`修改时间：${formatDateTime(connection.updatedAt)}`}
        >
          <i className="ri-edit-2-line" aria-hidden="true" />
          {formatRelativeTime(connection.updatedAt)}
        </span>
        <span
          className="ct-ctx-time-item"
          title={connection.lastConnectedAt
            ? `上次连接：${formatDateTime(connection.lastConnectedAt)}`
            : "从未连接"}
        >
          <i className="ri-plug-line" aria-hidden="true" />
          {connection.lastConnectedAt
            ? formatRelativeTime(connection.lastConnectedAt)
            : "从未连接"}
        </span>
      </div>
    </div>
  );
};

export const ConnectionTreePanel = ({
  connections,
  sshKeys,
  sessions,
  activeConnectionId,
  onSelect,
  onConnectByDoubleClick,
  onConnect,
  onQuickSave,
  onOpenManagerForConnection
}: ConnectionTreePanelProps) => {
  const { modal } = AntdApp.useApp();
  const [keyword, setKeyword] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(["root"]));
  const [contextMenu, setContextMenu] = useState<ConnectionContextMenuState | null>(null);
  const [editingCredentialConnectionId, setEditingCredentialConnectionId] = useState<string>();
  const [savingName, setSavingName] = useState(false);
  const [savingCredential, setSavingCredential] = useState(false);
  const [credentialForm] = Form.useForm<CredentialEditFormValues>();
  const credentialAuthType = Form.useWatch("authType", credentialForm);

  const tree = useMemo(
    () => buildTree(connections, sessions, keyword),
    [connections, sessions, keyword]
  );

  const contextMenuConnection = useMemo(
    () =>
      contextMenu
        ? connections.find((connection) => connection.id === contextMenu.connectionId)
        : undefined,
    [connections, contextMenu]
  );
  const editingCredentialConnection = useMemo(
    () =>
      editingCredentialConnectionId
        ? connections.find((connection) => connection.id === editingCredentialConnectionId)
        : undefined,
    [connections, editingCredentialConnectionId]
  );
  const busy = savingName || savingCredential;

  useEffect(() => {
    if (
      contextMenu &&
      !connections.some((connection) => connection.id === contextMenu.connectionId)
    ) {
      setContextMenu(null);
    }
    if (
      editingCredentialConnectionId &&
      !connections.some((connection) => connection.id === editingCredentialConnectionId)
    ) {
      setEditingCredentialConnectionId(undefined);
    }
  }, [connections, contextMenu, editingCredentialConnectionId]);

  const toggleExpanded = (key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  // Auto-expand all groups when keyword changes
  useMemo(() => {
    if (keyword.trim()) {
      const keys = new Set<string>(["root"]);
      const walk = (node: GroupNode) => {
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
    if (!editingCredentialConnectionId || !credentialAuthType) {
      return;
    }

    if (credentialAuthType === "agent") {
      credentialForm.setFieldsValue({
        password: undefined,
        sshKeyId: undefined
      });
      return;
    }

    if (credentialAuthType === "password") {
      credentialForm.setFieldValue("sshKeyId", undefined);
      return;
    }

    credentialForm.setFieldValue("password", undefined);
  }, [credentialAuthType, credentialForm, editingCredentialConnectionId]);

  const handleServerContextMenu = useCallback(
    (event: React.MouseEvent<HTMLButtonElement>, connectionId: string) => {
      event.preventDefault();
      event.stopPropagation();
      onSelect(connectionId);
      setContextMenu({
        x: event.clientX,
        y: event.clientY,
        connectionId
      });
    },
    [onSelect]
  );

  const handleEditName = useCallback(
    async (connection: ConnectionProfile) => {
      const name = await promptModal(modal, "编辑连接名称", "输入新的连接名称", connection.name);
      if (!name || name === connection.name) {
        return;
      }

      setSavingName(true);
      try {
        await onQuickSave(toConnectionUpsertInput(connection, { name }));
        message.success("名称已更新");
      } catch (error) {
        const reason = error instanceof Error ? error.message : "更新名称失败";
        message.error(reason);
      } finally {
        setSavingName(false);
      }
    },
    [modal, onQuickSave]
  );

  const handleOpenCredentialEditor = useCallback(
    (connection: ConnectionProfile) => {
      credentialForm.resetFields();
      credentialForm.setFieldsValue({
        username: connection.username,
        authType: connection.authType,
        sshKeyId: connection.authType === "privateKey" ? connection.sshKeyId : undefined,
        password: undefined
      });
      setEditingCredentialConnectionId(connection.id);
    },
    [credentialForm]
  );

  const handleSaveCredentials = useCallback(async () => {
    if (!editingCredentialConnection) {
      return;
    }

    let values: CredentialEditFormValues;
    try {
      values = await credentialForm.validateFields();
    } catch {
      return;
    }

    const username = (values.username ?? "").trim();
    const password = sanitizeOptionalText(values.password);

    if (values.authType === "privateKey" && !values.sshKeyId) {
      message.error("私钥认证需要选择一个 SSH 密钥。");
      return;
    }

    if (
      values.authType === "password" &&
      editingCredentialConnection.authType !== "password" &&
      !password
    ) {
      message.error("切换到密码认证时需要提供密码。");
      return;
    }

    setSavingCredential(true);
    try {
      await onQuickSave(
        toConnectionUpsertInput(editingCredentialConnection, {
          username,
          authType: values.authType,
          password: values.authType === "password" ? password : undefined,
          sshKeyId: values.authType === "privateKey" ? values.sshKeyId : undefined
        })
      );
      message.success("凭据已更新");
      setEditingCredentialConnectionId(undefined);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "更新凭据失败";
      message.error(reason);
    } finally {
      setSavingCredential(false);
    }
  }, [credentialForm, editingCredentialConnection, onQuickSave]);

  const handleOpenManager = useCallback(
    (connectionId: string) => {
      onOpenManagerForConnection(connectionId);
    },
    [onOpenManagerForConnection]
  );

  return (
    <section className="ct-panel">

      {/* Search */}
      <div className="ct-search">
        <i className="ri-search-line ct-search-icon" aria-hidden="true" />
        <input
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          placeholder="搜索名称 / IP / 分组路径"
          className="ct-search-input"
        />
        {keyword && (
          <button
            className="ct-search-clear"
            onClick={() => setKeyword("")}
            title="清除搜索"
          >
            <i className="ri-close-line" aria-hidden="true" />
          </button>
        )}
      </div>

      {/* Tree */}
      <div className="ct-tree">
        {tree.children.length === 0 ? (
          <div className="ct-empty">
            {keyword ? (
              <>
                <i className="ri-search-line" aria-hidden="true" />
                <span>未找到匹配的连接</span>
              </>
            ) : (
              <>
                <i className="ri-server-line" aria-hidden="true" />
                <span>暂无连接，点击右上角设置添加</span>
              </>
            )}
          </div>
        ) : (
          <TreeGroup
            node={tree}
            depth={0}
            expanded={expanded}
            toggleExpanded={toggleExpanded}
            activeConnectionId={activeConnectionId}
            onSelect={onSelect}
            onDoubleClick={onConnectByDoubleClick}
            onConnect={onConnect}
            onContextMenu={handleServerContextMenu}
          />
        )}
      </div>

      {contextMenu && contextMenuConnection ? (
        <ConnectionContextMenu
          state={contextMenu}
          connection={contextMenuConnection}
          busy={busy}
          onClose={() => setContextMenu(null)}
          onEditName={(connection) => {
            void handleEditName(connection);
          }}
          onEditCredentials={handleOpenCredentialEditor}
          onEditServer={handleOpenManager}
        />
      ) : null}

      <Modal
        open={Boolean(editingCredentialConnection)}
        title={editingCredentialConnection ? `编辑凭据 · ${editingCredentialConnection.name}` : "编辑凭据"}
        okText="保存"
        cancelText="取消"
        confirmLoading={savingCredential}
        onCancel={() => {
          if (savingCredential) {
            return;
          }
          setEditingCredentialConnectionId(undefined);
        }}
        onOk={() => {
          void handleSaveCredentials();
        }}
        destroyOnHidden
      >
        <Form
          form={credentialForm}
          layout="vertical"
          requiredMark={false}
          onFinish={() => void handleSaveCredentials()}
        >
          <Form.Item label="用户名" name="username">
            <Input placeholder="root" autoFocus />
          </Form.Item>

          <Form.Item label="认证方式" name="authType" rules={[{ required: true }]}>
            <Select
              options={[
                { label: "密码", value: "password" },
                { label: "私钥", value: "privateKey" },
                { label: "SSH Agent", value: "agent" }
              ]}
            />
          </Form.Item>

          {credentialAuthType === "password" ? (
            <Form.Item label="密码" name="password">
              <Input.Password placeholder="输入新密码（留空则不更新）" />
            </Form.Item>
          ) : null}

          {credentialAuthType === "privateKey" ? (
            <Form.Item
              label="SSH 密钥"
              name="sshKeyId"
              rules={[{ required: true, message: "请选择一个 SSH 密钥" }]}
            >
              <Select
                placeholder="选择密钥..."
                allowClear
                options={sshKeys.map((key) => ({ label: key.name, value: key.id }))}
                notFoundContent={
                  <div style={{ textAlign: "center", padding: "8px 0", color: "var(--text-muted)" }}>
                    暂无密钥，请先在连接管理器添加
                  </div>
                }
              />
            </Form.Item>
          ) : null}
        </Form>
      </Modal>
    </section>
  );
};
