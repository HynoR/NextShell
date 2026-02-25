import { useCallback, useEffect, useMemo, useState, type CSSProperties } from "react";
import { message } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { ConnectionManagerModal } from "./components/ConnectionManagerModal";
import { SettingsCenterModal } from "./components/SettingsCenterModal";
import { WorkspaceLayout } from "./components/WorkspaceLayout";
import { AppSkeleton } from "./components/LoadingSkeletons";
import { useConnectionManager } from "./hooks/useConnectionManager";
import { useMonitorLifecycle } from "./hooks/useMonitorLifecycle";
import { useSessionLifecycle } from "./hooks/useSessionLifecycle";
import { useEditorTabStore } from "./store/useEditorTabStore";
import { usePreferencesStore } from "./store/usePreferencesStore";
import { useTransferQueueStore } from "./store/useTransferQueueStore";
import { useWorkspaceStore } from "./store/useWorkspaceStore";
import { formatErrorMessage } from "./utils/errorMessage";
import {
  buildQuickConnectUpsertInput,
  findExistingByAddress,
  parseQuickConnectInput
} from "./utils/quickConnectInput";

const isTerminalSession = (session: SessionDescriptor): boolean =>
  !session.type || session.type === "terminal";

export const App = () => {
  const {
    connections,
    sshKeys,
    proxies,
    activeConnectionId,
    sessions,
    activeSessionId,
    monitor,
    bottomTab,
    setConnections,
    setSshKeys,
    setProxies,
    setActiveConnection,
    upsertSession,
    removeSession,
    reorderSession,
    renameSessionTitle,
    setActiveSession,
    setMonitor,
    setBottomTab
  } = useWorkspaceStore();

  const [appReady, setAppReady] = useState(false);
  const [managerOpen, setManagerOpen] = useState(false);
  const [managerFocusConnectionId, setManagerFocusConnectionId] = useState<string>();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [transferPanelCollapsed, setTransferPanelCollapsed] = useState(false);

  const initializePreferences = usePreferencesStore((state) => state.initialize);
  const appBackgroundImagePath = usePreferencesStore((state) => state.preferences.window.backgroundImagePath);
  const appBackgroundOpacity = usePreferencesStore((state) => state.preferences.window.backgroundOpacity);
  const applyTransferEvent = useTransferQueueStore((state) => state.applyEvent);
  const transferTasks = useTransferQueueStore((state) => state.tasks);
  const enqueueTransferTask = useTransferQueueStore((state) => state.enqueueTask);
  const getTransferTask = useTransferQueueStore((state) => state.getTask);
  const markTransferFailed = useTransferQueueStore((state) => state.markFailed);
  const markTransferSuccess = useTransferQueueStore((state) => state.markSuccess);
  const clearFinishedTransfers = useTransferQueueStore((state) => state.clearFinished);

  const { loadConnections, handleConnectionSaved, handleConnectionRemoved } = useConnectionManager();

  const editorTabOpenTab = useEditorTabStore((state) => state.openTab);
  const editorTabCloseTab = useEditorTabStore((state) => state.closeTab);
  const editorTabFindByRemotePath = useEditorTabStore((state) => state.findByRemotePath);

  const loadSshKeys = useCallback(async () => {
    try {
      const list = await window.nextshell.sshKey.list({});
      setSshKeys(list);
    } catch (error) {
      message.error(`加载密钥失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [setSshKeys]);

  const loadProxies = useCallback(async () => {
    try {
      const list = await window.nextshell.proxy.list({});
      setProxies(list);
    } catch (error) {
      message.error(`加载代理失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [setProxies]);

  const {
    connectingIds,
    startSession,
    retrySessionAuth,
    activateConnection,
    handleCloseSession,
    handleReconnectSession
  } = useSessionLifecycle();

  const activeConnection = useMemo(
    () => connections.find((connection) => connection.id === activeConnectionId),
    [connections, activeConnectionId]
  );

  const activeSession = useMemo(
    () => sessions.find((session) => session.id === activeSessionId),
    [sessions, activeSessionId]
  );

  const activeSessionConnection = useMemo(() => {
    if (!activeSession) return undefined;
    return connections.find((connection) => connection.id === activeSession.connectionId);
  }, [activeSession, connections]);

  const isActiveConnectionTerminalConnected = useMemo(
    () =>
      Boolean(
        activeConnectionId &&
        sessions.some(
          (session) =>
            session.connectionId === activeConnectionId &&
            session.type === "terminal" &&
            session.status === "connected"
        )
      ),
    [activeConnectionId, sessions]
  );

  const activeConnectionConnectedTerminalSessionId = useMemo(
    () =>
      sessions.find(
        (session) =>
          session.connectionId === activeConnectionId &&
          session.type === "terminal" &&
          session.status === "connected"
      )?.id,
    [activeConnectionId, sessions]
  );

  const [lastActiveTerminalSessionId, setLastActiveTerminalSessionId] = useState<string>();

  useEffect(() => {
    if (activeSession && isTerminalSession(activeSession)) {
      setLastActiveTerminalSessionId(activeSession.id);
    }
  }, [activeSession]);

  useEffect(() => {
    if (!lastActiveTerminalSessionId) return;
    const exists = sessions.some(
      (session) => session.id === lastActiveTerminalSessionId && isTerminalSession(session)
    );
    if (!exists) setLastActiveTerminalSessionId(undefined);
  }, [lastActiveTerminalSessionId, sessions]);

  const activeTerminalSession = useMemo(() => {
    if (activeSession && isTerminalSession(activeSession)) return activeSession;
    if (lastActiveTerminalSessionId) {
      const last = sessions.find(
        (session) => session.id === lastActiveTerminalSessionId && isTerminalSession(session)
      );
      if (last) return last;
    }
    if (activeConnectionId) {
      const conn = sessions.find(
        (session) => session.connectionId === activeConnectionId && isTerminalSession(session)
      );
      if (conn) return conn;
    }
    return sessions.find((session) => isTerminalSession(session));
  }, [activeConnectionId, activeSession, lastActiveTerminalSessionId, sessions]);

  const activeTerminalConnection = useMemo(() => {
    if (!activeTerminalSession) return undefined;
    return connections.find((connection) => connection.id === activeTerminalSession.connectionId);
  }, [activeTerminalSession, connections]);

  const terminalSessionIds = useMemo(
    () => sessions.filter((session) => isTerminalSession(session)).map((session) => session.id),
    [sessions]
  );

  const { openMonitorTab } = useMonitorLifecycle(
    activeConnectionId,
    activeConnection?.monitorSession,
    isActiveConnectionTerminalConnected,
    sessions
  );

  // Initialize app
  useEffect(() => {
    Promise.all([loadConnections(), loadSshKeys(), loadProxies(), initializePreferences()]).finally(() => {
      setAppReady(true);
    });
  }, [loadConnections, loadSshKeys, loadProxies, initializePreferences]);

  // Transfer status events
  useEffect(() => {
    const unsubscribe = window.nextshell.sftp.onTransferStatus((event) => {
      applyTransferEvent(event);
    });
    return () => { unsubscribe(); };
  }, [applyTransferEvent]);

  const connectActiveConnection = useCallback(async () => {
    if (!activeConnectionId) {
      message.warning("请先选择连接。");
      return;
    }
    await startSession(activeConnectionId);
  }, [activeConnectionId, startSession]);

  const handleRenameSession = useCallback(
    (sessionId: string, title: string) => {
      const next = title.trim();
      if (!next) return;
      renameSessionTitle(sessionId, next);
    },
    [renameSessionTitle]
  );

  const handleOpenProcessManager = useCallback(
    (connectionId: string) => openMonitorTab(
      connectionId, "processManager", connections,
      setActiveSession, setActiveConnection, upsertSession
    ),
    [openMonitorTab, connections, setActiveSession, setActiveConnection, upsertSession]
  );

  const handleOpenNetworkMonitor = useCallback(
    (connectionId: string) => openMonitorTab(
      connectionId, "networkMonitor", connections,
      setActiveSession, setActiveConnection, upsertSession
    ),
    [openMonitorTab, connections, setActiveSession, setActiveConnection, upsertSession]
  );

  const handleOpenEditorTab = useCallback(
    async (connectionId: string, remotePath: string) => {
      const existing = editorTabFindByRemotePath(connectionId, remotePath);
      if (existing) {
        setActiveSession(existing.sessionId);
        setActiveConnection(connectionId);
        return;
      }

      try {
        const result = await window.nextshell.sftp.editOpenBuiltin({ connectionId, remotePath });
        const fileName = remotePath.split("/").pop() ?? remotePath;
        const conn = connections.find((c) => c.id === connectionId);
        const serverLabel = conn?.name ?? conn?.host ?? connectionId.slice(0, 8);
        const sessionId = `editor-${result.editId}`;
        const session: SessionDescriptor = {
          id: sessionId,
          connectionId,
          type: "editor",
          title: `${fileName} [${serverLabel}]`,
          status: "connected",
          createdAt: new Date().toISOString(),
          reconnectable: false
        };
        upsertSession(session);
        editorTabOpenTab({
          sessionId,
          connectionId,
          remotePath,
          editId: result.editId,
          initialContent: result.content,
          dirty: false,
          saving: false
        });
        setActiveSession(sessionId);
        setActiveConnection(connectionId);
      } catch (err) {
        message.error(`打开编辑器失败：${formatErrorMessage(err, "请检查连接状态")}`);
      }
    },
    [connections, editorTabFindByRemotePath, editorTabOpenTab, setActiveConnection, setActiveSession, upsertSession]
  );

  const handleCloseMonitorTab = useCallback(
    (sessionId: string) => {
      const target = sessions.find((s) => s.id === sessionId);
      if (!target) return;
      if (target.type === "editor") {
        editorTabCloseTab(sessionId);
      }
      removeSession(sessionId);
    },
    [sessions, removeSession, editorTabCloseTab]
  );

  const handleSelectSystemNetworkInterface = useCallback(
    (networkInterface: string) => {
      if (!activeConnectionId) return;
      void window.nextshell.monitor.selectSystemInterface({
        connectionId: activeConnectionId,
        networkInterface
      }).catch((error) => {
        message.error(`切换监控网卡失败：${formatErrorMessage(error, "请稍后重试")}`);
      });
    },
    [activeConnectionId]
  );

  const handleRetryTransferTask = useCallback(async (taskId: string) => {
    const failedTask = getTransferTask(taskId);
    if (!failedTask || failedTask.status !== "failed") return;

    const retryTask = enqueueTransferTask({
      direction: failedTask.direction,
      connectionId: failedTask.connectionId,
      localPath: failedTask.localPath,
      remotePath: failedTask.remotePath,
      retryOfTaskId: failedTask.id
    });

    try {
      if (failedTask.direction === "upload") {
        await window.nextshell.sftp.upload({
          connectionId: failedTask.connectionId,
          localPath: failedTask.localPath,
          remotePath: failedTask.remotePath,
          taskId: retryTask.id
        });
      } else {
        await window.nextshell.sftp.download({
          connectionId: failedTask.connectionId,
          remotePath: failedTask.remotePath,
          localPath: failedTask.localPath,
          taskId: retryTask.id
        });
      }
      markTransferSuccess(retryTask.id);
    } catch (error) {
      const reason = formatErrorMessage(error, "重试失败");
      markTransferFailed(retryTask.id, reason);
      message.error(`重试传输失败：${reason}`);
    }
  }, [enqueueTransferTask, getTransferTask, markTransferFailed, markTransferSuccess]);

  const handleOpenTransferLocalFile = useCallback(async (localPath: string) => {
    const result = await window.nextshell.dialog.openPath({
      path: localPath,
      revealInFolder: false
    });
    if (!result.ok) {
      message.error(`打开文件失败：${formatErrorMessage(result.error, "请检查文件路径")}`);
    }
  }, []);

  const handleTreeQuickSaveConnection = useCallback(
    async (payload: ConnectionUpsertInput) => {
      await window.nextshell.connection.upsert(payload);
      await loadConnections();
    },
    [loadConnections]
  );

  const handleTitlebarQuickConnect = useCallback(
    async (raw: string): Promise<boolean> => {
      const parsed = parseQuickConnectInput(raw);
      if (!parsed.ok) {
        message.warning(parsed.message);
        return false;
      }

      try {
        const existing = findExistingByAddress(connections, parsed.value);
        const connectionId = existing
          ? existing.id
          : (await window.nextshell.connection.upsert(
              buildQuickConnectUpsertInput(parsed.value)
            )).id;

        if (!existing) {
          const refreshed = await window.nextshell.connection.list({});
          setConnections(refreshed);
        }

        await startSession(connectionId);
        return true;
      } catch (error) {
        message.error(`快速连接失败：${formatErrorMessage(error, "请稍后重试")}`);
        return false;
      }
    },
    [connections, setConnections, startSession]
  );

  const handleOpenManager = useCallback(() => {
    setManagerFocusConnectionId(undefined);
    setManagerOpen(true);
  }, []);

  const handleOpenManagerForConnection = useCallback((connectionId: string) => {
    setManagerFocusConnectionId(connectionId);
    setManagerOpen(true);
  }, []);

  const isConnecting = activeConnectionId ? connectingIds.has(activeConnectionId) : false;
  const normalizedAppBackgroundImagePath = appBackgroundImagePath.trim();
  const hasAppBackgroundImage = normalizedAppBackgroundImagePath.length > 0;

  const appShellStyle = useMemo<CSSProperties | undefined>(() => {
    if (!hasAppBackgroundImage) {
      return undefined;
    }
    return {
      "--app-background-opacity": String(appBackgroundOpacity)
    } as CSSProperties;
  }, [appBackgroundOpacity, hasAppBackgroundImage]);

  if (!appReady) {
    return <AppSkeleton />;
  }

  return (
    <div
      className={hasAppBackgroundImage ? "app-shell app-shell--with-wallpaper" : "app-shell"}
      style={appShellStyle}
    >
      {hasAppBackgroundImage ? (
        <div
          className="app-wallpaper-layer"
          style={{
            backgroundImage: `url("nextshell-asset://local${normalizedAppBackgroundImagePath}")`
          }}
        />
      ) : null}
      <div className="app-shell-content">
        <WorkspaceLayout
          connections={connections}
          sshKeys={sshKeys}
          sessions={sessions}
          activeConnectionId={activeConnectionId}
          activeSessionId={activeSessionId}
          activeConnection={activeConnection}
          activeSession={activeSession}
          activeSessionConnection={activeSessionConnection}
          activeTerminalSession={activeTerminalSession}
          activeTerminalConnection={activeTerminalConnection}
          activeConnectionConnectedTerminalSessionId={activeConnectionConnectedTerminalSessionId}
          terminalSessionIds={terminalSessionIds}
          isActiveConnectionTerminalConnected={isActiveConnectionTerminalConnected}
          monitor={monitor}
          transferTasks={transferTasks}
          transferPanelCollapsed={transferPanelCollapsed}
          bottomTab={bottomTab}
          onLoadConnections={() => void loadConnections()}
          onOpenManager={handleOpenManager}
          onOpenSettings={() => setSettingsOpen(true)}
          onActivateConnection={activateConnection}
          onTreeDoubleConnect={(connectionId) => void startSession(connectionId)}
          onTreeConnect={(connectionId) => void startSession(connectionId)}
          onTreeQuickSaveConnection={handleTreeQuickSaveConnection}
          onTitlebarQuickConnect={handleTitlebarQuickConnect}
          onTreeEditServer={handleOpenManagerForConnection}
          onCloseSession={handleCloseSession}
          onReconnectSession={handleReconnectSession}
          onRenameSession={handleRenameSession}
          onOpenProcessManager={handleOpenProcessManager}
          onOpenNetworkMonitor={handleOpenNetworkMonitor}
          onCloseMonitorTab={handleCloseMonitorTab}
          onOpenEditorTab={handleOpenEditorTab}
          onRetrySessionAuth={retrySessionAuth}
          onSetActiveSession={setActiveSession}
          onSetActiveConnection={setActiveConnection}
          onReorderSession={reorderSession}
          onSelectNetworkInterface={handleSelectSystemNetworkInterface}
          onRetryTransfer={(taskId) => void handleRetryTransferTask(taskId)}
          onClearFinishedTransfers={clearFinishedTransfers}
          onOpenLocalFile={(task) => void handleOpenTransferLocalFile(task.localPath)}
          onTransferPanelToggle={() => setTransferPanelCollapsed((v) => !v)}
          onSetBottomTab={(tab) => {
            if (
              tab === "commands" ||
              tab === "files" ||
              tab === "connections" ||
              tab === "live-edit" ||
              tab === "system-info" ||
              tab === "traceroute"
            ) {
              setBottomTab(tab);
            }
          }}
        />

        <ConnectionManagerModal
          open={managerOpen}
          focusConnectionId={managerFocusConnectionId}
          connections={connections}
          sshKeys={sshKeys}
          proxies={proxies}
          onClose={() => {
            setManagerOpen(false);
            setManagerFocusConnectionId(undefined);
          }}
          onConnectionSaved={(payload: ConnectionUpsertInput) => handleConnectionSaved(payload)}
          onConnectConnection={async (connectionId: string) => {
            await startSession(connectionId);
          }}
          onConnectionRemoved={(connectionId: string) => handleConnectionRemoved(connectionId)}
          onConnectionsImported={loadConnections}
          onReloadSshKeys={loadSshKeys}
          onReloadProxies={loadProxies}
        />

        <SettingsCenterModal
          open={settingsOpen}
          onClose={() => setSettingsOpen(false)}
        />
      </div>
    </div>
  );
};
