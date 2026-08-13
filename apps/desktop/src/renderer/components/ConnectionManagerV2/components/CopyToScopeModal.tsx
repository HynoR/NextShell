import { useEffect, useMemo, useState } from "react";
import { Alert, App as AntdApp, Modal, Select } from "antd";
import type { ConnectionFolder } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";
import type { ManagerScope } from "../utils/scopes";

interface CopyToScopeModalProps {
  open: boolean;
  connectionIds: string[];
  label: string;
  scopes: ManagerScope[];
  currentScopeKey: string;
  onClose: () => void;
  onCopied: () => Promise<void>;
}

/**
 * 跨作用域搬运的唯一入口。作用域切换器一次只显示一个隔离域,拖拽跨域在结构上不存在,所以搬运
 * 必须是一条显式命令——而且要先说清楚密钥会被重建,别让用户以为是原地移动。
 */
export const CopyToScopeModal = ({
  open,
  connectionIds,
  label,
  scopes,
  currentScopeKey,
  onClose,
  onCopied
}: CopyToScopeModalProps) => {
  const { message } = AntdApp.useApp();
  const targets = useMemo(
    () => scopes.filter((scope) => scope.key !== currentScopeKey),
    [currentScopeKey, scopes]
  );
  const [targetKey, setTargetKey] = useState<string>();
  const [folders, setFolders] = useState<ConnectionFolder[]>([]);
  const [folderId, setFolderId] = useState<string>();
  const [copying, setCopying] = useState(false);

  useEffect(() => {
    if (!open) {
      return;
    }
    setTargetKey(targets[0]?.key);
    setFolderId(undefined);
  }, [open, targets]);

  useEffect(() => {
    if (!open || !targetKey) {
      setFolders([]);
      return;
    }
    window.nextshell.connectionFolder
      .list({ scopeKey: targetKey })
      .then(setFolders)
      .catch(() => setFolders([]));
  }, [open, targetKey]);

  const target = targets.find((scope) => scope.key === targetKey);

  const handleCopy = async () => {
    if (!target) {
      return;
    }
    setCopying(true);
    try {
      for (const sourceId of connectionIds) {
        await window.nextshell.resourceOps.copyConnection({
          sourceId,
          targetOriginKind: target.kind,
          targetWorkspaceId: target.workspaceId,
          targetGroupSubPath: folders.find((folder) => folder.id === folderId)?.name
        });
      }
      message.success(`已复制 ${connectionIds.length} 个连接到「${target.label}」`);
      await onCopied();
      onClose();
    } catch (error) {
      message.error(`复制失败：${formatErrorMessage(error, "请稍后重试")}`);
    } finally {
      setCopying(false);
    }
  };

  return (
    <Modal
      open={open}
      title="复制到作用域"
      okText="复制"
      cancelText="取消"
      confirmLoading={copying}
      okButtonProps={{ disabled: !target || connectionIds.length === 0 }}
      onOk={() => void handleCopy()}
      onCancel={onClose}
      destroyOnHidden
      width={460}
    >
      <div style={{ display: "grid", gap: 12 }}>
        <Alert
          type="info"
          showIcon
          message={`复制 ${label}`}
          description="作用域之间不共享密钥与代理：目标域里会建立副本，私钥认证的连接可能需要在目标域重新绑定密钥。原连接保持不变。"
        />
        <label className="cm2-modal-field">
          <span>目标作用域</span>
          <Select
            value={targetKey}
            onChange={(value) => {
              setTargetKey(value);
              setFolderId(undefined);
            }}
            options={targets.map((scope) => ({ value: scope.key, label: scope.label }))}
            notFoundContent="没有其他作用域，先在设置中心添加云同步 workspace"
          />
        </label>
        <label className="cm2-modal-field">
          <span>目标目录</span>
          <Select
            allowClear
            placeholder="顶层"
            value={folderId}
            onChange={setFolderId}
            options={folders.map((folder) => ({ value: folder.id, label: folder.name }))}
            notFoundContent="目标作用域下还没有目录"
          />
        </label>
      </div>
    </Modal>
  );
};
