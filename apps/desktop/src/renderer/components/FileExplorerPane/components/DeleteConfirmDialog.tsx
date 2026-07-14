import { useEffect, useState } from "react";
import { Checkbox, Modal } from "antd";
import type { RemoteFileEntry } from "@nextshell/core";

interface DeleteConfirmDialogProps {
  open: boolean;
  targets: RemoteFileEntry[];
  onCancel: () => void;
  onConfirm: (force: boolean) => void | Promise<void>;
}

export const DeleteConfirmDialog = ({
  open,
  targets,
  onCancel,
  onConfirm
}: DeleteConfirmDialogProps) => {
  const [force, setForce] = useState(false);
  const [busy, setBusy] = useState(false);

  // 每次打开对话框时重置高危选项，避免上次勾选残留。
  useEffect(() => {
    if (open) {
      setForce(false);
      setBusy(false);
    }
  }, [open]);

  const single = targets.length === 1 ? targets[0] : undefined;

  const handleOk = async () => {
    setBusy(true);
    try {
      await onConfirm(force);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open={open}
      title="删除远端文件"
      okText={force ? "强制删除" : "删除"}
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

      <Checkbox checked={force} onChange={(event) => setForce(event.target.checked)}>
        强制删除（<code>rm -rf</code>，可删除只读 / 非空目录，<strong>不可恢复</strong>）
      </Checkbox>

      {force ? (
        <p style={{ margin: "8px 0 0", color: "var(--err)", fontSize: 12 }}>
          将在远端直接执行 <code>rm -rf</code>，不经回收站，请谨慎确认。
        </p>
      ) : null}
    </Modal>
  );
};
