import { useCallback, useEffect, useState } from "react";
import { Form, Input, Modal, Select, Typography, message } from "antd";
import type { ConnectionProfile, SshKeyProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { formatErrorMessage } from "../utils/errorMessage";

interface CredentialEditFormValues {
  username?: string;
  authType: ConnectionProfile["authType"];
  password?: string;
  sshKeyId?: string;
}

const sanitizeOptionalText = (value: string | undefined): string | undefined => {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
};

export const toConnectionUpsertInput = (
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

export interface CredentialEditModalProps {
  open: boolean;
  connection: ConnectionProfile | undefined;
  sshKeys: SshKeyProfile[];
  /** 显示在顶部的失败原因，通常来自连接失败时 */
  failureReason?: string;
  onClose: () => void;
  onSave: (payload: ConnectionUpsertInput) => Promise<void>;
}

export const CredentialEditModal = ({
  open,
  connection,
  sshKeys,
  failureReason,
  onClose,
  onSave
}: CredentialEditModalProps) => {
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<CredentialEditFormValues>();
  const authType = Form.useWatch("authType", form);

  useEffect(() => {
    if (!open || !connection) {
      return;
    }
    form.resetFields();
    form.setFieldsValue({
      username: connection.username,
      authType: connection.authType,
      sshKeyId: connection.authType === "privateKey" ? connection.sshKeyId : undefined,
      password: undefined
    });
  }, [open, connection, form]);

  useEffect(() => {
    if (!open || !authType) {
      return;
    }
    if (authType === "agent") {
      form.setFieldsValue({ password: undefined, sshKeyId: undefined });
      return;
    }
    if (authType === "password" || authType === "interactive") {
      form.setFieldValue("sshKeyId", undefined);
      return;
    }
    form.setFieldValue("password", undefined);
  }, [authType, form, open]);

  const handleSave = useCallback(async () => {
    if (!connection) {
      return;
    }
    let values: CredentialEditFormValues;
    try {
      values = await form.validateFields();
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
      (values.authType === "password" || values.authType === "interactive") &&
      (connection.authType !== "password" && connection.authType !== "interactive") &&
      !password
    ) {
      message.error("切换到密码/交互式认证时需要提供密码。");
      return;
    }

    setSaving(true);
    try {
      await onSave(
        toConnectionUpsertInput(connection, {
          username,
          authType: values.authType,
          password: values.authType === "password" || values.authType === "interactive" ? password : undefined,
          sshKeyId: values.authType === "privateKey" ? values.sshKeyId : undefined
        })
      );
      message.success("凭据已更新");
      onClose();
    } catch (error) {
      message.error(`更新凭据失败：${formatErrorMessage(error, "请检查认证信息")}`);
    } finally {
      setSaving(false);
    }
  }, [connection, form, onClose, onSave]);

  const title = connection ? `编辑凭据 · ${connection.name}` : "编辑凭据";

  return (
    <Modal
      open={open}
      title={title}
      okText="保存"
      cancelText="取消"
      confirmLoading={saving}
      onCancel={() => {
        if (saving) return;
        onClose();
      }}
      onOk={() => { void handleSave(); }}
      destroyOnHidden
    >
      {failureReason ? (
        <Typography.Paragraph type="danger" style={{ marginBottom: 8 }}>
          {failureReason}
        </Typography.Paragraph>
      ) : null}

      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        onFinish={() => { void handleSave(); }}
      >
        <Form.Item label="用户名" name="username" rules={[{ required: true, message: "请输入用户名" }]}>
          <Input placeholder="root" autoFocus />
        </Form.Item>

        <Form.Item label="认证方式" name="authType" rules={[{ required: true }]}>
          <Select
            options={[
              { label: "密码", value: "password" },
              { label: "交互式登录", value: "interactive" },
              { label: "私钥", value: "privateKey" },
              { label: "SSH Agent", value: "agent" }
            ]}
          />
        </Form.Item>

        {authType === "password" || authType === "interactive" ? (
          <Form.Item label="密码" name="password">
            <Input.Password placeholder="输入新密码（留空则不更新）" />
          </Form.Item>
        ) : null}

        {authType === "privateKey" ? (
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
  );
};
