import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Form, Input, InputNumber, Modal, Select } from "antd";
import type { ConnectionProfile, SessionDescriptor, SshKeyProfile } from "@nextshell/core";
import type { QuickCreateConnectionInput } from "../utils/quickConnectInput";

interface QuickConnectBarProps {
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  sessions: SessionDescriptor[];
  onConnect: (connectionId: string) => void;
  onQuickConnectInput: (raw: string) => Promise<boolean>;
  onQuickCreateConnection: (input: QuickCreateConnectionInput) => Promise<boolean>;
}

interface ResultItem {
  connection: ConnectionProfile;
  isConnected: boolean;
}

interface QuickCreateFormValues {
  name?: string;
  host: string;
  port: number;
  username?: string;
  authType: "password" | "privateKey";
  password?: string;
  sshKeyId?: string;
}

type DisplayItem =
  | { type: "create-action"; id: "create-action" }
  | { type: "quick-input-action"; id: "quick-input-action" }
  | { type: "connection"; item: ResultItem };

const MAX_RECENT = 6;
const QUICK_CREATE_DEFAULT_VALUES: Pick<QuickCreateFormValues, "port" | "authType"> = {
  port: 22,
  authType: "password"
};

export const QuickConnectBar = ({
  connections,
  sshKeys,
  sessions,
  onConnect,
  onQuickConnectInput,
  onQuickCreateConnection
}: QuickConnectBarProps) => {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [activeIndex, setActiveIndex] = useState(-1);
  const [submitting, setSubmitting] = useState(false);
  const [quickInputMode, setQuickInputMode] = useState(false);
  const [quickCreateOpen, setQuickCreateOpen] = useState(false);
  const [quickCreateSaving, setQuickCreateSaving] = useState(false);
  const [quickCreateForm] = Form.useForm<QuickCreateFormValues>();
  const quickCreateAuthType = Form.useWatch("authType", quickCreateForm);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const isPlusPrefixed = keyword.trimStart().startsWith("+");

  const connectedIds = useMemo(
    () =>
      new Set(
        sessions
          .filter((s) => s.status === "connected" && s.type === "terminal")
          .map((s) => s.connectionId),
      ),
    [sessions],
  );

  const recentConnections = useMemo<ResultItem[]>(() => {
    return [...connections]
      .filter((c) => c.lastConnectedAt)
      .sort(
        (a, b) =>
          new Date(b.lastConnectedAt!).getTime() -
          new Date(a.lastConnectedAt!).getTime(),
      )
      .slice(0, MAX_RECENT)
      .map((c) => ({ connection: c, isConnected: connectedIds.has(c.id) }));
  }, [connections, connectedIds]);

  const filteredResults = useMemo<ResultItem[]>(() => {
    const lower = keyword.trim().toLowerCase();
    if (!lower) return recentConnections;
    return connections
      .filter((c) => {
        const searchable =
          `${c.name} ${c.host} ${c.tags.join(" ")} ${c.groupPath} ${c.notes ?? ""}`.toLowerCase();
        return searchable.includes(lower);
      })
      .slice(0, 12)
      .map((c) => ({ connection: c, isConnected: connectedIds.has(c.id) }));
  }, [keyword, connections, connectedIds, recentConnections]);

  const displayItems = useMemo<DisplayItem[]>(() => {
    if (quickInputMode) {
      return [];
    }

    if (isPlusPrefixed) {
      return [{ type: "quick-input-action", id: "quick-input-action" }];
    }

    if (keyword.trim()) {
      return filteredResults.map((item) => ({
        type: "connection",
        item
      }));
    }

    return [
      { type: "create-action", id: "create-action" },
      ...filteredResults.map((item) => ({
        type: "connection" as const,
        item
      }))
    ];
  }, [filteredResults, isPlusPrefixed, keyword, quickInputMode]);

  const handleOpen = useCallback(() => {
    setOpen(true);
    setActiveIndex(-1);
  }, []);

  const handleClose = useCallback(() => {
    setOpen(false);
    setKeyword("");
    setActiveIndex(-1);
    setSubmitting(false);
    setQuickInputMode(false);
    inputRef.current?.blur();
  }, []);

  const handleSelect = useCallback(
    (connectionId: string) => {
      onConnect(connectionId);
      handleClose();
    },
    [handleClose, onConnect]
  );

  const handleOpenQuickCreateDialog = useCallback(() => {
    quickCreateForm.resetFields();
    quickCreateForm.setFieldsValue(QUICK_CREATE_DEFAULT_VALUES);
    setQuickCreateOpen(true);
    setOpen(false);
    setActiveIndex(-1);
  }, [quickCreateForm]);

  const handleOpenQuickInputMode = useCallback(() => {
    setQuickInputMode(true);
    setKeyword("");
    setActiveIndex(-1);
    inputRef.current?.focus();
  }, []);

  useEffect(() => {
    if (!quickCreateOpen || !quickCreateAuthType) {
      return;
    }

    if (quickCreateAuthType === "password") {
      quickCreateForm.setFieldValue("sshKeyId", undefined);
      return;
    }

    quickCreateForm.setFieldValue("password", undefined);
  }, [quickCreateAuthType, quickCreateForm, quickCreateOpen]);

  const handleSubmitQuickCreate = useCallback(async (): Promise<void> => {
    if (quickCreateSaving) {
      return;
    }

    let values: QuickCreateFormValues;
    try {
      values = await quickCreateForm.validateFields();
    } catch {
      return;
    }

    const nextPort = Number(values.port);
    if (!Number.isInteger(nextPort) || nextPort < 1 || nextPort > 65535) {
      return;
    }
    if (values.authType === "privateKey" && !values.sshKeyId) {
      quickCreateForm.setFields([
        {
          name: "sshKeyId",
          errors: ["请选择一个 SSH 密钥"]
        }
      ]);
      return;
    }

    setQuickCreateSaving(true);
    try {
      const accepted = await onQuickCreateConnection({
        name: values.name,
        host: values.host,
        port: nextPort,
        username: values.username,
        authType: values.authType,
        password: values.password,
        sshKeyId: values.sshKeyId
      });
      if (!accepted) {
        return;
      }
      setQuickCreateOpen(false);
      quickCreateForm.resetFields();
      handleClose();
    } finally {
      setQuickCreateSaving(false);
    }
  }, [
    handleClose,
    onQuickCreateConnection,
    quickCreateForm,
    quickCreateSaving
  ]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!open) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((i) => Math.min(i + 1, displayItems.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const item = displayItems[activeIndex];
        if (item?.type === "create-action") {
          handleOpenQuickCreateDialog();
          return;
        }
        if (item?.type === "quick-input-action") {
          handleOpenQuickInputMode();
          return;
        }
        if (item?.type === "connection") {
          handleSelect(item.item.connection.id);
          return;
        }

        const raw = keyword.trim();
        if (!quickInputMode || !raw || submitting) {
          return;
        }

        setSubmitting(true);
        void onQuickConnectInput(raw)
          .then((accepted) => {
            if (accepted) {
              handleClose();
            }
          })
          .finally(() => {
            setSubmitting(false);
          });
      } else if (e.key === "Escape") {
        e.preventDefault();
        handleClose();
      }
    },
    [
      activeIndex,
      displayItems,
      handleClose,
      handleOpenQuickCreateDialog,
      handleOpenQuickInputMode,
      handleSelect,
      keyword,
      onQuickConnectInput,
      open,
      quickInputMode,
      submitting,
    ],
  );

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        handleClose();
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, handleClose]);

  const sectionLabel = keyword.trim()
    ? `${filteredResults.length} 个结果`
    : "最近连接";
  const firstConnectionIndex = displayItems.findIndex((item) => item.type === "connection");
  const inputPlaceholder = quickInputMode
    ? "输入 username@host[:port] 后按 Enter 连接…"
    : "快速连接服务器…";

  return (
    <div
      ref={containerRef}
      className={`qcb-wrap${open ? " qcb-open" : ""}`}
    >
      <div className="qcb-field" onClick={handleOpen}>
        <i className="ri-search-line qcb-icon" aria-hidden="true" />
        <input
          ref={inputRef}
          className="qcb-input"
          placeholder={inputPlaceholder}
          value={keyword}
          onFocus={handleOpen}
          onChange={(e) => {
            setKeyword(e.target.value);
            setActiveIndex(-1);
          }}
          onKeyDown={handleKeyDown}
          aria-label="快速连接"
          spellCheck={false}
          autoComplete="off"
        />
        {open && keyword && (
          <button
            className="qcb-clear"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              setKeyword("");
              setActiveIndex(-1);
              inputRef.current?.focus();
            }}
          >
            <i className="ri-close-line" aria-hidden="true" />
          </button>
        )}
        {!open && (
          <kbd className="qcb-shortcut">⌘K</kbd>
        )}
      </div>

      {open && (
        <div className="qcb-dropdown">
          {quickInputMode ? (
            <>
              <div className="qcb-section-label">快速输入服务器</div>
              <div className="qcb-empty qcb-empty-hint">
                <i className="ri-terminal-box-line" aria-hidden="true" />
                <span>输入 username@host[:port] 后按 Enter 连接</span>
              </div>
              <div className="qcb-footer">
                <span>↵ 连接</span>
                <span>Esc 关闭</span>
              </div>
            </>
          ) : keyword.trim() && filteredResults.length === 0 && !isPlusPrefixed ? (
            <div className="qcb-empty">
              <i className="ri-server-line" aria-hidden="true" />
              <span>
                未找到匹配的服务器
              </span>
            </div>
          ) : (
            <>
              {displayItems.map((item, idx) => {
                if (item.type === "create-action") {
                  return (
                    <QuickCreateActionItem
                      key={item.id}
                      isActive={idx === activeIndex}
                      onSelect={handleOpenQuickCreateDialog}
                      onMouseEnter={() => setActiveIndex(idx)}
                    />
                  );
                }
                if (item.type === "quick-input-action") {
                  return (
                    <QuickInputActionItem
                      key={item.id}
                      isActive={idx === activeIndex}
                      onSelect={handleOpenQuickInputMode}
                      onMouseEnter={() => setActiveIndex(idx)}
                    />
                  );
                }

                const node = (
                  <QuickConnectItem
                    key={item.item.connection.id}
                    item={item.item}
                    isActive={idx === activeIndex}
                    keyword={keyword}
                    onSelect={() => handleSelect(item.item.connection.id)}
                    onMouseEnter={() => setActiveIndex(idx)}
                  />
                );

                if (idx === firstConnectionIndex) {
                  return (
                    <div key={`section-${item.item.connection.id}`}>
                      <div className="qcb-section-label">{sectionLabel}</div>
                      {node}
                    </div>
                  );
                }

                return node;
              })}
              <div className="qcb-footer">
                <span>↑↓ 导航</span>
                <span>↵ 打开/连接</span>
                <span>Esc 关闭</span>
              </div>
            </>
          )}
        </div>
      )}

      <Modal
        open={quickCreateOpen}
        title="添加新服务器"
        okText="保存并连接"
        cancelText="取消"
        confirmLoading={quickCreateSaving}
        destroyOnHidden
        onCancel={() => {
          if (quickCreateSaving) return;
          setQuickCreateOpen(false);
        }}
        onOk={() => {
          void handleSubmitQuickCreate();
        }}
      >
        <Form
          form={quickCreateForm}
          layout="vertical"
          requiredMark={false}
          initialValues={QUICK_CREATE_DEFAULT_VALUES}
          onFinish={() => {
            void handleSubmitQuickCreate();
          }}
        >
          <Form.Item label="名称" name="name">
            <Input placeholder="可选，留空使用 host:port" />
          </Form.Item>
          <div className="qcb-create-row">
            <Form.Item
              label="Host / IP"
              name="host"
              rules={[
                { required: true, message: "请输入主机地址" },
                {
                  validator: (_, value: string | undefined) => {
                    if (typeof value === "string" && value.trim().length > 0) {
                      return Promise.resolve();
                    }
                    return Promise.reject(new Error("请输入主机地址"));
                  }
                }
              ]}
              style={{ flex: 1 }}
            >
              <Input placeholder="192.168.1.10 或 example.com" autoFocus />
            </Form.Item>
            <Form.Item
              label="端口"
              name="port"
              rules={[{ required: true, message: "请输入端口" }]}
              className="qcb-create-port"
            >
              <InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} />
            </Form.Item>
          </div>
          <Form.Item label="用户名" name="username">
            <Input placeholder="可选，默认留空" />
          </Form.Item>
          <Form.Item label="登录方式" name="authType" rules={[{ required: true }]}>
            <Select
              options={[
                { label: "密码登录", value: "password" },
                { label: "密钥登录", value: "privateKey" }
              ]}
            />
          </Form.Item>
          {quickCreateAuthType === "password" ? (
            <Form.Item label="密码" name="password">
              <Input.Password placeholder="可选，留空后首次连接时再输入" />
            </Form.Item>
          ) : null}
          {quickCreateAuthType === "privateKey" ? (
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
    </div>
  );
};

