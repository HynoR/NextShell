import { useCallback, useMemo, useRef, useState } from "react";
import { message, Tabs } from "antd";
import { Group, Panel, Separator, usePanelRef } from "react-resizable-panels";
import type { ConnectionProfile, SessionDescriptor, SessionType, SshKeyProfile } from "@nextshell/core";
import type { SessionAuthOverrideInput } from "@nextshell/shared";
import { CommandCenterPane } from "./CommandCenterPane";
import { CommandInputBar } from "./CommandInputBar";
import { ConnectionTreePanel } from "./ConnectionTreePanel";
import { FileExplorerPane } from "./FileExplorerPane";
import { LiveEditPane } from "./LiveEditPane";
import { NetworkMonitorPane } from "./NetworkMonitorPane";
import { ProcessManagerPane } from "./ProcessManagerPane";
import { SessionAuthRetryModal } from "./SessionAuthRetryModal";
import { SystemInfoPanel } from "./SystemInfoPanel";
import { TerminalPane, type TerminalPaneHandle } from "./TerminalPane";
import { TransferQueuePanel } from "./TransferQueuePanel";
import type { AuthPromptState } from "../hooks/useSessionLifecycle";
import { useCommandHistory } from "../hooks/useCommandHistory";
import type { TransferTask } from "../store/useTransferQueueStore";
import { promptModal } from "../utils/promptModal";

const SESSION_TYPE_ICON: Record<SessionType, string> = {
  terminal: "ri-terminal-line",
  processManager: "ri-cpu-line",
  networkMonitor: "ri-global-line"
};

const isTerminalSession = (session: SessionDescriptor): boolean =>
  !session.type || session.type === "terminal";

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
  terminalSessionIds: string[];
  isActiveConnectionTerminalConnected: boolean;
  isConnecting: boolean;
  monitor?: import("@nextshell/core").MonitorSnapshot;
  transferTasks: TransferTask[];
  transferPanelCollapsed: boolean;
  bottomTab: string;
  authPromptState?: AuthPromptState;
  MAX_SESSION_OPEN_ATTEMPTS: number;
  onLoadConnections: () => void;
  onConnectActiveConnection: () => void;
  onOpenManager: () => void;
  onOpenSettings: () => void;
  onActivateConnection: (connectionId: string) => void;
  onTreeDoubleConnect: (connectionId: string) => void;
  onCloseSession: (sessionId: string) => void;
  onReconnectSession: (sessionId: string) => void;
  onRenameSession: (sessionId: string, title: string) => void;
  onOpenProcessManager: (connectionId: string) => void;
  onOpenNetworkMonitor: (connectionId: string) => void;
  onCloseMonitorTab: (sessionId: string) => void;
  onSetActiveSession: (sessionId?: string) => void;
  onSetActiveConnection: (connectionId?: string) => void;
  onReorderSession: (sourceId: string, targetId: string) => void;
  onSelectNetworkInterface: (networkInterface: string) => void;
  onRetryTransfer: (taskId: string) => void;
  onClearFinishedTransfers: () => void;
  onOpenLocalFile: (task: TransferTask) => void;
  onTransferPanelToggle: () => void;
  onSetBottomTab: (tab: string) => void;
  onAuthPromptCancel: () => void;
  onAuthPromptSubmit: (payload: SessionAuthOverrideInput) => Promise<void>;
}

