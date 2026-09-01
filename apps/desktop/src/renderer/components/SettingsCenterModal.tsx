import { useCallback, useEffect, useMemo, useState } from "react";
import { App as AntdApp, Modal } from "antd";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { formatErrorMessage } from "../utils/errorMessage";
import {
  type SettingsSection,
  type LocalShellPreference,
  SECTIONS,
  readLocalShellPreference,
  resolvePresetByColors,
  WindowSection,
  TransferSection,
  EditorSection,
  TerminalSection,
  NetworkSection,
  CloudSyncSection,
  RecycleBinSection,
  AgentSection,
  AboutSection
} from "./settings-center";

interface SettingsCenterModalProps {
  open: boolean;
  /**
   * 打开时直接停在哪一节。云同步/回收站迁进设置中心后(D8),连接管理器需要能深链过来——
   * 否则用户只知道"它们在设置里",得自己在 12 个节里翻。省略时沿用上次看的那一节。
   */
  initialSection?: SettingsSection;
  onClose: () => void;
}

export const SettingsCenterModal = ({
  open,
  initialSection,
  onClose
}: SettingsCenterModalProps) => {
  const { message } = AntdApp.useApp();
  const preferences = usePreferencesStore((s) => s.preferences);
  const loading = usePreferencesStore((s) => s.loading);
  const initialize = usePreferencesStore((s) => s.initialize);
  const updatePreferences = usePreferencesStore((s) => s.updatePreferences);

  const [activeSection, setActiveSection] = useState<SettingsSection>(initialSection ?? "window");

  // ─── Local state mirrors (synced from store) ────────────────────────
  const [uploadDefaultDir, setUploadDefaultDir] = useState(preferences.transfer.uploadDefaultDir);
  const [downloadDefaultDir, setDownloadDefaultDir] = useState(
    preferences.transfer.downloadDefaultDir
  );
  const [editorCommand, setEditorCommand] = useState(preferences.remoteEdit.defaultEditorCommand);
  const [editorMode, setEditorMode] = useState<"builtin" | "external">(
    preferences.remoteEdit.editorMode ?? "builtin"
  );
  const [terminalBackgroundColor, setTerminalBackgroundColor] = useState(
    preferences.terminal.backgroundColor
  );
  const [terminalForegroundColor, setTerminalForegroundColor] = useState(
    preferences.terminal.foregroundColor
  );
  const [terminalThemePreset, setTerminalThemePreset] = useState<string>(
    resolvePresetByColors(
      preferences.terminal.backgroundColor,
      preferences.terminal.foregroundColor
    )
  );

  const [appBackgroundImagePath, setAppBackgroundImagePath] = useState(
    preferences.window.backgroundImagePath
  );
  const [localShell, setLocalShell] = useState<LocalShellPreference>(() =>
    readLocalShellPreference(
      preferences.terminal as unknown as Record<string, unknown>,
      window.nextshell.platform
    )
  );

  const [nexttracePath, setNexttracePath] = useState(preferences.traceroute.nexttracePath);

  useEffect(() => {
    if (!open) return;
    void initialize();
  }, [initialize, open]);

  // 弹窗首次打开后一直挂着（保留关闭动画与内部状态），所以 useState 的初值只生效一次——
  // 深链必须在每次打开时重新定位。没给 initialSection 就不动，保留用户上次看的那一节。
  useEffect(() => {
    if (!open || !initialSection) return;
    setActiveSection(initialSection);
  }, [initialSection, open]);

  useEffect(() => {
    if (!open) return;

    setUploadDefaultDir(preferences.transfer.uploadDefaultDir);
    setDownloadDefaultDir(preferences.transfer.downloadDefaultDir);
    setEditorCommand(preferences.remoteEdit.defaultEditorCommand);
    setEditorMode(preferences.remoteEdit.editorMode ?? "builtin");
    setTerminalBackgroundColor(preferences.terminal.backgroundColor);
    setTerminalForegroundColor(preferences.terminal.foregroundColor);
    setTerminalThemePreset(
      resolvePresetByColors(
        preferences.terminal.backgroundColor,
        preferences.terminal.foregroundColor
      )
    );
    setLocalShell(
      readLocalShellPreference(
        preferences.terminal as unknown as Record<string, unknown>,
        window.nextshell.platform
      )
    );
    setAppBackgroundImagePath(preferences.window.backgroundImagePath);
    setNexttracePath(preferences.traceroute.nexttracePath);
  }, [open, preferences]);

  useEffect(() => {
    const next = resolvePresetByColors(terminalBackgroundColor, terminalForegroundColor);
    setTerminalThemePreset((cur) => (cur === next ? cur : next));
  }, [terminalBackgroundColor, terminalForegroundColor]);

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
    async (
      title: string,
      currentPath: string,
      setter: (v: string) => void,
      field: "uploadDefaultDir" | "downloadDefaultDir"
    ) => {
      try {
        const result = await window.nextshell.dialog.openDirectory({
          title,
          defaultPath: currentPath
        });
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

  // ─── Memoized section content ───────────────────────────────────────
  const sectionContent = useMemo(() => {
    switch (activeSection) {
      // 云同步是账号/服务配置，回收站是全局删除历史（含密钥）：两者都是低频全局设置，
      // 不属于「管理这台机器的连接」，所以从连接管理器搬到这里。
      case "cloudSync":
        return <CloudSyncSection />;

      case "recycleBin":
        return <RecycleBinSection />;

      case "window":
        return (
          <WindowSection
            loading={loading}
            appearance={preferences.window.appearance}
            minimizeToTray={preferences.window.minimizeToTray}
            confirmBeforeClose={preferences.window.confirmBeforeClose}
            leftSidebarDefaultCollapsed={preferences.window.leftSidebarDefaultCollapsed}
            bottomWorkbenchDefaultCollapsed={preferences.window.bottomWorkbenchDefaultCollapsed}
            save={save}
          />
        );

      case "transfer":
        return (
          <TransferSection
            loading={loading}
            uploadDefaultDir={uploadDefaultDir}
            downloadDefaultDir={downloadDefaultDir}
            setUploadDefaultDir={setUploadDefaultDir}
            setDownloadDefaultDir={setDownloadDefaultDir}
            save={save}
            pickDirectory={pickDirectory}
          />
        );

      case "editor":
        return (
          <EditorSection
            loading={loading}
            editorMode={editorMode}
            editorCommand={editorCommand}
            setEditorMode={setEditorMode}
            setEditorCommand={setEditorCommand}
            save={save}
            message={message}
          />
        );

      case "terminal":
        return (
          <TerminalSection
            loading={loading}
            terminalBackgroundColor={terminalBackgroundColor}
            terminalForegroundColor={terminalForegroundColor}
            terminalThemePreset={terminalThemePreset}
            terminalFontSize={preferences.terminal.fontSize}
            terminalLineHeight={preferences.terminal.lineHeight}
            terminalFontFamily={preferences.terminal.fontFamily}
            localShell={localShell}
            oscClipboardWrite={preferences.terminal.oscClipboardWrite}
            oscClipboardRead={preferences.terminal.oscClipboardRead}
            oscNotifications={preferences.terminal.oscNotifications}
            oscTitleUpdates={preferences.terminal.oscTitleUpdates}
            hyperlinkConfirm={preferences.terminal.hyperlinkConfirm}
            shellIntegration={preferences.terminal.shellIntegration}
            appBackgroundImagePath={appBackgroundImagePath}
            appBackgroundOpacity={preferences.window.backgroundOpacity}
            terminalWallpaper={preferences.terminal.wallpaper}
            setTerminalBackgroundColor={setTerminalBackgroundColor}
            setTerminalForegroundColor={setTerminalForegroundColor}
            setTerminalThemePreset={setTerminalThemePreset}
            setLocalShell={setLocalShell}
            setAppBackgroundImagePath={setAppBackgroundImagePath}
            save={save}
            message={message}
          />
        );

      case "network":
        return (
          <NetworkSection
            loading={loading}
            nexttracePath={nexttracePath}
            setNexttracePath={setNexttracePath}
            ssh={preferences.ssh}
            traceroute={preferences.traceroute}
            save={save}
            message={message}
          />
        );

      case "agent":
        return <AgentSection />;

      case "about":
        return <AboutSection message={message} />;
    }
  }, [
    activeSection,
    loading,
    preferences,
    uploadDefaultDir,
    downloadDefaultDir,
    editorMode,
    editorCommand,
    terminalBackgroundColor,
    terminalForegroundColor,
    terminalThemePreset,
    localShell,
    appBackgroundImagePath,
    nexttracePath,
    save,
    pickDirectory,
    message
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
        body: { padding: 0, overflow: "hidden" }
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
        <div className="stg-content">{sectionContent}</div>
      </div>
    </Modal>
  );
};
