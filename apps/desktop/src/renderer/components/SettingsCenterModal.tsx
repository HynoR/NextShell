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
  Tooltip,
  Typography
} from "antd";
import { usePreferencesStore } from "../store/usePreferencesStore";
import type { BackupArchiveMeta, WindowAppearance } from "@nextshell/core";
import { SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS } from "@nextshell/shared";
import type { DebugLogEntry, UpdateCheckResult } from "@nextshell/shared";
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
  | "network"
  | "backup"
  | "security"
  | "about";

const SECTIONS: Array<{ key: SettingsSection; label: string; icon: string }> = [
  { key: "window", label: "窗口行为", icon: "ri-window-line" },
  { key: "transfer", label: "文件传输", icon: "ri-upload-cloud-2-line" },
  { key: "editor", label: "远端编辑", icon: "ri-code-s-slash-line" },
  { key: "command", label: "命令中心", icon: "ri-terminal-box-line" },
  { key: "terminal", label: "终端主题", icon: "ri-palette-line" },
  { key: "network", label: "网络工具", icon: "ri-route-line" },
  { key: "backup", label: "云存档", icon: "ri-cloud-line" },
  { key: "security", label: "安全与审计", icon: "ri-shield-keyhole-line" },
  { key: "about", label: "关于", icon: "ri-information-line" },
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
  { label: "默认", value: "default", backgroundColor: "#000000", foregroundColor: "#d8eaff" },
  { label: "Dracula", value: "dracula", backgroundColor: "#282a36", foregroundColor: "#f8f8f2" },
  { label: "Solarized Dark", value: "solarized-dark", backgroundColor: "#002b36", foregroundColor: "#93a1a1" },
  { label: "Gruvbox Dark", value: "gruvbox-dark", backgroundColor: "#282828", foregroundColor: "#ebdbb2" },
  { label: "Nord", value: "nord", backgroundColor: "#2e3440", foregroundColor: "#d8dee9" },
] as const;

const TERMINAL_FONT_OPTIONS = [
  { label: "系统默认", value: "monospace" },
  { label: "JetBrains Mono", value: "JetBrains Mono" },
  { label: "Fira Code", value: "Fira Code" },
  { label: "Cascadia Mono", value: "Cascadia Mono" },
  { label: "Consolas", value: "Consolas" },
  { label: "Menlo", value: "Menlo" },
  { label: "Monaco", value: "Monaco" },
  { label: "SF Mono", value: "SF Mono" },
  { label: "Source Code Pro", value: "Source Code Pro" },
  { label: "Ubuntu Mono", value: "Ubuntu Mono" },
  { label: "DejaVu Sans Mono", value: "DejaVu Sans Mono" },
  { label: "Noto Sans Mono", value: "Noto Sans Mono" },
  { label: "Roboto Mono", value: "Roboto Mono" },
  { label: "Courier New", value: "Courier New" }
];

const SUPPORTED_FONT_EXTENSIONS = ["ttf", "otf", "woff", "woff2", "ttc"];

const getFontFileStem = (filePath: string): string => {
  const normalized = filePath.replace(/\\/g, "/");
  const base = normalized.split("/").pop() ?? "";
  return base.replace(/\.[^.]+$/, "") || base;
};

const appVersion = __APP_VERSION__;
const githubRepo = __GITHUB_REPO__;
const normalizedRepo = githubRepo.trim();
const hasRepo = normalizedRepo.length > 0;

const displayRepo = hasRepo ? normalizedRepo : "owner/repo";
const displayRepoUrl = `https://github.com/${displayRepo}`;
const licenseUrl = `https://github.com/${displayRepo}/blob/main/LICENSE`;

const DEBUG_MAX_ENTRIES = 300;

const resolvePresetByColors = (bg: string, fg: string): string => {
  const nb = bg.trim().toLowerCase();
  const nf = fg.trim().toLowerCase();
  const preset = TERMINAL_THEME_PRESETS.find(
    (p) => p.backgroundColor.toLowerCase() === nb && p.foregroundColor.toLowerCase() === nf
  );
  return preset?.value ?? CUSTOM_THEME_PRESET;
};

const formatTimestamp = (ts: number): string => {
  const d = new Date(ts);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  const ss = String(d.getSeconds()).padStart(2, "0");
  const ms = String(d.getMilliseconds()).padStart(3, "0");
  return `${hh}:${mm}:${ss}.${ms}`;
};

