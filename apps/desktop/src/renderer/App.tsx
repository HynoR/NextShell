import {
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type CSSProperties
} from "react";
import { App as AntdApp } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import type { SettingsSection } from "./components/settings-center/types";
import { WorkspaceLayout } from "./components/WorkspaceLayout";
import { AppSkeleton } from "./components/LoadingSkeletons";
import { useConnectionManager } from "./hooks/useConnectionManager";
import { useMonitorLifecycle } from "./hooks/useMonitorLifecycle";
import { useSessionLifecycle } from "./hooks/useSessionLifecycle";
import { useEditorTabStore } from "./store/useEditorTabStore";
import { useAgentActivityStore } from "./store/useAgentActivityStore";
import { usePreferencesStore } from "./store/usePreferencesStore";
import { useTransferQueueStore, type TransferTask } from "./store/useTransferQueueStore";
import { useWorkspaceStore } from "./store/useWorkspaceStore";
import { formatErrorMessage } from "./utils/errorMessage";
import { resolveFollowTerminalSessionId } from "./utils/followTerminalSession";
import {
  buildQuickConnectUpsertInput,
  findExistingByAddress,
  parseQuickConnectInput
} from "./utils/quickConnectInput";

const isTerminalSession = (session: SessionDescriptor): boolean =>
  !session.type || session.type === "terminal";

// 连接管理器/设置中心体积较大,按需加载,首次打开时才拉取 chunk
const LazyConnectionManagerV2 = lazy(() =>
  import("./components/ConnectionManagerV2").then((module) => ({
    default: module.ConnectionManagerV2
  }))
);
const LazySettingsCenterModal = lazy(() =>
  import("./components/SettingsCenterModal").then((module) => ({
    default: module.SettingsCenterModal
  }))
);

type LocalAwareSessionDescriptor = SessionDescriptor & {
  target?: "remote" | "local";
  connectionId?: string;
};

const isLocalSession = (session?: SessionDescriptor): boolean =>
  (session as LocalAwareSessionDescriptor | undefined)?.target === "local";

const getSessionConnectionId = (session?: SessionDescriptor): string | undefined =>
  (session as LocalAwareSessionDescriptor | undefined)?.connectionId;

