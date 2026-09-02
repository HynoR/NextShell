import {
  lazy,
  memo,
  Suspense,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState
} from "react";
import { App as AntdApp, Drawer, Tabs } from "antd";
import { Group, Panel, Separator, usePanelRef } from "react-resizable-panels";
import { sessionStatusLabel } from "../utils/sessionStatus";
import {
  applySessionIndexSuffix,
  isOscTitleEligibleStatus,
  resolveSessionBaseTitle
} from "../utils/sessionTitle";
import type {
  ConnectionProfile,
  SessionDescriptor,
  SessionType,
  SshKeyProfile
} from "@nextshell/core";
import type { SessionAuthOverrideInput } from "@nextshell/shared";
import { CommandCenterPane } from "./command-center";
import { QuickConnectBar } from "./QuickConnectBar";
import { CommandInputBar } from "./CommandInputBar";
import { FileExplorerPane } from "./FileExplorerPane";
import { QuickTransferPane } from "./QuickTransferPane";
import { LiveEditPane } from "./LiveEditPane";
import { NetworkMonitorPane } from "./NetworkMonitorPane";
import { ProcessManagerPane } from "./ProcessManagerPane";
import { PingCard } from "./PingCard";
import { SystemInfoPanel } from "./SystemInfoPanel";
import { SystemStaticInfoPane } from "./SystemStaticInfoPane";
import { TerminalPane, type TerminalPaneHandle } from "./TerminalPane";
import { SessionPreviewGrid } from "./SessionPreviewGrid";
import { TransferQueuePanel } from "./TransferQueuePanel";
import { AgentActivityPanel } from "./AgentActivityPanel";
import { TraceroutePane } from "./TraceroutePane";
import { useCommandHistory } from "../hooks/useCommandHistory";
import { useDeviceKeyNotice } from "../hooks/useDeviceKeyNotice";
import { useSessionTabShortcuts, type SessionSwitcherState } from "../hooks/useSessionTabShortcuts";
import { SessionSwitcherOverlay } from "./SessionSwitcherOverlay";
import { resolveSwitcherSelection } from "./SessionSwitcherOverlay.selection";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { connectionColor } from "../utils/connectionColor";
import { recordSentCommand } from "../hooks/commandHistoryBus";
import { useAgentActivityStore } from "../store/useAgentActivityStore";
import { useEditorTabStore } from "../store/useEditorTabStore";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { useSessionOscStore } from "../store/useSessionOscStore";
import { useTransferQueueStore, type TransferTask } from "../store/useTransferQueueStore";
import { formatErrorMessage } from "../utils/errorMessage";
import type { QuickCreateConnectionInput } from "../utils/quickConnectInput";
import { promptModal } from "../utils/promptModal";
import {
  persistWorkspacePanelState,
  resolveWorkspacePanelState
} from "../utils/workspaceLayoutState";

const LazyEditorPane = lazy(() =>
  import("./EditorPane").then((module) => ({ default: module.EditorPane }))
);

const SESSION_TYPE_ICON: Record<SessionType, string> = {
  terminal: "ri-terminal-line",
  processManager: "ri-cpu-line",
  networkMonitor: "ri-global-line",
  editor: "ri-file-code-line",
  quickTransfer: "ri-folder-transfer-line"
};

const isTerminalSession = (session: SessionDescriptor): boolean =>
  !session.type || session.type === "terminal";

const LEFT_SIDEBAR_STORAGE_KEY = "nextshell.workspace.leftSidebarCollapsed";
const LEFT_SIDEBAR_WIDTH_EXPANDED = 240;
const LEFT_SIDEBAR_WIDTH_COLLAPSED = 52;
const BOTTOM_WORKBENCH_STORAGE_KEY = "nextshell.workspace.bottomWorkbenchCollapsed";
const BOTTOM_WORKBENCH_RESIZE_TARGET_MIN_SIZE = {
  coarse: 12,
  fine: 4
};

const TransferTaskBadge = () => {
  const taskCount = useTransferQueueStore((state) => state.tasks.length);

  if (taskCount === 0) {
    return null;
  }

  return (
    <div className="sidebar-collapsed-badge" title={`传输任务 ${taskCount}`}>
      {taskCount > 99 ? "99+" : taskCount}
    </div>
  );
};

const getWorkspaceLayoutStorage = (): Storage | undefined => {
  if (typeof window === "undefined") {
    return undefined;
  }

  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
};

interface SessionTabContextMenuState {
  x: number;
  y: number;
  sessionId: string;
}

// 按 sessionId 粒度订阅 dirty,避免整个标签条因编辑器输入而重渲染
const EditorTabDirtyDot = ({ sessionId }: { sessionId: string }) => {
  const dirty = useEditorTabStore((state) => state.tabs.get(sessionId)?.dirty ?? false);

  if (!dirty) {
    return null;
  }

  return <span className="tab-dirty-dot" title="有未保存的修改" aria-hidden="true" />;
};

// 同样按 sessionId 粒度订阅：Agent 注入输入期间给标签打标，让人一眼看出
// 这个终端此刻不是只有自己在敲。
const AgentControlBadge = ({ sessionId }: { sessionId: string }) => {
  const controlledBy = useAgentActivityStore((state) =>
    sessionId in state.controlledSessions
      ? (state.controlledSessions[sessionId] ?? "未知客户端")
      : null
  );

  if (controlledBy === null) {
    return null;
  }

  return (
    <span className="tab-agent-badge" title={`${controlledBy} 正在控制该会话`}>
      <i className="ri-robot-2-line" aria-hidden="true" />
    </span>
  );
};

