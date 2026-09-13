import { useEffect, useMemo, useState } from "react";
import { Alert, App as AntdApp, Button, Modal, Select } from "antd";
import type { ConnectionFolder, ConnectionProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { copyConnectionsToScope, describeCopyOutcome } from "../utils/connectionMove";
import { buildFolderPathLabels } from "../utils/folderTree";
import type { ManagerScope } from "../utils/scopes";

interface CopyFromLocalModalProps {
  open: boolean;
  /** 本地作用域里的连接——候选源。 */
  sources: ConnectionProfile[];
  /** 当前云作用域——固定的目标。 */
  target: ManagerScope;
  /** 目标作用域的目录树。 */
  folders: ConnectionFolder[];
  /** 打开时默认落到用户正在浏览的目录。 */
  defaultFolderId?: string;
  onClose: () => void;
  onCopied: () => Promise<void>;
}

/**
 * 「复制到…」的反向入口：站在（多半还是空的）工作区里，把本地服务器搬进来。
 * 老路径要求先切回本地、选中、右键——刚建好工作区的人根本不知道有这一步。
 */
export const CopyFromLocalModal = ({
  open,
  sources,
  target,
  folders,
  defaultFolderId,
  onClose,
  onCopied
}: CopyFromLocalModalProps) => {
  const { message } = AntdApp.useApp();
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [folderId, setFolderId] = useState<string>();
  const [copying, setCopying] = useState(false);

  useEffect(() => {
    if (open) {
      setSelectedIds([]);
      setFolderId(defaultFolderId);
    }
  }, [defaultFolderId, open]);

  const sourceOptions = useMemo(
    () =>
      [...sources]
        .sort((left, right) => left.name.localeCompare(right.name))
        .map((connection) => ({
          value: connection.id,
          label: `${connection.name}（${connection.username}@${connection.host}:${connection.port}）`
        })),
    [sources]
  );
  const folderOptions = useMemo(() => {
    const labels = buildFolderPathLabels(folders);
    return folders
      .map((folder) => ({ value: folder.id, label: labels.get(folder.id) ?? folder.name }))
      .sort((left, right) => left.label.localeCompare(right.label));
  }, [folders]);

  const handleCopy = async () => {
    setCopying(true);
    const outcome = await copyConnectionsToScope(selectedIds, target, folderId).finally(() =>
      setCopying(false)
    );
    await onCopied();
    const summary = describeCopyOutcome({ ...outcome, targetLabel: target.label });
    if (outcome.failed === 0) {
      message.success(summary);
      onClose();
      return;
    }
    message.error(`${summary}：${formatErrorMessage(outcome.failure, "请稍后重试")}`);
  };

  return (
    <Modal
      open={open}
      title={`从本地复制服务器到「${target.label}」`}
      okText={selectedIds.length > 0 ? `复制 ${selectedIds.length} 个` : "复制"}
      cancelText="取消"
      confirmLoading={copying}
      okButtonProps={{ disabled: selectedIds.length === 0 }}
      cancelButtonProps={{ disabled: copying }}
      onOk={() => void handleCopy()}
      onCancel={copying ? undefined : onClose}
      destroyOnHidden
      width={520}
    >
      <div style={{ display: "grid", gap: 12 }}>
        <Alert
          type="info"
          showIcon
          message="本地连接会以副本形式进入工作区并同步给团队"
          description="密码、密钥、代理会一并复制到工作区；本地原连接保持不变。"
        />
        <label className="cm2-modal-field">
          <span style={{ display: "flex", justifyContent: "space-between" }}>
            <span>本地服务器</span>
            <Button
              type="link"
              size="small"
              disabled={sources.length === 0}
              onClick={() => setSelectedIds(sources.map((connection) => connection.id))}
            >
              全选（{sources.length}）
            </Button>
          </span>
          <Select
            mode="multiple"
            allowClear
            showSearch
            optionFilterProp="label"
            maxTagCount="responsive"
            placeholder="选择要复制的本地服务器"
            value={selectedIds}
            onChange={setSelectedIds}
            options={sourceOptions}
            notFoundContent="本地作用域里还没有连接"
          />
        </label>
        <label className="cm2-modal-field">
          <span>放到目录</span>
          <Select
            allowClear
            placeholder="顶层"
            value={folderId}
            onChange={(value) => setFolderId(value ?? undefined)}
            options={folderOptions}
            notFoundContent="工作区里还没有目录"
          />
        </label>
      </div>
    </Modal>
  );
};