const QuickCreateActionItem = ({
  isActive,
  onSelect,
  onMouseEnter
}: {
  isActive: boolean;
  onSelect: () => void;
  onMouseEnter: () => void;
}) => (
  <button
    type="button"
    className={`qcb-item qcb-item-create${isActive ? " active" : ""}`}
    onMouseDown={(e) => e.preventDefault()}
    onClick={onSelect}
    onMouseEnter={onMouseEnter}
  >
    <span className="qcb-dot qcb-dot-create">
      <i className="ri-add-circle-line" aria-hidden="true" />
    </span>
    <span className="qcb-item-body">
      <span className="qcb-item-name">添加新服务器</span>
      <span className="qcb-item-group">快速创建并立即连接</span>
    </span>
  </button>
);

const QuickInputActionItem = ({
  isActive,
  onSelect,
  onMouseEnter
}: {
  isActive: boolean;
  onSelect: () => void;
  onMouseEnter: () => void;
}) => (
  <button
    type="button"
    className={`qcb-item qcb-item-quick-input${isActive ? " active" : ""}`}
    onMouseDown={(e) => e.preventDefault()}
    onClick={onSelect}
    onMouseEnter={onMouseEnter}
  >
    <span className="qcb-dot qcb-dot-quick-input">
      <i className="ri-keyboard-box-line" aria-hidden="true" />
    </span>
    <span className="qcb-item-body">
      <span className="qcb-item-name">快速输入服务器</span>
      <span className="qcb-item-group">输入 username@host[:port] 进行连接</span>
    </span>
  </button>
);

