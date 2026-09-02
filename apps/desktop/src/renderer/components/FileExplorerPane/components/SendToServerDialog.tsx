import { useEffect, useMemo, useState } from "react";
import { App as AntdApp, Checkbox, Input, Modal } from "antd";
import type { RemoteFileEntry } from "@nextshell/core";
import { useTransferQueueStore } from "../../../store/useTransferQueueStore";
import { useWorkspaceStore } from "../../../store/useWorkspaceStore";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { normalizeRemotePath } from "../shared";

interface SendToServerDialogProps {
  open: boolean;
  sourceConnectionId: string;
  sourceDir: string;
  entries: RemoteFileEntry[];
  onClose: () => void;
}

export const SendToServerDialog = ({
  open,
  sourceConnectionId,
  sourceDir,
  entries,
  onClose
}: SendToServerDialogProps) => {
  const { message } = AntdApp.useApp();
  const sessions = useWorkspaceStore((state) => state.sessions);
  const connections = useWorkspaceStore((state) => state.connections);
  const enqueueTask = useTransferQueueStore((state) => state.enqueueTask);
  const markFailed = useTransferQueueStore((state) => state.markFailed);
  const markSuccess = useTransferQueueStore((state) => state.markSuccess);

  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [targetDir, setTargetDir] = useState(sourceDir);
  const [sending, setSending] = useState(false);

  useEffect(() => {
    if (open) {
      setSelectedIds([]);
      setTargetDir(sourceDir);
      setSending(false);
    }
  }, [open, sourceDir]);

  // 只列「其他已连接终端标签页」对应的连接（transferPacked 要求两端都在连接池里）。
  const targets = useMemo(() => {
    const ids = new Set<string>();
    for (const session of sessions) {
      if (session.status !== "connected") continue;
      if (session.type !== "terminal") continue;
      if (!session.connectionId || session.connectionId === sourceConnectionId) continue;
      ids.add(session.connectionId);
    }
    return connections.filter((item) => ids.has(item.id));
  }, [sessions, connections, sourceConnectionId]);

  const allChecked = targets.length > 0 && selectedIds.length === targets.length;

  const handleToggle = (id: string) => {
    setSelectedIds((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id]
    );
  };

  const handleConfirm = async () => {
    const normalizedSourceDir = normalizeRemotePath(sourceDir);
    const normalizedTargetDir = normalizeRemotePath(targetDir);
    const entryNames = entries.map((entry) => entry.name);
    setSending(true);
    let failed = 0;
    try {
      for (const targetId of selectedIds) {
        const target = targets.find((item) => item.id === targetId);
        const targetLabel = target?.name ?? targetId;
        const task = enqueueTask({
          direction: "upload",
          connectionId: sourceConnectionId,
          localPath: `${normalizedSourceDir} (${entryNames.length} 项)`,
          remotePath: `${targetLabel}(${normalizedTargetDir})`,
          retryable: false
        });
        try {
          await window.nextshell.sftp.transferPacked({
            sourceConnectionId,
            sourceDir: normalizedSourceDir,
            entryNames,
            targetConnectionId: targetId,
            targetDir: normalizedTargetDir,
            taskId: task.id
          });
          markSuccess(task.id);
        } catch (error) {
          markFailed(task.id, formatErrorMessage(error, "发送到服务器失败"));
          failed += 1;
        }
      }
      if (failed === 0) {
        message.success(`已发送到 ${selectedIds.length} 台服务器`);
      } else {
        message.warning(`${selectedIds.length - failed} 台成功，${failed} 台失败，详见传输队列`);
      }
      onClose();
    } finally {
      setSending(false);
    }
  };

  return (
    <Modal
      open={open}
      title={`发送到服务器（已选 ${entries.length} 项）`}
      okText="发送"
      cancelText="取消"
      okButtonProps={{ disabled: selectedIds.length === 0 || !targetDir.trim() }}
      confirmLoading={sending}
      onCancel={() => {
        if (sending) return;
        onClose();
      }}
      onOk={() => {
        void handleConfirm();
      }}
      destroyOnHidden
    >
      {targets.length === 0 ? (
        <p style={{ marginTop: 0 }}>暂无其他已连接的终端标签页，请先连接目标服务器。</p>
      ) : (
        <>
          <Checkbox
            checked={allChecked}
            indeterminate={selectedIds.length > 0 && !allChecked}
            onChange={() => setSelectedIds(allChecked ? [] : targets.map((item) => item.id))}
          >
            全选（{targets.length} 台）
          </Checkbox>
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              gap: 4,
              margin: "8px 0 12px 24px"
            }}
          >
            {targets.map((item) => (
              <Checkbox
                key={item.id}
                checked={selectedIds.includes(item.id)}
                onChange={() => handleToggle(item.id)}
              >
                {item.name}（{item.host}）
              </Checkbox>
            ))}
          </div>
          <div>
            <div style={{ marginBottom: 4 }}>目标目录</div>
            <Input value={targetDir} onChange={(event) => setTargetDir(event.target.value)} />
          </div>
        </>
      )}
    </Modal>
  );
};
