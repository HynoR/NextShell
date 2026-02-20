import { useCallback, useEffect, useState } from "react";
import {
  Badge,
  Button,
  Divider,
  Drawer,
  Input,
  InputNumber,
  List,
  Modal,
  Select,
  Skeleton,
  Space,
  Spin,
  Switch,
  Tag,
  Typography,
  message
} from "antd";
import { usePreferencesStore } from "../store/usePreferencesStore";
import type { BackupArchiveMeta } from "@nextshell/core";

interface SettingsCenterDrawerProps {
  open: boolean;
  onClose: () => void;
}

const EDITOR_PRESETS: Array<{ label: string; value: string }> = [
  { label: "VS Code", value: "code" },
  { label: "Cursor", value: "cursor" },
  { label: "Sublime", value: "subl" },
  { label: "Vim", value: "vim" },
  { label: "Nano", value: "nano" },
  { label: "Notepad++", value: "notepad++" },
  { label: "TextEdit", value: "open -t" },
  { label: "Xcode", value: "open -a Xcode" }
];
const HEX_COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;
const CUSTOM_THEME_PRESET = "custom";
const TERMINAL_THEME_PRESETS = [
  { label: "默认深蓝", value: "default", backgroundColor: "#0b2740", foregroundColor: "#d8eaff" },
  { label: "Dracula", value: "dracula", backgroundColor: "#282a36", foregroundColor: "#f8f8f2" },
  { label: "Solarized Dark", value: "solarized-dark", backgroundColor: "#002b36", foregroundColor: "#93a1a1" },
  { label: "Gruvbox Dark", value: "gruvbox-dark", backgroundColor: "#282828", foregroundColor: "#ebdbb2" },
  { label: "Nord", value: "nord", backgroundColor: "#2e3440", foregroundColor: "#d8dee9" }
] as const;

const resolvePresetByColors = (backgroundColor: string, foregroundColor: string): string => {
  const normalizedBackground = backgroundColor.trim().toLowerCase();
  const normalizedForeground = foregroundColor.trim().toLowerCase();

  const preset = TERMINAL_THEME_PRESETS.find((item) =>
    item.backgroundColor.toLowerCase() === normalizedBackground &&
    item.foregroundColor.toLowerCase() === normalizedForeground
  );

  return preset?.value ?? CUSTOM_THEME_PRESET;
};