interface QuickConnectItemProps {
  item: ResultItem;
  isActive: boolean;
  keyword: string;
  onSelect: () => void;
  onMouseEnter: () => void;
}

const QuickConnectItem = ({
  item,
  isActive,
  keyword,
  onSelect,
  onMouseEnter,
}: QuickConnectItemProps) => {
  const c = item.connection;
  const groupLabel = c.groupPath && c.groupPath !== "/" ? c.groupPath : null;

  return (
    <button
      type="button"
      className={`qcb-item${isActive ? " active" : ""}`}
      onMouseDown={(e) => e.preventDefault()}
      onClick={onSelect}
      onMouseEnter={onMouseEnter}
    >
      <span className={`qcb-dot${item.isConnected ? " online" : ""}`} />
      <span className="qcb-item-body">
        <span className="qcb-item-name">
          {highlight(c.name, keyword)}
        </span>
        {groupLabel && (
          <span className="qcb-item-group">{groupLabel}</span>
        )}
      </span>
      <span className="qcb-item-host">
        {highlight(c.host, keyword)}
        <span className="qcb-item-port">:{c.port}</span>
      </span>
      <span className="qcb-item-action" title="新建终端连接" aria-label="新建终端连接">
        <i className="ri-terminal-box-line" aria-hidden="true" />
      </span>
    </button>
  );
};

function highlight(text: string, keyword: string): React.ReactNode {
  if (!keyword.trim()) return text;
  const lower = keyword.trim().toLowerCase();
  const idx = text.toLowerCase().indexOf(lower);
  if (idx === -1) return text;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="qcb-hl">{text.slice(idx, idx + lower.length)}</mark>
      {text.slice(idx + lower.length)}
    </>
  );
}