// 按 sessionId 粒度订阅 OSC 标题,会话存活期间优先展示远端设置的标题,
// 断开后回退到连接名,避免整个标签条随 OSC 更新重渲染
const SessionTabTitle = ({ session }: { session: SessionDescriptor }) => {
  const oscTitle = useSessionOscStore((state) => state.titleBySession[session.id]);

  // OSC 标题接管时保留存储标题上的 (n) 序号,同主机多标签不至于变成同名
  const title =
    oscTitle && isOscTitleEligibleStatus(session.status)
      ? applySessionIndexSuffix(oscTitle, session.title)
      : session.title;

  return <span className="session-title">{title}</span>;
};

const SessionTabContextMenu = ({
  state,
  session,
  displayTitle,
  onClose,
  onOpenManager,
  onRename,
  onDuplicate,
  onReconnect,
  onCloseTab
}: {
  state: SessionTabContextMenuState;
  session: SessionDescriptor;
  displayTitle: string;
  onClose: () => void;
  onOpenManager: () => void;
  onRename: (session: SessionDescriptor) => void;
  onDuplicate: (session: SessionDescriptor) => void;
  onReconnect: (session: SessionDescriptor) => void;
  onCloseTab: (session: SessionDescriptor) => void;
}) => {
  const menuRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ left: number; top: number }>({
    left: state.x,
    top: state.y
  });
  const [visible, setVisible] = useState(false);

  useLayoutEffect(() => {
    const element = menuRef.current;
    if (!element) {
      return;
    }

    const { offsetWidth: width, offsetHeight: height } = element;
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const gap = 4;

    let top = state.y - height - gap;
    if (top < gap) {
      top = state.y + gap;
    }
    if (top + height > viewportHeight - gap) {
      top = viewportHeight - height - gap;
    }

    let left = state.x;
    if (left + width > viewportWidth - gap) {
      left = state.x - width;
    }
    if (left < gap) {
      left = gap;
    }

    setPos({ left, top });
    setVisible(true);
  }, [state.x, state.y]);

  useEffect(() => {
    const handleWindowMouseDown = (event: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        onClose();
      }
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        onClose();
      }
    };

    window.addEventListener("mousedown", handleWindowMouseDown);
    window.addEventListener("keydown", handleEscape);
    return () => {
      window.removeEventListener("mousedown", handleWindowMouseDown);
      window.removeEventListener("keydown", handleEscape);
    };
  }, [onClose]);

  return (
    <div
      ref={menuRef}
      className="session-tab-menu"
      style={{ left: pos.left, top: pos.top, visibility: visible ? "visible" : "hidden" }}
      onContextMenu={(event) => event.preventDefault()}
    >
      <button
        className="session-tab-menu-item"
        onClick={() => {
          onOpenManager();
          onClose();
        }}
      >
        <span className="session-tab-menu-icon">
          <i className="ri-server-line" aria-hidden="true" />
        </span>
        打开连接管理器
      </button>
      <button
        className="session-tab-menu-item"
        onClick={() => {
          onRename(session);
          onClose();
        }}
      >
        <span className="session-tab-menu-icon">
          <i className="ri-edit-line" aria-hidden="true" />
        </span>
        重命名当前标签
      </button>
      <button
        className="session-tab-menu-item"
        onClick={() => {
          onDuplicate(session);
          onClose();
        }}
      >
        <span className="session-tab-menu-icon">
          <i className="ri-add-box-line" aria-hidden="true" />
        </span>
        再开一个终端
      </button>
      {session.status === "disconnected" || session.status === "failed" ? (
        <button
          className="session-tab-menu-item"
          onClick={() => {
            onReconnect(session);
            onClose();
          }}
        >
          <span className="session-tab-menu-icon">
            <i className="ri-refresh-line" aria-hidden="true" />
          </span>
          重新连接
        </button>
      ) : null}
      <button
        className="session-tab-menu-item"
        onClick={() => {
          onCloseTab(session);
          onClose();
        }}
      >
        <span className="session-tab-menu-icon">
          <i className="ri-close-line" aria-hidden="true" />
        </span>
        关闭标签
      </button>
      <div className="session-tab-menu-hint" title={displayTitle}>
        {displayTitle}
      </div>
    </div>
  );
};

interface WorkspaceLayoutProps {
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  sessions: SessionDescriptor[];
  activeConnectionId?: string;
  activeSessionId?: string;
  activeConnection?: ConnectionProfile;
  activeSession?: SessionDescriptor;
  activeSessionConnection?: ConnectionProfile;
  activeTerminalSession?: SessionDescriptor;
  activeTerminalConnection?: ConnectionProfile;
  activeConnectionConnectedTerminalSessionId?: string;
  followTerminalSessionId?: string;
  terminalSessionIds: string[];
  isActiveConnectionTerminalConnected: boolean;
  transferPanelCollapsed: boolean;
  liveEditPanelCollapsed: boolean;
  bottomTab: string;
  onLoadConnections: () => void;
  onOpenManager: () => void;
  /**
   * 打开管理器并定位到某条连接(A6)。原来的调用方是侧栏连接树,树在 7b2bd8a 删掉之后
   * 这条链路一直断着;标签右键菜单是它现在唯一的自然入口——右键的就是"这一台机器"。
   */
  onOpenManagerForConnection: (connectionId: string) => void;
  onOpenSettings: () => void;
  onActivateConnection: (connectionId: string) => void;
  onTreeConnect: (connectionId: string) => void;
  onTitlebarQuickConnect: (raw: string) => Promise<boolean>;
  onTitlebarQuickCreateConnection: (input: QuickCreateConnectionInput) => Promise<boolean>;
  onCloseSession: (sessionId: string) => void;
  onReconnectSession: (sessionId: string) => void;
  onDuplicateSession: (sessionId: string) => void;
  onRenameSession: (sessionId: string, title: string) => void;
  onOpenProcessManager: (connectionId: string) => void;
  onOpenNetworkMonitor: (connectionId: string) => void;
  onOpenQuickTransfer: () => void;
  onCloseMonitorTab: (sessionId: string) => void;
  onOpenEditorTab: (connectionId: string, remotePath: string) => Promise<void>;
  onRetrySessionAuth: (
    sessionId: string,
    authOverride: SessionAuthOverrideInput
  ) => Promise<{ ok: true } | { ok: false; authRequired: boolean; reason: string }>;
  onSetActiveSession: (sessionId?: string) => void;
  onSetActiveConnection: (connectionId?: string) => void;
  onReorderSession: (sourceId: string, targetId: string) => void;
  onSelectNetworkInterface: (networkInterface: string) => void;
  onRetryTransfer: (taskId: string) => void;
  onClearFinishedTransfers: () => void;
  onOpenLocalFile: (task: TransferTask) => void;
  onTransferPanelToggle: () => void;
  onLiveEditPanelToggle: () => void;
  onSetBottomTab: (tab: string) => void;
}