const truncateCommand = (cmd: string, maxLen = 80): string => {
  return cmd.length > maxLen ? `${cmd.slice(0, maxLen)}…` : cmd;
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
  const [terminalFontFamily, setTerminalFontFamily] = useState(preferences.terminal.fontFamily);
  const [terminalCustomFontPath, setTerminalCustomFontPath] = useState(preferences.terminal.customFontPath ?? "");
  const [terminalThemePreset, setTerminalThemePreset] = useState<string>(
    resolvePresetByColors(preferences.terminal.backgroundColor, preferences.terminal.foregroundColor)
  );

  const [appBackgroundImagePath, setAppBackgroundImagePath] = useState(preferences.window.backgroundImagePath);

  // ─── Backup state ───────────────────────────────────────────────────
  const [backupRemotePath, setBackupRemotePath] = useState(preferences.backup.remotePath);
  const [rclonePath, setRclonePath] = useState(preferences.backup.rclonePath);
  const [nexttracePath, setNexttracePath] = useState(preferences.traceroute.nexttracePath);
  const [backupConflictPolicy, setBackupConflictPolicy] = useState<"skip" | "force">(
    preferences.backup.defaultBackupConflictPolicy
  );
  const [restoreConflictPolicy, setRestoreConflictPolicy] = useState<"skip_older" | "force">(
    preferences.backup.defaultRestoreConflictPolicy
  );

  const [auditRetentionDays, setAuditRetentionDays] = useState(preferences.audit.retentionDays);

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
    setTerminalFontFamily(preferences.terminal.fontFamily);
    setTerminalCustomFontPath(preferences.terminal.customFontPath ?? "");
    setTerminalThemePreset(
      resolvePresetByColors(preferences.terminal.backgroundColor, preferences.terminal.foregroundColor)
    );
    setAppBackgroundImagePath(preferences.window.backgroundImagePath);
    setBackupRemotePath(preferences.backup.remotePath);
    setRclonePath(preferences.backup.rclonePath);
    setNexttracePath(preferences.traceroute.nexttracePath);
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
        const status = await window.nextshell.masterPassword.passwordStatus();
        setPwdStatus(status);
      } catch { /* ignore */ } finally {
        setPwdStatusLoading(false);
      }
    })();
  }, [open]);

  const refreshPasswordStatus = useCallback(async () => {
    try {
      const status = await window.nextshell.masterPassword.passwordStatus();
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
      message.warning("主密码至少需要 6 个字符。");
      return;
    }
    if (pwdInput !== pwdConfirm) {
      message.warning("两次输入的密码不一致。");
      return;
    }
    setPwdBusy(true);
    try {
      await window.nextshell.masterPassword.setPassword({ password: pwdInput, confirmPassword: pwdConfirm });
      message.success("主密码已设置");
      setPwdInput(""); setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`设置密码失败：${formatErrorMessage(error, "请检查输入内容")}`);
    } finally { setPwdBusy(false); }
  };

  const handleUnlockPassword = async (): Promise<void> => {
    if (!pwdInput) { message.warning("请输入主密码。"); return; }
    setPwdBusy(true);
    try {
      await window.nextshell.masterPassword.unlockPassword({ password: pwdInput });
      message.success("主密码已解锁");
      setPwdInput(""); setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`解锁密码失败：${formatErrorMessage(error, "请检查密码是否正确")}`);
    } finally { setPwdBusy(false); }
  };

  const handleClearRemembered = async (): Promise<void> => {
    try {
      await window.nextshell.masterPassword.clearRemembered();
      message.success("已清除钥匙串中的主密码缓存");
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
      case "security":
        return <SecuritySection
          pwdStatus={pwdStatus}
          pwdStatusLoading={pwdStatusLoading}
          pwdInput={pwdInput}
          pwdConfirm={pwdConfirm}
          pwdBusy={pwdBusy}
          backupRememberPassword={preferences.backup.rememberPassword}
          loading={loading}
          auditRetentionDays={auditRetentionDays}
          setAuditRetentionDays={setAuditRetentionDays}
          setPwdInput={setPwdInput}
          setPwdConfirm={setPwdConfirm}
          onSetPassword={() => void handleSetPassword()}
          onUnlockPassword={() => void handleUnlockPassword()}
          onClearRemembered={() => void handleClearRemembered()}
          save={save}
        />;
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
          batchMaxConcurrency={preferences.commandCenter.batchMaxConcurrency}
          batchRetryCount={preferences.commandCenter.batchRetryCount}
          save={save}
        />;

      case "terminal":
        return <TerminalSection
          loading={loading}
          terminalBackgroundColor={terminalBackgroundColor}
          terminalForegroundColor={terminalForegroundColor}
          terminalThemePreset={terminalThemePreset}
          terminalFontFamily={terminalFontFamily}
          terminalCustomFontPath={terminalCustomFontPath}
          terminalFontSize={preferences.terminal.fontSize}
          terminalLineHeight={preferences.terminal.lineHeight}
          appBackgroundImagePath={appBackgroundImagePath}
          appBackgroundOpacity={preferences.window.backgroundOpacity}
          setTerminalBackgroundColor={setTerminalBackgroundColor}
          setTerminalForegroundColor={setTerminalForegroundColor}
          setTerminalThemePreset={setTerminalThemePreset}
          setTerminalFontFamily={setTerminalFontFamily}
          setTerminalCustomFontPath={setTerminalCustomFontPath}
          setAppBackgroundImagePath={setAppBackgroundImagePath}
          save={save}
          message={message}
        />;

      case "network":
        return <NetworkSection
          loading={loading}
          nexttracePath={nexttracePath}
          setNexttracePath={setNexttracePath}
          ssh={preferences.ssh}
          traceroute={preferences.traceroute}
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

      case "about":
        return <AboutSection message={message} />;
    }
  }, [
    activeSection, loading, preferences,
    uploadDefaultDir, downloadDefaultDir, editorMode, editorCommand,
    terminalBackgroundColor, terminalForegroundColor, terminalThemePreset,
    appBackgroundImagePath, nexttracePath,
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
  loading, rememberTemplateParams, batchMaxConcurrency, batchRetryCount, save,
}: {
  loading: boolean;
  rememberTemplateParams: boolean;
  batchMaxConcurrency: number;
  batchRetryCount: number;
  save: (patch: Record<string, unknown>) => void;
}) => (
  <SettingsCard title="命令中心" description="模板参数与批量执行默认参数">
    <SettingsSwitchRow
      label="记住模板参数"
      checked={rememberTemplateParams}
      disabled={loading}
      onChange={(v) => save({ commandCenter: { rememberTemplateParams: v } })}
    />
    <SettingsRow label="批量并发" hint="命令库批量执行时每轮并发数量（1-50）">
      <InputNumber
        min={1}
        max={50}
        precision={0}
        value={batchMaxConcurrency}
        disabled={loading}
        onChange={(value) => {
          if (value === null) return;
          const next = Math.min(50, Math.max(1, Number(value) || 1));
          save({ commandCenter: { batchMaxConcurrency: next } });
        }}
      />
    </SettingsRow>
    <SettingsRow label="批量重试" hint="命令库批量执行失败时的额外重试次数（0-5）">
      <InputNumber
        min={0}
        max={5}
        precision={0}
        value={batchRetryCount}
        disabled={loading}
        onChange={(value) => {
          if (value === null) return;
          const next = Math.min(5, Math.max(0, Number(value) || 0));
          save({ commandCenter: { batchRetryCount: next } });
        }}
      />
    </SettingsRow>
  </SettingsCard>
);

