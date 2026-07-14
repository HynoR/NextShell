import { Tag, Tooltip } from "antd";
import type { ScopedCommandItem } from "@nextshell/core";

interface CommandItemProps {
  command: ScopedCommandItem;
  canExecuteSingle: boolean;
  batchRunning: boolean;
  onRun: (cmd: ScopedCommandItem) => void;
  onRunBatch: (cmd: ScopedCommandItem) => void;
  onEdit: (cmd: ScopedCommandItem) => void;
  onRemove: (cmd: ScopedCommandItem) => void;
}

export const CommandItem = ({
  command,
  canExecuteSingle,
  batchRunning,
  onRun,
  onRunBatch,
  onEdit,
  onRemove
}: CommandItemProps) => (
  <div className="cc-item">
    <div className="cc-item-body">
      <span className="cc-item-name">{command.name}</span>
      {command.isTemplate && (
        <Tag color="blue" className="cc-item-tag">
          模板
        </Tag>
      )}
      <Tooltip title={command.command} placement="topLeft" mouseEnterDelay={0.4}>
        <code className="cc-item-cmd">{command.command}</code>
      </Tooltip>
    </div>

    <span className="cc-item-actions">
      <Tooltip title="执行" mouseEnterDelay={0.3}>
        <button className="cc-act run" disabled={!canExecuteSingle} onClick={() => onRun(command)}>
          <i className="ri-play-line" aria-hidden="true" />
        </button>
      </Tooltip>
      <Tooltip title="批量执行" mouseEnterDelay={0.3}>
        <button
          className="cc-act batch"
          disabled={batchRunning}
          onClick={() => onRunBatch(command)}
        >
          <i className="ri-stack-line" aria-hidden="true" />
        </button>
      </Tooltip>
      <Tooltip title="编辑" mouseEnterDelay={0.3}>
        <button className="cc-act" onClick={() => onEdit(command)}>
          <i className="ri-edit-line" aria-hidden="true" />
        </button>
      </Tooltip>
      <Tooltip title="删除" mouseEnterDelay={0.3}>
        <button className="cc-act del" onClick={() => onRemove(command)}>
          <i className="ri-delete-bin-line" aria-hidden="true" />
        </button>
      </Tooltip>
    </span>
  </div>
);