export const WorkspaceLayout = ({
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
  terminalSessionIds,
  isActiveConnectionTerminalConnected,
  isConnecting,
  monitor,
  transferTasks,
  transferPanelCollapsed,
  bottomTab,
  authPromptState,
  MAX_SESSION_OPEN_ATTEMPTS,
  onLoadConnections,
  onConnectActiveConnection,
  onOpenManager,
  onOpenSettings,
  onActivateConnection,
  onTreeDoubleConnect,
  onCloseSession,
  onReconnectSession,
  onRenameSession,
  onOpenProcessManager,
  onOpenNetworkMonitor,
  onCloseMonitorTab,
  onSetActiveSession,
  onSetActiveConnection,
  onReorderSession,
  onSelectNetworkInterface,
  onRetryTransfer,
  onClearFinishedTransfers,
  onOpenLocalFile,
  onTransferPanelToggle,
  onSetBottomTab,
  onAuthPromptCancel,
  onAuthPromptSubmit,
}: WorkspaceLayoutProps) => {
  const [draggingSessionId, setDraggingSessionId] = useState<string>();
  const [bottomCollapsed, setBottomCollapsed] = useState(false);
  const [terminalSearchMode, setTerminalSearchMode] = useState(false);
  const [terminalSearchTerm, setTerminalSearchTerm] = useState("");
  const bottomPanelRef = usePanelRef();
  const terminalPaneRef = useRef<TerminalPaneHandle | null>(null);
  const resizeFitRafRef = useRef(0);
  const commandHistory = useCommandHistory();

  const handleExecuteCommand = useCallback((command: string) => {
    if (!activeTerminalSession || activeTerminalSession.status !== "connected") {
      return;
    }
    window.nextshell.session
      .write({ sessionId: activeTerminalSession.id, data: `${command}\r` })
      .catch(() => message.error("发送命令失败"));
    void commandHistory.push(command);
  }, [activeTerminalSession, commandHistory]);

  const headerSessionText = useMemo(() => {
    if (!activeSession) return "no session";
    const sessionIndex = activeSession.title.match(/#\d+$/)?.[0];
    const baseLabel = (
      activeSessionConnection?.name?.trim() ||
      activeSessionConnection?.host?.trim() ||
      activeSession.title.replace(/\s+#\d+$/, "").trim()
    ) || "session";
    return `${activeSession.status} ${baseLabel}${sessionIndex ? ` ${sessionIndex}` : ""}`;
  }, [activeSession, activeSessionConnection]);

  const headerSessionClass = activeSession?.status ?? "disconnected";

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

  return (
    <div className="h-screen flex flex-col overflow-hidden">
      <header className="shell-header">
        <div className="titlebar-brand">
          <span className="brand-icon">NS</span>
          <span className="brand-name">NextShell</span>
        </div>
        <div className="header-actions">
          <button
            className="hdr-btn"
            onClick={() => void onLoadConnections()}
            title="刷新连接列表"
          >
            <i className="ri-refresh-line" aria-hidden="true" />
            刷新
          </button>
          <div className={`header-session-status ${headerSessionClass}`} title={headerSessionText}>
            <span className="header-session-dot" />
            <span className="header-session-text">{headerSessionText}</span>
          </div>
          <span className="hdr-sep" />
          <button
            className={`hdr-btn primary${!activeConnectionId || isConnecting ? " disabled" : ""}`}
            disabled={!activeConnectionId || isConnecting}
            onClick={() => void onConnectActiveConnection()}
            title="新建终端（可多开）"
          >
            {isConnecting ? (
              <><i className="ri-loader-4-line ri-spin" aria-hidden="true" /> 连接中…</>
            ) : (
              "连接"
            )}
          </button>
          <button
            className="hdr-btn"
            onClick={onOpenManager}
            title="管理连接"
          >
            <i className="ri-links-line" aria-hidden="true" />
            连接管理器
          </button>
          <button
            className="hdr-btn"
            onClick={onOpenSettings}
            title="打开设置中心"
          >
            <i className="ri-settings-3-line" aria-hidden="true" />
            设置中心
          </button>
        </div>
      </header>

      <main className="flex flex-1 min-w-0 min-h-0 overflow-hidden">
        <Group orientation="horizontal" className="w-full h-full min-w-0 min-h-0">
          <Panel defaultSize="18%" minSize="14%" maxSize="36%">
            <aside className="w-full h-full flex flex-col bg-[var(--bg-surface)] border-r border-[var(--border)] overflow-hidden">
              {activeConnection?.monitorSession ? (
                <SystemInfoPanel
                  monitorSessionEnabled
                  hasVisibleTerminal={isActiveConnectionTerminalConnected}
                  snapshot={monitor}
                  onSelectNetworkInterface={onSelectNetworkInterface}
                />
              ) : null}
              <TransferQueuePanel
                tasks={transferTasks}
                collapsed={transferPanelCollapsed}
                onToggle={onTransferPanelToggle}
                onRetry={(taskId) => void onRetryTransfer(taskId)}
                onClearFinished={onClearFinishedTransfers}
                onOpenLocalFile={(task) => {
                  if (task.direction === "download" && task.status === "success") {
                    onOpenLocalFile(task);
                  }
                }}
              />
            </aside>
          </Panel>
          <Separator className="panel-resize-handle horizontal" />
          <Panel minSize="48%">
            <section className="w-full h-full min-w-0 min-h-0 flex flex-col overflow-hidden">
              <Group orientation="vertical" className="w-full h-full min-w-0 min-h-0">
                <Panel defaultSize="68%" minSize="38%">
                  <div className="terminal-shell">
                    <div className="session-tabs">
                      {sessions.map((session) => {
                        const isTerminal = isTerminalSession(session);
                        const iconClass = SESSION_TYPE_ICON[session.type ?? "terminal"];
                        return (
                          <button
                            key={session.id}
                            type="button"
                            className={[
                              "session-tab",
                              session.id === activeSessionId ? "active" : "",
                              session.id === draggingSessionId ? "dragging" : ""
                            ].filter(Boolean).join(" ")}
                            onClick={() => {
                              onSetActiveSession(session.id);
                              onSetActiveConnection(session.connectionId);
                            }}
                            onDoubleClick={() => {
                              if (!isTerminal) return;
                              void (async () => {
                                const title = await promptModal("会话标题", undefined, session.title);
                                if (title) onRenameSession(session.id, title);
                              })();
                            }}
                            draggable={isTerminal}
                            onDragStart={() => { if (isTerminal) setDraggingSessionId(session.id); }}
                            onDragEnd={() => setDraggingSessionId(undefined)}
                            onDragOver={(event) => event.preventDefault()}
                            onDrop={(event) => {
                              event.preventDefault();
                              if (!draggingSessionId) return;
                              onReorderSession(draggingSessionId, session.id);
                              setDraggingSessionId(undefined);
                            }}
                          >
                            <i className={`tab-type-icon ${iconClass}`} aria-hidden="true" />
                            <span className="session-title">{session.title}</span>
                            {isTerminal && session.status !== "connected" ? (
                              <span
                                className="tab-action tab-reconnect"
                                title="重新连接"
                                onClick={(event) => {
                                  event.stopPropagation();
                                  void onReconnectSession(session.id);
                                }}
                                role="button"
                                tabIndex={0}
                                onKeyDown={(event) => {
                                  if (event.key === "Enter" || event.key === " ") {
                                    event.preventDefault();
                                    void onReconnectSession(session.id);
                                  }
                                }}
                              >
                                <i className="ri-refresh-line" aria-hidden="true" />
                              </span>
                            ) : null}
                            {isTerminal ? (
                              <span className="tab-action tab-drag" title="拖拽重排 / 双击重命名">
                                <i className="ri-drag-move-2-line" aria-hidden="true" />
                              </span>
                            ) : null}
                            <span
                              className="tab-action tab-close"
                              title="关闭会话"
                              onClick={(event) => {
                                event.stopPropagation();
                                if (isTerminal) {
                                  void onCloseSession(session.id);
                                } else {
                                  onCloseMonitorTab(session.id);
                                }
                              }}
                              role="button"
                              tabIndex={0}
                              onKeyDown={(event) => {
                                if (event.key === "Enter" || event.key === " ") {
                                  event.preventDefault();
                                  if (isTerminal) {
                                    void onCloseSession(session.id);
                                  } else {
                                    onCloseMonitorTab(session.id);
                                  }
                                }
                              }}
                            >
                              <i className="ri-close-line" aria-hidden="true" />
                            </span>
                          </button>
                        );
                      })}
                    </div>
                    <div
                      className={
                        activeSession?.type === "processManager" || activeSession?.type === "networkMonitor"
                          ? "hidden"
                          : "flex-1 min-h-0 flex flex-col"
                      }
                    >
                      <TerminalPane
                        ref={terminalPaneRef}
                        session={activeTerminalSession}
                        connection={activeTerminalConnection}
                        sessionIds={terminalSessionIds}
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
                      <ProcessManagerPane session={activeSession} />
                    ) : null}
                    {activeSession?.type === "networkMonitor" ? (
                      <NetworkMonitorPane session={activeSession} />
                    ) : null}
                  </div>
                </Panel>
                <Separator className="panel-resize-handle vertical" />
                <Panel
                  panelRef={bottomPanelRef}
                  defaultSize="32%"
                  minSize="16%"
                  collapsible
                  collapsedSize="4%"
                  onResize={() => {
                    setBottomCollapsed(bottomPanelRef.current?.isCollapsed() ?? false);
                    cancelAnimationFrame(resizeFitRafRef.current);
                    resizeFitRafRef.current = requestAnimationFrame(() => {
                      terminalPaneRef.current?.fit();
                    });
                  }}
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
                            onClick={() => {
                              if (bottomCollapsed) {
                                bottomPanelRef.current?.expand();
                              } else {
                                bottomPanelRef.current?.collapse();
                              }
                            }}
                          >
                            <i
                              className={bottomCollapsed ? "ri-arrow-up-s-line" : "ri-arrow-down-s-line"}
                              aria-hidden="true"
                            />
                          </button>
                        )
                      }}
                      items={[
                        {
                          key: "connections",
                          label: "连接",
                          children: (
                            <ConnectionTreePanel
                              connections={connections}
                              sessions={sessions}
                              activeConnectionId={activeConnectionId}
                              onSelect={onActivateConnection}
                              onConnectByDoubleClick={(connectionId) => {
                                void onTreeDoubleConnect(connectionId);
                              }}
                              onOpenProcessManager={onOpenProcessManager}
                              onOpenNetworkMonitor={onOpenNetworkMonitor}
                            />
                          )
                        },
                        {
                          key: "files",
                          label: "SFTP",
                          children: (
                            <FileExplorerPane
                              connection={activeConnection}
                              connected={isActiveConnectionTerminalConnected}
                              onOpenSettings={onOpenSettings}
                            />
                          )
                        },
                        {
                          key: "live-edit",
                          label: "实时编辑",
                          children: (
                            <LiveEditPane connections={connections} />
                          )
                        },
                        {
                          key: "commands",
                          label: "命令",
                          children: (
                            <CommandCenterPane
                              connection={activeConnection}
                              connected={isActiveConnectionTerminalConnected}
                              connections={connections}
                              onExecuteCommand={handleExecuteCommand}
                            />
                          )
                        }
                      ]}
                    />
                  </div>
                </Panel>
              </Group>
            </section>
          </Panel>
        </Group>
      </main>

      <SessionAuthRetryModal
        open={Boolean(authPromptState)}
        attempt={authPromptState?.attempt ?? 1}
        maxAttempts={authPromptState?.maxAttempts ?? MAX_SESSION_OPEN_ATTEMPTS}
        initialUsername={authPromptState?.initialUsername}
        defaultAuthType={authPromptState?.defaultAuthType ?? "password"}
        hasExistingPrivateKey={authPromptState?.hasExistingPrivateKey ?? false}
        sshKeys={sshKeys}
        onCancel={onAuthPromptCancel}
        onSubmit={onAuthPromptSubmit}
      />
    </div>
  );
};