const WorkspaceLayoutComponent = ({
  connections,
  sshKeys,
  sessions,
  activeConnectionId,
  activeSessionId,
  activeConnection,
  activeSession,
  activeSessionConnection,
  activeTerminalSession,
  activeTerminalConnection,
  activeConnectionConnectedTerminalSessionId,
  followTerminalSessionId,
  terminalSessionIds,
  isActiveConnectionTerminalConnected,
  transferPanelCollapsed,
  liveEditPanelCollapsed,
  bottomTab,
  onLoadConnections,
  onOpenManager,
  onOpenManagerForConnection,
  onOpenSettings,
  onActivateConnection,
  onTreeConnect,
  onTitlebarQuickConnect,
  onTitlebarQuickCreateConnection,
  onCloseSession,
  onReconnectSession,
  onDuplicateSession,
  onRenameSession,
  onOpenProcessManager,
  onOpenNetworkMonitor,
  onOpenQuickTransfer,
  onCloseMonitorTab,
  onOpenEditorTab,
  onRetrySessionAuth,
  onSetActiveSession,
  onSetActiveConnection,
  onReorderSession,
  onSelectNetworkInterface,
  onRetryTransfer,
  onClearFinishedTransfers,
  onOpenLocalFile,
  onTransferPanelToggle,
  onLiveEditPanelToggle,
  onSetBottomTab
}: WorkspaceLayoutProps) => {
  const { message, modal } = AntdApp.useApp();
  const windowPreferences = usePreferencesStore((state) => state.preferences.window);
  const activeOscTitle = useSessionOscStore((state) =>
    activeSession ? state.titleBySession[activeSession.id] : undefined
  );
  const [draggingSessionId, setDraggingSessionId] = useState<string>();
  const [leftSidebarCollapsed, setLeftSidebarCollapsed] = useState(() =>
    resolveWorkspacePanelState(
      getWorkspaceLayoutStorage(),
      LEFT_SIDEBAR_STORAGE_KEY,
      windowPreferences.leftSidebarDefaultCollapsed
    )
  );
  const [bottomCollapsed, setBottomCollapsed] = useState(() =>
    resolveWorkspacePanelState(
      getWorkspaceLayoutStorage(),
      BOTTOM_WORKBENCH_STORAGE_KEY,
      windowPreferences.bottomWorkbenchDefaultCollapsed
    )
  );
  const [terminalSearchMode, setTerminalSearchMode] = useState(false);
  const [terminalSearchTerm, setTerminalSearchTerm] = useState("");
  const [addressCopied, setAddressCopied] = useState(false);
  const [tracerouteOpen, setTracerouteOpen] = useState(false);
  const [updateReleaseUrl, setUpdateReleaseUrl] = useState<string | null>(null);
  const [sessionContextMenu, setSessionContextMenu] = useState<SessionTabContextMenuState | null>(
    null
  );
  const bottomPanelRef = usePanelRef();
  const lastExpandedSizeRef = useRef("32%");
  const terminalPaneRef = useRef<TerminalPaneHandle | null>(null);
  const resizeFitRafRef = useRef(0);
  const commandHistory = useCommandHistory();
  useDeviceKeyNotice();
  const activeTerminalSessionId = activeTerminalSession?.id;
  const activeTerminalSessionStatus = activeTerminalSession?.status;

  const handleExecuteCommand = useCallback(
    (command: string, appendCr: boolean) => {
      if (!activeTerminalSessionId || activeTerminalSessionStatus !== "connected") {
        return;
      }
      window.nextshell.session
        .write({ sessionId: activeTerminalSessionId, data: appendCr ? `${command}\r` : command })
        .catch(() => message.error("发送命令失败"));
      recordSentCommand(activeTerminalSessionId, command);
    },
    [activeTerminalSessionId, activeTerminalSessionStatus, message]
  );

  const handleExecuteAllCommands = useCallback(
    (command: string, appendCr: boolean) => {
      const data = appendCr ? `${command}\r` : command;
      const writes = sessions
        .filter((session) => isTerminalSession(session) && session.status === "connected")
        .map((session) => {
          recordSentCommand(session.id, command);
          return window.nextshell.session.write({ sessionId: session.id, data });
        });
      void Promise.all(writes).catch(() => message.error("发送命令失败"));
    },
    [message, sessions]
  );

  const headerSessionText = useMemo(() => {
    if (!activeSession) return "未选择会话";
    const baseLabel = resolveSessionBaseTitle(
      activeSession.title,
      activeSessionConnection,
      isOscTitleEligibleStatus(activeSession.status) ? activeOscTitle : undefined
    );
    return `${sessionStatusLabel(activeSession.status)} ${baseLabel}`;
  }, [activeSession, activeSessionConnection, activeOscTitle]);

  const headerSessionClass = activeSession?.status ?? "disconnected";

  const contextMenuSession = useMemo(
    () =>
      sessionContextMenu
        ? sessions.find(
            (session) => session.id === sessionContextMenu.sessionId && isTerminalSession(session)
          )
        : undefined,
    [sessionContextMenu, sessions]
  );
  const contextMenuSessionTitle = useMemo(() => contextMenuSession?.title, [contextMenuSession]);

  useEffect(() => {
    if (sessionContextMenu && !contextMenuSession) {
      setSessionContextMenu(null);
    }
  }, [contextMenuSession, sessionContextMenu]);

  const handlePromptRenameSession = useCallback(
    async (session: SessionDescriptor) => {
      const title = await promptModal(modal, "会话标题", undefined, session.title);
      if (title) {
        onRenameSession(session.id, title);
      }
    },
    [modal, onRenameSession]
  );

  const handleTerminalSearchTermChange = useCallback((value: string) => {
    setTerminalSearchTerm(value);
    terminalPaneRef.current?.setSearchTerm(value);
  }, []);

  const handleTerminalSearchNext = useCallback(() => {
    terminalPaneRef.current?.setSearchTerm(terminalSearchTerm);
    terminalPaneRef.current?.findNext();
  }, [terminalSearchTerm]);

  const handleTerminalSearchPrevious = useCallback(() => {
    terminalPaneRef.current?.setSearchTerm(terminalSearchTerm);
    terminalPaneRef.current?.findPrevious();
  }, [terminalSearchTerm]);

  const handleRequestTerminalSearchMode = useCallback(() => {
    setTerminalSearchMode(true);
  }, []);

  const handleOpenProcessManagerFromMonitor = useCallback(() => {
    if (!activeConnectionId) return;
    onOpenProcessManager(activeConnectionId);
  }, [activeConnectionId, onOpenProcessManager]);

  const handleOpenNetworkMonitorFromMonitor = useCallback(() => {
    if (!activeConnectionId) return;
    onOpenNetworkMonitor(activeConnectionId);
  }, [activeConnectionId, onOpenNetworkMonitor]);

  const sidebarAddress = activeSessionConnection
    ? `${activeSessionConnection.host}:${activeSessionConnection.port}`
    : null;

  const handleCopyAddress = useCallback(() => {
    if (!sidebarAddress) return;
    navigator.clipboard
      .writeText(sidebarAddress)
      .then(() => {
        setAddressCopied(true);
        setTimeout(() => setAddressCopied(false), 1500);
      })
      .catch(() => undefined);
  }, [sidebarAddress]);

  const handleSessionTabContextMenu = useCallback(
    (event: React.MouseEvent<HTMLElement>, session: SessionDescriptor) => {
      if (!isTerminalSession(session)) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      onSetActiveSession(session.id);
      onSetActiveConnection(session.connectionId);
      setSessionContextMenu({
        x: event.clientX,
        y: event.clientY,
        sessionId: session.id
      });
    },
    [onSetActiveConnection, onSetActiveSession]
  );

  const sessionTabElementsRef = useRef(new Map<string, HTMLDivElement>());

  const connectionById = useMemo(
    () => new Map(connections.map((connection) => [connection.id, connection])),
    [connections]
  );

  // 监视网格:同屏只读并排最近使用的至多 4 个终端会话
  const [previewGridOpen, setPreviewGridOpen] = useState(false);
  const sessionMruIds = useWorkspaceStore((state) => state.sessionMruIds);
  const previewSessions = useMemo(() => {
    if (!previewGridOpen) {
      return [];
    }
    const rank = new Map(sessionMruIds.map((id, index) => [id, index]));
    return sessions
      .filter((session) => isTerminalSession(session))
      .sort(
        (a, b) =>
          (rank.get(a.id) ?? Number.POSITIVE_INFINITY) -
          (rank.get(b.id) ?? Number.POSITIVE_INFINITY)
      )
      .slice(0, 4);
  }, [previewGridOpen, sessionMruIds, sessions]);

  const activateSessionTab = useCallback(
    (session: SessionDescriptor) => {
      setSessionContextMenu(null);
      onSetActiveSession(session.id);
      if (session.connectionId) {
        onSetActiveConnection(session.connectionId);
      }
    },
    [onSetActiveConnection, onSetActiveSession]
  );

  const closeSessionTab = useCallback(
    (session: SessionDescriptor) => {
      if (isTerminalSession(session)) {
        void onCloseSession(session.id);
      } else {
        onCloseMonitorTab(session.id);
      }
    },
    [onCloseMonitorTab, onCloseSession]
  );

  // Ctrl+Tab 切换器:状态机只发布「本轮快照 + 选中项」,这里负责画面。
  const [switcherState, setSwitcherState] = useState<SessionSwitcherState | null>(null);

  // 全局标签快捷键。getter 走 store 的 getState,监听器不随 props 重建。
  const { closeSwitcher } = useSessionTabShortcuts({
    getSessionIds: useCallback(
      () => useWorkspaceStore.getState().sessions.map((session) => session.id),
      []
    ),
    getMruSessionIds: useCallback(() => {
      const { sessions: storeSessions, sessionMruIds } = useWorkspaceStore.getState();
      const alive = new Set(storeSessions.map((session) => session.id));
      return sessionMruIds.filter((id) => alive.has(id));
    }, []),
    getActiveSessionId: useCallback(() => useWorkspaceStore.getState().activeSessionId, []),
    activateSession: useCallback(
      (sessionId: string) => {
        const session = useWorkspaceStore.getState().sessions.find((item) => item.id === sessionId);
        if (session) {
          activateSessionTab(session);
        }
      },
      [activateSessionTab]
    ),
    closeActiveSession: useCallback(() => {
      const { sessions: storeSessions, activeSessionId: activeId } = useWorkspaceStore.getState();
      const session = storeSessions.find((item) => item.id === activeId);
      if (session) {
        closeSessionTab(session);
      }
    }, [closeSessionTab]),
    duplicateActiveSession: useCallback(() => {
      const activeId = useWorkspaceStore.getState().activeSessionId;
      if (activeId) {
        onDuplicateSession(activeId);
      }
    }, [onDuplicateSession]),
    // useState 的 setter 身份稳定,而且 hook 是通过 ref 读回调的,不会因此重建监听。
    onSwitcherStateChange: setSwitcherState
  });

  // 快照里的 id 映射回会话;循环期间被关掉的标签直接从面板里消失,选中项跟着夹。
  const switcherSelection = useMemo(() => {
    if (!switcherState) {
      return null;
    }
    const sessionById = new Map(sessions.map((session) => [session.id, session]));
    return resolveSwitcherSelection(switcherState.ids, switcherState.index, (sessionId) =>
      sessionById.get(sessionId)
    );
  }, [sessions, switcherState]);

  // 鼠标落定:切换 + 关面板,同时把状态机里那一轮作废,免得随后的 Ctrl keyup 再切一次。
  const handleSwitcherSelect = useCallback(
    (sessionId: string) => {
      const session = useWorkspaceStore.getState().sessions.find((item) => item.id === sessionId);
      setSwitcherState(null);
      closeSwitcher();
      if (session) {
        activateSessionTab(session);
      }
    },
    [activateSessionTab, closeSwitcher]
  );

  const handleSwitcherCancel = useCallback(() => {
    setSwitcherState(null);
    closeSwitcher();
  }, [closeSwitcher]);

  // roving tabindex:方向键/Home/End 移动焦点并切换激活标签;仅在标签上获得焦点时触发,不影响终端按键
  const handleSessionTabKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLElement>, session: SessionDescriptor) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        activateSessionTab(session);
        return;
      }

      const navigationKeys = ["ArrowLeft", "ArrowRight", "Home", "End"];
      if (!navigationKeys.includes(event.key) || event.ctrlKey || event.metaKey || event.altKey) {
        return;
      }

      event.preventDefault();
      if (sessions.length === 0) return;
      const currentIndex = sessions.findIndex((item) => item.id === session.id);
      if (currentIndex < 0) return;

      let nextIndex: number;
      if (event.key === "Home") {
        nextIndex = 0;
      } else if (event.key === "End") {
        nextIndex = sessions.length - 1;
      } else {
        const delta = event.key === "ArrowRight" ? 1 : -1;
        nextIndex = (currentIndex + delta + sessions.length) % sessions.length;
      }

      const nextSession = sessions[nextIndex];
      if (!nextSession || nextSession.id === session.id) return;
      activateSessionTab(nextSession);
      sessionTabElementsRef.current.get(nextSession.id)?.focus();
    },
    [activateSessionTab, sessions]
  );

  const handleSessionTabAuxClick = useCallback(
    (event: React.MouseEvent<HTMLElement>, session: SessionDescriptor) => {
      if (event.button !== 1) return;
      event.preventDefault();
      event.stopPropagation();
      closeSessionTab(session);
    },
    [closeSessionTab]
  );

  // 激活标签变化时滚动到可见区域(标签条容器 overflow-x: auto)
  useEffect(() => {
    if (!activeSessionId) return;
    sessionTabElementsRef.current.get(activeSessionId)?.scrollIntoView({ inline: "nearest" });
  }, [activeSessionId]);

  useEffect(() => {
    let disposed = false;
    void (async () => {
      try {
        const result = await window.nextshell.about.checkUpdate();
        if (disposed) return;
        setUpdateReleaseUrl(result.hasUpdate && result.releaseUrl ? result.releaseUrl : null);
      } catch {
        if (!disposed) {
          setUpdateReleaseUrl(null);
        }
      }
    })();
    return () => {
      disposed = true;
    };
  }, []);

  const handleOpenReleasePage = useCallback(async () => {
    if (!updateReleaseUrl) return;
    const result = await window.nextshell.dialog.openPath({
      path: updateReleaseUrl,
      revealInFolder: false
    });
    if (!result.ok) {
      void message.error(`打开链接失败：${formatErrorMessage(result.error, "请稍后重试")}`);
    }
  }, [updateReleaseUrl]);

  useEffect(() => {
    const panel = bottomPanelRef.current;
    if (!panel) return;
    if (bottomCollapsed && !panel.isCollapsed()) {
      panel.resize("4%");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    cancelAnimationFrame(resizeFitRafRef.current);
    resizeFitRafRef.current = requestAnimationFrame(() => {
      terminalPaneRef.current?.fit();
    });
  }, [bottomCollapsed]);

  const syncBottomCollapsed = useCallback(
    (panelSize?: unknown, _panelId?: string | number, prevPanelSize?: unknown) => {
      if (prevPanelSize === undefined) return;
      const collapsed = bottomPanelRef.current?.isCollapsed() ?? false;
      setBottomCollapsed(collapsed);
      if (!collapsed && panelSize && typeof panelSize === "object" && "asPercentage" in panelSize) {
        const pct = (panelSize as { asPercentage: number }).asPercentage;
        if (pct > 5) {
          lastExpandedSizeRef.current = `${pct}%`;
        }
      }
      persistWorkspacePanelState(
        getWorkspaceLayoutStorage(),
        BOTTOM_WORKBENCH_STORAGE_KEY,
        collapsed
      );
      cancelAnimationFrame(resizeFitRafRef.current);
      resizeFitRafRef.current = requestAnimationFrame(() => {
        terminalPaneRef.current?.fit();
      });
    },
    [bottomPanelRef]
  );

  const setLeftSidebarCollapsedWithPersistence = useCallback((collapsed: boolean) => {
    persistWorkspacePanelState(getWorkspaceLayoutStorage(), LEFT_SIDEBAR_STORAGE_KEY, collapsed);
    setLeftSidebarCollapsed(collapsed);
  }, []);

  const handleToggleLeftSidebar = useCallback(() => {
    setLeftSidebarCollapsedWithPersistence(!leftSidebarCollapsed);
  }, [leftSidebarCollapsed, setLeftSidebarCollapsedWithPersistence]);

  const handleToggleBottomWorkbench = useCallback(() => {
    const panel = bottomPanelRef.current;
    if (!panel) return;
    if (panel.isCollapsed()) {
      panel.resize(lastExpandedSizeRef.current);
    } else {
      const size = panel.getSize();
      if (size && size.asPercentage > 5) {
        lastExpandedSizeRef.current = `${size.asPercentage}%`;
      }
      panel.resize("4%");
    }
  }, [bottomPanelRef]);

  const bottomTabItems = useMemo(() => {
    const base: Array<{
      key: string;
      label: string;
      children: React.ReactNode;
    }> = [
      {
        key: "files",
        label: "SFTP",
        children: (
          <FileExplorerPane
            connection={activeConnection}
            connected={isActiveConnectionTerminalConnected}
            followSessionId={followTerminalSessionId}
            active={bottomTab === "files"}
            onOpenSettings={onOpenSettings}
            onOpenEditorTab={onOpenEditorTab}
          />
        )
      },
      {
        key: "commands",
        label: "命令库",
        children: (
          <CommandCenterPane
            connected={isActiveConnectionTerminalConnected}
            sessions={sessions}
            activeTerminalSession={activeTerminalSession}
            onExecuteCommand={handleExecuteCommand}
            onExecuteAllCommands={handleExecuteAllCommands}
          />
        )
      },
      {
        key: "system-info",
        label: "系统信息",
        children: (
          <SystemStaticInfoPane
            connection={activeConnection}
            connected={isActiveConnectionTerminalConnected}
            active={bottomTab === "system-info"}
            connectedTerminalSessionId={activeConnectionConnectedTerminalSessionId}
            onOpenSettings={onOpenSettings}
          />
        )
      }
    ];
    return base;
  }, [
    activeConnection,
    isActiveConnectionTerminalConnected,
    followTerminalSessionId,
    bottomTab,
    onOpenSettings,
    onOpenEditorTab,
    connections,
    sessions,
    handleExecuteCommand,
    activeConnectionConnectedTerminalSessionId
  ]);

  const handleOpenTraceroute = useCallback(() => {
    if (activeConnection?.host) {
      setTracerouteOpen(true);
    }
  }, [activeConnection?.host]);

  const handleCloseTraceroute = useCallback(() => {
    setTracerouteOpen(false);
    void window.nextshell.traceroute.stop();
  }, []);

  return (
    <div className="h-screen flex flex-col overflow-hidden">
      <header className="shell-header">
        <div className="titlebar-brand" />
        <div className="titlebar-center">
          <QuickConnectBar
            connections={connections}
            sshKeys={sshKeys}
            sessions={sessions}
            onConnect={(connectionId) => void onTreeConnect(connectionId)}
            onQuickConnectInput={onTitlebarQuickConnect}
            onQuickCreateConnection={onTitlebarQuickCreateConnection}
          />
        </div>
        <div className="header-actions">
          {updateReleaseUrl ? (
            <>
              <a
                className="hdr-btn update"
                href={updateReleaseUrl}
                target="_blank"
                rel="noopener noreferrer"
                title="有可用更新，点击查看发布说明"
                onClick={(event) => {
                  event.preventDefault();
                  void handleOpenReleasePage();
                }}
              >
                <i className="ri-download-cloud-line" aria-hidden="true" /> 有更新
              </a>
              <span className="hdr-sep" />
            </>
          ) : null}
          <button className="hdr-btn" onClick={onOpenManager} title="管理连接">
            <i className="ri-links-line" aria-hidden="true" />
            服务器
          </button>
          <button className="hdr-btn" onClick={onOpenQuickTransfer} title="文件快传">
            <i className="ri-folder-transfer-line" aria-hidden="true" />
            快传
          </button>
          <button className="hdr-btn" onClick={onOpenSettings} title="打开设置中心">
            <i className="ri-settings-3-line" aria-hidden="true" />
            设置
          </button>
        </div>
      </header>

      <main className="flex flex-1 min-w-0 min-h-0 overflow-hidden">
        <aside
          className="workspace-left-sidebar flex-shrink-0 flex flex-col h-full overflow-hidden transition-[width] duration-200 ease-out bg-[var(--bg-surface)] border-r border-[var(--border)]"
          style={{
            width: leftSidebarCollapsed ? LEFT_SIDEBAR_WIDTH_COLLAPSED : LEFT_SIDEBAR_WIDTH_EXPANDED
          }}
        >
          {leftSidebarCollapsed ? (
            <div className="sidebar-collapsed-shell w-full h-full flex flex-col items-center gap-2">
              <button
                type="button"
                className="sidebar-collapsed-toggle"
                onClick={handleToggleLeftSidebar}
                title="展开侧栏"
              >
                <i className="ri-layout-left-line" aria-hidden="true" />
              </button>
              <div
                className={`sidebar-collapsed-status ${headerSessionClass}`}
                title={headerSessionText}
              >
                <span className="sidebar-session-dot" />
              </div>
              <TransferTaskBadge />
            </div>
          ) : (
            <div className="w-full h-full flex flex-col overflow-hidden">
              <div className={`sidebar-session-card ${headerSessionClass}`}>
                <div className="sidebar-session-row">
                  <span className="sidebar-session-dot" />
                  <span className="sidebar-session-status">
                    {sessionStatusLabel(activeSession?.status ?? "disconnected")}
                  </span>
                  <button
                    type="button"
                    className="sidebar-refresh-btn"
                    onClick={() => void onLoadConnections()}
                    title="刷新连接列表"
                  >
                    <i className="ri-refresh-line" aria-hidden="true" />
                  </button>
                  <button
                    type="button"
                    className="sidebar-collapse-btn"
                    onClick={handleToggleLeftSidebar}
                    title="折叠侧栏"
                  >
                    <i className="ri-layout-left-line" aria-hidden="true" />
                  </button>
                </div>
                {sidebarAddress ? (
                  <button
                    type="button"
                    className="sidebar-session-addr"
                    title={addressCopied ? "已复制" : "点击复制地址"}
                    onClick={handleCopyAddress}
                  >
                    {addressCopied ? (
                      <>
                        <i className="ri-check-line" aria-hidden="true" /> 已复制
                      </>
                    ) : (
                      <>
                        <i className="ri-clipboard-line" aria-hidden="true" /> {sidebarAddress}
                      </>
                    )}
                  </button>
                ) : (
                  <span className="sidebar-session-addr empty">未选择服务器</span>
                )}
              </div>
              {activeConnection?.monitorSession ? (
                <SystemInfoPanel
                  monitorSessionEnabled
                  hasVisibleTerminal={isActiveConnectionTerminalConnected}
                  onSelectNetworkInterface={onSelectNetworkInterface}
                  onOpenProcessManager={handleOpenProcessManagerFromMonitor}
                  onOpenNetworkMonitor={handleOpenNetworkMonitorFromMonitor}
                  monitorActionsDisabled={
                    !activeConnectionId || !isActiveConnectionTerminalConnected
                  }
                />
              ) : null}
              <PingCard host={activeConnection?.host} onClick={handleOpenTraceroute} />
              <Drawer
                title="路由追踪"
                open={tracerouteOpen}
                width={720}
                destroyOnHidden
                onClose={handleCloseTraceroute}
              >
                <TraceroutePane
                  connection={activeConnection}
                  connected={isActiveConnectionTerminalConnected}
                />
              </Drawer>
              <TransferQueuePanel
                collapsed={transferPanelCollapsed}
                onToggle={onTransferPanelToggle}
                onRetry={(taskId) => void onRetryTransfer(taskId)}
                onCancel={(taskId) => void window.nextshell.sftp.cancelTransfer({ taskId })}
                onClearFinished={onClearFinishedTransfers}
                onOpenLocalFile={(task) => {
                  if (task.direction === "download" && task.status === "success") {
                    onOpenLocalFile(task);
                  }
                }}
              />
              <AgentActivityPanel />
              <LiveEditPane
                connections={connections}
                active={!liveEditPanelCollapsed}
                collapsed={liveEditPanelCollapsed}
                onToggle={onLiveEditPanelToggle}
              />
            </div>
          )}
        </aside>
        <section className="flex-1 min-w-0 min-h-0 flex flex-col overflow-hidden">
          <Group
            orientation="vertical"
            className="w-full h-full min-w-0 min-h-0"
            resizeTargetMinimumSize={BOTTOM_WORKBENCH_RESIZE_TARGET_MIN_SIZE}
          >
            <Panel defaultSize="68%" minSize="38%">
              <div className="terminal-shell">
                <div className="session-tabs" role="tablist" aria-orientation="horizontal">
                  {sessions.map((session) => {
                    const isTerminal = isTerminalSession(session);
                    const iconClass = SESSION_TYPE_ICON[session.type ?? "terminal"];
                    const sessionConnection = session.connectionId
                      ? connectionById.get(session.connectionId)
                      : undefined;
                    const tabTooltip = sessionConnection
                      ? `${
                          sessionConnection.username.trim() ? `${sessionConnection.username}@` : ""
                        }${sessionConnection.host}:${sessionConnection.port}`
                      : session.target === "local"
                        ? "本地终端"
                        : undefined;
                    return (
                      <div
                        key={session.id}
                        ref={(element) => {
                          if (element) {
                            sessionTabElementsRef.current.set(session.id, element);
                          } else {
                            sessionTabElementsRef.current.delete(session.id);
                          }
                        }}
                        role="tab"
                        tabIndex={session.id === activeSessionId ? 0 : -1}
                        aria-selected={session.id === activeSessionId}
                        className={[
                          "session-tab",
                          session.id === activeSessionId ? "active" : "",
                          session.id === draggingSessionId ? "dragging" : ""
                        ]
                          .filter(Boolean)
                          .join(" ")}
                        onClick={() => activateSessionTab(session)}
                        onKeyDown={(event) => handleSessionTabKeyDown(event, session)}
                        onAuxClick={(event) => handleSessionTabAuxClick(event, session)}
                        onContextMenu={(event) => handleSessionTabContextMenu(event, session)}
                        draggable={isTerminal}
                        onDragStart={() => {
                          if (isTerminal) setDraggingSessionId(session.id);
                        }}
                        onDragEnd={() => setDraggingSessionId(undefined)}
                        onDragOver={(event) => event.preventDefault()}
                        onDrop={(event) => {
                          event.preventDefault();
                          if (!draggingSessionId) return;
                          onReorderSession(draggingSessionId, session.id);
                          setDraggingSessionId(undefined);
                        }}
                        title={tabTooltip}
                      >
                        {session.connectionId ? (
                          <span
                            className="tab-connection-dot"
                            style={{ background: connectionColor(session.connectionId) }}
                            aria-hidden="true"
                          />
                        ) : null}
                        <i className={`tab-type-icon ${iconClass}`} aria-hidden="true" />
                        <SessionTabTitle session={session} />
                        <AgentControlBadge sessionId={session.id} />
                        {session.type === "editor" ? (
                          <EditorTabDirtyDot sessionId={session.id} />
                        ) : null}
                        {isTerminal &&
                        (session.status === "disconnected" || session.status === "failed") ? (
                          <button
                            type="button"
                            className="tab-action tab-reconnect"
                            aria-label={`重新连接 ${session.title}`}
                            onClick={(event) => {
                              event.stopPropagation();
                              void onReconnectSession(session.id);
                            }}
                          >
                            <i className="ri-refresh-line" aria-hidden="true" />
                          </button>
                        ) : null}
                        <button
                          type="button"
                          className="tab-action tab-close"
                          aria-label={`关闭 ${session.title}`}
                          onClick={(event) => {
                            event.stopPropagation();
                            closeSessionTab(session);
                          }}
                        >
                          <i className="ri-close-line" aria-hidden="true" />
                        </button>
                      </div>
                    );
                  })}
                  {sessions.some((session) => isTerminalSession(session)) ? (
                    <button
                      type="button"
                      className={`session-tabs-grid-btn${previewGridOpen ? " active" : ""}`}
                      title="监视网格：同屏预览最近的多个会话"
                      aria-label="监视网格"
                      aria-pressed={previewGridOpen}
                      onClick={() => setPreviewGridOpen((open) => !open)}
                    >
                      <i className="ri-layout-grid-line" aria-hidden="true" />
                    </button>
                  ) : null}
                </div>
                {previewGridOpen ? (
                  <SessionPreviewGrid
                    sessions={previewSessions}
                    activeSessionId={activeSessionId}
                    onActivateSession={(sessionId) => {
                      const session = sessions.find((item) => item.id === sessionId);
                      if (session) {
                        activateSessionTab(session);
                      }
                      setPreviewGridOpen(false);
                    }}
                    onClose={() => setPreviewGridOpen(false)}
                  />
                ) : null}
                {switcherSelection ? (
                  <SessionSwitcherOverlay
                    sessions={switcherSelection.entries}
                    selectedIndex={switcherSelection.selectedIndex}
                    connectionById={connectionById}
                    onSelect={handleSwitcherSelect}
                    onCancel={handleSwitcherCancel}
                  />
                ) : null}
                {sessionContextMenu && contextMenuSession ? (
                  <SessionTabContextMenu
                    state={sessionContextMenu}
                    session={contextMenuSession}
                    displayTitle={contextMenuSessionTitle ?? contextMenuSession.title}
                    onClose={() => setSessionContextMenu(null)}
                    // 右键的是「这个标签」，所以顺手定位到它对应的连接；本地终端这类
                    // 没有连接的标签退回普通打开。
                    onOpenManager={() =>
                      contextMenuSession.connectionId
                        ? onOpenManagerForConnection(contextMenuSession.connectionId)
                        : onOpenManager()
                    }
                    onRename={(session) => {
                      void handlePromptRenameSession(session);
                    }}
                    onDuplicate={(session) => onDuplicateSession(session.id)}
                    onReconnect={(session) => void onReconnectSession(session.id)}
                    onCloseTab={closeSessionTab}
                  />
                ) : null}
                <div
                  className={
                    activeSession?.type === "processManager" ||
                    activeSession?.type === "networkMonitor" ||
                    activeSession?.type === "editor" ||
                    activeSession?.type === "quickTransfer"
                      ? "hidden"
                      : "flex-1 min-h-0 flex flex-col"
                  }
                >
                  <TerminalPane
                    ref={terminalPaneRef}
                    session={activeTerminalSession}
                    connection={activeTerminalConnection}
                    sessionIds={terminalSessionIds}
                    onReconnectSession={onReconnectSession}
                    onRetrySessionAuth={onRetrySessionAuth}
                    onRequestSearchMode={handleRequestTerminalSearchMode}
                  />
                  <CommandInputBar
                    session={activeTerminalSession}
                    commandHistory={commandHistory}
                    searchMode={terminalSearchMode}
                    onSearchModeChange={setTerminalSearchMode}
                    terminalSearchTerm={terminalSearchTerm}
                    onTerminalSearchTermChange={handleTerminalSearchTermChange}
                    onTerminalSearchNext={handleTerminalSearchNext}
                    onTerminalSearchPrevious={handleTerminalSearchPrevious}
                  />
                </div>
                {activeSession?.type === "processManager" ? (
                  <ProcessManagerPane key={activeSession.id} session={activeSession} />
                ) : null}
                {activeSession?.type === "networkMonitor" ? (
                  <NetworkMonitorPane key={activeSession.id} session={activeSession} />
                ) : null}
                {sessions
                  .filter((session) => session.type === "editor")
                  .map((session) => (
                    <div
                      key={session.id}
                      className={
                        session.id === activeSessionId ? "flex-1 min-h-0 flex flex-col" : "hidden"
                      }
                    >
                      <Suspense
                        fallback={
                          <div className="flex-1 flex items-center justify-center text-[var(--t3)]">
                            编辑器加载中...
                          </div>
                        }
                      >
                        <LazyEditorPane session={session} />
                      </Suspense>
                    </div>
                  ))}
                {activeSession?.type === "quickTransfer" ? (
                  <QuickTransferPane
                    key={activeSession.id}
                    connections={connections}
                    sessions={sessions}
                  />
                ) : null}
              </div>
            </Panel>
            <Separator className="panel-resize-handle vertical" />
            <Panel
              panelRef={bottomPanelRef}
              defaultSize={bottomCollapsed ? "4%" : "32%"}
              minSize="16%"
              collapsible
              collapsedSize="4%"
              onResize={syncBottomCollapsed}
            >
              <div className="bottom-workbench">
                <Tabs
                  activeKey={bottomTab}
                  onChange={onSetBottomTab}
                  tabBarExtraContent={{
                    right: (
                      <button
                        type="button"
                        className="bottom-collapse-btn"
                        title={bottomCollapsed ? "展开面板" : "折叠面板"}
                        onClick={handleToggleBottomWorkbench}
                      >
                        <i
                          className={
                            bottomCollapsed ? "ri-arrow-up-s-line" : "ri-arrow-down-s-line"
                          }
                          aria-hidden="true"
                        />
                      </button>
                    )
                  }}
                  items={bottomTabItems}
                />
              </div>
            </Panel>
          </Group>
        </section>
      </main>
    </div>
  );
};

export const WorkspaceLayout = memo(WorkspaceLayoutComponent);