const AboutSection = ({
  message: msg,
}: {
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => {
  const [checking, setChecking] = useState(false);
  const [result, setResult] = useState<UpdateCheckResult | null>(null);
  const releaseUrl = result?.hasUpdate ? result.releaseUrl : null;

  const [debugEnabled, setDebugEnabled] = useState(false);
  const [debugLogs, setDebugLogs] = useState<DebugLogEntry[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const logBoxRef = useRef<HTMLDivElement>(null);
  const autoScrollRef = useRef(autoScroll);
  autoScrollRef.current = autoScroll;

  const handleOpenLink = useCallback(async (url: string) => {
    const openResult = await window.nextshell.dialog.openPath({ path: url, revealInFolder: false });
    if (!openResult.ok) {
      void msg.error(`打开链接失败：${formatErrorMessage(openResult.error, "请稍后重试")}`);
    }
  }, [msg]);

  const handleCheckUpdate = useCallback(async () => {
    setChecking(true);
    try {
      const res = await window.nextshell.about.checkUpdate();
      setResult(res);
      if (res.error) {
        void msg.warning(`检查更新失败：${formatErrorMessage(res.error, "请稍后重试")}`);
      } else if (res.hasUpdate) {
        void msg.success(`发现新版本 ${res.latestVersion}`);
      } else {
        void msg.info("当前已是最新版本");
      }
    } catch {
      void msg.error("检查更新失败");
    } finally {
      setChecking(false);
    }
  }, [msg]);

  const handleToggleDebug = useCallback(async () => {
    if (debugEnabled) {
      await window.nextshell.debug.disableLog();
      setDebugEnabled(false);
    } else {
      setDebugLogs([]);
      await window.nextshell.debug.enableLog();
      setDebugEnabled(true);
    }
  }, [debugEnabled]);

  const handleClearLogs = useCallback(() => {
    setDebugLogs([]);
  }, []);

  useEffect(() => {
    if (!debugEnabled) return;

    const unsub = window.nextshell.debug.onLogEvent((entry) => {
      setDebugLogs((prev) => {
        const next = prev.length >= DEBUG_MAX_ENTRIES
          ? [...prev.slice(-(DEBUG_MAX_ENTRIES - 1)), entry]
          : [...prev, entry];
        return next;
      });
    });

    return () => {
      unsub();
    };
  }, [debugEnabled]);

  useEffect(() => {
    if (autoScrollRef.current && logBoxRef.current) {
      logBoxRef.current.scrollTop = logBoxRef.current.scrollHeight;
    }
  }, [debugLogs]);

  const handleLogScroll = useCallback(() => {
    const el = logBoxRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setAutoScroll(atBottom);
  }, []);

  return (
    <div className="about-pane">
      <div className="about-header">
        <div className="about-logo">
          <i className="ri-terminal-box-fill" aria-hidden="true" />
          <span className="about-app-name">NextShell</span>
          <Tag color="blue">v{appVersion}</Tag>
        </div>
        <p className="about-desc">现代化的 SSH 终端管理工具</p>
      </div>

      <div className="about-actions">
        <Tooltip title={!hasRepo ? "未配置 GitHub 仓库，无法检查更新" : undefined}>
          <Button
            type="primary"
            icon={<i className="ri-refresh-line" aria-hidden="true" />}
            loading={checking}
            disabled={!hasRepo}
            onClick={() => void handleCheckUpdate()}
          >
            检查更新
          </Button>
        </Tooltip>
        <Tooltip title={debugEnabled ? "关闭后台 Shell 日志监听" : "开启后台 Shell 日志监听，实时查看数据采集命令执行情况"}>
          <Button
            type={debugEnabled ? "default" : "dashed"}
            danger={debugEnabled}
            icon={<i className={debugEnabled ? "ri-stop-circle-line" : "ri-bug-line"} aria-hidden="true" />}
            onClick={() => void handleToggleDebug()}
          >
            {debugEnabled ? "停止诊断日志" : "诊断日志"}
          </Button>
        </Tooltip>
      </div>

      {debugEnabled && (
        <div className="about-debug-section">
          <div className="about-debug-header">
            <span className="about-debug-title">
              <i className="ri-terminal-line" aria-hidden="true" />
              后台命令执行日志
              <Tag color="processing" style={{ marginLeft: 8 }}>实时</Tag>
              <span className="about-debug-count">{debugLogs.length} 条</span>
            </span>
            <div className="about-debug-actions">
              <Tooltip title={autoScroll ? "已开启自动滚动" : "点击启用自动滚动"}>
                <button
                  className={`about-debug-icon-btn ${autoScroll ? "active" : ""}`}
                  onClick={() => {
                    setAutoScroll(true);
                    if (logBoxRef.current) {
                      logBoxRef.current.scrollTop = logBoxRef.current.scrollHeight;
                    }
                  }}
                  aria-label="自动滚动"
                >
                  <i className="ri-arrow-down-double-line" aria-hidden="true" />
                </button>
              </Tooltip>
              <Tooltip title="清空日志">
                <button
                  className="about-debug-icon-btn"
                  onClick={handleClearLogs}
                  aria-label="清空日志"
                >
                  <i className="ri-delete-bin-line" aria-hidden="true" />
                </button>
              </Tooltip>
            </div>
          </div>
          <div
            className="about-debug-log-box"
            ref={logBoxRef}
            onScroll={handleLogScroll}
          >
            {debugLogs.length === 0 ? (
              <div className="about-debug-empty">
                等待后台命令执行...
              </div>
            ) : (
              debugLogs.map((entry) => (
                <div key={entry.id} className={`about-debug-entry ${entry.ok ? "ok" : "fail"}`}>
                  <div className="about-debug-entry-header">
                    <span className="about-debug-time">{formatTimestamp(entry.timestamp)}</span>
                    <span className={`about-debug-badge ${entry.ok ? "ok" : "fail"}`}>
                      {entry.ok ? "OK" : `ERR ${entry.exitCode}`}
                    </span>
                    <span className="about-debug-duration">{entry.durationMs}ms</span>
                    <span className="about-debug-conn" title={entry.connectionId}>
                      {entry.connectionId.slice(0, 8)}
                    </span>
                  </div>
                  <div className="about-debug-cmd" title={entry.command}>
                    <i className="ri-terminal-line" aria-hidden="true" />
                    {truncateCommand(entry.command)}
                  </div>
                  {entry.error ? (
                    <div className="about-debug-error">{entry.error}</div>
                  ) : entry.stdout ? (
                    <pre className="about-debug-stdout">{entry.stdout}</pre>
                  ) : null}
                </div>
              ))
            )}
          </div>
        </div>
      )}


      <div className="about-section">
        <div className="about-row">
          <span className="about-label">
            <i className="ri-github-fill" aria-hidden="true" /> GitHub 仓库
          </span>
          {hasRepo ? (
            <a
              className="about-link"
              href={displayRepoUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => {
                e.preventDefault();
                void handleOpenLink(displayRepoUrl);
              }}
            >
              {displayRepo}
              <i className="ri-external-link-line" aria-hidden="true" />
            </a>
          ) : (
            <span className="about-value about-placeholder">{displayRepo}</span>
          )}
        </div>

        <div className="about-row">
          <span className="about-label">
            <i className="ri-information-line" aria-hidden="true" /> 当前版本
          </span>
          <span className="about-value">v{appVersion}</span>
        </div>

        <div className="about-row">
          <span className="about-label">
            <i className="ri-book-2-line" aria-hidden="true" /> License
          </span>
          {hasRepo ? (
            <a
              className="about-link"
              href={licenseUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => {
                e.preventDefault();
                void handleOpenLink(licenseUrl);
              }}
            >
              GNU General Public License v3.0
              <i className="ri-external-link-line" aria-hidden="true" />
            </a>
          ) : (
            <span className="about-value about-placeholder">GNU General Public License v3.0</span>
          )}
        </div>

        <div className="about-row">
          <span className="about-label">
            <i className="ri-download-cloud-line" aria-hidden="true" /> 最新版本
          </span>
          {result?.latestVersion ? (
            <span className="about-value">
              {result.latestVersion}
              {result.hasUpdate ? (
                <Tag color="green" style={{ marginLeft: 8 }}>有更新</Tag>
              ) : (
                <Tag color="default" style={{ marginLeft: 8 }}>已是最新</Tag>
              )}
            </span>
          ) : (
            <span className="about-value about-placeholder">—</span>
          )}
        </div>

        {releaseUrl ? (
          <div className="about-row">
            <span className="about-label">
              <i className="ri-links-line" aria-hidden="true" /> 下载地址
            </span>
            <a
              className="about-link"
              href={releaseUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => {
                e.preventDefault();
                void handleOpenLink(releaseUrl);
              }}
            >
              前往 Release 页面
              <i className="ri-external-link-line" aria-hidden="true" />
            </a>
          </div>
        ) : null}
      </div>
    </div>
  );
};

const TERMINAL_DEBOUNCE_MS = 3000;

const TerminalSection = ({
  loading, terminalBackgroundColor, terminalForegroundColor,
  terminalThemePreset, terminalFontFamily, terminalCustomFontPath,
  terminalFontSize, terminalLineHeight,
  appBackgroundImagePath, appBackgroundOpacity,
  setTerminalBackgroundColor, setTerminalForegroundColor,
  setTerminalThemePreset, setTerminalFontFamily, setTerminalCustomFontPath,
  setAppBackgroundImagePath, save, message: msg,
}: {
  loading: boolean;
  terminalBackgroundColor: string;
  terminalForegroundColor: string;
  terminalThemePreset: string;
  terminalFontFamily: string;
  terminalCustomFontPath: string;
  terminalFontSize: number;
  terminalLineHeight: number;
  appBackgroundImagePath: string;
  appBackgroundOpacity: number;
  setTerminalBackgroundColor: (v: string) => void;
  setTerminalForegroundColor: (v: string) => void;
  setTerminalThemePreset: (v: string) => void;
  setTerminalFontFamily: (v: string) => void;
  setTerminalCustomFontPath: (v: string) => void;
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
            placeholder="#000000"
          />
          <input
            className="settings-color-input"
            type="color"
            disabled={loading}
            value={HEX_COLOR_PATTERN.test(terminalBackgroundColor) ? terminalBackgroundColor : "#000000"}
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

    <SettingsCard title="终端排版" description="字体、字号和行距设置（修改后 3 秒生效）">
      <SettingsRow label="终端字体" hint="选择一个字体">
        <div className="flex gap-2 items-center">
          <Select
            style={{ flex: 1 }}
            value={terminalFontFamily}
            disabled={loading}
            placeholder="选择字体"
            options={TERMINAL_FONT_OPTIONS}
            onChange={(value) => {
              const next = String(value);
              setTerminalFontFamily(next);
              debouncedSaveTerminal({ fontFamily: next });
            }}
          />
          <Button
            onClick={() =>
              void (async () => {
                try {
                  const result = await window.nextshell.dialog.openFiles({
                    title: "选择字体文件",
                    filters: [{ name: "字体文件", extensions: SUPPORTED_FONT_EXTENSIONS }],
                    multi: false
                  });
                  if (!result.canceled && result.filePaths[0]) {
                    setTerminalCustomFontPath(result.filePaths[0]);
                    save({ terminal: { customFontPath: result.filePaths[0] } });
                  }
                } catch { msg.error("打开文件选择器失败"); }
              })()
            }
          >
            ...
          </Button>
          {terminalCustomFontPath ? (
            <Button
              danger
              onClick={() => {
                setTerminalCustomFontPath("");
                save({ terminal: { customFontPath: "" } });
              }}
            >
              删除
            </Button>
          ) : null}
        </div>
        {terminalCustomFontPath ? (
          <div className="stg-note">
            自定义字体：{getFontFileStem(terminalCustomFontPath)}
          </div>
        ) : null}
      </SettingsRow>
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

const NetworkSection = ({
  loading, nexttracePath, setNexttracePath, ssh, traceroute, save, message: msg,
}: {
  loading: boolean;
  nexttracePath: string;
  setNexttracePath: (v: string) => void;
  ssh: import("@nextshell/core").AppPreferences["ssh"];
  traceroute: import("@nextshell/core").AppPreferences["traceroute"];
  save: (patch: Record<string, unknown>) => void;
  message: ReturnType<typeof AntdApp.useApp>["message"];
}) => (
  <>
    <SettingsCard title="路由追踪工具" description="配置 nexttrace 可执行文件路径">
      <SettingsRow label="nexttrace 可执行文件路径">
        <div className="flex gap-2">
          <Input
            style={{ flex: 1 }}
            value={nexttracePath}
            disabled={loading}
            onChange={(e) => setNexttracePath(e.target.value)}
            onBlur={() => save({ traceroute: { nexttracePath: nexttracePath.trim() } })}
            placeholder="留空则自动从 PATH 查找"
          />
          <Button
            onClick={() =>
              void (async () => {
                try {
                  const result = await window.nextshell.dialog.openFiles({ title: "选择 nexttrace 可执行文件", multi: false });
                  if (!result.canceled && result.filePaths[0]) {
                    setNexttracePath(result.filePaths[0]);
                    save({ traceroute: { nexttracePath: result.filePaths[0] } });
                  }
                } catch { msg.error("打开文件选择器失败"); }
              })()
            }
          >
            浏览
          </Button>
        </div>
      </SettingsRow>
      <div className="stg-note">
        尚未安装？前往{" "}
        <Typography.Link
          href="https://github.com/nxtrace/NTrace-core"
          target="_blank"
          style={{ fontSize: "inherit" }}
        >
          github.com/nxtrace/NTrace-core
        </Typography.Link>
        {" "}下载安装。
      </div>
    </SettingsCard>

    <SettingsCard title="SSH Keepalive" description="发送空包保持 SSH 连接稳定">
      <SettingsSwitchRow
        label="启用 Keepalive"
        hint="对所有连接生效（可在连接管理器中单独覆盖）"
        checked={ssh.keepAliveEnabled}
        disabled={loading}
        onChange={(v) => save({ ssh: { keepAliveEnabled: v } })}
      />
      <SettingsRow label="保活间隔（秒）" hint="范围 5–600">
        <InputNumber
          style={{ width: "100%" }}
          min={5} max={600} precision={0}
          value={ssh.keepAliveIntervalSec}
          disabled={loading || !ssh.keepAliveEnabled}
          onChange={(v) => {
            if (typeof v === "number" && Number.isInteger(v)) {
              save({ ssh: { keepAliveIntervalSec: v } });
            }
          }}
        />
      </SettingsRow>
      <div className="stg-note">修改后新连接生效，已连接会话需重连。</div>
    </SettingsCard>

    <SettingsCard title="探测参数" description="下次点击「开始追踪」时生效">
      <SettingsRow label="探测协议">
        <Radio.Group
          value={traceroute.protocol}
          disabled={loading}
          onChange={(e) => save({ traceroute: { protocol: e.target.value as string } })}
        >
          <Radio value="icmp">ICMP（默认）</Radio>
          <Radio value="tcp">TCP SYN</Radio>
          <Radio value="udp">UDP</Radio>
        </Radio.Group>
      </SettingsRow>

      {(traceroute.protocol === "tcp" || traceroute.protocol === "udp") && (
        <SettingsRow
          label="目标端口"
          hint={traceroute.protocol === "tcp" ? "默认 80" : "默认 33494"}
        >
          <InputNumber
            style={{ width: "100%" }}
            min={0} max={65535} precision={0}
            value={traceroute.port}
            disabled={loading}
            placeholder="0 = 使用协议默认值"
            onChange={(v) => save({ traceroute: { port: typeof v === "number" ? v : 0 } })}
          />
        </SettingsRow>
      )}

      <SettingsRow label="IP 版本">
        <Select
          style={{ width: "100%" }}
          value={traceroute.ipVersion}
          disabled={loading}
          onChange={(v) => save({ traceroute: { ipVersion: v } })}
          options={[
            { label: "自动", value: "auto" },
            { label: "仅 IPv4", value: "ipv4" },
            { label: "仅 IPv6", value: "ipv6" },
          ]}
        />
      </SettingsRow>

      <SettingsRow label="每跳探测次数" hint="默认 3，范围 1–10">
        <InputNumber
          style={{ width: "100%" }}
          min={1} max={10} precision={0}
          value={traceroute.queries}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && v >= 1 && v <= 10) {
              save({ traceroute: { queries: v } });
            }
          }}
        />
      </SettingsRow>

      <SettingsRow label="最大跳数（TTL）" hint="默认 30">
        <InputNumber
          style={{ width: "100%" }}
          min={1} max={64} precision={0}
          value={traceroute.maxHops}
          disabled={loading}
          onChange={(v) => {
            if (typeof v === "number" && v >= 1 && v <= 64) {
              save({ traceroute: { maxHops: v } });
            }
          }}
        />
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="数据来源与显示" description="IP 归属地查询、反向解析等选项">
      <SettingsRow label="IP 地理数据来源">
        <Select
          style={{ width: "100%" }}
          value={traceroute.dataProvider}
          disabled={loading}
          onChange={(v) => save({ traceroute: { dataProvider: v } })}
          options={[
            { label: "LeoMoeAPI（默认）", value: "LeoMoeAPI" },
            { label: "IP-API.com", value: "ip-api.com" },
            { label: "IPInfo", value: "IPInfo" },
            { label: "IPInsight", value: "IPInsight" },
            { label: "IP.SB", value: "IP.SB" },
            { label: "禁用 GeoIP", value: "disable-geoip" },
          ]}
        />
      </SettingsRow>

      <SettingsRow label="PoW 服务商" hint="国内用户建议选 sakura">
        <Select
          style={{ width: "100%" }}
          value={traceroute.powProvider}
          disabled={loading}
          onChange={(v) => save({ traceroute: { powProvider: v } })}
          options={[
            { label: "api.nxtrace.org（默认）", value: "api.nxtrace.org" },
            { label: "sakura（国内推荐）", value: "sakura" },
          ]}
        />
      </SettingsRow>

      <SettingsRow label="界面语言">
        <Radio.Group
          value={traceroute.language}
          disabled={loading}
          onChange={(e) => save({ traceroute: { language: e.target.value as string } })}
        >
          <Radio value="cn">中文</Radio>
          <Radio value="en">English</Radio>
        </Radio.Group>
      </SettingsRow>

      <SettingsSwitchRow
        label="禁用反向 DNS 解析"
        hint="启用后不解析每跳的 PTR 记录，追踪速度更快"
        checked={traceroute.noRdns}
        disabled={loading}
        onChange={(v) => save({ traceroute: { noRdns: v } })}
      />
    </SettingsCard>
  </>
);

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
    <SettingsCard title="主密码" description="用于云存档备份、导出加密默认填充和连接密码查看授权">
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

      <SettingsRow label={pwdStatus.isSet ? "输入主密码" : "设置主密码"}>
        <Input.Password
          value={pwdInput}
          onChange={(e) => setPwdInput(e.target.value)}
          placeholder={pwdStatus.isSet ? "输入主密码以解锁" : "新主密码（至少 6 个字符）"}
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
              设置主密码
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
        label="使用系统钥匙串记住主密码"
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
          请先设置并解锁主密码后再执行备份/还原操作。
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

const SecuritySection = ({
  pwdStatus, pwdStatusLoading, pwdInput, pwdConfirm, pwdBusy,
  backupRememberPassword, loading,
  auditRetentionDays, setAuditRetentionDays,
  setPwdInput, setPwdConfirm,
  onSetPassword, onUnlockPassword, onClearRemembered,
  save,
}: {
  pwdStatus: { isSet: boolean; isUnlocked: boolean; keytarAvailable: boolean };
  pwdStatusLoading: boolean;
  pwdInput: string;
  pwdConfirm: string;
  pwdBusy: boolean;
  backupRememberPassword: boolean;
  loading: boolean;
  auditRetentionDays: number;
  setAuditRetentionDays: (v: number) => void;
  setPwdInput: (v: string) => void;
  setPwdConfirm: (v: string) => void;
  onSetPassword: () => void;
  onUnlockPassword: () => void;
  onClearRemembered: () => void;
  save: (patch: Record<string, unknown>) => void;
}) => (
  <>
  <SettingsCard title="主密码" description="用于云同步备份、导出加密默认填充和连接密码查看授权">
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

    <SettingsRow label={pwdStatus.isSet ? "输入主密码" : "设置主密码"}>
      <Input.Password
        value={pwdInput}
        onChange={(e) => setPwdInput(e.target.value)}
        placeholder={pwdStatus.isSet ? "输入主密码以解锁" : "新主密码（至少 6 个字符）"}
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
            设置主密码
          </Button>
        )}
        {pwdStatus.keytarAvailable && pwdStatus.isSet && (
          <Button onClick={onClearRemembered}>清除钥匙串缓存</Button>
        )}
      </Space>
    </SettingsRow>

    <SettingsSwitchRow
      label="使用系统钥匙串记住主密码"
      checked={backupRememberPassword}
      disabled={loading || !pwdStatus.keytarAvailable}
      onChange={(v) => save({ backup: { rememberPassword: v } })}
    />
  </SettingsCard>

  <SettingsCard title="审计日志" description="设置操作日志的自动清理策略">
    <SettingsRow label="日志保留天数" hint="设为 0 表示永不清理">
      <InputNumber
        min={0} max={365} precision={0}
        value={auditRetentionDays}
        disabled={loading}
        onChange={(v) => {
          if (typeof v === "number" && v >= 0 && v <= 365) {
            setAuditRetentionDays(v);
            save({ audit: { retentionDays: v } });
          }
        }}
        addonAfter="天"
      />
    </SettingsRow>
    <div className="stg-note">
      超过保留天数的审计日志将在应用启动时自动清理。审计日志不包含在云同步备份中。
    </div>
  </SettingsCard>
  </>
);
