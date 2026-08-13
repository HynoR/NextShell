import { useMemo } from "react";
import { Button, Collapse, Form, Input, InputNumber, Select, Switch } from "antd";
import type { ConnectionFolder, ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { buildFolderPathLabels } from "../utils/folderTree";

export interface ConnectionEditorValues extends ConnectionUpsertInput {
  folderId?: string;
}

interface ConnectionEditorProps {
  connection?: ConnectionProfile;
  folders: ConnectionFolder[];
  currentFolderId?: string;
  sshKeys: SshKeyProfile[];
  proxies: ProxyProfile[];
  saving: boolean;
  /** 已保存的登录密码，输入主密码后才由主进程返回；30 秒后自动清空。 */
  revealedPassword?: string;
  revealingPassword: boolean;
  onRevealPassword: () => void;
  onSubmit: (values: ConnectionEditorValues) => void;
  onCancel: () => void;
  onCreateKey: () => void;
}

/**
 * 编辑态。七个常驻字段 + 四个默认折叠区,取代旧表单的「基本/属性/网络/高级」四个 tab:
 * 一台机器的全部配置在同一页,校验失败时展开对应区块而不是切 tab。
 */
export const ConnectionEditor = ({
  connection,
  folders,
  currentFolderId,
  sshKeys,
  proxies,
  saving,
  revealedPassword,
  revealingPassword,
  onRevealPassword,
  onSubmit,
  onCancel,
  onCreateKey
}: ConnectionEditorProps) => {
  const [form] = Form.useForm<ConnectionEditorValues>();
  // 首帧就用 `initialValues` 给全值，而不是等 effect 里 setFieldsValue：后者会先渲染一遍空表单，
  // 也让 authType 的分支在首帧走错。调用方按连接 id 重挂载本组件来切换目标。
  const initialValues = useMemo<ConnectionEditorValues>(
    () =>
      ({
        id: connection?.id,
        name: connection?.name ?? "",
        host: connection?.host ?? "",
        port: connection?.port ?? 22,
        username: connection?.username ?? "",
        authType: connection?.authType ?? "password",
        sshKeyId: connection?.sshKeyId,
        folderId: connection?.folderId ?? currentFolderId,
        hostFingerprint: connection?.hostFingerprint,
        strictHostKeyChecking: connection?.strictHostKeyChecking ?? false,
        proxyId: connection?.proxyId,
        keepAliveEnabled: connection?.keepAliveEnabled,
        keepAliveIntervalSec: connection?.keepAliveIntervalSec,
        terminalEncoding: connection?.terminalEncoding ?? "utf-8",
        backspaceMode: connection?.backspaceMode ?? "ascii-backspace",
        deleteMode: connection?.deleteMode ?? "vt220-delete",
        monitorSession: connection?.monitorSession ?? true,
        tags: connection?.tags ?? [],
        notes: connection?.notes,
        favorite: connection?.favorite ?? false,
        agentAccess: connection?.agentAccess ?? "off",
        password: undefined
      }) as ConnectionEditorValues,
    [connection, currentFolderId]
  );
  // 表单未注册完成前（首帧、SSR）退回已知值，避免认证分支闪错。
  const authType = Form.useWatch("authType", form) ?? initialValues.authType;
  // 不同层级的同名目录只显示名字无法区分，下拉里给完整路径。
  const folderOptions = useMemo(() => {
    const labels = buildFolderPathLabels(folders);
    return folders
      .map((folder) => ({ value: folder.id, label: labels.get(folder.id) ?? folder.name }))
      .sort((left, right) => left.label.localeCompare(right.label));
  }, [folders]);

  return (
    <Form
      form={form}
      initialValues={initialValues}
      layout="vertical"
      requiredMark={false}
      className="cm2-editor"
      onFinish={onSubmit}
    >
      <div className="cm2-editor-body">
        <Form.Item label="名称" name="name">
          <Input placeholder="留空则使用 host:port" autoFocus />
        </Form.Item>

        <div className="cm2-field-pair">
          <Form.Item
            label="主机"
            name="host"
            rules={[{ required: true, message: "请输入主机地址" }]}
            className="cm2-field-grow"
          >
            <Input placeholder="10.0.0.1 或 example.com" className="cm2-mono" />
          </Form.Item>
          <Form.Item
            label="端口"
            name="port"
            rules={[{ required: true, message: "请输入端口" }]}
            className="cm2-field-port"
          >
            <InputNumber min={1} max={65535} precision={0} style={{ width: "100%" }} />
          </Form.Item>
        </div>

        <div className="cm2-field-pair">
          <Form.Item label="用户名" name="username" className="cm2-field-grow">
            <Input placeholder="root（可留空，连接时再输）" />
          </Form.Item>
          <Form.Item label="认证方式" name="authType" className="cm2-field-grow">
            <Select
              options={[
                { label: "密码", value: "password" },
                { label: "交互式登录", value: "interactive" },
                { label: "私钥", value: "privateKey" },
                { label: "SSH Agent", value: "agent" }
              ]}
            />
          </Form.Item>
        </div>

        {authType === "privateKey" ? (
          <Form.Item
            label={
              <span className="cm2-label-with-action">
                SSH 密钥
                <button type="button" className="cm2-inline-btn" onClick={onCreateKey}>
                  <i className="ri-add-line" aria-hidden="true" />
                  新建 / 生成
                </button>
              </span>
            }
            name="sshKeyId"
            rules={[{ required: true, message: "私钥认证需要选择一个密钥" }]}
          >
            <Select
              allowClear
              placeholder="选择密钥…"
              options={sshKeys.map((key) => ({
                value: key.id,
                label: key.keyType ? `${key.name} · ${key.keyType}` : key.name
              }))}
              notFoundContent="当前作用域内没有密钥，点上方新建"
            />
          </Form.Item>
        ) : null}

        {authType === "password" || authType === "interactive" ? (
          <>
            <Form.Item label="密码" name="password" preserve={false}>
              <Input.Password placeholder={connection ? "留空则不修改" : "输入登录密码"} />
            </Form.Item>
            {connection &&
            (connection.authType === "password" || connection.authType === "interactive") ? (
              <Form.Item label="已保存的密码" preserve={false}>
                {revealedPassword ? (
                  <Input.Password value={revealedPassword} readOnly visibilityToggle />
                ) : (
                  <Button
                    icon={<i className="ri-eye-line" aria-hidden="true" />}
                    loading={revealingPassword}
                    onClick={onRevealPassword}
                  >
                    {revealingPassword ? "验证中…" : "输入主密码查看"}
                  </Button>
                )}
              </Form.Item>
            ) : null}
          </>
        ) : null}

        <Form.Item label="目录" name="folderId">
          <Select
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="顶层"
            options={folderOptions}
          />
        </Form.Item>

        <Collapse
          ghost
          size="small"
          className="cm2-editor-sections"
          items={[
            {
              key: "security",
              label: "安全",
              children: (
                <>
                  <Form.Item label="主机指纹" name="hostFingerprint">
                    <Input placeholder="SHA256:…" className="cm2-mono" />
                  </Form.Item>
                  <Form.Item
                    label="严格主机校验"
                    name="strictHostKeyChecking"
                    valuePropName="checked"
                  >
                    <Switch size="small" />
                  </Form.Item>
                </>
              )
            },
            {
              key: "network",
              label: "网络",
              children: (
                <>
                  <Form.Item label="代理" name="proxyId">
                    <Select
                      allowClear
                      placeholder="不使用代理"
                      options={proxies.map((proxy) => ({ value: proxy.id, label: proxy.name }))}
                    />
                  </Form.Item>
                  <Form.Item label="启用 Keepalive" name="keepAliveEnabled" valuePropName="checked">
                    <Switch size="small" />
                  </Form.Item>
                  <Form.Item label="Keepalive 间隔（秒）" name="keepAliveIntervalSec">
                    <InputNumber min={5} max={600} precision={0} style={{ width: "100%" }} />
                  </Form.Item>
                </>
              )
            },
            {
              key: "terminal",
              label: "终端",
              children: (
                <>
                  <Form.Item label="编码" name="terminalEncoding">
                    <Select
                      options={[
                        { label: "UTF-8", value: "utf-8" },
                        { label: "GB18030", value: "gb18030" },
                        { label: "GBK", value: "gbk" },
                        { label: "Big5", value: "big5" }
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="Backspace" name="backspaceMode">
                    <Select
                      options={[
                        { label: "ASCII Backspace", value: "ascii-backspace" },
                        { label: "ASCII Delete", value: "ascii-delete" }
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="Delete" name="deleteMode">
                    <Select
                      options={[
                        { label: "VT220 Delete", value: "vt220-delete" },
                        { label: "ASCII Delete", value: "ascii-delete" },
                        { label: "ASCII Backspace", value: "ascii-backspace" }
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="启用会话监控" name="monitorSession" valuePropName="checked">
                    <Switch size="small" />
                  </Form.Item>
                </>
              )
            },
            {
              key: "meta",
              label: "标签与 Agent",
              children: (
                <>
                  <Form.Item label="标签" name="tags">
                    <Select mode="tags" tokenSeparators={[","]} placeholder="prod, hk" />
                  </Form.Item>
                  <Form.Item label="收藏" name="favorite" valuePropName="checked">
                    <Switch size="small" />
                  </Form.Item>
                  <Form.Item
                    label="Agent 授权"
                    name="agentAccess"
                    extra="默认关闭时该主机对 AI Agent 完全不可见。Agent 永远拿不到密码与私钥。"
                  >
                    <Select
                      options={[
                        { label: "关闭（不可见）", value: "off" },
                        { label: "只读", value: "readonly" },
                        { label: "完全", value: "full" }
                      ]}
                    />
                  </Form.Item>
                  <Form.Item label="备注" name="notes">
                    <Input.TextArea rows={2} placeholder="可选" />
                  </Form.Item>
                </>
              )
            }
          ]}
        />
      </div>

      <footer className="cm2-detail-foot">
        <Button type="primary" htmlType="submit" loading={saving}>
          保存
        </Button>
        <Button onClick={onCancel} disabled={saving}>
          取消
        </Button>
      </footer>
    </Form>
  );
};