export const SettingsCenterDrawer = ({ open, onClose }: SettingsCenterDrawerProps) => {
  const preferences = usePreferencesStore((state) => state.preferences);
  const loading = usePreferencesStore((state) => state.loading);
  const initialize = usePreferencesStore((state) => state.initialize);
  const updatePreferences = usePreferencesStore((state) => state.updatePreferences);

  const [uploadDefaultDir, setUploadDefaultDir] = useState(preferences.transfer.uploadDefaultDir);
  const [downloadDefaultDir, setDownloadDefaultDir] = useState(preferences.transfer.downloadDefaultDir);
  const [editorCommand, setEditorCommand] = useState(preferences.remoteEdit.defaultEditorCommand);
  const [rememberTemplateParams, setRememberTemplateParams] = useState(
    preferences.commandCenter.rememberTemplateParams
  );
  const [terminalBackgroundColor, setTerminalBackgroundColor] = useState(
    preferences.terminal.backgroundColor
  );
  const [terminalForegroundColor, setTerminalForegroundColor] = useState(
    preferences.terminal.foregroundColor
  );
  const [terminalThemePreset, setTerminalThemePreset] = useState<string>(
    resolvePresetByColors(preferences.terminal.backgroundColor, preferences.terminal.foregroundColor)
  );
  const [terminalFontSize, setTerminalFontSize] = useState<number>(preferences.terminal.fontSize);
  const [terminalLineHeight, setTerminalLineHeight] = useState<number>(preferences.terminal.lineHeight);
  const [saving, setSaving] = useState(false);

  // ─── Backup state ─────────────────────────────────────────────────────────
  const [backupRemotePath, setBackupRemotePath] = useState(preferences.backup.remotePath);
  const [rclonePath, setRclonePath] = useState(preferences.backup.rclonePath);
  const [backupConflictPolicy, setBackupConflictPolicy] = useState<"skip" | "force">(
    preferences.backup.defaultBackupConflictPolicy
  );
  const [restoreConflictPolicy, setRestoreConflictPolicy] = useState<"skip_older" | "force">(
    preferences.backup.defaultRestoreConflictPolicy
  );
  const [backupRememberPassword, setBackupRememberPassword] = useState(preferences.backup.rememberPassword);

  const [pwdStatus, setPwdStatus] = useState<{
    isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean;
  }>({ isSet: false, isUnlocked: false, keytarAvailable: false });
  const [pwdStatusLoading, setPwdStatusLoading] = useState(false);
  const [pwdInput, setPwdInput] = useState("");
  const [pwdConfirm, setPwdConfirm] = useState("");
  const [pwdBusy, setPwdBusy] = useState(false);

  const [backupRunning, setBackupRunning] = useState(false);
  const [archiveList, setArchiveList] = useState<BackupArchiveMeta[]>([]);
  const [archiveListVisible, setArchiveListVisible] = useState(false);
  const [archiveListLoading, setArchiveListLoading] = useState(false);
  const [restoring, setRestoring] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }

    void initialize();
  }, [initialize, open]);

  useEffect(() => {
    if (!open) {
      return;
    }

    setUploadDefaultDir(preferences.transfer.uploadDefaultDir);
    setDownloadDefaultDir(preferences.transfer.downloadDefaultDir);
    setEditorCommand(preferences.remoteEdit.defaultEditorCommand);
    setRememberTemplateParams(preferences.commandCenter.rememberTemplateParams);
    setTerminalBackgroundColor(preferences.terminal.backgroundColor);
    setTerminalForegroundColor(preferences.terminal.foregroundColor);
    setTerminalThemePreset(
      resolvePresetByColors(preferences.terminal.backgroundColor, preferences.terminal.foregroundColor)
    );
    setTerminalFontSize(preferences.terminal.fontSize);
    setTerminalLineHeight(preferences.terminal.lineHeight);
    setBackupRemotePath(preferences.backup.remotePath);
    setRclonePath(preferences.backup.rclonePath);
    setBackupConflictPolicy(preferences.backup.defaultBackupConflictPolicy);
    setRestoreConflictPolicy(preferences.backup.defaultRestoreConflictPolicy);
    setBackupRememberPassword(preferences.backup.rememberPassword);
  }, [open, preferences]);

  useEffect(() => {
    const nextPreset = resolvePresetByColors(terminalBackgroundColor, terminalForegroundColor);
    setTerminalThemePreset((current) => (current === nextPreset ? current : nextPreset));
  }, [terminalBackgroundColor, terminalForegroundColor]);

  // ─── Fetch password status on drawer open ─────────────────────────────────
  useEffect(() => {
    if (!open) return;
    setPwdStatusLoading(true);
    void (async () => {
      try {
        const status = await window.nextshell.backup.passwordStatus();
        setPwdStatus(status);
      } catch {
        // ignore – main process may not support yet
      } finally {
        setPwdStatusLoading(false);
      }
    })();
  }, [open]);

  const refreshPasswordStatus = useCallback(async () => {
    try {
      const status = await window.nextshell.backup.passwordStatus();
      setPwdStatus(status);
    } catch { /* noop */ }
  }, []);

  const handleSetPassword = async (): Promise<void> => {
    if (!pwdInput || pwdInput.length < 6) {
      message.warning("备份密码至少需要 6 个字符。");
      return;
    }
    if (pwdInput !== pwdConfirm) {
      message.warning("两次输入的密码不一致。");
      return;
    }
    setPwdBusy(true);
    try {
      await window.nextshell.backup.setPassword({ password: pwdInput, confirmPassword: pwdConfirm });
      message.success("备份密码已设置");
      setPwdInput("");
      setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(error instanceof Error ? error.message : "设置密码失败");
    } finally {
      setPwdBusy(false);
    }
  };

  const handleUnlockPassword = async (): Promise<void> => {
    if (!pwdInput) {
      message.warning("请输入备份密码。");
      return;
    }
    setPwdBusy(true);
    try {
      await window.nextshell.backup.unlockPassword({ password: pwdInput });
      message.success("备份密码已解锁");
      setPwdInput("");
      setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(error instanceof Error ? error.message : "解锁密码失败");
    } finally {
      setPwdBusy(false);
    }
  };

  const handleClearRemembered = async (): Promise<void> => {
    try {
      await window.nextshell.backup.clearRemembered();
      message.success("已清除钥匙串中的备份密码缓存");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(error instanceof Error ? error.message : "清除缓存失败");
    }
  };

  const handleRunBackup = async (): Promise<void> => {
    setBackupRunning(true);
    try {
      const result = await window.nextshell.backup.run({ conflictPolicy: backupConflictPolicy });
      message.success(result.fileName ? `备份完成: ${result.fileName}` : "备份完成");
    } catch (error) {
      message.error(error instanceof Error ? error.message : "备份失败");
    } finally {
      setBackupRunning(false);
    }
  };

  const handleListArchives = async (): Promise<void> => {
    setArchiveListVisible(true);
    setArchiveListLoading(true);
    try {
      const list = await window.nextshell.backup.list();
      setArchiveList(list);
    } catch (error) {
      message.error(error instanceof Error ? error.message : "获取存档列表失败");
    } finally {
      setArchiveListLoading(false);
    }
  };

  const handleRestore = async (archiveId: string): Promise<void> => {
    Modal.confirm({
      title: "确认还原",
      content: "还原操作会在下次启动时覆盖当前数据库。确定继续？",
      okText: "确认还原",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        setRestoring(archiveId);
        try {
          await window.nextshell.backup.restore({
            archiveId,
            conflictPolicy: restoreConflictPolicy
          });
          message.success("还原文件已准备，重启应用后生效。");
        } catch (error) {
          message.error(error instanceof Error ? error.message : "还原失败");
        } finally {
          setRestoring(null);
        }
      }
    });
  };

  const pickDirectory = async (
    title: string,
    currentPath: string,
    setter: (value: string) => void
  ): Promise<void> => {
    try {
      const result = await window.nextshell.dialog.openDirectory({
        title,
        defaultPath: currentPath
      });

      if (!result.canceled && result.filePath) {
        setter(result.filePath);
      }
    } catch (error) {
      const reason = error instanceof Error ? error.message : "打开目录选择器失败";
      message.error(reason);
    }
  };

  const handleSave = async (): Promise<void> => {
    const normalizedUploadDir = uploadDefaultDir.trim();
    const normalizedDownloadDir = downloadDefaultDir.trim();
    const normalizedEditorCommand = editorCommand.trim();
    const normalizedTerminalBackgroundColor = terminalBackgroundColor.trim();
    const normalizedTerminalForegroundColor = terminalForegroundColor.trim();

    if (!normalizedUploadDir || !normalizedDownloadDir || !normalizedEditorCommand) {
      message.warning("路径和编辑器命令不能为空。");
      return;
    }

    if (!HEX_COLOR_PATTERN.test(normalizedTerminalBackgroundColor)) {
      message.warning("终端背景颜色格式无效，请使用 #RRGGBB。");
      return;
    }

    if (!HEX_COLOR_PATTERN.test(normalizedTerminalForegroundColor)) {
      message.warning("终端文字颜色格式无效，请使用 #RRGGBB。");
      return;
    }

    if (!Number.isInteger(terminalFontSize) || terminalFontSize < 10 || terminalFontSize > 24) {
      message.warning("终端字号需为 10-24 的整数。");
      return;
    }

    if (!Number.isFinite(terminalLineHeight) || terminalLineHeight < 1 || terminalLineHeight > 2) {
      message.warning("终端行距需在 1.0 - 2.0 之间。");
      return;
    }

    setSaving(true);
    try {
      await updatePreferences({
        transfer: {
          uploadDefaultDir: normalizedUploadDir,
          downloadDefaultDir: normalizedDownloadDir
        },
        remoteEdit: {
          defaultEditorCommand: normalizedEditorCommand
        },
        commandCenter: {
          rememberTemplateParams
        },
        terminal: {
          backgroundColor: normalizedTerminalBackgroundColor,
          foregroundColor: normalizedTerminalForegroundColor,
          fontSize: terminalFontSize,
          lineHeight: terminalLineHeight
        },
        backup: {
          remotePath: backupRemotePath.trim(),
          rclonePath: rclonePath.trim(),
          defaultBackupConflictPolicy: backupConflictPolicy,
          defaultRestoreConflictPolicy: restoreConflictPolicy,
          rememberPassword: backupRememberPassword
        }
      });

      message.success("设置已保存");
      onClose();
    } catch (error) {
      const reason = error instanceof Error ? error.message : "保存设置失败";
      message.error(reason);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title="设置中心"
      width={520}
      extra={(
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={saving} onClick={() => void handleSave()}>
            保存
          </Button>
        </Space>
      )}
    >
      <div className="flex flex-col gap-3">
        <Typography.Title level={5}>文件传输</Typography.Title>
        <Typography.Text type="secondary">统一设置上传/下载默认路径。</Typography.Text>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>上传默认目录</Typography.Text>
          <div className="flex gap-2">
            <Input
              style={{ flex: 1 }}
              value={uploadDefaultDir}
              disabled={loading}
              onChange={(event) => setUploadDefaultDir(event.target.value)}
              placeholder="例如 ~/Desktop"
            />
            <Button
              onClick={() =>
                void pickDirectory("选择上传默认目录", uploadDefaultDir, setUploadDefaultDir)
              }
            >
              选择目录
            </Button>
          </div>
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>下载默认目录</Typography.Text>
          <div className="flex gap-2">
            <Input
              style={{ flex: 1 }}
              value={downloadDefaultDir}
              disabled={loading}
              onChange={(event) => setDownloadDefaultDir(event.target.value)}
              placeholder="例如 ~/Downloads"
            />
            <Button
              onClick={() =>
                void pickDirectory("选择下载默认目录", downloadDefaultDir, setDownloadDefaultDir)
              }
            >
              选择目录
            </Button>
          </div>
        </div>

        <Divider />

        <Typography.Title level={5}>远端编辑</Typography.Title>
        <Typography.Text type="secondary">默认编辑器命令用于打开远端编辑文件。</Typography.Text>
        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>默认编辑器命令</Typography.Text>
          <Input
            value={editorCommand}
            disabled={loading}
            onChange={(event) => setEditorCommand(event.target.value)}
            placeholder="例如 code 或 cursor"
          />
          <Space wrap size={[8, 8]} style={{ marginTop: 8 }}>
            {EDITOR_PRESETS.map((preset) => (
              <Button
                key={preset.value}
                size="small"
                type={editorCommand === preset.value ? "primary" : "default"}
                onClick={() => setEditorCommand(preset.value)}
              >
                {preset.label}
              </Button>
            ))}
          </Space>
        </div>

        <Divider />

        <Typography.Title level={5}>命令中心</Typography.Title>
        <Typography.Text type="secondary">是否记住模板命令参数输入。</Typography.Text>
        <div className="settings-switch-row">
          <span>记住模板参数</span>
          <Switch
            checked={rememberTemplateParams}
            disabled={loading}
            onChange={setRememberTemplateParams}
          />
        </div>

        <Divider />

        <Typography.Title level={5}>终端主题</Typography.Title>
        <Typography.Text type="secondary">设置终端背景、文字颜色和排版参数。</Typography.Text>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>主题预设</Typography.Text>
          <Select
            value={terminalThemePreset}
            disabled={loading}
            options={[
              ...TERMINAL_THEME_PRESETS.map((item) => ({ label: item.label, value: item.value })),
              { label: "自定义", value: CUSTOM_THEME_PRESET }
            ]}
            onChange={(value) => {
              setTerminalThemePreset(value);
              const preset = TERMINAL_THEME_PRESETS.find((item) => item.value === value);
              if (!preset) {
                return;
              }
              setTerminalBackgroundColor(preset.backgroundColor);
              setTerminalForegroundColor(preset.foregroundColor);
            }}
          />
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>终端背景颜色</Typography.Text>
          <div className="flex gap-2 items-center">
            <Input
              style={{ flex: 1 }}
              value={terminalBackgroundColor}
              disabled={loading}
              onChange={(event) => setTerminalBackgroundColor(event.target.value)}
              placeholder="#0b2740"
            />
            <input
              className="settings-color-input"
              type="color"
              disabled={loading}
              value={HEX_COLOR_PATTERN.test(terminalBackgroundColor) ? terminalBackgroundColor : "#0b2740"}
              onChange={(event) => setTerminalBackgroundColor(event.target.value)}
            />
          </div>
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>终端文字颜色</Typography.Text>
          <div className="flex gap-2 items-center">
            <Input
              style={{ flex: 1 }}
              value={terminalForegroundColor}
              disabled={loading}
              onChange={(event) => setTerminalForegroundColor(event.target.value)}
              placeholder="#d8eaff"
            />
            <input
              className="settings-color-input"
              type="color"
              disabled={loading}
              value={HEX_COLOR_PATTERN.test(terminalForegroundColor) ? terminalForegroundColor : "#d8eaff"}
              onChange={(event) => setTerminalForegroundColor(event.target.value)}
            />
          </div>
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>终端字号</Typography.Text>
          <InputNumber
            min={10}
            max={24}
            precision={0}
            value={terminalFontSize}
            disabled={loading}
            onChange={(value) => setTerminalFontSize(typeof value === "number" ? value : preferences.terminal.fontSize)}
          />
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>终端行距</Typography.Text>
          <InputNumber
            min={1}
            max={2}
            step={0.05}
            precision={2}
            value={terminalLineHeight}
            disabled={loading}
            onChange={(value) => setTerminalLineHeight(typeof value === "number" ? value : preferences.terminal.lineHeight)}
          />
        </div>

        <Divider />

        <Typography.Title level={5}>云存档</Typography.Title>
        <Typography.Text type="secondary">
          使用 rclone 将加密备份同步到远端存储。备份密码仅用于加密存档，不影响日常使用。
        </Typography.Text>

        {/* Password status indicator */}
        <div className="flex items-center gap-2 mt-2">
          <Typography.Text>备份密码状态: </Typography.Text>
          {pwdStatusLoading ? (
            <Skeleton.Input active size="small" style={{ width: 120 }} />
          ) : pwdStatus.isSet ? (
            pwdStatus.isUnlocked ? (
              <Badge status="success" text="已设置 · 本次已解锁" />
            ) : (
              <Badge status="processing" text="已设置" />
            )
          ) : (
            <Badge status="default" text="未设置" />
          )}
          {pwdStatus.keytarAvailable && (
            <Tag color="blue" style={{ marginLeft: 4 }}>钥匙串可用</Tag>
          )}
        </div>

        {/* Password input section */}
        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>{pwdStatus.isSet ? "输入备份密码" : "设置备份密码"}</Typography.Text>
          <Input.Password
            value={pwdInput}
            onChange={(e) => setPwdInput(e.target.value)}
            placeholder={pwdStatus.isSet ? "输入备份密码以解锁" : "新备份密码（至少 6 个字符）"}
            disabled={pwdBusy}
          />
          {!pwdStatus.isSet && (
            <>
              <Typography.Text>确认密码</Typography.Text>
              <Input.Password
                value={pwdConfirm}
                onChange={(e) => setPwdConfirm(e.target.value)}
                placeholder="再次输入密码"
                disabled={pwdBusy}
              />
            </>
          )}
          <Space style={{ marginTop: 4 }}>
            {pwdStatus.isSet ? (
              <Button
                type="primary"
                loading={pwdBusy}
                disabled={pwdStatus.isUnlocked}
                onClick={() => void handleUnlockPassword()}
              >
                解锁
              </Button>
            ) : (
              <Button
                type="primary"
                loading={pwdBusy}
                onClick={() => void handleSetPassword()}
              >
                设置备份密码
              </Button>
            )}
            {pwdStatus.keytarAvailable && pwdStatus.isSet && (
              <Button onClick={() => void handleClearRemembered()}>
                清除钥匙串缓存
              </Button>
            )}
          </Space>
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>远端路径</Typography.Text>
          <Input
            value={backupRemotePath}
            disabled={loading}
            onChange={(e) => setBackupRemotePath(e.target.value)}
            placeholder="例如 myremote:nextshell-backups"
          />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            对应 rclone 已配置的 remote:path 格式。
          </Typography.Text>
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>rclone 可执行文件路径</Typography.Text>
          <div className="flex gap-2">
            <Input
              value={rclonePath}
              disabled={loading}
              onChange={(e) => setRclonePath(e.target.value)}
              placeholder="留空则自动从 PATH 查找（macOS / Linux）"
            />
            <Button
              onClick={() =>
                void (async () => {
                  try {
                    const result = await window.nextshell.dialog.openFiles({
                      title: "选择 rclone 可执行文件",
                      multi: false
                    });
                    if (!result.canceled && result.filePaths[0]) {
                      setRclonePath(result.filePaths[0]);
                    }
                  } catch {
                    message.error("打开文件选择器失败");
                  }
                })()
              }
            >
              浏览
            </Button>
          </div>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            Windows 用户请手动指定路径，例如 C:\\Program Files\\rclone\\rclone.exe。
            macOS / Linux 留空即可自动检测。
          </Typography.Text>
        </div>

        <div className="flex flex-col gap-2 mt-1.5">
          <Typography.Text>记住密码</Typography.Text>
          <div className="settings-switch-row">
            <span>使用系统钥匙串记住备份密码</span>
            <Switch
              checked={backupRememberPassword}
              disabled={loading || !pwdStatus.keytarAvailable}
              onChange={setBackupRememberPassword}
            />
          </div>
        </div>

        <div className="flex gap-4 mt-1.5">
          <div className="flex flex-col gap-2" style={{ flex: 1 }}>
            <Typography.Text>备份冲突策略</Typography.Text>
            <Select
              value={backupConflictPolicy}
              disabled={loading}
              onChange={setBackupConflictPolicy}
              options={[
                { label: "跳过已存在", value: "skip" },
                { label: "强制覆盖", value: "force" }
              ]}
            />
          </div>
          <div className="flex flex-col gap-2" style={{ flex: 1 }}>
            <Typography.Text>还原冲突策略</Typography.Text>
            <Select
              value={restoreConflictPolicy}
              disabled={loading}
              onChange={setRestoreConflictPolicy}
              options={[
                { label: "跳过较旧存档", value: "skip_older" },
                { label: "强制覆盖", value: "force" }
              ]}
            />
          </div>
        </div>

        {/* Backup / Restore actions */}
        <div className="flex flex-col gap-2 mt-3">
          <Space>
            <Button
              type="primary"
              loading={backupRunning}
              disabled={!pwdStatus.isUnlocked || !backupRemotePath.trim()}
              onClick={() => void handleRunBackup()}
            >
              立即备份
            </Button>
            <Button
              disabled={!pwdStatus.isUnlocked || !backupRemotePath.trim()}
              onClick={() => void handleListArchives()}
            >
              查看存档列表
            </Button>
          </Space>
          {!pwdStatus.isUnlocked && (
            <Typography.Text type="warning" style={{ fontSize: 12 }}>
              请先设置并解锁备份密码后再执行备份/还原操作。
            </Typography.Text>
          )}
          {preferences.backup.lastBackupAt && (
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              上次备份: {new Date(preferences.backup.lastBackupAt).toLocaleString()}
            </Typography.Text>
          )}
        </div>

        {/* Archive list modal */}
        <Modal
          title="远端存档列表"
          open={archiveListVisible}
          onCancel={() => setArchiveListVisible(false)}
          footer={null}
          width={600}
        >
          {archiveListLoading ? (
            <div style={{ textAlign: "center", padding: 24 }}>
              <Spin />
            </div>
          ) : (
            <List
              dataSource={archiveList}
              locale={{ emptyText: "暂无远端存档" }}
              renderItem={(item) => (
                <List.Item
                  actions={[
                    <Button
                      key="restore"
                      size="small"
                      danger
                      loading={restoring === item.id}
                      onClick={() => void handleRestore(item.id)}
                    >
                      还原
                    </Button>
                  ]}
                >
                  <List.Item.Meta
                    title={item.fileName}
                    description={
                      <>
                        <span>{new Date(item.timestamp).toLocaleString()}</span>
                        {" · "}
                        <span>{(item.sizeBytes / 1024).toFixed(1)} KB</span>
                        {" · "}
                        <span>设备: {item.deviceId}</span>
                      </>
                    }
                  />
                </List.Item>
              )}
            />
          )}
        </Modal>
      </div>
    </Drawer>
  );
};