export const App = () => {
  const { message, modal } = AntdApp.useApp();
  const connections = useWorkspaceStore((state) => state.connections);
  const sshKeys = useWorkspaceStore((state) => state.sshKeys);
  const proxies = useWorkspaceStore((state) => state.proxies);
  const activeConnectionId = useWorkspaceStore((state) => state.activeConnectionId);
  const sessions = useWorkspaceStore((state) => state.sessions);
  const activeSessionId = useWorkspaceStore((state) => state.activeSessionId);
  const bottomTab = useWorkspaceStore((state) => state.bottomTab);
  const lastActiveRemoteTerminalByConnection = useWorkspaceStore(
    (state) => state.lastActiveRemoteTerminalByConnection
  );
  const setConnections = useWorkspaceStore((state) => state.setConnections);
  const setSshKeys = useWorkspaceStore((state) => state.setSshKeys);
  const setProxies = useWorkspaceStore((state) => state.setProxies);
  const setActiveConnection = useWorkspaceStore((state) => state.setActiveConnection);
  const upsertSession = useWorkspaceStore((state) => state.upsertSession);
  const removeSession = useWorkspaceStore((state) => state.removeSession);
  const reorderSession = useWorkspaceStore((state) => state.reorderSession);
  const renameSessionTitle = useWorkspaceStore((state) => state.renameSessionTitle);
  const setActiveSession = useWorkspaceStore((state) => state.setActiveSession);
  const setBottomTab = useWorkspaceStore((state) => state.setBottomTab);

  const [appReady, setAppReady] = useState(false);
  const [managerOpen, setManagerOpen] = useState(false);
  const [managerFocusConnectionId, setManagerFocusConnectionId] = useState<string>();
  // ⌘K 面板「添加新服务器」打开管理器时直接进新建态(D31);关闭管理器时复位。
  const [managerInitialAction, setManagerInitialAction] = useState<"create" | undefined>(undefined);
  const [settingsOpen, setSettingsOpen] = useState(false);
  // 深链进设置中心的目标节(C4)。undefined = 普通打开,停在用户上次看的那一节。
  const [settingsSection, setSettingsSection] = useState<SettingsSection>();
  const [managerModalLoaded, setManagerModalLoaded] = useState(false);
  const [settingsModalLoaded, setSettingsModalLoaded] = useState(false);
  const [transferPanelCollapsed, setTransferPanelCollapsed] = useState(false);
  const [liveEditPanelCollapsed, setLiveEditPanelCollapsed] = useState(false);

  const initializePreferences = usePreferencesStore((state) => state.initialize);
  const appBackgroundImagePath = usePreferencesStore(
    (state) => state.preferences.window.backgroundImagePath
  );
  const appBackgroundOpacity = usePreferencesStore(
    (state) => state.preferences.window.backgroundOpacity
  );
  const terminalBackgroundColor = usePreferencesStore(
    (state) => state.preferences.terminal.backgroundColor
  );
  const applyTransferEvent = useTransferQueueStore((state) => state.applyEvent);
  const enqueueTransferTask = useTransferQueueStore((state) => state.enqueueTask);
  const getTransferTask = useTransferQueueStore((state) => state.getTask);
  const markTransferFailed = useTransferQueueStore((state) => state.markFailed);
  const markTransferSuccess = useTransferQueueStore((state) => state.markSuccess);
  const clearFinishedTransfers = useTransferQueueStore((state) => state.clearFinished);
  const applyAgentActivity = useAgentActivityStore((state) => state.applyEvent);
  const applyAgentSessionControl = useAgentActivityStore((state) => state.applySessionControl);
  const setAgentHalted = useAgentActivityStore((state) => state.setHalted);
  const setAgentEnabled = useAgentActivityStore((state) => state.setEnabled);

  useEffect(() => window.nextshell.agent.onActivity(applyAgentActivity), [applyAgentActivity]);
  useEffect(
    () => window.nextshell.agent.onSessionControl(applyAgentSessionControl),
    [applyAgentSessionControl]
  );
  // `session_focus`: the agent is handing something back that wants a human.
  useEffect(
    () => window.nextshell.agent.onSessionFocus(({ sessionId }) => setActiveSession(sessionId)),
    [setActiveSession]
  );
  // The breaker lives in the main process; mirror its state on mount so a
  // reload does not present a halted endpoint as running.
  useEffect(() => {
    void window.nextshell.agent
      .status()
      .then((status) => {
        setAgentHalted(status.halted);
        setAgentEnabled(status.enabled);
      })
      .catch(() => undefined);
  }, [setAgentHalted, setAgentEnabled]);

  const { loadConnections, handleConnectionSaved, handleConnectionRemoved } =
    useConnectionManager();

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

  const refreshSyncResources = useCallback(async () => {
    await Promise.all([loadConnections(), loadSshKeys(), loadProxies()]);
  }, [loadConnections, loadProxies, loadSshKeys]);

  const {
    connectingIds,
    startSession,
    startLocalSession,
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
    const activeSessionConnectionId = getSessionConnectionId(activeSession);
    if (!activeSessionConnectionId) {
      return undefined;
    }
    return connections.find((connection) => connection.id === activeSessionConnectionId);
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

  const followTerminalSessionId = useMemo(
    () =>
      resolveFollowTerminalSessionId({
        activeConnectionId,
        activeSessionId,
        connections,
        sessions,
        lastActiveRemoteTerminalByConnection
      }),
    [
      activeConnectionId,
      activeSessionId,
      connections,
      lastActiveRemoteTerminalByConnection,
      sessions
    ]
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
        (session) =>
          getSessionConnectionId(session) === activeConnectionId && isTerminalSession(session)
      );
      if (conn) return conn;
    }
    return sessions.find((session) => isTerminalSession(session));
  }, [activeConnectionId, activeSession, lastActiveTerminalSessionId, sessions]);

  const activeTerminalConnection = useMemo(() => {
    if (!activeTerminalSession) return undefined;
    const terminalConnectionId = getSessionConnectionId(activeTerminalSession);
    if (!terminalConnectionId) {
      return undefined;
    }
    return connections.find((connection) => connection.id === terminalConnectionId);
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
    Promise.all([refreshSyncResources(), initializePreferences()]).finally(() => {
      setAppReady(true);
    });
  }, [refreshSyncResources, initializePreferences]);

  // 懒加载的 Modal 在首次打开后才挂载,之后保持挂载以保留关闭动画与内部状态
  useEffect(() => {
    if (managerOpen) setManagerModalLoaded(true);
    if (settingsOpen) setSettingsModalLoaded(true);
  }, [managerOpen, settingsOpen]);

  useEffect(() => {
    const unsubscribe = window.nextshell.cloudSync.onApplied(() => {
      void refreshSyncResources();
    });
    return () => {
      unsubscribe();
    };
  }, [refreshSyncResources]);

  // Transfer status events
  useEffect(() => {
    const unsubscribe = window.nextshell.sftp.onTransferStatus((event) => {
      applyTransferEvent(event);
    });
    return () => {
      unsubscribe();
    };
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
    (connectionId: string) => openMonitorTab(connectionId, "processManager"),
    [openMonitorTab]
  );

  const handleOpenNetworkMonitor = useCallback(
    (connectionId: string) => openMonitorTab(connectionId, "networkMonitor"),
    [openMonitorTab]
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
        const result = await window.nextshell.sftp.editReadFile({ connectionId, remotePath });
        const fileName = remotePath.split("/").pop() ?? remotePath;
        const conn = connections.find((c) => c.id === connectionId);
        const serverLabel = conn?.name ?? conn?.host ?? connectionId.slice(0, 8);
        const sessionId = `editor-${crypto.randomUUID()}`;
        const session: SessionDescriptor = {
          id: sessionId,
          target: "remote",
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
          initialContent: result.content,
          syntaxMode: "auto",
          dirty: false,
          saving: false
        });
        setActiveSession(sessionId);
        setActiveConnection(connectionId);
      } catch (err) {
        message.error(`打开编辑器失败：${formatErrorMessage(err, "请检查连接状态")}`);
      }
    },
    [
      connections,
      editorTabFindByRemotePath,
      editorTabOpenTab,
      setActiveConnection,
      setActiveSession,
      upsertSession
    ]
  );

  const handleCloseMonitorTab = useCallback(
    (sessionId: string) => {
      const target = sessions.find((s) => s.id === sessionId);
      if (!target) return;
      if (target.type === "editor") {
        const editorTab = useEditorTabStore.getState().getTab(sessionId);
        if (editorTab?.dirty) {
          modal.confirm({
            title: "关闭未保存的编辑器",
            content: `「${target.title}」有未保存的修改，关闭后将丢失。确定关闭？`,
            okText: "关闭",
            cancelText: "取消",
            okButtonProps: { danger: true },
            onOk: () => {
              editorTabCloseTab(sessionId);
              removeSession(sessionId);
            }
          });
          return;
        }
        editorTabCloseTab(sessionId);
      }
      removeSession(sessionId);
    },
    [sessions, removeSession, editorTabCloseTab, modal]
  );

  const handleSelectSystemNetworkInterface = useCallback(
    (networkInterface: string) => {
      if (!activeConnectionId) return;
      void window.nextshell.monitor
        .selectSystemInterface({
          connectionId: activeConnectionId,
          networkInterface
        })
        .catch((error) => {
          message.error(`切换监控网卡失败：${formatErrorMessage(error, "请稍后重试")}`);
        });
    },
    [activeConnectionId]
  );

  const handleRetryTransferTask = useCallback(
    async (taskId: string) => {
      const failedTask = getTransferTask(taskId);
      if (!failedTask || failedTask.status !== "failed") return;
      if (failedTask.retryable === false) return;

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
    },
    [enqueueTransferTask, getTransferTask, markTransferFailed, markTransferSuccess]
  );

  const handleOpenTransferLocalFile = useCallback(async (localPath: string) => {
    const result = await window.nextshell.dialog.openPath({
      path: localPath,
      revealInFolder: true
    });
    if (!result.ok) {
      message.error(`打开所在目录失败：${formatErrorMessage(result.error, "请检查文件路径")}`);
    }
  }, []);

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
          : (await window.nextshell.connection.upsert(buildQuickConnectUpsertInput(parsed.value)))
              .id;

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
    setManagerInitialAction(undefined);
    setManagerOpen(true);
  }, []);

  const handleOpenManagerForConnection = useCallback((connectionId: string) => {
    setManagerFocusConnectionId(connectionId);
    setManagerInitialAction(undefined);
    setManagerOpen(true);
  }, []);

  /** ⌘K 面板「添加新服务器」(D31):打开管理器并直接进入新建态。 */
  const handleOpenManagerForCreate = useCallback(() => {
    setManagerFocusConnectionId(undefined);
    setManagerInitialAction("create");
    setManagerOpen(true);
  }, []);

  /**
   * 本地终端(A1)。管理器是目前唯一的入口,新标签会被弹窗盖住,所以顺手关掉它——
   * 与「双击一行直连」同一个语义:动作发生在主界面,管理器就该让开。
   */
  const handleOpenLocalTerminal = useCallback(() => {
    setManagerOpen(false);
    setManagerFocusConnectionId(undefined);
    setManagerInitialAction(undefined);
    void startLocalSession();
  }, [startLocalSession]);

  const handleLoadConnections = useCallback(() => {
    void loadConnections();
  }, [loadConnections]);

  const handleOpenSettings = useCallback(() => {
    setSettingsSection(undefined);
    setSettingsOpen(true);
  }, []);

  /** 从管理器深链到设置中心的某一节(C4)。两个弹窗不叠着开,先关管理器。 */
  const handleOpenSettingsSection = useCallback((section: SettingsSection) => {
    setManagerOpen(false);
    setManagerFocusConnectionId(undefined);
    setManagerInitialAction(undefined);
    setSettingsSection(section);
    setSettingsOpen(true);
  }, []);

  const handleTreeConnect = useCallback(
    (connectionId: string) => {
      void startSession(connectionId);
    },
    [startSession]
  );

  // 「再开一个终端」：同一主机新开一个标签,绕过双击合并器。
  const handleDuplicateSession = useCallback(
    (sessionId: string) => {
      const session = useWorkspaceStore.getState().sessions.find((item) => item.id === sessionId);
      if (!session || session.type !== "terminal") {
        return;
      }
      if (session.target === "local") {
        void startLocalSession();
        return;
      }
      if (session.connectionId) {
        void startSession(session.connectionId, { forceNewTab: true });
      }
    },
    [startLocalSession, startSession]
  );

  const handleRetryTransfer = useCallback(
    (taskId: string) => {
      void handleRetryTransferTask(taskId);
    },
    [handleRetryTransferTask]
  );

  const handleOpenTransferTask = useCallback(
    (task: TransferTask) => {
      void handleOpenTransferLocalFile(task.localPath);
    },
    [handleOpenTransferLocalFile]
  );

  const handleTransferPanelToggle = useCallback(() => {
    setTransferPanelCollapsed((collapsed) => !collapsed);
  }, []);

  const handleLiveEditPanelToggle = useCallback(() => {
    setLiveEditPanelCollapsed((collapsed) => !collapsed);
  }, []);

  const handleSetBottomTab = useCallback(
    (tab: string) => {
      if (tab === "commands" || tab === "files" || tab === "system-info") {
        setBottomTab(tab);
      }
    },
    [setBottomTab]
  );

  const isConnecting = activeConnectionId ? connectingIds.has(activeConnectionId) : false;
  const normalizedAppBackgroundImagePath = appBackgroundImagePath.trim();
  const hasAppBackgroundImage = normalizedAppBackgroundImagePath.length > 0;

  const appShellStyle = useMemo<CSSProperties | undefined>(() => {
    if (!hasAppBackgroundImage) {
      return undefined;
    }
    return {
      "--app-background-opacity": String(appBackgroundOpacity),
      // The terminal shell's translucent pad doubles as the see-through
      // terminal's dimming layer, so it tracks the user's terminal background
      // colour instead of a hard-coded navy.
      "--terminal-tint": terminalBackgroundColor
    } as CSSProperties;
  }, [appBackgroundOpacity, hasAppBackgroundImage, terminalBackgroundColor]);

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
          sessions={sessions}
          activeConnectionId={activeConnectionId}
          activeSessionId={activeSessionId}
          activeConnection={activeConnection}
          activeSession={activeSession}
          activeSessionConnection={activeSessionConnection}
          activeTerminalSession={activeTerminalSession}
          activeTerminalConnection={activeTerminalConnection}
          activeConnectionConnectedTerminalSessionId={activeConnectionConnectedTerminalSessionId}
          followTerminalSessionId={followTerminalSessionId}
          terminalSessionIds={terminalSessionIds}
          isActiveConnectionTerminalConnected={isActiveConnectionTerminalConnected}
          transferPanelCollapsed={transferPanelCollapsed}
          liveEditPanelCollapsed={liveEditPanelCollapsed}
          bottomTab={bottomTab}
          onLoadConnections={handleLoadConnections}
          onOpenManager={handleOpenManager}
          onOpenManagerForConnection={handleOpenManagerForConnection}
          onOpenSettings={handleOpenSettings}
          onActivateConnection={activateConnection}
          onTreeConnect={handleTreeConnect}
          onTitlebarQuickConnect={handleTitlebarQuickConnect}
          onOpenManagerForCreate={handleOpenManagerForCreate}
          onCloseSession={handleCloseSession}
          onReconnectSession={handleReconnectSession}
          onDuplicateSession={handleDuplicateSession}
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
          onRetryTransfer={handleRetryTransfer}
          onClearFinishedTransfers={clearFinishedTransfers}
          onOpenLocalFile={handleOpenTransferTask}
          onTransferPanelToggle={handleTransferPanelToggle}
          onLiveEditPanelToggle={handleLiveEditPanelToggle}
          onSetBottomTab={handleSetBottomTab}
        />

        {managerModalLoaded ? (
          <Suspense fallback={null}>
            <LazyConnectionManagerV2
              open={managerOpen}
              connections={connections}
              sshKeys={sshKeys}
              proxies={proxies}
              focusConnectionId={managerFocusConnectionId}
              initialAction={managerInitialAction}
              onClose={() => {
                setManagerOpen(false);
                setManagerFocusConnectionId(undefined);
                setManagerInitialAction(undefined);
              }}
              onConnectConnection={async (connectionId: string) => {
                await startSession(connectionId);
              }}
              onOpenLocalTerminal={handleOpenLocalTerminal}
              onOpenSettingsSection={handleOpenSettingsSection}
              onReloadConnections={loadConnections}
              onReloadSshKeys={loadSshKeys}
              onReloadProxies={loadProxies}
            />
          </Suspense>
        ) : null}

        {settingsModalLoaded ? (
          <Suspense fallback={null}>
            <LazySettingsCenterModal
              open={settingsOpen}
              initialSection={settingsSection}
              onClose={() => setSettingsOpen(false)}
            />
          </Suspense>
        ) : null}
      </div>
    </div>
  );
};
