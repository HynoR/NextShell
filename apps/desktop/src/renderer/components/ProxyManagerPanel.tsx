import { useCallback, useState } from "react";
import { Form, Input, InputNumber, Modal, Select, message } from "antd";
import type { ProxyProfile } from "@nextshell/core";
import { formatErrorMessage } from "../utils/errorMessage";

interface ProxyManagerPanelProps {
  proxies: ProxyProfile[];
  onReload: () => Promise<void>;
}

interface ProxyFormValues {
  id?: string;
  name: string;
  proxyType: "socks4" | "socks5";
  host: string;
  port: number;
  username?: string;
  password?: string;
}

export const ProxyManagerPanel = ({ proxies, onReload }: ProxyManagerPanelProps) => {
  const [mode, setMode] = useState<"idle" | "new" | "edit">("idle");
  const [selectedProxyId, setSelectedProxyId] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<ProxyFormValues>();
  const proxyType = Form.useWatch("proxyType", form);

  const selectedProxy = proxies.find((p) => p.id === selectedProxyId);

  const handleNew = useCallback(() => {
    setSelectedProxyId(undefined);
    form.resetFields();
    form.setFieldsValue({ proxyType: "socks5", port: 1080 });
    setMode("new");
  }, [form]);

  const handleSelect = useCallback(
    (proxyId: string) => {
      const proxy = proxies.find((p) => p.id === proxyId);
      if (!proxy) return;
      setSelectedProxyId(proxyId);
      form.setFieldsValue({
        id: proxy.id,
        name: proxy.name,
        proxyType: proxy.proxyType,
        host: proxy.host,
        port: proxy.port,
        username: proxy.username,
        password: undefined
      });
      setMode("edit");
    },
    [proxies, form]
  );

  const handleDelete = useCallback(() => {
    if (!selectedProxyId) return;
    Modal.confirm({
      title: "确认删除",
      content: `删除代理「${selectedProxy?.name ?? ""}」？如果仍有连接引用将无法删除。`,
      okText: "删除",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await window.nextshell.proxy.remove({ id: selectedProxyId, force: false });
          await onReload();
          setSelectedProxyId(undefined);
          form.resetFields();
          setMode("idle");
          message.success("代理已删除");
        } catch (error) {
          message.error(`删除代理失败：${formatErrorMessage(error, "请稍后重试")}`);
        }
      }
    });
  }, [selectedProxyId, selectedProxy, form, onReload]);

  return (
    <div className={mode !== "idle" ? "grid grid-cols-[230px_1fr] h-[540px] overflow-hidden" : "h-[540px] overflow-hidden"}>
      {/* ── Sidebar ─────────────────────────── */}
      <div className={`flex flex-col bg-[var(--bg-elevated)] overflow-hidden${mode !== "idle" ? " border-r border-[var(--border)]" : ""}`}>
        <div className="mgr-sidebar-head">
          <div className="mgr-sidebar-title-row">
            <span className="mgr-sidebar-title">代理</span>
            {proxies.length > 0 && (
              <span className="mgr-count-badge">{proxies.length}</span>
            )}
          </div>
          <button className="mgr-new-btn" onClick={handleNew} title="新建代理">
            <i className="ri-add-line" aria-hidden="true" />
          </button>
        </div>

        <div className="mgr-tree-wrap">
          {proxies.length === 0 ? (
            <div className="mgr-tree-empty">
              <i className="ri-shield-line" aria-hidden="true" />
              <span>暂无代理</span>
            </div>
          ) : (
            <div className="mgr-flat-list">
              {proxies.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className={`mgr-flat-item${p.id === selectedProxyId ? " mgr-flat-item--selected" : ""}`}
                  onClick={() => handleSelect(p.id)}
                >
                  <i className="ri-shield-line" aria-hidden="true" />
                  <span className="mgr-flat-item-name">{p.name}</span>
                  <span className="mgr-flat-item-meta">{p.proxyType.toUpperCase()}</span>
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="mgr-sidebar-footer">
          <span className="mgr-count">{proxies.length} 个代理</span>
        </div>
      </div>

      {/* ── Right panel ─────────────────────── */}
      {mode !== "idle" && (
        <div className="flex flex-col overflow-hidden">
          <div className="mgr-form-header">
            <div>
              <div className="mgr-form-title">
                {mode === "new" ? "新建代理" : (selectedProxy?.name ?? "编辑代理")}
              </div>
              <div className="mgr-form-subtitle">
                {mode === "new"
                  ? "填写代理信息后保存"
                  : `${selectedProxy?.proxyType.toUpperCase()} ${selectedProxy?.host}:${selectedProxy?.port}`}
              </div>
            </div>
            <div className="mgr-form-header-right">
              {mode === "edit" ? (
                <button
                  type="button"
                  className="mgr-form-header-icon-btn mgr-form-header-icon-btn--danger"
                  onClick={handleDelete}
                  aria-label="删除代理"
                  title="删除代理"
                >
                  <i className="ri-trash-line" aria-hidden="true" />
                </button>
              ) : (
                <button
                  type="button"
                  className="mgr-form-header-icon-btn"
                  onClick={() => setMode("idle")}
                  aria-label="取消"
                  title="取消"
                >
                  <i className="ri-arrow-left-line" aria-hidden="true" />
                </button>
              )}
              <button
                type="button"
                className="mgr-form-header-icon-btn mgr-form-header-icon-btn--primary"
                onClick={() => form.submit()}
                disabled={saving}
                aria-label="保存代理"
                title="保存代理"
              >
                {saving ? (
                  <i className="ri-loader-4-line mgr-form-header-icon-spin" aria-hidden="true" />
                ) : (
                  <i className="ri-save-line" aria-hidden="true" />
                )}
              </button>
              <button
                type="button"
                className="mgr-form-close-btn"
                onClick={() => { setMode("idle"); setSelectedProxyId(undefined); }}
                aria-label="收起表单"
                title="收起表单"
              >
                <i className="ri-close-line" aria-hidden="true" />
              </button>
            </div>
          </div>

          <Form
            form={form}
            layout="vertical"
            requiredMark={false}
            className="mgr-form"
            initialValues={{ proxyType: "socks5", port: 1080 }}
            onFinish={async (values) => {
              const name = values.name?.trim();
              if (!name) {
                message.error("请输入代理名称。");
                return;
              }

              const host = values.host?.trim();
              if (!host) {
                message.error("请输入代理地址。");
                return;
              }

              // InputNumber may return null when cleared; coerce safely
              const rawPort = values.port as unknown as number | null | undefined;
              const port = rawPort == null ? NaN : Number(rawPort);
              if (!Number.isInteger(port) || port < 1 || port > 65535) {
                message.error("代理端口必须是 1-65535 的整数。");
                return;
              }

              const username = values.username?.trim() || undefined;
              const password = values.password?.trim() || undefined;

              if (values.proxyType === "socks5" && password && !username) {
                message.error("设置 SOCKS5 代理密码时必须填写代理用户名。");
                return;
              }

              setSaving(true);
              try {
                await window.nextshell.proxy.upsert({
                  id: selectedProxyId,
                  name,
                  proxyType: values.proxyType,
                  host,
                  port,
                  username,
                  password
                });
                await onReload();
                message.success(selectedProxyId ? "代理已更新" : "代理已创建");
                if (!selectedProxyId) {
                  setMode("idle");
                }
                form.setFieldValue("password", undefined);
              } catch (error) {
                message.error(`保存代理失败：${formatErrorMessage(error, "请检查输入内容")}`);
              } finally {
                setSaving(false);
              }
            }}
          >
            <div className="mgr-section-label">代理信息</div>

            <Form.Item label="名称" name="name" rules={[{ required: true, message: "请输入代理名称" }]}>
              <Input placeholder="办公室代理" />
            </Form.Item>

            <Form.Item label="协议" name="proxyType" rules={[{ required: true }]}>
              <Select
                options={[
                  { label: "SOCKS5", value: "socks5" },
                  { label: "SOCKS4", value: "socks4" }
                ]}
              />
            </Form.Item>

            <div className="flex gap-3 items-start">
              <Form.Item
                label="代理地址"
                name="host"
                rules={[{ required: true, message: "请输入代理地址" }]}
                className="flex-1"
              >
                <Input placeholder="127.0.0.1" className="mgr-mono-input" />
              </Form.Item>
              <Form.Item
                label="端口"
                name="port"
                rules={[{ required: true, message: "请输入端口" }]}
                className="w-[90px] shrink-0"
              >
                <InputNumber min={1} max={65535} precision={0} placeholder="1080" style={{ width: "100%" }} />
              </Form.Item>
            </div>

            <div className="mgr-section-label mgr-section-gap">认证（可选）</div>

            <div className="flex gap-3 items-start">
              <Form.Item
                label={proxyType === "socks4" ? "User ID" : "用户名"}
                name="username"
                className="flex-1"
              >
                <Input placeholder="可选" />
              </Form.Item>
              {proxyType === "socks5" ? (
                <Form.Item
                  label="密码"
                  name="password"
                  className="flex-1"
                  preserve={false}
                >
                  <Input.Password placeholder="留空则不更新" />
                </Form.Item>
              ) : null}
            </div>
          </Form>
        </div>
      )}
    </div>
  );
};
