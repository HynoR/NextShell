import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  App as AntdApp,
  Badge,
  Button,
  Input,
  InputNumber,
  List,
  Modal,
  Radio,
  Select,
  Skeleton,
  Slider,
  Space,
  Spin,
  Switch,
  Tag,
  Typography
} from "antd";
import { usePreferencesStore } from "../store/usePreferencesStore";
import type { BackupArchiveMeta, WindowAppearance } from "@nextshell/core";
import { SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS } from "@nextshell/shared";
import { formatErrorMessage } from "../utils/errorMessage";

interface SettingsCenterModalProps {
  open: boolean;
  onClose: () => void;
}

type SettingsSection =
  | "window"
  | "transfer"
  | "editor"
  | "command"
  | "terminal"
  | "backup";

const SECTIONS: Array<{ key: SettingsSection; label: string; icon: string }> = [
  { key: "window", label: "窗口行为", icon: "ri-window-line" },
  { key: "transfer", label: "文件传输", icon: "ri-upload-cloud-2-line" },
  { key: "editor", label: "远端编辑", icon: "ri-code-s-slash-line" },
  { key: "command", label: "命令中心", icon: "ri-terminal-box-line" },
  { key: "terminal", label: "终端主题", icon: "ri-palette-line" },
  { key: "backup", label: "云存档", icon: "ri-cloud-line" },
];

const EDITOR_PRESETS: Array<{ label: string; value: string }> = [
  { label: "VS Code", value: "code" },
  { label: "Cursor", value: "cursor" },
  { label: "Sublime", value: "subl" },
  { label: "Vim", value: "vim" },
  { label: "Nano", value: "nano" },
  { label: "Notepad++", value: "notepad++" },
  { label: "TextEdit", value: "open -t" },
  { label: "Xcode", value: "open -a Xcode" },
];

const HEX_COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;
const CUSTOM_THEME_PRESET = "custom";
const TERMINAL_THEME_PRESETS = [
  { label: "默认深蓝", value: "default", backgroundColor: "#0b2740", foregroundColor: "#d8eaff" },
  { label: "Dracula", value: "dracula", backgroundColor: "#282a36", foregroundColor: "#f8f8f2" },
  { label: "Solarized Dark", value: "solarized-dark", backgroundColor: "#002b36", foregroundColor: "#93a1a1" },
  { label: "Gruvbox Dark", value: "gruvbox-dark", backgroundColor: "#282828", foregroundColor: "#ebdbb2" },
  { label: "Nord", value: "nord", backgroundColor: "#2e3440", foregroundColor: "#d8dee9" },
] as const;

const resolvePresetByColors = (bg: string, fg: string): string => {
  const nb = bg.trim().toLowerCase();
  const nf = fg.trim().toLowerCase();
  const preset = TERMINAL_THEME_PRESETS.find(
    (p) => p.backgroundColor.toLowerCase() === nb && p.foregroundColor.toLowerCase() === nf
  );
  return preset?.value ?? CUSTOM_THEME_PRESET;
};

