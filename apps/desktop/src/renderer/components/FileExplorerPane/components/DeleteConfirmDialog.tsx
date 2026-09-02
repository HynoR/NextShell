import { useEffect, useState } from "react";
import { Modal } from "antd";
import type { RemoteFileEntry } from "@nextshell/core";

interface DeleteConfirmDialogProps {
  open: boolean;
  targets: RemoteFileEntry[];
  onCancel: () => void;
  onConfirm: () => void | Promise<void>;
}

export const DeleteConfirmDialog = ({
  open,
  targets,
  onCancel,
  onConfirm
}: DeleteConfirmDialogProps) => {
  const [busy, setBusy] = useState(false);

  // 每次打开对话框时重置确认 loading，避免上次操作残留。
  useEffect(() => {
    if (open) {
      setBusy(false);
    }
  }, [open]);

  const single = targets.length === 1 ? targets[0] : undefined;

  const handleOk = async () => {
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open={open}
      title="删除远端文件"
      okText="删除"
      cancelText="取消"
      okButtonProps={{ danger: true }}
      confirmLoading={busy}
      onCancel={() => {
        if (busy) return;
        onCancel();
      }}
      onOk={() => {
        void handleOk();
      }}
      destroyOnHidden
    >
      <p style={{ marginTop: 0 }}>
        {single ? (
          <>
            确认删除 <code>{single.path}</code> ?
          </>
        ) : (
          `确认删除选中的 ${targets.length} 项?`
        )}
      </p>
    </Modal>
  );
};
