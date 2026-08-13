import { useCallback, useState } from "react";
import { App as AntdApp, Button, Form, Input, InputNumber, Select, Table } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { ProxyProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";

interface ProxyPaneProps {
  /** 已按当前作用域过滤。 */
  proxies: ProxyProfile[];
  /** 新代理落在当前作用域;本地为 undefined。 */
  workspaceId?: string;
  onReload: () => Promise<void>;
}

interface ProxyFormValues {
  name: string;
  proxyType: "socks4" | "socks5";
  host: string;
  port: number;
  username?: string;
  password?: string;
}

type PaneMode = { kind: "empty" } | { kind: "new" } | { kind: "edit"; proxy: ProxyProfile };

/**
 * 代理态。旧 ProxyManagerPanel 的样式随旧管理器 CSS 一起删除后已经完全裸奔,这里按密钥态的
 * 双栏模式重建:左表格右表单。保存位置不再让用户选——ScopeBar 的作用域就是保存位置。
 */
export const ProxyPane = ({ proxies, workspaceId, onReload }: ProxyPaneProps) => {
  const { message, modal } = AntdApp.useApp();
  const [mode, setMode] = useState<PaneMode>({ kind: "empty" });
  const [saving, setSaving] = useState(false);

  const editing = mode.kind === "edit" ? mode.proxy : undefined;

  const handleSubmit = useCallback(
    async (values: ProxyFormValues) => {
      const username = values.username?.trim() || undefined;
      const password = values.password?.trim() || undefined;
      if (values.proxyType === "socks5" && password && !username) {
        message.error("设置 SOCKS5 代理密码时必须填写代理用户名。");
        return;
      }
      setSaving(true);
      try {
        await window.nextshell.proxy.upsert({
          id: editing?.id,
          name: values.name.trim(),
          proxyType: values.proxyType,
          host: values.host.trim(),
          port: values.port,
          username,
          password,
          workspaceId
        });
        await onReload();
        message.success(editing ? "代理已更新" : "代理已创建");
        setMode({ kind: "empty" });
      } catch (error) {
        message.error(`保存代理失败：${formatErrorMessage(error, "请检查输入内容")}`);
      } finally {
        setSaving(false);
      }
    },
    [editing, message, onReload, workspaceId]
  );

  const handleDelete = useCallback(
    (proxy: ProxyProfile) => {
      modal.confirm({
        title: `删除代理「${proxy.name}」`,
        content: "如果仍有连接引用它将无法删除。",
        okText: "删除",
        cancelText: "取消",
        okButtonProps: { danger: true },
        onOk: async () => {
          try {
            await window.nextshell.proxy.remove({ id: proxy.id, force: false });
            await onReload();
            setMode({ kind: "empty" });
            message.success("代理已删除");
          } catch (error) {
            message.error(`删除代理失败：${formatErrorMessage(error, "请稍后重试")}`);
          }
        }
      });
    },
    [message, modal, onReload]
  );

  const columns: ColumnsType<ProxyProfile> = [
    { title: "名称", dataIndex: "name", key: "name", ellipsis: true },
    {
      title: "协议",
      key: "type",
      width: 90,
      render: (_value, proxy) => proxy.proxyType.toUpperCase()
    },
    {
      title: "地址",
      key: "address",
      width: 200,
      ellipsis: true,
      render: (_value, proxy) => (
        <span className="cm2-cell-mono">
          {proxy.host}:{proxy.port}
        </span>
      )
    },
    {
      title: "认证",
      key: "auth",
      width: 110,
      ellipsis: true,
      render: (_value, proxy) => proxy.username || <span className="cm2-muted">无</span>
    }
  ];

  return (
    <>
      <div className="cm2-main">
        <div className="cm2-toolbar">
          <Button
            type="primary"
            icon={<i className="ri-add-line" aria-hidden="true" />}
            onClick={() => setMode({ kind: "new" })}
          >
            新建代理
          </Button>
        </div>
        <div className="cm2-table-wrap">
          <Table<ProxyProfile>
            className="cm2-table app-table"
            size="small"
            rowKey="id"
            dataSource={proxies}
            columns={columns}
            pagination={false}
            locale={{ emptyText: "当前作用域没有代理" }}
            rowClassName={(proxy) =>
              proxy.id === editing?.id ? "cm2-row cm2-row--focused" : "cm2-row"
            }
            onRow={(proxy) => ({ onClick: () => setMode({ kind: "edit", proxy }) })}
          />
        </div>
      </div>

      <div className="cm2-detail-col">
        {mode.kind === "empty" ? (
          <p className="cm2-placeholder">单击一个代理进行编辑，或新建</p>
        ) : (
          <ProxyEditor
            key={editing?.id ?? "__new__"}
            proxy={editing}
            saving={saving}
            onSubmit={handleSubmit}
            onCancel={() => setMode({ kind: "empty" })}
            onDelete={editing ? () => handleDelete(editing) : undefined}
          />
        )}
      </div>
    </>
  );
};

const ProxyEditor = ({
  proxy,
  saving,
  onSubmit,
  onCancel,
  onDelete
}: {
  proxy?: ProxyProfile;
  saving: boolean;
  onSubmit: (values: ProxyFormValues) => void;
  onCancel: () => void;
  onDelete?: () => void;
}) => {
  const [form] = Form.useForm<ProxyFormValues>();
  const initialValues: ProxyFormValues = {
    name: proxy?.name ?? "",
    proxyType: proxy?.proxyType ?? "socks5",
    host: proxy?.host ?? "",
    port: proxy?.port ?? 1080,
    username: proxy?.username,
    password: undefined
  };
  const proxyType = Form.useWatch("proxyType", form) ?? initialValues.proxyType;

  return (
    <Form
      form={form}
      layout="vertical"
      requiredMark={false}
      className="cm2-editor"
      initialValues={initialValues}
      onFinish={onSubmit}
    >
      <div className="cm2-editor-body">
        <Form.Item label="名称" name="name" rules={[{ required: true, message: "请输入代理名称" }]}>
          <Input placeholder="办公室代理" autoFocus />
        </Form.Item>
        <Form.Item label="协议" name="proxyType">
          <Select
            options={[
              { label: "SOCKS5", value: "socks5" },
              { label: "SOCKS4", value: "socks4" }
            ]}
          />
        </Form.Item>
        <div className="cm2-field-pair">
          <Form.Item
            label="代理地址"
            name="host"
            rules={[{ required: true, message: "请输入代理地址" }]}
            className="cm2-field-grow"
          >
            <Input placeholder="127.0.0.1" className="cm2-mono" />
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
        <Form.Item label={proxyType === "socks4" ? "User ID（可选）" : "用户名（可选）"} name="username">
          <Input placeholder="可选" />
        </Form.Item>
        {proxyType === "socks5" ? (
          <Form.Item label="密码" name="password" preserve={false}>
            <Input.Password placeholder={proxy ? "留空则不修改" : "可选"} />
          </Form.Item>
        ) : null}
      </div>
      <footer className="cm2-detail-foot">
        <Button type="primary" htmlType="submit" loading={saving}>
          保存
        </Button>
        <Button onClick={onCancel} disabled={saving}>
          取消
        </Button>
        {onDelete ? (
          <Button danger onClick={onDelete} disabled={saving} style={{ marginLeft: "auto" }}>
            删除
          </Button>
        ) : null}
      </footer>
    </Form>
  );
};
