import { useCallback, useRef, useState } from "react";
import { Form, Input, Modal, Tooltip, message } from "antd";
import type { SshKeyProfile } from "@nextshell/core";
import { formatErrorMessage } from "../utils/errorMessage";

interface SshKeyManagerPanelProps {
  sshKeys: SshKeyProfile[];
  onReload: () => Promise<void>;
}

interface SshKeyFormValues {
  id?: string;
  name: string;
  keyContent?: string;
  passphrase?: string;
}

export const SshKeyManagerPanel = ({ sshKeys, onReload }: SshKeyManagerPanelProps) => {
  const [mode, setMode] = useState<"idle" | "new" | "edit">("idle");
  const [selectedKeyId, setSelectedKeyId] = useState<string>();
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<SshKeyFormValues>();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleImportFromFile = useCallback(() => {
    fileInputRef.current?.click();
  }, []);

  const handleFileChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = (ev) => {
        const text = ev.target?.result;
        if (typeof text !== "string") return;
        form.setFieldValue("keyContent", text);
        if (!form.getFieldValue("name")) {
          form.setFieldValue("name", file.name.replace(/\.[^.]*$/, ""));
        }
        message.success(`已从文件「${file.name}」导入私钥`);
      };
      reader.onerror = () => {
        message.error("读取文件失败，请重试");
      };
      reader.readAsText(file);
      e.target.value = "";
    },
    [form]
  );

  const selectedKey = sshKeys.find((k) => k.id === selectedKeyId);

  const handleNew = useCallback(() => {
    setSelectedKeyId(undefined);
    form.resetFields();
    setMode("new");
  }, [form]);

  const handleSelect = useCallback(
    (keyId: string) => {
      const key = sshKeys.find((k) => k.id === keyId);
      if (!key) return;
      setSelectedKeyId(keyId);
      form.setFieldsValue({
        id: key.id,
        name: key.name,
        keyContent: undefined,
        passphrase: undefined
      });
      setMode("edit");
    },
    [sshKeys, form]
  );

  const handleDelete = useCallback(() => {
    if (!selectedKeyId) return;
    Modal.confirm({
      title: "确认删除",
      content: `删除密钥「${selectedKey?.name ?? ""}」？如果仍有连接引用将无法删除。`,
      okText: "删除",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await window.nextshell.sshKey.remove({ id: selectedKeyId, force: false });
          await onReload();
          setSelectedKeyId(undefined);
          form.resetFields();
          setMode("idle");
          message.success("密钥已删除");
        } catch (error) {
          message.error(`删除密钥失败：${formatErrorMessage(error, "请稍后重试")}`);
        }
      }
    });
  }, [selectedKeyId, selectedKey, form, onReload]);

  return (
    <div className={mode !== "idle" ? "grid grid-cols-[230px_1fr] h-[540px] overflow-hidden" : "h-[540px] overflow-hidden"}>
      {/* ── Sidebar ─────────────────────────── */}
      <div className={`flex flex-col bg-[var(--bg-elevated)] overflow-hidden${mode !== "idle" ? " border-r border-[var(--border)]" : ""}`}>
        <div className="mgr-sidebar-head">
          <div className="mgr-sidebar-title-row">
            <span className="mgr-sidebar-title">SSH 密钥</span>
            {sshKeys.length > 0 && (
              <span className="mgr-count-badge">{sshKeys.length}</span>
            )}
          </div>
          <button className="mgr-new-btn" onClick={handleNew} title="新建密钥">
            <i className="ri-add-line" aria-hidden="true" />
          </button>
        </div>

        <div className="mgr-tree-wrap">
          {sshKeys.length === 0 ? (
            <div className="mgr-tree-empty">
              <i className="ri-key-2-line" aria-hidden="true" />
              <span>暂无密钥</span>
            </div>
          ) : (
            <div className="mgr-flat-list">
              {sshKeys.map((k) => (
                <button
                  key={k.id}
                  type="button"
                  className={`mgr-flat-item${k.id === selectedKeyId ? " mgr-flat-item--selected" : ""}`}
                  onClick={() => handleSelect(k.id)}
                >
                  <i className="ri-key-2-line" aria-hidden="true" />
                  <span className="mgr-flat-item-name">{k.name}</span>
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="mgr-sidebar-footer">
          <span className="mgr-count">{sshKeys.length} 个密钥</span>
        </div>
      </div>

      {/* ── Right panel ─────────────────────── */}
      {mode !== "idle" && (
        <div className="flex flex-col overflow-hidden">
          <div className="mgr-form-header">
            <div>
              <div className="mgr-form-title">
                {mode === "new" ? "新建密钥" : (selectedKey?.name ?? "编辑密钥")}
              </div>
              <div className="mgr-form-subtitle">
                {mode === "new" ? "导入私钥内容后保存" : "修改密钥信息后保存"}
              </div>
            </div>
            <div className="mgr-form-header-right">
              {mode === "edit" ? (
                <button
                  type="button"
                  className="mgr-form-header-icon-btn mgr-form-header-icon-btn--danger"
                  onClick={handleDelete}
                  aria-label="删除密钥"
                  title="删除密钥"
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
                aria-label="保存密钥"
                title="保存密钥"
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
                onClick={() => { setMode("idle"); setSelectedKeyId(undefined); }}
                aria-label="收起表单"
                title="收起表单"
              >
                <i className="ri-close-line" aria-hidden="true" />
              </button>
            </div>
          </div>

          <input
            ref={fileInputRef}
            type="file"
            accept=".pem,.key,.ppk,.pub,*"
            aria-label="选择私钥文件"
            className="sr-only"
            onChange={handleFileChange}
          />

          <Form
            form={form}
            layout="vertical"
            requiredMark={false}
            className="mgr-form"
            onFinish={async (values) => {
              const name = (values.name ?? "").trim();
              if (!name) {
                message.error("请输入密钥名称。");
                return;
              }

              const keyContent = values.keyContent?.trim() || undefined;
              const passphrase = values.passphrase?.trim() || undefined;

              if (mode === "new" && !keyContent) {
                message.error("新建密钥必须提供私钥内容。");
                return;
              }

              setSaving(true);
              try {
                await window.nextshell.sshKey.upsert({
                  id: selectedKeyId,
                  name: name,
                  keyContent,
                  passphrase
                });
                await onReload();
                message.success(selectedKeyId ? "密钥已更新" : "密钥已创建");
                if (!selectedKeyId) {
                  // Find the newly created key
                  setMode("idle");
                }
                form.setFieldsValue({ keyContent: undefined, passphrase: undefined });
              } catch (error) {
                message.error(`保存密钥失败：${formatErrorMessage(error, "请检查输入内容")}`);
              } finally {
                setSaving(false);
              }
            }}
          >
            <div className="mgr-section-label">密钥信息</div>

            <Form.Item label="名称" name="name" rules={[{ required: true, message: "请输入密钥名称" }]}>
              <Input placeholder="my-server-key" />
            </Form.Item>

            <Form.Item
              label={
                <span className="mgr-key-label">
                  <span>{mode === "new" ? "私钥内容" : "替换私钥内容（留空则不更新）"}</span>
                  <Tooltip title="从本地文件导入私钥">
                    <button
                      type="button"
                      className="mgr-import-file-btn"
                      onClick={handleImportFromFile}
                    >
                      <i className="ri-folder-open-line" aria-hidden="true" />
                      从文件导入
                    </button>
                  </Tooltip>
                </span>
              }
              name="keyContent"
              rules={mode === "new" ? [{ required: true, message: "请粘贴私钥内容或从文件导入" }] : []}
            >
              <Input.TextArea
                rows={6}
                placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n...\n-----END OPENSSH PRIVATE KEY-----"}
                className="mgr-mono-input"
              />
            </Form.Item>

            <Form.Item
              label="Passphrase（可选）"
              name="passphrase"
            >
              <Input.Password placeholder="留空表示无 Passphrase 或不更新" />
            </Form.Item>
          </Form>
        </div>
      )}
    </div>
  );
};
