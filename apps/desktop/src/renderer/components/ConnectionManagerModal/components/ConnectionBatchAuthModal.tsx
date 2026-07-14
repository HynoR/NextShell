import { useMemo, useState } from "react";
import { Alert, App as AntdApp, Form, Input, Modal, Radio, Select, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  LOCAL_DEFAULT_SCOPE_KEY,
  type AuthType,
  type ConnectionProfile,
  type SshKeyProfile
} from "@nextshell/core";
import type { ConnectionBatchAuthUpdateInput } from "@nextshell/shared";
import { formatErrorMessage } from "../../../utils/errorMessage";
import type { BatchAuthTarget } from "../types";

interface ConnectionBatchAuthModalProps {
  open: boolean;
  target: BatchAuthTarget | null;
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  onClose: () => void;
  onUpdated: () => Promise<void>;
}

interface BatchAuthFormValues {
  authType: "password" | "interactive" | "privateKey" | "agent";
  password?: string;
  sshKeyId?: string;
}

const getConnectionScopeKey = (connection: ConnectionProfile): string =>
  connection.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;

const getSshKeyScopeKey = (sshKey: SshKeyProfile): string =>
  sshKey.originScopeKey ?? LOCAL_DEFAULT_SCOPE_KEY;

const isUnderGroupPath = (connection: ConnectionProfile, groupPath: string): boolean =>
  connection.groupPath === groupPath || connection.groupPath.startsWith(`${groupPath}/`);

const authTypeLabel: Record<AuthType, string> = {
  password: "密码",
  interactive: "交互式",
  privateKey: "私钥",
  agent: "Agent"
};

export const ConnectionBatchAuthModal = ({
  open,
  target,
  connections,
  sshKeys,
  onClose,
  onUpdated
}: ConnectionBatchAuthModalProps) => {
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm<BatchAuthFormValues>();
  const [saving, setSaving] = useState(false);
  const authType = Form.useWatch("authType", form) ?? "password";

  const targetConnections = useMemo(() => {
    if (!target) {
      return [];
    }
    if (target.type === "connections") {
      const idSet = new Set(target.connectionIds);
      return connections.filter((connection) => idSet.has(connection.id));
    }
    return connections.filter((connection) => isUnderGroupPath(connection, target.groupPath));
  }, [connections, target]);

  const scopeKeys = useMemo(
    () => new Set(targetConnections.map(getConnectionScopeKey)),
    [targetConnections]
  );
  const sameScope = scopeKeys.size <= 1;
  const targetScopeKey = targetConnections[0]
    ? getConnectionScopeKey(targetConnections[0])
    : LOCAL_DEFAULT_SCOPE_KEY;
  const scopedSshKeys = useMemo(
    () => sshKeys.filter((key) => getSshKeyScopeKey(key) === targetScopeKey),
    [sshKeys, targetScopeKey]
  );

  const columns: ColumnsType<ConnectionProfile> = [
    {
      title: "名称",
      dataIndex: "name",
      key: "name",
      width: 160,
      ellipsis: true
    },
    {
      title: "主机",
      key: "host",
      width: 170,
      render: (_value, record) => `${record.host}:${record.port}`
    },
    {
      title: "用户",
      dataIndex: "username",
      key: "username",
      width: 100,
      ellipsis: true
    },
    {
      title: "当前认证",
      dataIndex: "authType",
      key: "authType",
      width: 100,
      render: (value: AuthType) => <Tag>{authTypeLabel[value]}</Tag>
    },
    {
      title: "分组",
      dataIndex: "groupPath",
      key: "groupPath",
      ellipsis: true
    }
  ];

  const handleSubmit = async (values: BatchAuthFormValues) => {
    if (!target || targetConnections.length === 0) {
      message.warning("没有可批量绑定的连接");
      return;
    }
    if (!sameScope) {
      message.error("批量绑定认证要求所有目标连接属于同一来源范围");
      return;
    }

    const auth: ConnectionBatchAuthUpdateInput["auth"] =
      values.authType === "password" || values.authType === "interactive"
        ? { authType: values.authType, password: values.password ?? "" }
        : values.authType === "privateKey"
          ? { authType: "privateKey", sshKeyId: values.sshKeyId ?? "" }
          : { authType: "agent" };

    const payload: ConnectionBatchAuthUpdateInput = {
      target:
        target.type === "connections"
          ? { type: "connections", connectionIds: target.connectionIds }
          : { type: "group", groupPath: target.groupPath },
      auth
    };

    setSaving(true);
    try {
      const result = await window.nextshell.connection.batchUpdateAuth(payload);
      const parts = [`更新 ${result.updated}`];
      if (result.failed > 0) {
        parts.push(`失败 ${result.failed}`);
      }
      message.success(`批量绑定完成：${parts.join("，")}`);
      result.errors.slice(0, 8).forEach((error) => {
        message.warning(formatErrorMessage(error, "部分连接更新失败"));
      });
      if (result.errors.length > 8) {
        message.warning(`还有 ${result.errors.length - 8} 条错误未显示`);
      }
      await onUpdated();
      onClose();
    } catch (error) {
      message.error(`批量绑定失败：${formatErrorMessage(error, "请检查目标连接和认证配置")}`);
    } finally {
      setSaving(false);
    }
  };

  const canSubmit = Boolean(target) && targetConnections.length > 0 && sameScope;

  return (
    <Modal
      open={open}
      title="批量绑定认证"
      width={860}
      okText="确认绑定"
      cancelText="取消"
      confirmLoading={saving}
      okButtonProps={{ disabled: !canSubmit }}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
    >
      <div style={{ display: "grid", gap: 12 }}>
        <Alert
          type={sameScope ? "info" : "warning"}
          showIcon
          message={target ? `${target.label} · ${targetConnections.length} 个连接` : "未选择目标"}
          description={
            sameScope
              ? "确认后会统一替换这些连接的认证方式，其他连接属性保持不变。"
              : "当前目标跨了本地或不同 workspace，请缩小选择范围后再批量绑定。"
          }
        />

        <Form
          form={form}
          layout="vertical"
          initialValues={{ authType: "password" }}
          onFinish={handleSubmit}
        >
          <Form.Item label="认证方式" name="authType" rules={[{ required: true }]}>
            <Radio.Group
              optionType="button"
              buttonStyle="solid"
              options={[
                { label: "密码", value: "password" },
                { label: "交互式", value: "interactive" },
                { label: "私钥", value: "privateKey" },
                { label: "SSH Agent", value: "agent" }
              ]}
            />
          </Form.Item>

          {authType === "password" || authType === "interactive" ? (
            <Form.Item
              label="密码"
              name="password"
              preserve={false}
              rules={[{ required: true, message: "请输入要绑定的密码" }]}
            >
              <Input.Password placeholder="输入要批量绑定的登录密码" />
            </Form.Item>
          ) : null}

          {authType === "privateKey" ? (
            <Form.Item
              label="SSH 密钥"
              name="sshKeyId"
              preserve={false}
              rules={[{ required: true, message: "请选择要绑定的 SSH 密钥" }]}
            >
              <Select
                placeholder="选择同来源范围内的密钥"
                options={scopedSshKeys.map((key) => ({ label: key.name, value: key.id }))}
                notFoundContent="当前来源范围内没有可用密钥"
              />
            </Form.Item>
          ) : null}
        </Form>

        <Table
          dataSource={targetConnections.slice(0, 80)}
          columns={columns}
          rowKey="id"
          size="small"
          pagination={false}
          scroll={{ y: 260 }}
          className="app-table"
        />
      </div>
    </Modal>
  );
};
