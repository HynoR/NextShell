import { useCallback, useEffect, useMemo, useState } from "react";
import { App as AntdApp, Form, Input, Modal, Select, Table, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { SshKeyProfile } from "@nextshell/core";
import type { SshKeyUsageItem } from "@nextshell/shared";
import { formatErrorMessage } from "../../../utils/errorMessage";

interface SshKeyPaneProps {
  sshKeys: SshKeyProfile[];
  /** 新密钥落在当前作用域；本地为 undefined。 */
  workspaceId?: string;
  onReload: () => Promise<void>;
}

interface CreateKeyValues {
  mode: "import" | "generate";
  name: string;
  keyContent?: string;
  passphrase?: string;
  algorithm?: "ed25519" | "rsa-2048" | "rsa-4096";
  comment?: string;
}

/**
 * 密钥态。相对旧面板补齐的是"能把密钥管好"的最小集:保存时解析出的类型与指纹、被几个连接
 * 引用、可复制的公钥,以及直接生成一把新密钥——过去必须先去终端 ssh-keygen。
 */
export const SshKeyPane = ({ sshKeys, workspaceId, onReload }: SshKeyPaneProps) => {
  const { message, modal } = AntdApp.useApp();
  const [selectedId, setSelectedId] = useState<string>();
  const [usage, setUsage] = useState<SshKeyUsageItem[]>([]);
  const [createOpen, setCreateOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<CreateKeyValues>();
  const mode = Form.useWatch("mode", form) ?? "generate";

  const selected = useMemo(
    () => sshKeys.find((key) => key.id === selectedId),
    [selectedId, sshKeys]
  );

  useEffect(() => {
    if (!selectedId) {
      setUsage([]);
      return;
    }
    window.nextshell.sshKey
      .usage({ id: selectedId })
      .then(setUsage)
      .catch(() => setUsage([]));
  }, [selectedId, sshKeys]);

  const handleCreate = useCallback(
    async (values: CreateKeyValues) => {
      setSaving(true);
      try {
        const created =
          values.mode === "generate"
            ? await window.nextshell.sshKey.generate({
                name: values.name,
                algorithm: values.algorithm ?? "ed25519",
                comment: values.comment,
                workspaceId
              })
            : await window.nextshell.sshKey.upsert({
                name: values.name,
                keyContent: values.keyContent,
                passphrase: values.passphrase,
                workspaceId
              });
        await onReload();
        setSelectedId(created.id);
        setCreateOpen(false);
        form.resetFields();
        message.success(`密钥「${created.name}」已保存`);
      } catch (error) {
        message.error(`保存密钥失败：${formatErrorMessage(error, "请检查私钥内容")}`);
      } finally {
        setSaving(false);
      }
    },
    [form, message, onReload, workspaceId]
  );

  const handleDelete = useCallback(() => {
    if (!selected) {
      return;
    }
    modal.confirm({
      title: `删除密钥「${selected.name}」`,
      content:
        usage.length > 0
          ? `以下 ${usage.length} 个连接正在引用它：${usage.map((item) => item.name).join("、")}。删除后这些连接需要重新绑定。`
          : "没有连接引用它，可以安全删除。",
      okText: "删除",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await window.nextshell.sshKey.remove({ id: selected.id, force: false });
          await onReload();
          setSelectedId(undefined);
        } catch (error) {
          message.error(`删除密钥失败：${formatErrorMessage(error, "请稍后重试")}`);
        }
      }
    });
  }, [message, modal, onReload, selected, usage]);

  const columns: ColumnsType<SshKeyProfile> = [
    { title: "名称", dataIndex: "name", key: "name", ellipsis: true },
    {
      title: "类型",
      key: "type",
      width: 130,
      render: (_value, key) =>
        key.keyType ? (
          `${key.keyType}${key.keyBits ? ` · ${key.keyBits}` : ""}`
        ) : (
          <span className="cm2-muted">未解析</span>
        )
    },
    {
      title: "指纹",
      key: "fingerprint",
      ellipsis: true,
      render: (_value, key) =>
        key.fingerprint ? (
          <span className="cm2-cell-mono">{key.fingerprint}</span>
        ) : (
          <span className="cm2-muted">—</span>
        )
    }
  ];

  return (
    <>
      <div className="cm2-main">
        <div className="cm2-toolbar">
          <button
            type="button"
            className="cm2-btn cm2-btn--primary"
            onClick={() => {
              form.resetFields();
              form.setFieldsValue({ mode: "generate", algorithm: "ed25519" });
              setCreateOpen(true);
            }}
          >
            <i className="ri-add-line" aria-hidden="true" />
            新建 / 生成密钥
          </button>
        </div>
        <Table<SshKeyProfile>
          className="cm2-table app-table"
          size="small"
          rowKey="id"
          dataSource={sshKeys}
          columns={columns}
          pagination={false}
          scroll={{ y: "100%" }}
          locale={{ emptyText: "当前作用域没有密钥" }}
          rowClassName={(key) => (key.id === selectedId ? "cm2-row cm2-row--focused" : "cm2-row")}
          onRow={(key) => ({ onClick: () => setSelectedId(key.id) })}
        />
      </div>

      <div className="cm2-detail-col">
        {selected ? (
          <div className="cm2-detail">
            <header className="cm2-detail-head">
              <h3 className="cm2-detail-title">{selected.name}</h3>
              <p className="cm2-detail-sub">{selected.keyType ?? "类型未解析"}</p>
            </header>
            <div className="cm2-detail-body">
              <div className="cm2-detail-row">
                <span className="cm2-detail-label">指纹</span>
                <span className="cm2-detail-value cm2-detail-mono">
                  {selected.fingerprint ?? "未解析（保存一次即可补齐）"}
                </span>
              </div>
              {selected.keyComment ? (
                <div className="cm2-detail-row">
                  <span className="cm2-detail-label">注释</span>
                  <span className="cm2-detail-value">{selected.keyComment}</span>
                </div>
              ) : null}
              <div className="cm2-detail-row">
                <span className="cm2-detail-label">被引用</span>
                <span className="cm2-detail-value">
                  {usage.length === 0 ? (
                    <span className="cm2-muted">没有连接引用</span>
                  ) : (
                    usage.map((item) => item.name).join("、")
                  )}
                </span>
              </div>
              {selected.publicKeyLine ? (
                <div className="cm2-detail-row">
                  <span className="cm2-detail-label">公钥</span>
                  <span className="cm2-detail-value cm2-detail-mono cm2-pubkey">
                    {selected.publicKeyLine}
                  </span>
                </div>
              ) : null}
            </div>
            <footer className="cm2-detail-foot">
              <Tooltip title="复制可直接贴进 authorized_keys 的公钥">
                <button
                  type="button"
                  className="cm2-btn"
                  disabled={!selected.publicKeyLine}
                  onClick={() => {
                    void navigator.clipboard.writeText(selected.publicKeyLine ?? "");
                    message.success("已复制公钥");
                  }}
                >
                  <i className="ri-file-copy-line" aria-hidden="true" />
                  复制公钥
                </button>
              </Tooltip>
              <button type="button" className="cm2-btn cm2-btn--danger" onClick={handleDelete}>
                <i className="ri-delete-bin-line" aria-hidden="true" />
                删除
              </button>
            </footer>
          </div>
        ) : (
          <p className="cm2-placeholder">单击一行查看密钥详情</p>
        )}
      </div>

      <Modal
        open={createOpen}
        title="新建 SSH 密钥"
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        onOk={() => form.submit()}
        onCancel={() => setCreateOpen(false)}
        destroyOnHidden
        width={520}
      >
        <Form form={form} layout="vertical" requiredMark={false} onFinish={handleCreate}>
          <Form.Item label="方式" name="mode">
            <Select
              options={[
                { label: "生成新密钥", value: "generate" },
                { label: "导入已有私钥", value: "import" }
              ]}
            />
          </Form.Item>
          <Form.Item label="名称" name="name" rules={[{ required: true, message: "请输入名称" }]}>
            <Input placeholder="deploy-key" />
          </Form.Item>
          {mode === "generate" ? (
            <>
              <Form.Item label="算法" name="algorithm">
                <Select
                  options={[
                    { label: "ed25519（推荐）", value: "ed25519" },
                    { label: "RSA 2048", value: "rsa-2048" },
                    { label: "RSA 4096", value: "rsa-4096" }
                  ]}
                />
              </Form.Item>
              <Form.Item label="注释（可选）" name="comment">
                <Input placeholder="ops@laptop" />
              </Form.Item>
            </>
          ) : (
            <>
              <Form.Item
                label="私钥内容"
                name="keyContent"
                rules={[{ required: true, message: "请粘贴私钥内容" }]}
              >
                <Input.TextArea rows={6} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
              </Form.Item>
              <Form.Item label="Passphrase（可选）" name="passphrase">
                <Input.Password placeholder="加密私钥才需要" />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>
    </>
  );
};
