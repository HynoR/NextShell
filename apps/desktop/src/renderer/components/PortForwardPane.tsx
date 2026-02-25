import { useCallback, useMemo, useState } from "react";
import { Form, Input, InputNumber, Modal, Select, Switch, Tag, message } from "antd";
import type { ConnectionProfile, SshPortForwardRule } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { formatErrorMessage } from "../utils/errorMessage";

interface PortForwardPaneProps {
  connection?: ConnectionProfile;
  connected: boolean;
  onSaveConnection: (payload: ConnectionUpsertInput) => Promise<void>;
}

interface PortForwardFormValues {
  name?: string;
  type: SshPortForwardRule["type"];
  sourceHost: string;
  sourcePort?: number;
  destinationHost: string;
  destinationPort?: number;
  enabled: boolean;
}

const buildUpsertInput = (
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

const typeLabel: Record<SshPortForwardRule["type"], string> = {
  local: "本地转发",
  remote: "远程转发"
};

const typeTagColor: Record<SshPortForwardRule["type"], string> = {
  local: "blue",
  remote: "purple"
};

export const PortForwardPane = ({
  connection,
  connected,
  onSaveConnection
}: PortForwardPaneProps) => {
  const [editingRule, setEditingRule] = useState<SshPortForwardRule | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<PortForwardFormValues>();
  const watchedType = Form.useWatch("type", form);

  const rules = useMemo(() => connection?.portForwards ?? [], [connection?.portForwards]);

  const openEditor = useCallback(
    (rule?: SshPortForwardRule, presetType?: SshPortForwardRule["type"]) => {
      const nextRule = rule ?? null;
      setEditingRule(nextRule);
      form.resetFields();
      const type = nextRule?.type ?? presetType ?? "local";
      form.setFieldsValue({
        name: nextRule?.name,
        type,
        sourceHost: nextRule?.sourceHost ?? "127.0.0.1",
        sourcePort: nextRule?.sourcePort,
        destinationHost: nextRule?.destinationHost ?? "127.0.0.1",
        destinationPort: nextRule?.destinationPort,
        enabled: nextRule?.enabled ?? true
      });
      setModalOpen(true);
    },
    [form]
  );

  const saveRules = useCallback(
    async (nextRules: SshPortForwardRule[]) => {
      if (!connection) {
        message.warning("请先选择一个连接。");
        return;
      }
      setSaving(true);
      try {
        await onSaveConnection(buildUpsertInput(connection, { portForwards: nextRules }));
        message.success("端口转发已保存");
      } catch (error) {
        message.error(`保存失败：${formatErrorMessage(error, "请稍后重试")}`);
      } finally {
        setSaving(false);
      }
    },
    [connection, onSaveConnection]
  );

  const handleSaveRule = useCallback(async () => {
    let values: PortForwardFormValues;
    try {
      values = await form.validateFields();
    } catch {
      return;
    }

    if (!connection) {
      message.warning("请先选择一个连接。");
      return;
    }

    const rule: SshPortForwardRule = {
      id: editingRule?.id ?? crypto.randomUUID(),
      name: values.name?.trim() || undefined,
      type: values.type,
      sourceHost: values.sourceHost.trim(),
      sourcePort: Number(values.sourcePort),
      destinationHost: values.destinationHost.trim(),
      destinationPort: Number(values.destinationPort),
      enabled: values.enabled ?? true
    };

    const nextRules = editingRule
      ? rules.map((item) => (item.id === editingRule.id ? rule : item))
      : [...rules, rule];

    await saveRules(nextRules);
    setModalOpen(false);
    setEditingRule(null);
  }, [connection, editingRule, form, rules, saveRules]);

  const handleToggleEnabled = useCallback(
    (rule: SshPortForwardRule, enabled: boolean) => {
      const nextRules = rules.map((item) => (item.id === rule.id ? { ...item, enabled } : item));
      void saveRules(nextRules);
    },
    [rules, saveRules]
  );

  const handleDelete = useCallback(
    (rule: SshPortForwardRule) => {
      Modal.confirm({
        title: "删除端口转发",
        content: `确认删除「${rule.name || `${rule.sourceHost}:${rule.sourcePort}`}」？`,
        okText: "删除",
        cancelText: "取消",
        okButtonProps: { danger: true },
        onOk: async () => {
          const nextRules = rules.filter((item) => item.id !== rule.id);
          await saveRules(nextRules);
        }
      });
    },
    [rules, saveRules]
  );

  const effectiveType = watchedType ?? editingRule?.type ?? "local";
  const sourceLabel = effectiveType === "local" ? "本地监听" : "远程监听";

  return (
    <div className="pf-pane">
      <div className="pf-header">
        <div>
          <div className="pf-title">端口转发</div>
          <div className="pf-subtitle">在连接建立后自动启用本地 / 远程端口转发。</div>
          <div className="pf-status">
            {connection ? (
              <span className={connected ? "pf-status--online" : "pf-status--offline"}>
                {connected ? "连接已建立" : "未连接，转发将在连接后生效"}
              </span>
            ) : (
              <span className="pf-status--offline">请选择一个连接后配置端口转发</span>
            )}
          </div>
        </div>
        <div className="pf-actions">
          <button
            type="button"
            className="pf-action-btn"
            disabled={!connection || !connected || saving}
            onClick={() => openEditor(undefined, "local")}
          >
            <i className="ri-arrow-left-right-line" aria-hidden="true" />
            本地转发
          </button>
          <button
            type="button"
            className="pf-action-btn primary"
            disabled={!connection || !connected || saving}
            onClick={() => openEditor(undefined, "remote")}
          >
            <i className="ri-cloud-line" aria-hidden="true" />
            远程转发
          </button>
        </div>
      </div>

      <div className="pf-list">
        {!connection ? (
          <div className="pf-empty">
            <div>
              <i className="ri-share-forward-line" aria-hidden="true" />
              <div>暂未选择连接</div>
            </div>
          </div>
        ) : rules.length === 0 ? (
          <div className="pf-empty">
            <div>
              <i className="ri-share-forward-line" aria-hidden="true" />
              <div>暂无端口转发规则</div>
            </div>
          </div>
        ) : (
          rules.map((rule) => (
            <div key={rule.id} className={`pf-card${rule.enabled ? "" : " pf-card--disabled"}`}>
              <div className="pf-card-main">
                <div className="pf-card-head">
                  <span className="pf-card-name">
                    {rule.name || `${rule.sourceHost}:${rule.sourcePort}`}
                  </span>
                  <Tag color={typeTagColor[rule.type]}>{typeLabel[rule.type]}</Tag>
                  {!rule.enabled ? <span className="pf-card-muted">已停用</span> : null}
                </div>
                <div className="pf-card-meta">
                  {rule.sourceHost}:{rule.sourcePort} → {rule.destinationHost}:{rule.destinationPort}
                </div>
              </div>
              <div className="pf-card-actions">
                <Switch
                  checked={rule.enabled}
                  size="small"
                  disabled={saving}
                  onChange={(checked) => handleToggleEnabled(rule, checked)}
                />
                <button
                  type="button"
                  className="cc-action-btn"
                  disabled={saving}
                  onClick={() => openEditor(rule)}
                  title="编辑"
                >
                  <i className="ri-edit-2-line" aria-hidden="true" />
                </button>
                <button
                  type="button"
                  className="cc-action-btn danger"
                  disabled={saving}
                  onClick={() => handleDelete(rule)}
                  title="删除"
                >
                  <i className="ri-delete-bin-line" aria-hidden="true" />
                </button>
              </div>
            </div>
          ))
        )}
      </div>

      <Modal
        open={modalOpen}
        title={editingRule ? "编辑端口转发" : "新建端口转发"}
        okText={saving ? "保存中..." : "保存"}
        cancelText="取消"
        onCancel={() => {
          if (saving) return;
          setModalOpen(false);
          setEditingRule(null);
        }}
        onOk={() => { void handleSaveRule(); }}
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <Form.Item label="名称（可选）" name="name">
            <Input placeholder="如 Web API / DB" />
          </Form.Item>

          <Form.Item label="转发类型" name="type" rules={[{ required: true }]}>
            <Select
              options={[
                { label: "本地转发 (Local)", value: "local" },
                { label: "远程转发 (Remote)", value: "remote" }
              ]}
            />
          </Form.Item>

          <div className="flex gap-3 items-start">
            <Form.Item
              label={`${sourceLabel}地址`}
              name="sourceHost"
              rules={[{ required: true, message: "请输入监听地址" }]}
              className="flex-1"
            >
              <Input placeholder="127.0.0.1 / 0.0.0.0" className="mgr-mono-input" />
            </Form.Item>
            <Form.Item
              label={`${sourceLabel}端口`}
              name="sourcePort"
              rules={[{ required: true, message: "请输入监听端口" }]}
              className="w-[140px] shrink-0"
            >
              <InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} />
            </Form.Item>
          </div>

          <div className="flex gap-3 items-start">
            <Form.Item
              label="目标地址"
              name="destinationHost"
              rules={[{ required: true, message: "请输入目标地址" }]}
              className="flex-1"
            >
              <Input placeholder="127.0.0.1 / example.com" className="mgr-mono-input" />
            </Form.Item>
            <Form.Item
              label="目标端口"
              name="destinationPort"
              rules={[{ required: true, message: "请输入目标端口" }]}
              className="w-[140px] shrink-0"
            >
              <InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} />
            </Form.Item>
          </div>

          <Form.Item label="启用" name="enabled" valuePropName="checked">
            <Switch size="small" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
};
