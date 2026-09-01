import { useCallback, useEffect, useMemo, useState } from "react";
import { App as AntdApp, Modal } from "antd";
import { usePreferencesStore } from "../store/usePreferencesStore";
import type { BackupArchiveMeta } from "@nextshell/core";
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
  CommandSection,
  TerminalSection,
  NetworkSection,
  BackupSection,
  CloudSyncSection,
  RecycleBinSection,
  SecuritySection,
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

export const SettingsCenterModal = ({ open, initialSection, onClose }: SettingsCenterModalProps) => {
  const { message, modal } = AntdApp.useApp();
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

  const [pwdStatus, setPwdStatus] = useState<{
    isSet: boolean;
    isUnlocked: boolean;
    canRememberPassword: boolean;
  }>({ isSet: false, isUnlocked: false, canRememberPassword: false });
  const [pwdStatusLoading, setPwdStatusLoading] = useState(false);
  const [pwdStatusKnown, setPwdStatusKnown] = useState(false);
  const [pwdInput, setPwdInput] = useState("");
  const [pwdConfirm, setPwdConfirm] = useState("");
  const [pwdBusy, setPwdBusy] = useState(false);
  const [changeOldPwd, setChangeOldPwd] = useState("");
  const [changeNewPwd, setChangeNewPwd] = useState("");
  const [changeConfirmPwd, setChangeConfirmPwd] = useState("");
  const [changeAckRisk, setChangeAckRisk] = useState(false);
  const [changeBusy, setChangeBusy] = useState(false);

  const [backupRunning, setBackupRunning] = useState(false);
  const [archiveList, setArchiveList] = useState<BackupArchiveMeta[]>([]);
  const [archiveListVisible, setArchiveListVisible] = useState(false);
  const [archiveListLoading, setArchiveListLoading] = useState(false);
  const [restoring, setRestoring] = useState<string | null>(null);

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
    setBackupRemotePath(preferences.backup.remotePath);
    setRclonePath(preferences.backup.rclonePath);
    setNexttracePath(preferences.traceroute.nexttracePath);
    setBackupConflictPolicy(preferences.backup.defaultBackupConflictPolicy);
    setRestoreConflictPolicy(preferences.backup.defaultRestoreConflictPolicy);
    setChangeOldPwd("");
    setChangeNewPwd("");
    setChangeConfirmPwd("");
    setChangeAckRisk(false);
  }, [open, preferences]);

  useEffect(() => {
    const next = resolvePresetByColors(terminalBackgroundColor, terminalForegroundColor);
    setTerminalThemePreset((cur) => (cur === next ? cur : next));
  }, [terminalBackgroundColor, terminalForegroundColor]);

  useEffect(() => {
    if (!open) return;
    setPwdStatusLoading(true);
    setPwdStatusKnown(false);
    void (async () => {
      try {
        const status = await window.nextshell.masterPassword.passwordStatus();
        setPwdStatus(status);
        setPwdStatusKnown(true);
      } catch {
        /* ignore */
      } finally {
        setPwdStatusLoading(false);
      }
    })();
  }, [open]);

  const refreshPasswordStatus = useCallback(async () => {
    try {
      const status = await window.nextshell.masterPassword.passwordStatus();
      setPwdStatus(status);
      setPwdStatusKnown(true);
    } catch {
      /* noop */
    }
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

  // ─── Password & backup handlers ───────────────────────────────────
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
      await window.nextshell.masterPassword.setPassword({
        password: pwdInput,
        confirmPassword: pwdConfirm
      });
      message.success("主密码已设置");
      setPwdInput("");
      setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`设置密码失败：${formatErrorMessage(error, "请检查输入内容")}`);
    } finally {
      setPwdBusy(false);
    }
  };

  const handleUnlockPassword = async (): Promise<void> => {
    if (!pwdInput) {
      message.warning("请输入主密码。");
      return;
    }
    setPwdBusy(true);
    try {
      await window.nextshell.masterPassword.unlockPassword({ password: pwdInput });
      message.success("主密码已解锁");
      setPwdInput("");
      setPwdConfirm("");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`解锁密码失败：${formatErrorMessage(error, "请检查密码是否正确")}`);
    } finally {
      setPwdBusy(false);
    }
  };

  const handleClearRemembered = async (): Promise<void> => {
    try {
      await window.nextshell.masterPassword.clearRemembered();
      message.success("已清除记住的主密码");
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`清除失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  };

  const handleReauthorizeCredentialStore = async (): Promise<void> => {
    try {
      const result = await window.nextshell.masterPassword.reauthorizeCredentialStore();
      if (result.authorized) {
        message.success("钥匙串授权成功，已保存的密码恢复可用");
      } else {
        message.warning("钥匙串授权仍被拒绝，请在系统弹窗中选择「始终允许」");
      }
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`重新授权失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  };

  const handleChangePassword = async (): Promise<void> => {
    if (!changeOldPwd) {
      message.warning("请输入原密码。");
      return;
    }
    if (!changeNewPwd || changeNewPwd.length < 6) {
      message.warning("新密码至少需要 6 个字符。");
      return;
    }
    if (changeNewPwd !== changeConfirmPwd) {
      message.warning("两次输入的新密码不一致。");
      return;
    }
    if (!changeAckRisk) {
      message.warning("请先确认已知晓修改主密码对数据备份的影响。");
      return;
    }

    const sameAsOld = changeOldPwd === changeNewPwd;
    if (sameAsOld) {
      message.warning("新密码与原密码相同，将按原密码重新设置。");
    }

    setChangeBusy(true);
    try {
      await window.nextshell.masterPassword.changePassword({
        oldPassword: changeOldPwd,
        newPassword: changeNewPwd,
        confirmPassword: changeConfirmPwd
      });
      if (sameAsOld) {
        message.success("主密码已更新（与原密码相同）。");
      } else {
        message.success("主密码已修改。旧备份可能无法还原，请重新备份。");
      }
      setChangeOldPwd("");
      setChangeNewPwd("");
      setChangeConfirmPwd("");
      setChangeAckRisk(false);
      await refreshPasswordStatus();
    } catch (error) {
      message.error(`修改主密码失败：${formatErrorMessage(error, "请检查输入内容")}`);
    } finally {
      setChangeBusy(false);
    }
  };

  const handleRunBackup = async (): Promise<void> => {
    setBackupRunning(true);
    try {
      const result = await window.nextshell.backup.run({ conflictPolicy: backupConflictPolicy });
      message.success(result.fileName ? `备份完成: ${result.fileName}` : "备份完成");
    } catch (error) {
      message.error(`备份失败：${formatErrorMessage(error, "请检查数据备份配置")}`);
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
      message.error(`获取备份列表失败：${formatErrorMessage(error, "请检查数据备份配置")}`);
    } finally {
      setArchiveListLoading(false);
    }
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
          await window.nextshell.backup.restore({
            archiveId,
            conflictPolicy: restoreConflictPolicy
          });
          message.success("还原文件已准备，重启应用后生效。");
        } catch (error) {
          message.error(`还原失败：${formatErrorMessage(error, "请检查数据备份配置")}`);
        } finally {
          setRestoring(null);
        }
      }
    });
  };

  // ─── Memoized section content ───────────────────────────────────────
  const sectionContent = useMemo(() => {
    switch (activeSection) {
      // 云同步是账号/服务配置，回收站是全局删除历史（含密钥）：两者都是低频全局设置，
      // 不属于「管理这台机器的连接」，所以从连接管理器搬到这里。
      case "cloudSync":
        return <CloudSyncSection />;

      case "recycleBin":
        return <RecycleBinSection />;

      case "security":
        return (
          <SecuritySection
            pwdStatus={pwdStatus}
            pwdStatusLoading={pwdStatusLoading}
            pwdInput={pwdInput}
            pwdConfirm={pwdConfirm}
            pwdBusy={pwdBusy}
            changeOldPwd={changeOldPwd}
            changeNewPwd={changeNewPwd}
            changeConfirmPwd={changeConfirmPwd}
            changeAckRisk={changeAckRisk}
            changeBusy={changeBusy}
            backupRememberPassword={preferences.backup.rememberPassword}
            loading={loading}
            setPwdInput={setPwdInput}
            setPwdConfirm={setPwdConfirm}
            setChangeOldPwd={setChangeOldPwd}
            setChangeNewPwd={setChangeNewPwd}
            setChangeConfirmPwd={setChangeConfirmPwd}
            setChangeAckRisk={setChangeAckRisk}
            onSetPassword={() => void handleSetPassword()}
            onUnlockPassword={() => void handleUnlockPassword()}
            onChangePassword={() => void handleChangePassword()}
            onClearRemembered={() => void handleClearRemembered()}
            onReauthorizeCredentialStore={() => void handleReauthorizeCredentialStore()}
            save={save}
          />
        );
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

      case "command":
        return (
          <CommandSection
            loading={loading}
            rememberTemplateParams={preferences.commandCenter.rememberTemplateParams}
            batchMaxConcurrency={preferences.commandCenter.batchMaxConcurrency}
            batchRetryCount={preferences.commandCenter.batchRetryCount}
            save={save}
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

      case "backup":
        return (
          <BackupSection
            loading={loading}
            backupRemotePath={backupRemotePath}
            rclonePath={rclonePath}
            backupConflictPolicy={backupConflictPolicy}
            restoreConflictPolicy={restoreConflictPolicy}
            pwdStatus={pwdStatus}
            pwdStatusKnown={pwdStatusKnown}
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
            setArchiveListVisible={setArchiveListVisible}
            onOpenSecurity={() => setActiveSection("security")}
            onRunBackup={() => void handleRunBackup()}
            onListArchives={() => void handleListArchives()}
            onRestore={(id) => void handleRestore(id)}
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
    backupRemotePath,
    rclonePath,
    backupConflictPolicy,
    restoreConflictPolicy,
    pwdStatus,
    pwdStatusKnown,
    pwdStatusLoading,
    pwdInput,
    pwdConfirm,
    pwdBusy,
    changeOldPwd,
    changeNewPwd,
    changeConfirmPwd,
    changeAckRisk,
    changeBusy,
    backupRunning,
    archiveList,
    archiveListVisible,
    archiveListLoading,
    restoring,
    save,
    pickDirectory,
    message,
    modal
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
