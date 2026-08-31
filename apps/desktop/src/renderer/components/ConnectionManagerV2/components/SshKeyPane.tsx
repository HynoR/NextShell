import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { App as AntdApp, Button, Form, Input, Modal, Select, Table, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import type { SshKeyProfile } from "@nextshell/core";
import type { SshKeyUsageItem } from "@nextshell/shared";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { promptModal } from "../../../utils/promptModal";
import {
  planSshKeyRename,
  planSshKeyReplace,
  readPrivateKeyFile,
  suggestKeyNameFromFile
} from "../utils/sshKeyEdits";

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

interface ReplaceKeyValues {
  keyContent?: string;
  passphrase?: string;
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
  const [replaceOpen, setReplaceOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm<CreateKeyValues>();
  const [replaceForm] = Form.useForm<ReplaceKeyValues>();
  const mode = Form.useWatch("mode", form) ?? "generate";
  const createFileRef = useRef<HTMLInputElement>(null);
  const replaceFileRef = useRef<HTMLInputElement>(null);

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

  /**
   * 「从文件读取」的公共部分。渲染进程在 sandbox 下拿不到路径,只能读文本贴进表单——
   * 与 `InlineSshKeyModal` 共用 `readPrivateKeyFile`,两处行为保持一致。
   */
  const handlePickKeyFile = useCallback(
    async (event: ChangeEvent<HTMLInputElement>, apply: (text: string, fileName: string) => void) => {
      const file = event.target.files?.[0];
      // 读完就把 input 清空，否则连选两次同一个文件不会触发 change。
      event.target.value = "";
      if (!file) {
        return;
      }
      try {
        apply(await readPrivateKeyFile(file), file.name);
        message.success(`已读取私钥文件「${file.name}」`);
      } catch (error) {
        message.error(formatErrorMessage(error, "读取文件失败，请重试"));
      }
    },
    [message]
  );

  /**
   * 改名。走的还是 `sshKey.upsert`,但 payload 里不带 keyContent——契约把它标成可选,
   * 主进程见不到内容就沿用已存的私钥和解析出的类型/指纹,所以改名不会碰私钥。
   */
  const handleRename = useCallback(async () => {
    if (!selected) {
      return;
    }
    const name = await promptModal(modal, `重命名密钥「${selected.name}」`, "请输入新的名称", selected.name);
    const payload = planSshKeyRename({ key: selected, name: name ?? "", workspaceId });
    if (!payload) {
      return;
    }
    try {
      await window.nextshell.sshKey.upsert(payload);
      await onReload();
      message.success("密钥已重命名");
    } catch (error) {
      message.error(`重命名失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [message, modal, onReload, selected, workspaceId]);

  /** 换私钥内容。留空 = 不更新;新内容会在主进程重新解析,解析失败原样报出来并留在弹窗里改。 */
  const handleReplace = useCallback(
    async (values: ReplaceKeyValues) => {
      if (!selected) {
        return;
      }
      const payload = planSshKeyReplace({
        key: selected,
        keyContent: values.keyContent,
        passphrase: values.passphrase,
        workspaceId
      });
      if (!payload) {
        // 留空 = 不更新。说一声，否则"点了替换、弹窗关了、什么都没变"读起来像是失败了。
        message.info("没有填写新的私钥内容，密钥未改动");
        setReplaceOpen(false);
        return;
      }
      setSaving(true);
      try {
        await window.nextshell.sshKey.upsert(payload);
        await onReload();
        setReplaceOpen(false);
        replaceForm.resetFields();
        message.success("私钥内容已替换");
      } catch (error) {
        // 解析失败(贴成公钥、文件截断、passphrase 不对)在这里现形，不关弹窗好让用户直接改。
        message.error(`替换私钥失败：${formatErrorMessage(error, "请检查私钥内容与 passphrase")}`);
      } finally {
        setSaving(false);
      }
    },
    [message, onReload, replaceForm, selected, workspaceId]
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
          <Button
            type="primary"
            icon={<i className="ri-add-line" aria-hidden="true" />}
            onClick={() => {
              form.resetFields();
              form.setFieldsValue({ mode: "generate", algorithm: "ed25519" });
              setCreateOpen(true);
            }}
          >
            新建 / 生成密钥
          </Button>
        </div>
        <div className="cm2-table-wrap">
          <Table<SshKeyProfile>
            className="cm2-table app-table"
            size="small"
            rowKey="id"
            dataSource={sshKeys}
            columns={columns}
            pagination={false}
            locale={{ emptyText: "当前作用域没有密钥" }}
            rowClassName={(key) => (key.id === selectedId ? "cm2-row cm2-row--focused" : "cm2-row")}
            onRow={(key) => ({ onClick: () => setSelectedId(key.id) })}
          />
        </div>
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
                <Button
                  icon={<i className="ri-file-copy-line" aria-hidden="true" />}
                  disabled={!selected.publicKeyLine}
                  onClick={() => {
                    void navigator.clipboard.writeText(selected.publicKeyLine ?? "");
                    message.success("已复制公钥");
                  }}
                >
                  复制公钥
                </Button>
              </Tooltip>
              <Button
                icon={<i className="ri-pencil-line" aria-hidden="true" />}
                onClick={() => void handleRename()}
              >
                重命名
              </Button>
              <Tooltip title="用另一份私钥换掉这把密钥的内容，绑定它的连接不用重新设置">
                <Button
                  icon={<i className="ri-key-2-line" aria-hidden="true" />}
                  onClick={() => {
                    replaceForm.resetFields();
                    setReplaceOpen(true);
                  }}
                >
                  替换私钥
                </Button>
              </Tooltip>
              <Button
                danger
                icon={<i className="ri-delete-bin-line" aria-hidden="true" />}
                onClick={handleDelete}
              >
                删除
              </Button>
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
        // 保存请求已经在路上：取消拦不住它，只会让弹窗在结果落地前消失。
        cancelButtonProps={{ disabled: saving }}
        onOk={() => form.submit()}
        onCancel={() => setCreateOpen(false)}
        destroyOnHidden
        width={520}
      >
        <input
          ref={createFileRef}
          type="file"
          aria-label="选择私钥文件"
          style={{ display: "none" }}
          onChange={(event) =>
            void handlePickKeyFile(event, (text, fileName) => {
              form.setFieldValue("keyContent", text);
              if (!form.getFieldValue("name")) {
                form.setFieldValue("name", suggestKeyNameFromFile(fileName));
              }
            })
          }
        />
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
                label={
                  <span className="cm2-label-with-action">
                    私钥内容
                    <button
                      type="button"
                      className="cm2-inline-btn"
                      onClick={() => createFileRef.current?.click()}
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
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                />
              </Form.Item>
              <Form.Item label="Passphrase（可选）" name="passphrase">
                <Input.Password placeholder="加密私钥才需要" />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>

      <Modal
        open={replaceOpen}
        title={selected ? `替换「${selected.name}」的私钥内容` : "替换私钥内容"}
        okText="替换"
        cancelText="取消"
        confirmLoading={saving}
        cancelButtonProps={{ disabled: saving }}
        onOk={() => replaceForm.submit()}
        onCancel={() => setReplaceOpen(false)}
        destroyOnHidden
        width={520}
      >
        <input
          ref={replaceFileRef}
          type="file"
          aria-label="选择私钥文件"
          style={{ display: "none" }}
          onChange={(event) =>
            void handlePickKeyFile(event, (text) => replaceForm.setFieldValue("keyContent", text))
          }
        />
        <Form
          form={replaceForm}
          layout="vertical"
          requiredMark={false}
          onFinish={(values) => void handleReplace(values)}
        >
          <Form.Item
            label={
              <span className="cm2-label-with-action">
                新的私钥内容
                <button
                  type="button"
                  className="cm2-inline-btn"
                  onClick={() => replaceFileRef.current?.click()}
                >
                  <i className="ri-folder-open-line" aria-hidden="true" />
                  从文件读取
                </button>
              </span>
            }
            name="keyContent"
            extra="留空则不更新私钥内容。保存时会重新解析，类型与指纹随之刷新。"
          >
            <Input.TextArea
              rows={6}
              className="cm2-mono"
              placeholder={"-----BEGIN OPENSSH PRIVATE KEY-----\n…"}
            />
          </Form.Item>
          <Form.Item label="Passphrase（可选）" name="passphrase" extra="留空则沿用已存的口令。">
            <Input.Password placeholder="加密私钥才需要" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
};
