import { useCallback, useEffect, useMemo, useState } from "react";
import { message } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { ConnectionManagerModal } from "./components/ConnectionManagerModal";
import { SettingsCenterDrawer } from "./components/SettingsCenterDrawer";
import { WorkspaceLayout } from "./components/WorkspaceLayout";
import { AppSkeleton } from "./components/LoadingSkeletons";
import { useConnectionManager } from "./hooks/useConnectionManager";
import { useMonitorLifecycle } from "./hooks/useMonitorLifecycle";
import { useSessionLifecycle } from "./hooks/useSessionLifecycle";
import { usePreferencesStore } from "./store/usePreferencesStore";
import { useTransferQueueStore } from "./store/useTransferQueueStore";
import { useWorkspaceStore } from "./store/useWorkspaceStore";

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
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [transferPanelCollapsed, setTransferPanelCollapsed] = useState(false);

  const initializePreferences = usePreferencesStore((state) => state.initialize);
  const applyTransferEvent = useTransferQueueStore((state) => state.applyEvent);
  const transferTasks = useTransferQueueStore((state) => state.tasks);
  const enqueueTransferTask = useTransferQueueStore((state) => state.enqueueTask);
  const getTransferTask = useTransferQueueStore((state) => state.getTask);
  const markTransferFailed = useTransferQueueStore((state) => state.markFailed);
  const markTransferSuccess = useTransferQueueStore((state) => state.markSuccess);
  const clearFinishedTransfers = useTransferQueueStore((state) => state.clearFinished);

  const { loadConnections, handleConnectionSaved, handleConnectionRemoved } = useConnectionManager();

  const loadSshKeys = useCallback(async () => {
    try {
      const list = await window.nextshell.sshKey.list({});
      setSshKeys(list);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "加载密钥失败";
      message.error(reason);
    }
  }, [setSshKeys]);

  const loadProxies = useCallback(async () => {
    try {
      const list = await window.nextshell.proxy.list({});
      setProxies(list);
    } catch (error) {
      const reason = error instanceof Error ? error.message : "加载代理失败";
      message.error(reason);
    }
  }, [setProxies]);

  const {
    connectingIds,
    authPromptState,
    startSession,
    activateConnection,
    handleCloseSession,
    handleReconnectSession,
    handleAuthPromptCancel,
    handleAuthPromptSubmit,
    MAX_SESSION_OPEN_ATTEMPTS
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

  const handleCloseMonitorTab = useCallback(
    (sessionId: string) => {
      const target = sessions.find((s) => s.id === sessionId);
      if (!target) return;
      removeSession(sessionId);
    },
    [sessions, removeSession]
  );

  const handleSelectSystemNetworkInterface = useCallback(
    (networkInterface: string) => {
      if (!activeConnectionId) return;
      void window.nextshell.monitor.selectSystemInterface({
        connectionId: activeConnectionId,
        networkInterface
      }).catch((error) => {
        const reason = error instanceof Error ? error.message : "切换监控网卡失败";
        message.error(reason);
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
      const reason = error instanceof Error ? error.message : "Transfer retry failed";
      markTransferFailed(retryTask.id, reason);
      message.error(reason);
    }
  }, [enqueueTransferTask, getTransferTask, markTransferFailed, markTransferSuccess]);

  const handleOpenTransferLocalFile = useCallback(async (localPath: string) => {
    const result = await window.nextshell.dialog.openPath({
      path: localPath,
      revealInFolder: false
    });
    if (!result.ok) {
      message.error(result.error ? `打开文件失败: ${result.error}` : "打开文件失败");
    }
  }, []);

  const isConnecting = activeConnectionId ? connectingIds.has(activeConnectionId) : false;

  if (!appReady) {
    return <AppSkeleton />;
  }

  return (
    <>
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
        terminalSessionIds={terminalSessionIds}
        isActiveConnectionTerminalConnected={isActiveConnectionTerminalConnected}
        monitor={monitor}
        transferTasks={transferTasks}
        transferPanelCollapsed={transferPanelCollapsed}
        bottomTab={bottomTab}
        authPromptState={authPromptState}
        MAX_SESSION_OPEN_ATTEMPTS={MAX_SESSION_OPEN_ATTEMPTS}
        onLoadConnections={() => void loadConnections()}
        onOpenManager={() => setManagerOpen(true)}
        onOpenSettings={() => setSettingsOpen(true)}
        onActivateConnection={activateConnection}
        onTreeDoubleConnect={(connectionId) => void startSession(connectionId)}
        onTreeConnect={(connectionId) => void startSession(connectionId)}
        onCloseSession={handleCloseSession}
        onReconnectSession={handleReconnectSession}
        onRenameSession={handleRenameSession}
        onOpenProcessManager={handleOpenProcessManager}
        onOpenNetworkMonitor={handleOpenNetworkMonitor}
        onCloseMonitorTab={handleCloseMonitorTab}
        onSetActiveSession={setActiveSession}
        onSetActiveConnection={setActiveConnection}
        onReorderSession={reorderSession}
        onSelectNetworkInterface={handleSelectSystemNetworkInterface}
        onRetryTransfer={(taskId) => void handleRetryTransferTask(taskId)}
        onClearFinishedTransfers={clearFinishedTransfers}
        onOpenLocalFile={(task) => void handleOpenTransferLocalFile(task.localPath)}
        onTransferPanelToggle={() => setTransferPanelCollapsed((v) => !v)}
        onSetBottomTab={(tab) => {
          if (tab === "commands" || tab === "files" || tab === "connections" || tab === "live-edit") {
            setBottomTab(tab);
          }
        }}
        onAuthPromptCancel={handleAuthPromptCancel}
        onAuthPromptSubmit={handleAuthPromptSubmit}
      />

      <ConnectionManagerModal
        open={managerOpen}
        connections={connections}
        sshKeys={sshKeys}
        proxies={proxies}
        onClose={() => setManagerOpen(false)}
        onConnectionSaved={(payload: ConnectionUpsertInput) => handleConnectionSaved(payload)}
        onConnectionRemoved={(connectionId: string) => handleConnectionRemoved(connectionId)}
        onConnectionsImported={loadConnections}
        onReloadSshKeys={loadSshKeys}
        onReloadProxies={loadProxies}
      />

      <SettingsCenterDrawer
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
      />
    </>
  );
};
