import { useCallback, useEffect, useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { App as AntdApp, Form, Input, Modal, Select } from "antd";
import type { SshKeyProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { readPrivateKeyFile, suggestKeyNameFromFile } from "../utils/sshKeyEdits";

interface InlineSshKeyModalProps {
  open: boolean;
  /** 密钥落在连接表单所在的作用域；本地为 undefined。作用域不可在此切换。 */
  workspaceId?: string;
  onClose: () => void;
  /** 创建成功后回调，调用方负责刷新密钥列表并把新密钥写回表单。 */
  onCreated: (key: SshKeyProfile) => Promise<void> | void;
}

interface InlineSshKeyValues {
  mode: "import" | "generate";
  name: string;
  keyContent?: string;
  passphrase?: string;
  algorithm?: "ed25519" | "rsa-2048" | "rsa-4096";
  comment?: string;
}

/**
 * 连接编辑态内的快捷密钥创建。关键约束是**编辑中的连接表单不能卸载**——所以这里是一个叠在
 * 管理器之上的弹窗，而不是切到密钥面板（那两者互斥渲染，切走等于丢掉填了一半的表单）。
 */
export const InlineSshKeyModal = ({
  open,
  workspaceId,
  onClose,
  onCreated
}: InlineSshKeyModalProps) => {
  const { message } = AntdApp.useApp();
  const [form] = Form.useForm<InlineSshKeyValues>();
  const [saving, setSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const mode = Form.useWatch("mode", form) ?? "generate";

  // 每次打开都清一次。`destroyOnHidden` 只卸载 DOM，rc-field-form 的 store 挂在这个
  // 常驻组件的 form 实例上活得好好的——上次取消掉的私钥内容会原样躺在下次打开的输入框里。
  // 与密钥面板「新建/替换私钥」在打开前 resetFields 是同一套做法。
  const wasOpenRef = useRef(false);
  useEffect(() => {
    if (open && !wasOpenRef.current) {
      form.resetFields();
    }
    wasOpenRef.current = open;
  }, [form, open]);

  // 读文件的两处(这里和密钥面板的新建/替换)共用 `readPrivateKeyFile`,免得两边对
  // "读失败怎么提示""文件名怎么变成默认密钥名"各有一套。
  const handleFileChange = useCallback(
    async (event: ChangeEvent<HTMLInputElement>) => {
      const file = event.target.files?.[0];
      // 读完就把 input 清空，否则连选两次同一个文件不会触发 change。
      event.target.value = "";
      if (!file) {
        return;
      }
      try {
        form.setFieldValue("keyContent", await readPrivateKeyFile(file));
        if (!form.getFieldValue("name")) {
          form.setFieldValue("name", suggestKeyNameFromFile(file.name));
        }
        message.success(`已读取私钥文件「${file.name}」`);
      } catch (error) {
        message.error(formatErrorMessage(error, "读取文件失败，请重试"));
      }
    },
    [form, message]
  );

  const handleFinish = useCallback(
    async (values: InlineSshKeyValues) => {
      setSaving(true);
      try {
        const created =
          values.mode === "import"
            ? await window.nextshell.sshKey.upsert({
                name: values.name,
                keyContent: values.keyContent,
                passphrase: values.passphrase,
                workspaceId
              })
            : await window.nextshell.sshKey.generate({
                name: values.name,
                algorithm: values.algorithm ?? "ed25519",
                comment: values.comment,
                workspaceId
              });
        await onCreated(created);
        message.success(`密钥「${created.name}」已创建并选中`);
        form.resetFields();
        onClose();
      } catch (error) {
        message.error(`创建密钥失败：${formatErrorMessage(error, "请检查私钥内容")}`);
      } finally {
        setSaving(false);
      }
    },
    [form, message, onClose, onCreated, workspaceId]
  );

  return (
    <Modal
      open={open}
      title="新建 SSH 密钥"
      okText="创建并选用"
      cancelText="取消"
      confirmLoading={saving}
      // 创建请求已经发出去了，这时点「取消」既拦不住它，还会让弹窗在回调落地前消失。
      cancelButtonProps={{ disabled: saving }}
      onOk={() => form.submit()}
      onCancel={onClose}
      destroyOnHidden
      width={520}
    >
      <input
        ref={fileInputRef}
        type="file"
        aria-label="选择私钥文件"
        style={{ display: "none" }}
        onChange={(event) => void handleFileChange(event)}
      />
      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        initialValues={{ mode: "generate", algorithm: "ed25519" }}
        onFinish={(values) => void handleFinish(values)}
      >
        <Form.Item label="方式" name="mode">
          <Select
            options={[
              { label: "生成新密钥", value: "generate" },
              { label: "导入已有私钥", value: "import" }
            ]}
          />
        </Form.Item>
        <Form.Item label="名称" name="name" rules={[{ required: true, message: "请输入名称" }]}>
          <Input placeholder="deploy-key" autoFocus />
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
              label={
                <span className="cm2-label-with-action">
                  私钥内容
                  <button
                    type="button"
                    className="cm2-inline-btn"
                    onClick={() => fileInputRef.current?.click()}
                  >
                    <i className="ri-folder-open-line" aria-hidden="true" />
                    从文件读取
                  </button>
                </span>
              }
              name="keyContent"
              rules={[{ required: true, message: "请粘贴私钥内容或从文件读取" }]}
            >
              <Input.TextArea
                rows={6}
                className="cm2-mono"
                placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n…"}
              />
            </Form.Item>
            <Form.Item label="Passphrase（可选）" name="passphrase">
              <Input.Password placeholder="加密私钥才需要" />
            </Form.Item>
          </>
        )}
      </Form>
    </Modal>
  );
};
