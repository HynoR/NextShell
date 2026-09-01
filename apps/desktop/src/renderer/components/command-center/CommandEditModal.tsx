import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Checkbox, Input, Modal, Space, Typography } from "antd";
import type { ScopedCommandItem } from "@nextshell/core";

interface CommandEditModalProps {
  open: boolean;
  scopeLabel: string;
  editingCommand: ScopedCommandItem | null;
  onSubmit: (values: { name: string; command: string; appendCr: boolean }) => void;
  onCancel: () => void;
}

interface Selection {
  start: number;
  end: number;
}

export const CommandEditModal = ({
  open,
  scopeLabel,
  editingCommand,
  onSubmit,
  onCancel
}: CommandEditModalProps) => {
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [appendCr, setAppendCr] = useState(true);
  const [selection, setSelection] = useState<Selection>({ start: 0, end: 0 });
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  useEffect(() => {
    if (!open) return;
    setName(editingCommand?.name ?? "");
    setCommand(editingCommand?.command ?? "");
    setAppendCr(editingCommand?.appendCr !== false);
    setSelection({ start: 0, end: 0 });
  }, [open, editingCommand]);

  const insertParameter = useCallback(
    (index: number) => {
      const value = `[#参数${index}]`;
      const start = Math.min(selection.start, command.length);
      const end = Math.min(Math.max(start, selection.end), command.length);
      const next = `${command.slice(0, start)}${value}${command.slice(end)}`;
      const cursor = start + value.length;
      setCommand(next);
      setSelection({ start: cursor, end: cursor });
      requestAnimationFrame(() => {
        const textarea = textareaRef.current;
        textarea?.focus();
        textarea?.setSelectionRange(cursor, cursor);
      });
    },
    [command, selection]
  );

  const handleOk = useCallback(() => {
    onSubmit({ name: name.trim(), command: command.trim(), appendCr });
  }, [appendCr, command, name, onSubmit]);

  return (
    <Modal
      title={editingCommand ? "编辑命令" : "新建命令"}
      open={open}
      onOk={handleOk}
      onCancel={onCancel}
      destroyOnHidden
      width={560}
    >
      <Space direction="vertical" style={{ width: "100%" }} size="middle">
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          当前文件夹：{scopeLabel}
        </Typography.Text>
        <div>
          <Typography.Text type="secondary">名称</Typography.Text>
          <Input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="命令名称"
          />
        </div>
        <div>
          <Typography.Text type="secondary">命令</Typography.Text>
          <Input.TextArea
            ref={(instance) => {
              textareaRef.current = instance?.resizableTextArea?.textArea ?? null;
            }}
            value={command}
            onChange={(event) => setCommand(event.target.value)}
            onSelect={(event) =>
              setSelection({
                start: event.currentTarget.selectionStart,
                end: event.currentTarget.selectionEnd
              })
            }
            onClick={(event) =>
              setSelection({
                start: event.currentTarget.selectionStart,
                end: event.currentTarget.selectionEnd
              })
            }
            placeholder="例如：tail -n [#行数] /var/log/system.log"
            rows={5}
            style={{ fontFamily: "var(--mono)" }}
          />
        </div>
        <Space wrap>
          <Typography.Text type="secondary">插入参数</Typography.Text>
          {Array.from({ length: 5 }, (_, index) => (
            <Button key={index} size="small" onClick={() => insertParameter(index + 1)}>
              参数{index + 1}
            </Button>
          ))}
        </Space>
        <Checkbox checked={appendCr} onChange={(event) => setAppendCr(event.target.checked)}>
          末尾添加回车符
        </Checkbox>
      </Space>
    </Modal>
  );
};
