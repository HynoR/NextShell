import { useEffect, useMemo, useState } from "react";
import { Button, Drawer, Input, Space, Typography } from "antd";
import type { ScopedCommandItem } from "@nextshell/core";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import {
  clearParamsFromStorage,
  extractPlaceholderKeys,
  getCommandStorageKey,
  loadParamsFromStorage,
  saveParamsToStorage,
  substituteTemplate
} from "../../utils/commandTemplate";

export type TemplateExecutionMode = "single" | "batch";

interface TemplateParamDrawerProps {
  command: ScopedCommandItem | null;
  mode: TemplateExecutionMode;
  batchTargetCount: number;
  open: boolean;
  onExecute: (resolved: string, mode: TemplateExecutionMode) => void;
  onClose: () => void;
}

export const TemplateParamDrawer = ({
  command,
  mode,
  batchTargetCount,
  open,
  onExecute,
  onClose
}: TemplateParamDrawerProps) => {
  const rememberParams = usePreferencesStore(
    (s) => s.preferences.commandCenter.rememberTemplateParams
  );

  const [params, setParams] = useState<Record<string, string>>({});

  const keys = useMemo(() => (command ? extractPlaceholderKeys(command.command) : []), [command]);

  useEffect(() => {
    if (!open || !command) return;
    const storageKey = getCommandStorageKey(command);
    const initial = rememberParams ? loadParamsFromStorage(storageKey) : {};
    const next: Record<string, string> = {};
    for (const k of extractPlaceholderKeys(command.command)) {
      next[k] = initial[k] ?? "";
    }
    setParams(next);
  }, [open, command, rememberParams]);

  const handleExecute = () => {
    if (!command) return;
    const resolved = substituteTemplate(command.command, params);
    const storageKey = getCommandStorageKey(command);
    if (rememberParams) {
      saveParamsToStorage(storageKey, params);
    } else {
      clearParamsFromStorage(storageKey);
    }
    onExecute(resolved, mode);
  };

  return (
    <Drawer
      title={command ? `执行：${command.name}` : "参数"}
      open={open}
      onClose={onClose}
      size="default"
      footer={
        <Button type="primary" onClick={handleExecute}>
          执行
        </Button>
      }
    >
      {command && (
        <Space direction="vertical" style={{ width: "100%" }}>
          {keys.length > 0 ? (
            <>
              <Typography.Text type="secondary">
                填写参数后
                {mode === "batch"
                  ? `将对选定的 ${batchTargetCount} 个目标服务器批量执行，`
                  : "执行，"}
                {rememberParams ? "将自动记住本次输入。" : "本次输入不会被记住。"}
              </Typography.Text>
              {keys.map((key) => (
                <div key={key}>
                  <Typography.Text strong>[#{key}]</Typography.Text>
                  <Input
                    value={params[key] ?? ""}
                    onChange={(e) =>
                      setParams((prev) => ({
                        ...prev,
                        [key]: e.target.value
                      }))
                    }
                    placeholder={key}
                  />
                </div>
              ))}
            </>
          ) : (
            <Typography.Text type="secondary">无需参数，点击「执行」直接运行。</Typography.Text>
          )}
        </Space>
      )}
    </Drawer>
  );
};
