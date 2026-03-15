import { Modal, Typography } from "antd";

interface FileExplorerEditorModalProps {
  open: boolean;
  value: string;
  presets: { label: string; value: string }[];
  onChange: (value: string) => void;
  onOk: () => void;
  onCancel: () => void;
}

export const FileExplorerEditorModal = ({
  open,
  value,
  presets,
  onChange,
  onOk,
  onCancel
}: FileExplorerEditorModalProps) => (
  <Modal
    title="选择编辑器"
    open={open}
    onOk={onOk}
    onCancel={onCancel}
    okText="确认"
    cancelText="取消"
    width={420}
  >
    <div className="fe-editor-modal-body">
      <Typography.Text type="secondary">
        选择用于编辑远程文件的本地编辑器，文件保存后将自动同步回服务器。
      </Typography.Text>
      <div className="fe-editor-preset-list">
        {presets.map((preset) => (
          <button
            key={preset.value}
            className={`fe-editor-preset${value === preset.value ? " active" : ""}`}
            onClick={() => onChange(preset.value)}
          >
            {preset.label}
          </button>
        ))}
      </div>
      <input
        className="fe-path-input"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder="输入编辑器命令，如 code、cursor、vim"
        onKeyDown={(event) => {
          if (event.key === "Enter") onOk();
        }}
      />
    </div>
  </Modal>
);
