import { useEffect, useMemo, useState } from "react";
import { Form, Input, Modal, Select, Typography, message } from "antd";
import type { SshKeyProfile } from "@nextshell/core";
import type { SessionAuthOverrideInput } from "@nextshell/shared";

interface SessionAuthRetryModalProps {
  open: boolean;
  attempt: number;
  maxAttempts: number;
  initialUsername?: string;
  defaultAuthType: SessionAuthOverrideInput["authType"];
  hasExistingPrivateKey: boolean;
  sshKeys: SshKeyProfile[];
  onCancel: () => void;
  onSubmit: (payload: SessionAuthOverrideInput) => Promise<void>;
}

interface SessionAuthFormValues {
  username?: string;
  authType: SessionAuthOverrideInput["authType"];
  password?: string;
  sshKeyId?: string;
  privateKeyContent?: string;
  passphrase?: string;
}

const sanitizeOptionalText = (value?: string): string | undefined => {
  if (typeof value !== "string") {
    return undefined;
  }

  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
};

export const SessionAuthRetryModal = ({
  open,
  attempt,
  maxAttempts,
  initialUsername,
  defaultAuthType,
  hasExistingPrivateKey,
  sshKeys,
  onCancel,
  onSubmit
}: SessionAuthRetryModalProps) => {
  const [submitting, setSubmitting] = useState(false);
  const [form] = Form.useForm<SessionAuthFormValues>();
  const authType = Form.useWatch("authType", form);
  const normalizedInitialUsername = useMemo(
    () => sanitizeOptionalText(initialUsername),
    [initialUsername]
  );
  const needsUsername = !normalizedInitialUsername;

  useEffect(() => {
    if (!open) {
      return;
    }

    form.resetFields();
    form.setFieldsValue({
      authType: defaultAuthType
    });
  }, [defaultAuthType, form, open]);

  useEffect(() => {
    if (!open || !authType) {
      return;
    }

    if (authType === "password") {
      form.setFieldsValue({
        sshKeyId: undefined,
        privateKeyContent: undefined,
        passphrase: undefined
      });
      return;
    }

    form.setFieldValue("password", undefined);
  }, [authType, form, open]);

  return (
    <Modal
      open={open}
      title={`认证重试（第 ${attempt}/${maxAttempts} 次）`}
      okText="重试连接"
      cancelText="取消"
      confirmLoading={submitting}
      onCancel={onCancel}
      onOk={() => {
        void form.submit();
      }}
      destroyOnHidden
      mask={{ closable: false }}
    >
      <Typography.Paragraph type="secondary">
        连接需要身份验证。用户名如需修改，请在连接管理中心调整。
      </Typography.Paragraph>

      {normalizedInitialUsername ? (
        <Typography.Paragraph style={{ marginTop: -8 }}>
          当前用户名：<Typography.Text code>{normalizedInitialUsername}</Typography.Text>
        </Typography.Paragraph>
      ) : null}

      <Form<SessionAuthFormValues>
        form={form}
        layout="vertical"
        requiredMark={false}
        onFinish={async (values) => {
          const authTypeValue = values.authType ?? defaultAuthType;
          const username = sanitizeOptionalText(values.username) ?? normalizedInitialUsername;
          const password = sanitizeOptionalText(values.password);
          const privateKeyContent = sanitizeOptionalText(values.privateKeyContent);
          const passphrase = sanitizeOptionalText(values.passphrase);

          if (!username) {
            message.error("请输入用户名。");
            return;
          }

          if (authTypeValue === "password" && !password) {
            message.error("密码认证必须输入密码。");
            return;
          }

          if (authTypeValue === "privateKey" && !hasExistingPrivateKey && !values.sshKeyId && !privateKeyContent) {
            message.error("请选择密钥或粘贴私钥内容。");
            return;
          }

          setSubmitting(true);
          try {
            await onSubmit({
              username,
              authType: authTypeValue,
              password: authTypeValue === "password" ? password : undefined,
              sshKeyId: authTypeValue === "privateKey" ? values.sshKeyId : undefined,
              privateKeyContent: authTypeValue === "privateKey" ? privateKeyContent : undefined,
              passphrase: authTypeValue === "privateKey" ? passphrase : undefined
            });
            form.setFieldsValue({
              password: undefined,
              privateKeyContent: undefined
            });
          } finally {
            setSubmitting(false);
          }
        }}
      >
        {needsUsername ? (
          <Form.Item label="用户名" name="username" rules={[{ required: true, message: "请输入用户名" }]}>
            <Input placeholder="root" autoFocus />
          </Form.Item>
        ) : null}

        <Form.Item label="认证方式" name="authType" rules={[{ required: true }]}>
          <Select
            options={[
              { label: "密码", value: "password" },
              { label: "私钥", value: "privateKey" }
            ]}
          />
        </Form.Item>

        {authType === "password" ? (
          <Form.Item label="密码" name="password" rules={[{ required: true, message: "请输入密码" }]}>
            <Input.Password placeholder="请输入密码" />
          </Form.Item>
        ) : null}

        {authType === "privateKey" ? (
          <>
            <Form.Item label="选择密钥" name="sshKeyId">
              <Select
                placeholder="选择已有密钥..."
                allowClear
                options={sshKeys.map((k) => ({ label: k.name, value: k.id }))}
                notFoundContent={
                  <div style={{ textAlign: "center", padding: "8px 0", color: "var(--text-muted)" }}>
                    暂无密钥
                  </div>
                }
              />
            </Form.Item>
            <Form.Item label="或直接粘贴私钥内容" name="privateKeyContent">
              <Input.TextArea
                rows={4}
                placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----"}
              />
            </Form.Item>
            <Form.Item label="私钥 Passphrase（可选）" name="passphrase">
              <Input.Password placeholder="留空表示无 Passphrase 或沿用已有值" />
            </Form.Item>
          </>
        ) : null}
      </Form>
    </Modal>
  );
};
