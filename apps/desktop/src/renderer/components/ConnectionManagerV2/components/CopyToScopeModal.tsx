import { useEffect, useMemo, useRef, useState } from "react";
import { Alert, App as AntdApp, Modal, Select } from "antd";
import type { ConnectionFolder } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { copyConnectionsToScope, describeCopyOutcome } from "../utils/connectionMove";
import { buildFolderPathLabels } from "../utils/folderTree";
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

  // 只在"打开"这个瞬间复位。`targets` 派生自 workspaces，云同步每来一次 onStatus/onApplied
  // 都会换一个新引用——按值变化复位的话，弹窗开着时用户选好的目标会被静默打回默认。
  const wasOpenRef = useRef(false);
  useEffect(() => {
    if (open && !wasOpenRef.current) {
      setTargetKey(targets[0]?.key);
      setFolderId(undefined);
    } else if (open && targetKey === undefined) {
      // 打开时 workspace 列表还没拉到：等它到了补一个默认值，但绝不覆盖用户已经选的。
      setTargetKey(targets[0]?.key);
    }
    wasOpenRef.current = open;
  }, [open, targetKey, targets]);

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

  // 下拉必须显示全路径：只显示 name 时不同深度的同名目录无法区分，用户选完也不知道选中了哪个。
  const folderOptions = useMemo(() => {
    const labels = buildFolderPathLabels(folders);
    return folders
      .map((folder) => ({ value: folder.id, label: labels.get(folder.id) ?? folder.name }))
      .sort((left, right) => left.label.localeCompare(right.label));
  }, [folders]);

  const handleCopy = async () => {
    if (!target) {
      return;
    }
    setCopying(true);
    const outcome = await copyConnectionsToScope(connectionIds, target, folderId).finally(() =>
      setCopying(false)
    );
    // 哪怕只成功了一条，目标域的列表也已经变了，必须刷新。
    await onCopied();
    const summary = describeCopyOutcome({ ...outcome, targetLabel: target.label });
    if (outcome.failed === 0) {
      message.success(summary);
      onClose();
      return;
    }
    // 有失败就不关弹窗：目标作用域/目录还留在原处，用户可以直接重试。
    message.error(`${summary}：${formatErrorMessage(outcome.failure, "请稍后重试")}`);
  };

  return (
    <Modal
      open={open}
      title="复制到工作区 / 本地"
      okText="复制"
      cancelText="取消"
      confirmLoading={copying}
      okButtonProps={{ disabled: !target || connectionIds.length === 0 }}
      // 复制已经在逐条发了：中途关掉弹窗停不下这个循环，只会让用户以为取消成功。
      cancelButtonProps={{ disabled: copying }}
      onOk={() => void handleCopy()}
      onCancel={copying ? undefined : onClose}
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
          <span>复制到</span>
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
            onChange={(value) => setFolderId(value ?? undefined)}
            options={folderOptions}
            notFoundContent="目标作用域下还没有目录"
          />
        </label>
      </div>
    </Modal>
  );
};
