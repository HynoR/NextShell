import { useCallback, useState } from "react";
import type { ScopedCommandItem } from "@nextshell/core";
import { CommandItem } from "./CommandItem";

interface CommandGroup {
  name: string;
  commands: ScopedCommandItem[];
}

interface CommandListProps {
  groups: CommandGroup[];
  loading: boolean;
  emptyLabel: string;
  canExecuteSingle: boolean;
  batchRunning: boolean;
  onRun: (cmd: ScopedCommandItem) => void;
  onRunBatch: (cmd: ScopedCommandItem) => void;
  onEdit: (cmd: ScopedCommandItem) => void;
  onRemove: (cmd: ScopedCommandItem) => void;
}

export const CommandList = ({
  groups,
  loading,
  emptyLabel,
  canExecuteSingle,
  batchRunning,
  onRun,
  onRunBatch,
  onEdit,
  onRemove,
}: CommandListProps) => {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const toggle = useCallback((group: string) => {
    setCollapsed((prev) => ({ ...prev, [group]: !prev[group] }));
  }, []);

  if (loading) {
    return <div className="cc-empty">加载中…</div>;
  }

  if (groups.length === 0) {
    return (
      <div className="cc-empty">
        <i className="ri-inbox-2-line" aria-hidden="true" />
        <span>{emptyLabel} 下暂无命令，点击 + 添加</span>
      </div>
    );
  }

  return (
    <div className="cc-list">
      {groups.map((group) => {
        const isCollapsed = collapsed[group.name] ?? false;
        return (
          <div key={group.name} className="cc-group">
            <button
              type="button"
              className="cc-group-header"
              aria-expanded={!isCollapsed}
              onClick={() => toggle(group.name)}
            >
              <i
                className={
                  isCollapsed
                    ? "ri-arrow-right-s-line"
                    : "ri-arrow-down-s-line"
                }
                aria-hidden="true"
              />
              <i className="ri-folder-3-line" aria-hidden="true" />
              <span className="cc-group-name">{group.name}</span>
              <span className="cc-group-count">{group.commands.length}</span>
            </button>
            {!isCollapsed && (
              <div className="cc-group-items">
                {group.commands.map((cmd) => (
                  <CommandItem
                    key={cmd.id}
                    command={cmd}
                    canExecuteSingle={canExecuteSingle}
                    batchRunning={batchRunning}
                    onRun={onRun}
                    onRunBatch={onRunBatch}
                    onEdit={onEdit}
                    onRemove={onRemove}
                  />
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
};
