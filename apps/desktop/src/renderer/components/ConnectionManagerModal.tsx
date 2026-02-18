import { useCallback, useEffect, useMemo, useState } from "react";
import { Form, Input, InputNumber, Modal, Select, Switch, Tooltip, message } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";

interface ConnectionManagerModalProps {
  open: boolean;
  connections: ConnectionProfile[];
  onClose: () => void;
  onConnectionSaved: (payload: ConnectionUpsertInput) => Promise<void>;
  onConnectionRemoved: (connectionId: string) => Promise<void>;
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
  onSelect
}: {
  connection: ConnectionProfile;
  isSelected: boolean;
  onSelect: () => void;
}) => (
  <button
    type="button"
    className={`mgr-server-row${isSelected ? " selected" : ""}`}
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
);

const MgrTreeGroup = ({
  node,
  depth,
  expanded,
  toggleExpanded,
  selectedConnectionId,
  onSelect
}: {
  node: MgrGroupNode;
  depth: number;
  expanded: Set<string>;
  toggleExpanded: (key: string) => void;
  selectedConnectionId: string | undefined;
  onSelect: (id: string) => void;
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
                onSelect={onSelect}
              />
            ) : (
              <MgrServerRow
                key={child.connection.id}
                connection={child.connection}
                isSelected={child.connection.id === selectedConnectionId}
                onSelect={() => onSelect(child.connection.id)}
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
  proxyType: "none" as const,
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8" as const,
  backspaceMode: "ascii-backspace" as const,
  deleteMode: "vt220-delete" as const,
  groupPath: ["server"],
  tags: [],
  favorite: false,
  monitorSession: false
};

/* ── Main component ─────────────────────────────────────── */

export const ConnectionManagerModal = ({
  open,
  connections,
  onClose,
  onConnectionSaved,
  onConnectionRemoved
}: ConnectionManagerModalProps) => {
  const [mode, setMode] = useState<"idle" | "new" | "edit">("idle");
  const [keyword, setKeyword] = useState("");
  const [selectedConnectionId, setSelectedConnectionId] = useState<string>();
  const [expanded, setExpanded] = useState<Set<string>>(new Set(["root"]));
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<ConnectionUpsertInput>();
  const authType = Form.useWatch("authType", form);
  const proxyType = Form.useWatch("proxyType", form);

  const tree = useMemo(
    () => buildManagerTree(connections, keyword),
    [connections, keyword]
  );

  useEffect(() => {
    if (!open) return;
    form.resetFields();
    form.setFieldsValue(DEFAULT_VALUES);
    setSelectedConnectionId(undefined);
    setMode("idle");
    setKeyword("");
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
        privateKeyPath: undefined,
        privateKeyContent: undefined
      });
      return;
    }

    if (authType === "password") {
      form.setFieldsValue({
        privateKeyPath: undefined,
        privateKeyContent: undefined
      });
      return;
    }

    form.setFieldValue("password", undefined);
  }, [authType, form, open]);

  useEffect(() => {
    if (!open || !proxyType) return;

    if (proxyType === "none") {
      form.setFieldsValue({
        proxyHost: undefined,
        proxyPort: undefined,
        proxyUsername: undefined,
        proxyPassword: undefined
      });
      return;
    }

    if (proxyType === "socks4") {
      form.setFieldValue("proxyPassword", undefined);
    }
  }, [form, open, proxyType]);

  const selectedConnection = useMemo(
    () => connections.find((c) => c.id === selectedConnectionId),
    [connections, selectedConnectionId]
  );

  const applyConnectionToForm = useCallback((connection: ConnectionProfile) => {
    form.setFieldsValue({
      id: connection.id,
      name: connection.name,
      host: connection.host,
      port: connection.port,
      username: connection.username,
      authType: connection.authType,
      privateKeyPath: connection.privateKeyPath,
      hostFingerprint: connection.hostFingerprint,
      strictHostKeyChecking: connection.strictHostKeyChecking,
      proxyType: connection.proxyType,
      proxyHost: connection.proxyHost,
      proxyPort: connection.proxyPort,
      proxyUsername: connection.proxyUsername,
      terminalEncoding: connection.terminalEncoding,
      backspaceMode: connection.backspaceMode,
      deleteMode: connection.deleteMode,
      groupPath: connection.groupPath,
      tags: connection.tags,
      notes: connection.notes,
      favorite: connection.favorite,
      monitorSession: connection.monitorSession,
      password: undefined,
      proxyPassword: undefined
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
    setSelectedConnectionId(connectionId);
    applyConnectionToForm(connection);
    setMode("edit");
  }, [connections, applyConnectionToForm]);

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

  const toggleExpanded = useCallback((key: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }, []);

  return (
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
      <div className={mode !== "idle" ? "grid grid-cols-[230px_1fr] h-[580px] overflow-hidden" : "h-[580px] overflow-hidden"}>

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
                onSelect={handleSelect}
              />
            )}
          </div>

          {/* Footer */}
          <div className="mgr-sidebar-footer">
            <span className="mgr-count">{connections.length} 个连接</span>
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
                    disabled={saving}
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
                const password = sanitizeOptionalText(values.password);
                const privateKeyPath = sanitizeOptionalText(values.privateKeyPath);
                const privateKeyContent = sanitizeOptionalText(values.privateKeyContent);
                const hostFingerprint = sanitizeOptionalText(values.hostFingerprint);
                const selectedProxyType = values.proxyType ?? "none";
                const proxyHost = sanitizeOptionalText(values.proxyHost);
                const proxyPort =
                  values.proxyPort === undefined || values.proxyPort === null
                    ? undefined
                    : Number(values.proxyPort);
                const proxyUsername = sanitizeOptionalText(values.proxyUsername);
                const proxyPassword = sanitizeOptionalText(values.proxyPassword);
                const groupPath = sanitizeTextArray(values.groupPath);
                const tags = sanitizeTextArray(values.tags);
                const notes = sanitizeOptionalText(values.notes);
                const port = Number(values.port);
                const terminalEncoding = values.terminalEncoding ?? "utf-8";
                const backspaceMode = values.backspaceMode ?? "ascii-backspace";
                const deleteMode = values.deleteMode ?? "vt220-delete";

                if (
                  values.authType === "privateKey" &&
                  !privateKeyPath &&
                  !privateKeyContent &&
                  !selectedConnection?.privateKeyRef
                ) {
                  message.error("私钥认证需要提供私钥路径或导入私钥内容。");
                  return;
                }

                if (values.strictHostKeyChecking && !hostFingerprint) {
                  message.error("启用严格主机校验时必须填写主机指纹。");
                  return;
                }

                if (!Number.isInteger(port) || port < 1 || port > 65535) {
                  message.error("端口必须是 1-65535 的整数。");
                  return;
                }

                if (selectedProxyType !== "none") {
                  if (!proxyHost) {
                    message.error("启用代理时必须填写代理地址。");
                    return;
                  }

                  if (!Number.isInteger(proxyPort) || (proxyPort ?? 0) < 1 || (proxyPort ?? 0) > 65535) {
                    message.error("代理端口必须是 1-65535 的整数。");
                    return;
                  }
                }

                if (selectedProxyType === "socks4" && proxyPassword) {
                  message.error("SOCKS4 不支持代理密码。");
                  return;
                }

                if (selectedProxyType === "socks5" && proxyPassword && !proxyUsername) {
                  message.error("设置 SOCKS5 代理密码时必须填写代理用户名。");
                  return;
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
                    privateKeyPath: values.authType === "privateKey" ? privateKeyPath : undefined,
                    privateKeyContent: values.authType === "privateKey" ? privateKeyContent : undefined,
                    hostFingerprint,
                    strictHostKeyChecking: values.strictHostKeyChecking ?? false,
                    proxyType: selectedProxyType,
                    proxyHost: selectedProxyType === "none" ? undefined : proxyHost,
                    proxyPort: selectedProxyType === "none" ? undefined : proxyPort,
                    proxyUsername: selectedProxyType === "none" ? undefined : proxyUsername,
                    proxyPassword: selectedProxyType === "socks5" ? proxyPassword : undefined,
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
                    password: undefined,
                    privateKeyContent: undefined,
                    proxyPassword: undefined
                  });
                } catch (error) {
                  const reason = error instanceof Error ? error.message : "保存失败";
                  message.error(reason);
                } finally {
                  setSaving(false);
                }
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
                <>
                  <Form.Item
                    label="私钥路径（可选）"
                    name="privateKeyPath"
                    preserve={false}
                  >
                    <Input
                      placeholder="~/.ssh/id_ed25519"
                      className="mgr-mono-input"
                    />
                  </Form.Item>
                  <Form.Item
                    label="导入私钥内容（可选，保存为加密引用）"
                    name="privateKeyContent"
                    preserve={false}
                  >
                    <Input.TextArea
                      rows={4}
                      placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----"}
                      className="mgr-mono-input"
                    />
                  </Form.Item>
                </>
              ) : null}

              {authType === "password" || authType === "privateKey" ? (
                <Form.Item
                  label={authType === "password" ? "密码" : "私钥 Passphrase"}
                  name="password"
                  preserve={false}
                >
                  <Input.Password placeholder={authType === "password" ? "输入密码（留空则不更新）" : "输入 Passphrase（可选）"} />
                </Form.Item>
              ) : null}

              <div className="mgr-section-label mgr-section-gap">网络代理</div>

              <Form.Item
                label="代理类型"
                name="proxyType"
              >
                <Select
                  options={[
                    { label: "直连", value: "none" },
                    { label: "SOCKS4", value: "socks4" },
                    { label: "SOCKS5", value: "socks5" }
                  ]}
                />
              </Form.Item>

              {proxyType !== "none" ? (
                <>
                  <div className="flex gap-3 items-start">
                    <Form.Item
                      label="代理地址"
                      name="proxyHost"
                      className="flex-1"
                    >
                      <Input placeholder="127.0.0.1" className="mgr-mono-input" />
                    </Form.Item>
                    <Form.Item
                      label="代理端口"
                      name="proxyPort"
                      className="w-[90px] shrink-0"
                    >
                      <InputNumber min={1} max={65535} precision={0} placeholder="1080" style={{ width: "100%" }} />
                    </Form.Item>
                  </div>

                  <div className="flex gap-3 items-start">
                    <Form.Item
                      label={proxyType === "socks4" ? "代理 User ID（可选）" : "代理用户名（可选）"}
                      name="proxyUsername"
                      className="flex-1"
                    >
                      <Input placeholder={proxyType === "socks4" ? "可选 userId" : "可选用户名"} />
                    </Form.Item>
                    {proxyType === "socks5" ? (
                      <Form.Item
                        label="代理密码（可选）"
                        name="proxyPassword"
                        className="flex-1"
                      >
                        <Input.Password placeholder="留空则不更新" />
                      </Form.Item>
                    ) : null}
                  </div>

                  <div className="mgr-form-subtitle">修改代理设置后，需重连 SSH 会话才会生效。</div>
                </>
              ) : null}

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
    </Modal>
  );
};
