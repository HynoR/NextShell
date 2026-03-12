import { App as AntdApp, Button, Input, Radio, Space } from "antd";
import { SettingsCard, SettingsRow } from "./shared-components";
import { EDITOR_PRESETS } from "./constants";
import type { SaveFn } from "./types";

export const EditorSection = ({
  loading, editorMode, editorCommand,
  setEditorMode, setEditorCommand, save, message: msg,
}: {
  loading: boolean;
  editorMode: "builtin" | "external";
  editorCommand: string;
  setEditorMode: (v: "builtin" | "external") => void;
  setEditorCommand: (v: string) => void;
  save: SaveFn;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => (
  <SettingsCard title="编辑器" description="选择编辑模式和默认编辑器命令">
    <SettingsRow label="编辑器模式">
      <Radio.Group
        value={editorMode}
        onChange={(e) => {
          const v = e.target.value as "builtin" | "external";
          setEditorMode(v);
          save({ remoteEdit: { editorMode: v } });
        }}
        disabled={loading}
      >
        <Radio value="builtin">内置编辑器 (Monaco)</Radio>
        <Radio value="external">外部编辑器</Radio>
      </Radio.Group>
    </SettingsRow>
    {editorMode === "external" && (
      <SettingsRow label="默认编辑器命令" hint="留空时自动使用 VISUAL / EDITOR，最后回退系统默认">
        <div className="flex gap-2">
          <Input
            style={{ flex: 1 }}
            value={editorCommand}
            disabled={loading}
            onChange={(e) => setEditorCommand(e.target.value)}
            onBlur={() => {
              const v = editorCommand.trim();
              save({ remoteEdit: { defaultEditorCommand: v } });
            }}
            placeholder="例如 code、cursor，或留空使用系统默认"
          />
          <Button
            onClick={() =>
              void (async () => {
                try {
                  const result = await window.nextshell.dialog.openFiles({
                    title: "选择编辑器可执行文件",
                    multi: false,
                  });
                  if (!result.canceled && result.filePaths[0]) {
                    const filePath = result.filePaths[0];
                    const cmd = filePath.includes(" ") ? `"${filePath}"` : filePath;
                    setEditorCommand(cmd);
                    save({ remoteEdit: { defaultEditorCommand: cmd } });
                  }
                } catch { msg.error("打开文件选择器失败"); }
              })()
            }
          >
            浏览
          </Button>
        </div>
        <Space wrap size={[6, 6]} style={{ marginTop: 8 }}>
          {EDITOR_PRESETS.map((preset) => (
            <Button
              key={preset.value}
              size="small"
              type={editorCommand === preset.value ? "primary" : "default"}
              onClick={() => {
                setEditorCommand(preset.value);
                save({ remoteEdit: { defaultEditorCommand: preset.value } });
              }}
            >
              {preset.label}
            </Button>
          ))}
        </Space>
      </SettingsRow>
    )}
  </SettingsCard>
);
