import { Form, Input, InputNumber, Select, Switch } from "antd";
import type { ConnectionProfile, SshKeyProfile } from "@nextshell/core";
import type { ManagerMode } from "../../types";

interface BasicTabProps {
  authType?: string;
  mode: ManagerMode;
  selectedConnection?: ConnectionProfile;
  sshKeys: SshKeyProfile[];
  revealedLoginPassword?: string;
  revealingLoginPassword: boolean;
  onRevealConnectionPassword: () => void;
}

export const BasicTab = ({
  authType,
  mode,
  selectedConnection,
  sshKeys,
  revealedLoginPassword,
  revealingLoginPassword,
  onRevealConnectionPassword
}: BasicTabProps) => {
  return (
    <>
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
            options={sshKeys.map((key) => ({ label: key.name, value: key.id }))}
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
                  onClick={onRevealConnectionPassword}
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
    </>
  );
};
