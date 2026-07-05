import { useCallback, useEffect, useState } from "react";
import { Input, Modal, Space, Switch, Typography } from "antd";
import type { ScopedCommandItem } from "@nextshell/core";

interface CommandEditModalProps {
  open: boolean;
  scopeLabel: string;
  editingCommand: ScopedCommandItem | null;
  onSubmit: (values: {
    name: string;
    description: string;
    group: string;
    command: string;
    isTemplate: boolean;
  }) => void;
  onCancel: () => void;
}

export const CommandEditModal = ({
  open,
  scopeLabel,
  editingCommand,
  onSubmit,
  onCancel,
}: CommandEditModalProps) => {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [group, setGroup] = useState("默认");
  const [command, setCommand] = useState("");
  const [isTemplate, setIsTemplate] = useState(false);

  useEffect(() => {
    if (!open) return;
    if (editingCommand) {
      setName(editingCommand.name);
      setDescription(editingCommand.description ?? "");
      setGroup(editingCommand.group || "默认");
      setCommand(editingCommand.command);
      setIsTemplate(editingCommand.isTemplate);
    } else {
      setName("");
      setDescription("");
      setGroup("默认");
      setCommand("");
      setIsTemplate(false);
    }
  }, [open, editingCommand]);

  const handleOk = useCallback(() => {
    onSubmit({
      name: name.trim(),
      description: description.trim(),
      group: group.trim() || "默认",
      command: command.trim(),
      isTemplate,
    });
  }, [name, description, group, command, isTemplate, onSubmit]);

  return (
    <Modal
      title={editingCommand ? "编辑命令" : "新建命令"}
      open={open}
      onOk={handleOk}
      onCancel={onCancel}
      destroyOnHidden
      width={520}
    >
      <Space direction="vertical" style={{ width: "100%" }} size="middle">
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          当前作用域: {scopeLabel}
        </Typography.Text>
        <div>
          <Typography.Text type="secondary">名称</Typography.Text>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="命令名称"
          />
        </div>
        <div>
          <Typography.Text type="secondary">描述（可选）</Typography.Text>
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="简要说明"
          />
        </div>
        <div>
          <Typography.Text type="secondary">分组</Typography.Text>
          <Input
            value={group}
            onChange={(e) => setGroup(e.target.value)}
            placeholder="默认"
          />
        </div>
        <div>
          <Typography.Text type="secondary">
            命令内容（模板可用 [#key] 占位符）
          </Typography.Text>
          <Input.TextArea
            value={command}
            onChange={(e) => setCommand(e.target.value)}
            placeholder="例如: uname -a 或 tail -n [#lines] /var/log/syslog"
            rows={3}
          />
        </div>
        <div>
          <Space>
            <Typography.Text type="secondary">
              模板命令（含 [#占位符] 时勾选）
            </Typography.Text>
            <Switch checked={isTemplate} onChange={setIsTemplate} />
          </Space>
        </div>
      </Space>
    </Modal>
  );
};