const SettingsCard = ({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) => (
  <div className="stg-card">
    <div className="stg-card-header">
      <div className="stg-card-title">{title}</div>
      {description && <div className="stg-card-desc">{description}</div>}
    </div>
    <div className="stg-card-body">{children}</div>
  </div>
);

const SettingsRow = ({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) => (
  <div className="stg-row">
    <div className="stg-row-label">
      <span>{label}</span>
      {hint && <span className="stg-row-hint">{hint}</span>}
    </div>
    <div className="stg-row-control">{children}</div>
  </div>
);

const SettingsSwitchRow = ({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (value: boolean) => void;
}) => (
  <div className="stg-switch-row">
    <div className="stg-switch-label">
      <span>{label}</span>
      {hint && <span className="stg-row-hint">{hint}</span>}
    </div>
    <Switch size="small" checked={checked} disabled={disabled} onChange={onChange} />
  </div>
);

export const SettingsCenterModal = ({ open, onClose }: SettingsCenterModalProps) => {
  const { message, modal } = AntdApp.useApp();
  const preferences = usePreferencesStore((s) => s.preferences);
  const loading = usePreferencesStore((s) => s.loading);
  const initialize = usePreferencesStore((s) => s.initialize);
  const updatePreferences = usePreferencesStore((s) => s.updatePreferences);

  const [activeSection, setActiveSection] = useState<SettingsSection>("window");

  // ─── Local state mirrors (synced from store) ────────────────────────
  const [uploadDefaultDir, setUploadDefaultDir] = useState(preferences.transfer.uploadDefaultDir);
  const [downloadDefaultDir, setDownloadDefaultDir] = useState(preferences.transfer.downloadDefaultDir);
  const [editorCommand, setEditorCommand] = useState(preferences.remoteEdit.defaultEditorCommand);
  const [editorMode, setEditorMode] = useState<"builtin" | "external">(
    preferences.remoteEdit.editorMode ?? "builtin"
  );
  const [terminalBackgroundColor, setTerminalBackgroundColor] = useState(preferences.terminal.backgroundColor);
  const [terminalForegroundColor, setTerminalForegroundColor] = useState(preferences.terminal.foregroundColor);
  const [terminalThemePreset, setTerminalThemePreset] = useState<string>(
    resolvePresetByColors(preferences.terminal.backgroundColor, preferences.terminal.foregroundColor)
  );

  const [appBackgroundImagePath, setAppBackgroundImagePath] = useState(preferences.window.backgroundImagePath);

  // ─── Backup state ───────────────────────────────────────────────────
  const [backupRemotePath, setBackupRemotePath] = useState(preferences.backup.remotePath);
  const [rclonePath, setRclonePath] = useState(preferences.backup.rclonePath);
  const [backupConflictPolicy, setBackupConflictPolicy] = useState<"skip" | "force">(
    preferences.backup.defaultBackupConflictPolicy
  );
  const [restoreConflictPolicy, setRestoreConflictPolicy] = useState<"skip_older" | "force">(
    preferences.backup.defaultRestoreConflictPolicy
  );

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
    if (!open) return;
    void initialize();
  }, [initialize, open]);

  useEffect(() => {
    if (!open) return;

    setUploadDefaultDir(preferences.transfer.uploadDefaultDir);
    setDownloadDefaultDir(preferences.transfer.downloadDefaultDir);
    setEditorCommand(preferences.remoteEdit.defaultEditorCommand);
    setEditorMode(preferences.remoteEdit.editorMode ?? "builtin");
    setTerminalBackgroundColor(preferences.terminal.backgroundColor);
    setTerminalForegroundColor(preferences.terminal.foregroundColor);
    setTerminalThemePreset(
      resolvePresetByColors(preferences.terminal.backgroundColor, preferences.terminal.foregroundColor)
    );
    setAppBackgroundImagePath(preferences.window.backgroundImagePath);
    setBackupRemotePath(preferences.backup.remotePath);
    setRclonePath(preferences.backup.rclonePath);
    setBackupConflictPolicy(preferences.backup.defaultBackupConflictPolicy);
    setRestoreConflictPolicy(preferences.backup.defaultRestoreConflictPolicy);
  }, [open, preferences]);

  useEffect(() => {
    const next = resolvePresetByColors(terminalBackgroundColor, terminalForegroundColor);
    setTerminalThemePreset((cur) => (cur === next ? cur : next));
  }, [terminalBackgroundColor, terminalForegroundColor]);

  useEffect(() => {
    if (!open) return;
    setPwdStatusLoading(true);
    void (async () => {
      try {
        const status = await window.nextshell.backup.passwordStatus();
        setPwdStatus(status);
      } catch { /* ignore */ } finally {
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

  // ─── Immediate-save helpers ─────────────────────────────────────────
  const save = useCallback(
    (patch: Parameters<typeof updatePreferences>[0]) => {
      void updatePreferences(patch).catch((err) => {
        message.error(`保存设置失败：${formatErrorMessage(err, "请稍后重试")}`);
      });
    },
    [updatePreferences, message]
  );

  const pickDirectory = useCallback(
    async (title: string, currentPath: string, setter: (v: string) => void, field: "uploadDefaultDir" | "downloadDefaultDir") => {
      try {
        const result = await window.nextshell.dialog.openDirectory({ title, defaultPath: currentPath });
        if (!result.canceled && result.filePath) {
          setter(result.filePath);
          save({ transfer: { [field]: result.filePath } });
        }
      } catch (error) {
        message.error(`打开目录选择器失败：${formatErrorMessage(error, "请稍后重试")}`);
      }
    },
    [save, message]
  );

  // ─── Backup handlers ───────────────────────────────────────────────
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
      setPwdInput(""); setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`设置密码失败：${formatErrorMessage(error, "请检查输入内容")}`);
    } finally { setPwdBusy(false); }
  };

  const handleUnlockPassword = async (): Promise<void> => {
    if (!pwdInput) { message.warning("请输入备份密码。"); return; }
    setPwdBusy(true);
    try {
      await window.nextshell.backup.unlockPassword({ password: pwdInput });
      message.success("备份密码已解锁");
      setPwdInput(""); setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`解锁密码失败：${formatErrorMessage(error, "请检查密码是否正确")}`);
    } finally { setPwdBusy(false); }
  };

  const handleClearRemembered = async (): Promise<void> => {
    try {
      await window.nextshell.backup.clearRemembered();
      message.success("已清除钥匙串中的备份密码缓存");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`清除缓存失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  };

  const handleRunBackup = async (): Promise<void> => {
    setBackupRunning(true);
    try {
      const result = await window.nextshell.backup.run({ conflictPolicy: backupConflictPolicy });
      message.success(result.fileName ? `备份完成: ${result.fileName}` : "备份完成");
    } catch (error) {
      message.error(`备份失败：${formatErrorMessage(error, "请检查云存档配置")}`);
    } finally { setBackupRunning(false); }
  };

  const handleListArchives = async (): Promise<void> => {
    setArchiveListVisible(true);
    setArchiveListLoading(true);
    try {
      const list = await window.nextshell.backup.list();
      setArchiveList(list);
    } catch (error) {
      message.error(`获取存档列表失败：${formatErrorMessage(error, "请检查云存档配置")}`);
    } finally { setArchiveListLoading(false); }
  };

  const handleRestore = async (archiveId: string): Promise<void> => {
    modal.confirm({
      title: "确认还原",
      content: "还原操作会在下次启动时覆盖当前数据库。确定继续？",
      okText: "确认还原",
      cancelText: "取消",
      okButtonProps: { danger: true },
      onOk: async () => {
        setRestoring(archiveId);
        try {
          await window.nextshell.backup.restore({ archiveId, conflictPolicy: restoreConflictPolicy });
          message.success("还原文件已准备，重启应用后生效。");
        } catch (error) {
          message.error(`还原失败：${formatErrorMessage(error, "请检查云存档配置")}`);
        } finally { setRestoring(null); }
      },
    });
  };

  // ─── Memoized section content ───────────────────────────────────────
  const sectionContent = useMemo(() => {
    switch (activeSection) {
      case "window":
        return <WindowSection
          loading={loading}
          appearance={preferences.window.appearance}
          minimizeToTray={preferences.window.minimizeToTray}
          confirmBeforeClose={preferences.window.confirmBeforeClose}
          save={save}
        />;

      case "transfer":
        return <TransferSection
          loading={loading}
          uploadDefaultDir={uploadDefaultDir}
          downloadDefaultDir={downloadDefaultDir}
          setUploadDefaultDir={setUploadDefaultDir}
          setDownloadDefaultDir={setDownloadDefaultDir}
          save={save}
          pickDirectory={pickDirectory}
        />;

      case "editor":
        return <EditorSection
          loading={loading}
          editorMode={editorMode}
          editorCommand={editorCommand}
          setEditorMode={setEditorMode}
          setEditorCommand={setEditorCommand}
          save={save}
          message={message}
        />;

      case "command":
        return <CommandSection
          loading={loading}
          rememberTemplateParams={preferences.commandCenter.rememberTemplateParams}
          save={save}
        />;

      case "terminal":
        return <TerminalSection
          loading={loading}
          terminalBackgroundColor={terminalBackgroundColor}
          terminalForegroundColor={terminalForegroundColor}
          terminalThemePreset={terminalThemePreset}
          terminalFontSize={preferences.terminal.fontSize}
          terminalLineHeight={preferences.terminal.lineHeight}
          appBackgroundImagePath={appBackgroundImagePath}
          appBackgroundOpacity={preferences.window.backgroundOpacity}
          setTerminalBackgroundColor={setTerminalBackgroundColor}
          setTerminalForegroundColor={setTerminalForegroundColor}
          setTerminalThemePreset={setTerminalThemePreset}
          setAppBackgroundImagePath={setAppBackgroundImagePath}
          save={save}
          message={message}
        />;

      case "backup":
        return <BackupSection
          loading={loading}
          backupRemotePath={backupRemotePath}
          rclonePath={rclonePath}
          backupConflictPolicy={backupConflictPolicy}
          restoreConflictPolicy={restoreConflictPolicy}
          backupRememberPassword={preferences.backup.rememberPassword}
          pwdStatus={pwdStatus}
          pwdStatusLoading={pwdStatusLoading}
          pwdInput={pwdInput}
          pwdConfirm={pwdConfirm}
          pwdBusy={pwdBusy}
          backupRunning={backupRunning}
          archiveList={archiveList}
          archiveListVisible={archiveListVisible}
          archiveListLoading={archiveListLoading}
          restoring={restoring}
          lastBackupAt={preferences.backup.lastBackupAt ?? undefined}
          setBackupRemotePath={setBackupRemotePath}
          setRclonePath={setRclonePath}
          setBackupConflictPolicy={setBackupConflictPolicy}
          setRestoreConflictPolicy={setRestoreConflictPolicy}
          setPwdInput={setPwdInput}
          setPwdConfirm={setPwdConfirm}
          setArchiveListVisible={setArchiveListVisible}
          onSetPassword={() => void handleSetPassword()}
          onUnlockPassword={() => void handleUnlockPassword()}
          onClearRemembered={() => void handleClearRemembered()}
          onRunBackup={() => void handleRunBackup()}
          onListArchives={() => void handleListArchives()}
          onRestore={(id) => void handleRestore(id)}
          save={save}
          message={message}
        />;
    }
  }, [
    activeSection, loading, preferences,
    uploadDefaultDir, downloadDefaultDir, editorMode, editorCommand,
    terminalBackgroundColor, terminalForegroundColor, terminalThemePreset,
    appBackgroundImagePath,
    backupRemotePath, rclonePath, backupConflictPolicy, restoreConflictPolicy,
    pwdStatus, pwdStatusLoading, pwdInput, pwdConfirm, pwdBusy,
    backupRunning, archiveList, archiveListVisible, archiveListLoading, restoring,
    save, pickDirectory, message, modal,
  ]);

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={760}
      style={{ top: 48 }}
      styles={{
        header: { padding: "13px 18px", marginBottom: 0, borderBottom: "1px solid var(--border)" },
        body: { padding: 0, overflow: "hidden" },
      }}
      title={<span className="mgr-modal-title">设置中心</span>}
      destroyOnHidden
    >
      <div className="stg-layout">
        {/* ── Sidebar ───────────────────────────── */}
        <div className="stg-sidebar">
          {SECTIONS.map((sec) => (
            <button
              key={sec.key}
              type="button"
              className={`stg-nav-item${activeSection === sec.key ? " stg-nav-item--active" : ""}`}
              onClick={() => setActiveSection(sec.key)}
            >
              <i className={sec.icon} aria-hidden="true" />
              {sec.label}
            </button>
          ))}
        </div>

        {/* ── Content ──────────────────────────── */}
        <div className="stg-content">
          {sectionContent}
        </div>
      </div>
    </Modal>
  );
};

// ═══════════════════════════════════════════════════════════════════════
// Section components
// ═══════════════════════════════════════════════════════════════════════

const WindowSection = ({
  loading, appearance, minimizeToTray, confirmBeforeClose, save,
}: {
  loading: boolean;
  appearance: WindowAppearance;
  minimizeToTray: boolean;
  confirmBeforeClose: boolean;
  save: (patch: Record<string, unknown>) => void;
}) => (
  <>
    <SettingsCard title="界面风格" description="选择应用的外观主题">
      <SettingsRow label="外观模式">
        <Select<WindowAppearance>
          style={{ width: "100%" }}
          value={appearance}
          disabled={loading}
          onChange={(v) => save({ window: { appearance: v } })}
          options={[
            { label: "跟随系统（默认）", value: "system" },
            { label: "亮色模式", value: "light" },
            { label: "暗色模式", value: "dark" },
          ]}
        />
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="关闭行为" description="控制关闭按钮的行为方式">
      <SettingsSwitchRow
        label="关闭后最小化到托盘"
        checked={minimizeToTray}
        disabled={loading}
        onChange={(v) => save({ window: { minimizeToTray: v } })}
      />
      <SettingsSwitchRow
        label="关闭窗口前确认"
        hint={minimizeToTray ? "启用「最小化到托盘」时自动禁用" : undefined}
        checked={confirmBeforeClose}
        disabled={loading || minimizeToTray}
        onChange={(v) => save({ window: { confirmBeforeClose: v } })}
      />
      {minimizeToTray && (
        <div className="stg-note">
          关闭按钮将隐藏窗口到系统托盘；可从托盘菜单中退出应用。
        </div>
      )}
    </SettingsCard>
  </>
);

const TransferSection = ({
  loading, uploadDefaultDir, downloadDefaultDir,
  setUploadDefaultDir, setDownloadDefaultDir, save, pickDirectory,
}: {
  loading: boolean;
  uploadDefaultDir: string;
  downloadDefaultDir: string;
  setUploadDefaultDir: (v: string) => void;
  setDownloadDefaultDir: (v: string) => void;
  save: (patch: Record<string, unknown>) => void;
  pickDirectory: (title: string, currentPath: string, setter: (v: string) => void, field: "uploadDefaultDir" | "downloadDefaultDir") => Promise<void>;
}) => (
  <SettingsCard title="默认路径" description="统一设置上传/下载默认路径">
    <SettingsRow label="上传默认目录">
      <div className="flex gap-2">
        <Input
          style={{ flex: 1 }}
          value={uploadDefaultDir}
          disabled={loading}
          onChange={(e) => setUploadDefaultDir(e.target.value)}
          onBlur={() => {
            const v = uploadDefaultDir.trim();
            if (v) save({ transfer: { uploadDefaultDir: v } });
          }}
          placeholder="例如 ~/Desktop"
        />
        <Button
          onClick={() => void pickDirectory("选择上传默认目录", uploadDefaultDir, setUploadDefaultDir, "uploadDefaultDir")}
        >
          选择目录
        </Button>
      </div>
    </SettingsRow>
    <SettingsRow label="下载默认目录">
      <div className="flex gap-2">
        <Input
          style={{ flex: 1 }}
          value={downloadDefaultDir}
          disabled={loading}
          onChange={(e) => setDownloadDefaultDir(e.target.value)}
          onBlur={() => {
            const v = downloadDefaultDir.trim();
            if (v) save({ transfer: { downloadDefaultDir: v } });
          }}
          placeholder="例如 ~/Downloads"
        />
        <Button
          onClick={() => void pickDirectory("选择下载默认目录", downloadDefaultDir, setDownloadDefaultDir, "downloadDefaultDir")}
        >
          选择目录
        </Button>
      </div>
    </SettingsRow>
  </SettingsCard>
);

const EditorSection = ({
  loading, editorMode, editorCommand,
  setEditorMode, setEditorCommand, save, message: msg,
}: {
  loading: boolean;
  editorMode: "builtin" | "external";
  editorCommand: string;
  setEditorMode: (v: "builtin" | "external") => void;
  setEditorCommand: (v: string) => void;
  save: (patch: Record<string, unknown>) => void;
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
      <SettingsRow label="默认编辑器命令">
        <div className="flex gap-2">
          <Input
            style={{ flex: 1 }}
            value={editorCommand}
            disabled={loading}
            onChange={(e) => setEditorCommand(e.target.value)}
            onBlur={() => {
              const v = editorCommand.trim();
              if (v) save({ remoteEdit: { defaultEditorCommand: v } });
            }}
            placeholder="例如 code 或 cursor"
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
                    // Wrap in double-quotes if the path contains spaces so that
                    // the command tokeniser can handle it correctly.
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

const CommandSection = ({
  loading, rememberTemplateParams, save,
}: {
  loading: boolean;
  rememberTemplateParams: boolean;
  save: (patch: Record<string, unknown>) => void;
}) => (
  <SettingsCard title="模板参数" description="是否记住模板命令参数输入">
    <SettingsSwitchRow
      label="记住模板参数"
      checked={rememberTemplateParams}
      disabled={loading}
      onChange={(v) => save({ commandCenter: { rememberTemplateParams: v } })}
    />
  </SettingsCard>
);

const TERMINAL_DEBOUNCE_MS = 3000;

const TerminalSection = ({
  loading, terminalBackgroundColor, terminalForegroundColor,
  terminalThemePreset, terminalFontSize, terminalLineHeight,
  appBackgroundImagePath, appBackgroundOpacity,
  setTerminalBackgroundColor, setTerminalForegroundColor,
  setTerminalThemePreset, setAppBackgroundImagePath,
  save, message: msg,
}: {
  loading: boolean;
  terminalBackgroundColor: string;
  terminalForegroundColor: string;
  terminalThemePreset: string;
  terminalFontSize: number;
  terminalLineHeight: number;
  appBackgroundImagePath: string;
  appBackgroundOpacity: number;
  setTerminalBackgroundColor: (v: string) => void;
  setTerminalForegroundColor: (v: string) => void;
  setTerminalThemePreset: (v: string) => void;
  setAppBackgroundImagePath: (v: string) => void;
  save: (patch: Record<string, unknown>) => void;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => {
  const pendingRef = useRef<Record<string, Record<string, unknown>>>({});
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const flushPending = useCallback(() => {
    clearTimeout(timerRef.current);
    timerRef.current = undefined;
    const sections = pendingRef.current;
    if (Object.keys(sections).length > 0) {
      pendingRef.current = {};
      const merged: Record<string, unknown> = {};
      for (const [section, patch] of Object.entries(sections)) {
        merged[section] = patch;
      }
      save(merged);
    }
  }, [save]);

  useEffect(() => {
    return () => {
      clearTimeout(timerRef.current);
      const sections = pendingRef.current;
      if (Object.keys(sections).length > 0) {
        const merged: Record<string, unknown> = {};
        for (const [section, patch] of Object.entries(sections)) {
          merged[section] = patch;
        }
        save(merged);
      }
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const debouncedSave = useCallback((section: string, patch: Record<string, unknown>) => {
    const prev = pendingRef.current[section] ?? {};
    pendingRef.current[section] = { ...prev, ...patch };
    clearTimeout(timerRef.current);
    timerRef.current = setTimeout(flushPending, TERMINAL_DEBOUNCE_MS);
  }, [flushPending]);

  const debouncedSaveTerminal = useCallback((patch: Record<string, unknown>) => {
    debouncedSave("terminal", patch);
  }, [debouncedSave]);

  return (
  <>
    <SettingsCard title="APP 背景" description="设置应用背景图片和透明度（透明度修改后 3 秒生效）">
      <SettingsRow label="背景图片">
        <div className="flex gap-2 items-center">
          <Input
            style={{ flex: 1 }}
            value={appBackgroundImagePath}
            disabled={loading}
            readOnly
            placeholder="未设置（点击右侧按钮选择图片）"
          />
          <Button
            onClick={() =>
              void (async () => {
                try {
                  const result = await window.nextshell.dialog.openFiles({
                    title: "选择 APP 背景图片",
                    filters: [{ name: "图片文件", extensions: SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS }],
                    multi: false
                  });
                  if (!result.canceled && result.filePaths[0]) {
                    setAppBackgroundImagePath(result.filePaths[0]);
                    save({ window: { backgroundImagePath: result.filePaths[0] } });
                  }
                } catch { msg.error("打开文件选择器失败"); }
              })()
            }
          >
            选择图片
          </Button>
          {appBackgroundImagePath && (
            <Button
              danger
              onClick={() => {
                setAppBackgroundImagePath("");
                save({ window: { backgroundImagePath: "" } });
              }}
            >
              清除
            </Button>
          )}
        </div>
        {appBackgroundImagePath && (
          <div
            style={{
              height: 80, borderRadius: 6, overflow: "hidden", marginTop: 8,
              backgroundImage: `url("nextshell-asset://local${appBackgroundImagePath}")`,
              backgroundSize: "cover", backgroundPosition: "center",
              border: "1px solid rgba(255,255,255,0.1)",
            }}
          />
        )}
      </SettingsRow>

      <SettingsRow label="整体透明度" hint="可调范围 30%-80%">
        <div className="flex gap-3 items-center">
          <Slider
            min={30} max={80} step={1}
            disabled={loading || !appBackgroundImagePath}
            style={{ flex: 1, margin: 0 }}
            value={appBackgroundOpacity}
            onChange={(v) => debouncedSave("window", { backgroundOpacity: typeof v === "number" ? v : 60 })}
          />
          <div className="flex items-center gap-1">
            <InputNumber
              min={30} max={80} precision={0}
              disabled={loading || !appBackgroundImagePath}
              value={appBackgroundOpacity}
              onChange={(v) => debouncedSave("window", { backgroundOpacity: typeof v === "number" ? v : 60 })}
            />
            <span>%</span>
          </div>
        </div>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="终端颜色" description="选择终端配色主题或自定义颜色（修改后 3 秒生效）">
      <SettingsRow label="主题预设">
        <Select
          style={{ width: "100%" }}
          value={terminalThemePreset}
          disabled={loading}
          options={[
            ...TERMINAL_THEME_PRESETS.map((p) => ({ label: p.label, value: p.value })),
            { label: "自定义", value: CUSTOM_THEME_PRESET },
          ]}
          onChange={(value) => {
            setTerminalThemePreset(value);
            const preset = TERMINAL_THEME_PRESETS.find((p) => p.value === value);
            if (preset) {
              setTerminalBackgroundColor(preset.backgroundColor);
              setTerminalForegroundColor(preset.foregroundColor);
              debouncedSaveTerminal({
                backgroundColor: preset.backgroundColor,
                foregroundColor: preset.foregroundColor,
              });
            }
          }}
        />
      </SettingsRow>

      <SettingsRow label="终端背景颜色">
        <div className="flex gap-2 items-center">
          <Input
            style={{ flex: 1 }}
            value={terminalBackgroundColor}
            disabled={loading}
            onChange={(e) => setTerminalBackgroundColor(e.target.value)}
            onBlur={() => {
              if (HEX_COLOR_PATTERN.test(terminalBackgroundColor.trim())) {
                debouncedSaveTerminal({ backgroundColor: terminalBackgroundColor.trim() });
              }
            }}
            placeholder="#0b2740"
          />
          <input
            className="settings-color-input"
            type="color"
            disabled={loading}
            value={HEX_COLOR_PATTERN.test(terminalBackgroundColor) ? terminalBackgroundColor : "#0b2740"}
            onChange={(e) => {
              setTerminalBackgroundColor(e.target.value);
              debouncedSaveTerminal({ backgroundColor: e.target.value });
            }}
          />
        </div>
      </SettingsRow>

      <SettingsRow label="终端文字颜色">
        <div className="flex gap-2 items-center">
          <Input
            style={{ flex: 1 }}
            value={terminalForegroundColor}
            disabled={loading}
            onChange={(e) => setTerminalForegroundColor(e.target.value)}
            onBlur={() => {
              if (HEX_COLOR_PATTERN.test(terminalForegroundColor.trim())) {
                debouncedSaveTerminal({ foregroundColor: terminalForegroundColor.trim() });
              }
            }}
            placeholder="#d8eaff"
          />
          <input
            className="settings-color-input"
            type="color"
            disabled={loading}
            value={HEX_COLOR_PATTERN.test(terminalForegroundColor) ? terminalForegroundColor : "#d8eaff"}
            onChange={(e) => {
              setTerminalForegroundColor(e.target.value);
              debouncedSaveTerminal({ foregroundColor: e.target.value });
            }}
          />
        </div>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="终端排版" description="字号和行距设置（修改后 3 秒生效）">
      <SettingsRow label="终端字号">
        <InputNumber
          style={{ width: "100%" }}
          min={10} max={24} precision={0}
          value={terminalFontSize}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && Number.isInteger(v) && v >= 10 && v <= 24) {
              debouncedSaveTerminal({ fontSize: v });
            }
          }}
        />
      </SettingsRow>
      <SettingsRow label="终端行距">
        <InputNumber
          style={{ width: "100%" }}
          min={1} max={2} step={0.05} precision={2}
          value={terminalLineHeight}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && v >= 1 && v <= 2) {
              debouncedSaveTerminal({ lineHeight: v });
            }
          }}
        />
      </SettingsRow>
    </SettingsCard>
  </>
  );
};

const BackupSection = ({
  loading, backupRemotePath, rclonePath,
  backupConflictPolicy, restoreConflictPolicy, backupRememberPassword,
  pwdStatus, pwdStatusLoading, pwdInput, pwdConfirm, pwdBusy,
  backupRunning, archiveList, archiveListVisible, archiveListLoading,
  restoring, lastBackupAt,
  setBackupRemotePath, setRclonePath,
  setBackupConflictPolicy, setRestoreConflictPolicy,
  setPwdInput, setPwdConfirm, setArchiveListVisible,
  onSetPassword, onUnlockPassword, onClearRemembered,
  onRunBackup, onListArchives, onRestore,
  save, message: msg,
}: {
  loading: boolean;
  backupRemotePath: string;
  rclonePath: string;
  backupConflictPolicy: "skip" | "force";
  restoreConflictPolicy: "skip_older" | "force";
  backupRememberPassword: boolean;
  pwdStatus: { isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean };
  pwdStatusLoading: boolean;
  pwdInput: string;
  pwdConfirm: string;
  pwdBusy: boolean;
  backupRunning: boolean;
  archiveList: BackupArchiveMeta[];
  archiveListVisible: boolean;
  archiveListLoading: boolean;
  restoring: string | null;
  lastBackupAt?: string;
  setBackupRemotePath: (v: string) => void;
  setRclonePath: (v: string) => void;
  setBackupConflictPolicy: (v: "skip" | "force") => void;
  setRestoreConflictPolicy: (v: "skip_older" | "force") => void;
  setPwdInput: (v: string) => void;
  setPwdConfirm: (v: string) => void;
  setArchiveListVisible: (v: boolean) => void;
  onSetPassword: () => void;
  onUnlockPassword: () => void;
  onClearRemembered: () => void;
  onRunBackup: () => void;
  onListArchives: () => void;
  onRestore: (id: string) => void;
  save: (patch: Record<string, unknown>) => void;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => (
  <>
    <SettingsCard title="备份密码" description="使用 rclone 将加密备份同步到远端存储">
      <div className="flex items-center gap-2 mb-2">
        <Typography.Text style={{ fontSize: 12 }}>状态: </Typography.Text>
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

      <SettingsRow label={pwdStatus.isSet ? "输入备份密码" : "设置备份密码"}>
        <Input.Password
          value={pwdInput}
          onChange={(e) => setPwdInput(e.target.value)}
          placeholder={pwdStatus.isSet ? "输入备份密码以解锁" : "新备份密码（至少 6 个字符）"}
          disabled={pwdBusy}
        />
        {!pwdStatus.isSet && (
          <>
            <div style={{ marginTop: 8 }}>
              <Typography.Text style={{ fontSize: 12 }}>确认密码</Typography.Text>
            </div>
            <Input.Password
              value={pwdConfirm}
              onChange={(e) => setPwdConfirm(e.target.value)}
              placeholder="再次输入密码"
              disabled={pwdBusy}
              style={{ marginTop: 4 }}
            />
          </>
        )}
        <Space style={{ marginTop: 8 }}>
          {pwdStatus.isSet ? (
            <Button type="primary" loading={pwdBusy} disabled={pwdStatus.isUnlocked} onClick={onUnlockPassword}>
              解锁
            </Button>
          ) : (
            <Button type="primary" loading={pwdBusy} onClick={onSetPassword}>
              设置备份密码
            </Button>
          )}
          {pwdStatus.keytarAvailable && pwdStatus.isSet && (
            <Button onClick={onClearRemembered}>清除钥匙串缓存</Button>
          )}
        </Space>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="远端配置" description="配置 rclone 路径和冲突策略">
      <SettingsRow label="远端路径">
        <Input
          value={backupRemotePath}
          disabled={loading}
          onChange={(e) => setBackupRemotePath(e.target.value)}
          onBlur={() => save({ backup: { remotePath: backupRemotePath.trim() } })}
          placeholder="例如 myremote:nextshell-backups"
        />
        <div className="stg-note">对应 rclone 已配置的 remote:path 格式。</div>
      </SettingsRow>

      <SettingsRow label="rclone 可执行文件路径">
        <div className="flex gap-2">
          <Input
            style={{ flex: 1 }}
            value={rclonePath}
            disabled={loading}
            onChange={(e) => setRclonePath(e.target.value)}
            onBlur={() => save({ backup: { rclonePath: rclonePath.trim() } })}
            placeholder="留空则自动从 PATH 查找"
          />
          <Button
            onClick={() =>
              void (async () => {
                try {
                  const result = await window.nextshell.dialog.openFiles({ title: "选择 rclone 可执行文件", multi: false });
                  if (!result.canceled && result.filePaths[0]) {
                    setRclonePath(result.filePaths[0]);
                    save({ backup: { rclonePath: result.filePaths[0] } });
                  }
                } catch { msg.error("打开文件选择器失败"); }
              })()
            }
          >
            浏览
          </Button>
        </div>
      </SettingsRow>

      <SettingsSwitchRow
        label="使用系统钥匙串记住备份密码"
        checked={backupRememberPassword}
        disabled={loading || !pwdStatus.keytarAvailable}
        onChange={(v) => save({ backup: { rememberPassword: v } })}
      />

      <div className="flex gap-4 mt-2">
        <SettingsRow label="备份冲突策略">
          <Select
            style={{ width: "100%" }}
            value={backupConflictPolicy}
            disabled={loading}
            onChange={(v) => {
              setBackupConflictPolicy(v);
              save({ backup: { defaultBackupConflictPolicy: v } });
            }}
            options={[
              { label: "跳过已存在", value: "skip" },
              { label: "强制覆盖", value: "force" },
            ]}
          />
        </SettingsRow>
        <SettingsRow label="还原冲突策略">
          <Select
            style={{ width: "100%" }}
            value={restoreConflictPolicy}
            disabled={loading}
            onChange={(v) => {
              setRestoreConflictPolicy(v);
              save({ backup: { defaultRestoreConflictPolicy: v } });
            }}
            options={[
              { label: "跳过较旧存档", value: "skip_older" },
              { label: "强制覆盖", value: "force" },
            ]}
          />
        </SettingsRow>
      </div>
    </SettingsCard>

    <SettingsCard title="备份操作">
      <Space>
        <Button
          type="primary"
          loading={backupRunning}
          disabled={!pwdStatus.isUnlocked || !backupRemotePath.trim()}
          onClick={onRunBackup}
        >
          立即备份
        </Button>
        <Button
          disabled={!pwdStatus.isUnlocked || !backupRemotePath.trim()}
          onClick={onListArchives}
        >
          查看存档列表
        </Button>
      </Space>
      {!pwdStatus.isUnlocked && (
        <div className="stg-note" style={{ marginTop: 8, color: "var(--t-warning)" }}>
          请先设置并解锁备份密码后再执行备份/还原操作。
        </div>
      )}
      {lastBackupAt && (
        <div className="stg-note" style={{ marginTop: 4 }}>
          上次备份: {new Date(lastBackupAt).toLocaleString()}
        </div>
      )}

      <Modal
        title="远端存档列表"
        open={archiveListVisible}
        onCancel={() => setArchiveListVisible(false)}
        footer={null}
        width={600}
      >
        {archiveListLoading ? (
          <div style={{ textAlign: "center", padding: 24 }}><Spin /></div>
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
                    onClick={() => onRestore(item.id)}
                  >
                    还原
                  </Button>,
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
    </SettingsCard>
  </>
);
