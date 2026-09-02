import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";
import {
  getQuickConnectShortcutLabel,
  isQuickConnectShortcut,
  shouldIgnoreQuickConnectShortcutTarget
} from "../utils/quickConnectShortcut";
import { buildQuickConnectSessionResults, type SessionResultItem } from "../utils/quickConnectSessions";
import { useWorkspaceStore } from "../store/useWorkspaceStore";

interface QuickConnectBarProps {
  connections: ConnectionProfile[];
  sessions: SessionDescriptor[];
  onConnect: (connectionId: string) => void;
  onQuickConnectInput: (raw: string) => Promise<boolean>;
  /** 「添加新服务器」:打开连接管理器并进入新建态(D31,新建表单只有 V2 那一套)。 */
  onOpenManager: () => void;
}

interface ResultItem {
  connection: ConnectionProfile;
  isConnected: boolean;
}

type DisplayItem =
  | { type: "create-action"; id: "create-action" }
  | { type: "direct-connect"; id: "direct-connect"; raw: string }
  | { type: "session"; item: SessionResultItem }
  | { type: "connection"; item: ResultItem };

const MAX_RECENT = 6;

export const QuickConnectBar = ({
  connections,
  sessions,
  onConnect,
  onQuickConnectInput,
  onOpenManager
}: QuickConnectBarProps) => {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [activeIndex, setActiveIndex] = useState(-1);
  const [submitting, setSubmitting] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const shortcutLabel = getQuickConnectShortcutLabel(window.nextshell.platform);

  const openSessions = useWorkspaceStore((state) => state.sessions);
  const sessionMruIds = useWorkspaceStore((state) => state.sessionMruIds);
  const activeSessionId = useWorkspaceStore((state) => state.activeSessionId);
  const setActiveSession = useWorkspaceStore((state) => state.setActiveSession);

  const connectedIds = useMemo(
    () =>
      new Set(
        sessions
          .filter((s) => s.status === "connected" && s.type === "terminal")
          .map((s) => s.connectionId)
      ),
    [sessions]
  );

  const recentConnections = useMemo<ResultItem[]>(() => {
    return [...connections]
      .filter((c) => c.lastConnectedAt)
      .sort(
        (a, b) => new Date(b.lastConnectedAt!).getTime() - new Date(a.lastConnectedAt!).getTime()
      )
      .slice(0, MAX_RECENT)
      .map((c) => ({ connection: c, isConnected: connectedIds.has(c.id) }));
  }, [connections, connectedIds]);

  const filteredResults = useMemo<ResultItem[]>(() => {
    const lower = keyword.trim().toLowerCase();
    if (!lower) return recentConnections;
    return connections
      .filter((c) => {
        const searchable =
          `${c.name} ${c.host} ${c.tags.join(" ")} ${c.groupPath} ${c.notes ?? ""}`.toLowerCase();
        return searchable.includes(lower);
      })
      .slice(0, 12)
      .map((c) => ({ connection: c, isConnected: connectedIds.has(c.id) }));
  }, [keyword, connections, connectedIds, recentConnections]);

  const sessionResults = useMemo<SessionResultItem[]>(() => {
    return buildQuickConnectSessionResults({
      sessions: openSessions,
      sessionMruIds,
      activeSessionId,
      connections,
      keyword
    });
  }, [activeSessionId, connections, keyword, openSessions, sessionMruIds]);

  // 关键字里带 `@` 且没有命中任何已存连接时,顶部直接给「连接 user@host[:port]」一项,
  // 回车走 onQuickConnectInput 静默落库并连接(D32,取代旧的 `+` 三段式)。
  const showDirectConnect = keyword.trim().includes("@") && filteredResults.length === 0;

  const displayItems = useMemo<DisplayItem[]>(() => {
    const sessionItems: DisplayItem[] = sessionResults.map((item) => ({
      type: "session",
      item
    }));
    const connectionItems: DisplayItem[] = filteredResults.map((item) => ({
      type: "connection",
      item
    }));

    if (keyword.trim()) {
      const directItems: DisplayItem[] = showDirectConnect
        ? [{ type: "direct-connect", id: "direct-connect", raw: keyword.trim() }]
        : [];
      return [...directItems, ...sessionItems, ...connectionItems];
    }

    return [...sessionItems, { type: "create-action", id: "create-action" }, ...connectionItems];
  }, [filteredResults, keyword, sessionResults, showDirectConnect]);

  const focusInput = useCallback(() => {
    window.requestAnimationFrame(() => {
      inputRef.current?.focus();
    });
  }, []);

  const handleOpen = useCallback(() => {
    setOpen(true);
    setActiveIndex(-1);
    focusInput();
  }, [focusInput]);

  const handleClose = useCallback(() => {
    setOpen(false);
    setKeyword("");
    setActiveIndex(-1);
    setSubmitting(false);
    inputRef.current?.blur();
  }, []);

  const handleSelect = useCallback(
    (connectionId: string) => {
      onConnect(connectionId);
      handleClose();
    },
    [handleClose, onConnect]
  );

  const handleSelectSession = useCallback(
    (sessionId: string) => {
      setActiveSession(sessionId);
      handleClose();
    },
    [handleClose, setActiveSession]
  );

  const handleOpenManagerForCreate = useCallback(() => {
    handleClose();
    onOpenManager();
  }, [handleClose, onOpenManager]);

  const handleDirectConnect = useCallback(
    (raw: string) => {
      if (submitting) {
        return;
      }
      setSubmitting(true);
      void onQuickConnectInput(raw)
        .then((accepted) => {
          if (accepted) {
            handleClose();
          }
        })
        .finally(() => {
          setSubmitting(false);
        });
    },
    [handleClose, onQuickConnectInput, submitting]
  );

  useEffect(() => {
    const handleShortcut = (event: KeyboardEvent) => {
      if (!isQuickConnectShortcut(event, window.nextshell.platform)) {
        return;
      }
      if (shouldIgnoreQuickConnectShortcutTarget(event.target, containerRef.current)) {
        return;
      }

      event.preventDefault();
      if (!open) {
        setOpen(true);
        setActiveIndex(-1);
      }
      focusInput();
    };

    window.addEventListener("keydown", handleShortcut);
    return () => {
      window.removeEventListener("keydown", handleShortcut);
    };
  }, [focusInput, open]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!open) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((i) => Math.min(i + 1, displayItems.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const item = displayItems[activeIndex];
        if (item?.type === "create-action") {
          handleOpenManagerForCreate();
          return;
        }
        if (item?.type === "direct-connect") {
          handleDirectConnect(item.raw);
          return;
        }
        if (item?.type === "session") {
          handleSelectSession(item.item.session.id);
          return;
        }
        if (item?.type === "connection") {
          handleSelect(item.item.connection.id);
          return;
        }

        // 没选中任何项、地址栏里是 user@host 时,回车即连(等价于选中顶部那一项)。
        if (activeIndex < 0 && showDirectConnect) {
          handleDirectConnect(keyword.trim());
        }
      } else if (e.key === "Escape") {
        e.preventDefault();
        handleClose();
      }
    },
    [
      activeIndex,
      displayItems,
      handleClose,
      handleDirectConnect,
      handleOpenManagerForCreate,
      handleSelect,
      handleSelectSession,
      keyword,
      open,
      showDirectConnect
    ]
  );

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        handleClose();
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, handleClose]);

  const sectionLabel = keyword.trim() ? `${filteredResults.length} 个结果` : "最近连接";
  const firstConnectionIndex = displayItems.findIndex((item) => item.type === "connection");
  const firstSessionIndex = displayItems.findIndex((item) => item.type === "session");
  const sessionSectionLabel = keyword.trim() ? `${sessionResults.length} 个打开的会话` : "切换到";

  return (
    <div ref={containerRef} className={`qcb-wrap${open ? " qcb-open" : ""}`}>
      <div className="qcb-field" onClick={handleOpen}>
        <i className="ri-search-line qcb-icon" aria-hidden="true" />
        <input
          ref={inputRef}
          className="qcb-input"
          placeholder="快速连接服务器…"
          value={keyword}
          onFocus={handleOpen}
          onChange={(e) => {
            setKeyword(e.target.value);
            setActiveIndex(-1);
          }}
          onKeyDown={handleKeyDown}
          aria-label="快速连接"
          spellCheck={false}
          autoComplete="off"
        />
        {open && keyword && (
          <button
            className="qcb-clear"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              setKeyword("");
              setActiveIndex(-1);
              inputRef.current?.focus();
            }}
          >
            <i className="ri-close-line" aria-hidden="true" />
          </button>
        )}
        {!open && <kbd className="qcb-shortcut">{shortcutLabel}</kbd>}
      </div>

      {open && (
        <div className="qcb-dropdown">
          {keyword.trim() &&
          filteredResults.length === 0 &&
          sessionResults.length === 0 &&
          !showDirectConnect ? (
            <div className="qcb-empty">
              <i className="ri-server-line" aria-hidden="true" />
              <span>未找到匹配的服务器</span>
            </div>
          ) : (
            <>
              {displayItems.map((item, idx) => {
                if (item.type === "create-action") {
                  return (
                    <QuickCreateActionItem
                      key={item.id}
                      isActive={idx === activeIndex}
                      onSelect={handleOpenManagerForCreate}
                      onMouseEnter={() => setActiveIndex(idx)}
                    />
                  );
                }
                if (item.type === "direct-connect") {
                  return (
                    <QuickConnectDirectItem
                      key={item.id}
                      raw={item.raw}
                      isActive={idx === activeIndex}
                      onSelect={() => handleDirectConnect(item.raw)}
                      onMouseEnter={() => setActiveIndex(idx)}
                    />
                  );
                }

                if (item.type === "session") {
                  const sessionNode = (
                    <QuickConnectSessionItem
                      key={item.item.session.id}
                      item={item.item}
                      isActive={idx === activeIndex}
                      keyword={keyword}
                      onSelect={() => handleSelectSession(item.item.session.id)}
                      onMouseEnter={() => setActiveIndex(idx)}
                    />
                  );

                  if (idx === firstSessionIndex) {
                    return (
                      <div key={`session-section-${item.item.session.id}`}>
                        <div className="qcb-section-label">{sessionSectionLabel}</div>
                        {sessionNode}
                      </div>
                    );
                  }

                  return sessionNode;
                }

                const node = (
                  <QuickConnectItem
                    key={item.item.connection.id}
                    item={item.item}
                    isActive={idx === activeIndex}
                    keyword={keyword}
                    onSelect={() => handleSelect(item.item.connection.id)}
                    onMouseEnter={() => setActiveIndex(idx)}
                  />
                );

                if (idx === firstConnectionIndex) {
                  return (
                    <div key={`section-${item.item.connection.id}`}>
                      <div className="qcb-section-label">{sectionLabel}</div>
                      {node}
                    </div>
                  );
                }

                return node;
              })}
              <div className="qcb-footer">
                <span>↑↓ 导航</span>
                <span>↵ 打开/连接</span>
                <span>Esc 关闭</span>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
};

const QuickCreateActionItem = ({
  isActive,
  onSelect,
  onMouseEnter
}: {
  isActive: boolean;
  onSelect: () => void;
  onMouseEnter: () => void;
}) => (
  <button
    type="button"
    className={`qcb-item qcb-item-create${isActive ? " active" : ""}`}
    onMouseDown={(e) => e.preventDefault()}
    onClick={onSelect}
    onMouseEnter={onMouseEnter}
  >
    <span className="qcb-dot qcb-dot-create">
      <i className="ri-add-circle-line" aria-hidden="true" />
    </span>
    <span className="qcb-item-body">
      <span className="qcb-item-name">添加新服务器</span>
      <span className="qcb-item-group">在连接管理器中新建</span>
    </span>
  </button>
);

const QuickConnectDirectItem = ({
  raw,
  isActive,
  onSelect,
  onMouseEnter
}: {
  raw: string;
  isActive: boolean;
  onSelect: () => void;
  onMouseEnter: () => void;
}) => (
  <button
    type="button"
    className={`qcb-item qcb-item-quick-input${isActive ? " active" : ""}`}
    onMouseDown={(e) => e.preventDefault()}
    onClick={onSelect}
    onMouseEnter={onMouseEnter}
  >
    <span className="qcb-dot qcb-dot-quick-input">
      <i className="ri-terminal-box-line" aria-hidden="true" />
    </span>
    <span className="qcb-item-body">
      <span className="qcb-item-name">连接 {raw}</span>
      <span className="qcb-item-group">保存为快速连接并立即连接</span>
    </span>
  </button>
);

interface QuickConnectItemProps {
  item: ResultItem;
  isActive: boolean;
  keyword: string;
  onSelect: () => void;
  onMouseEnter: () => void;
}

const QuickConnectItem = ({
  item,
  isActive,
  keyword,
  onSelect,
  onMouseEnter
}: QuickConnectItemProps) => {
  const c = item.connection;
  const groupLabel = c.groupPath && c.groupPath !== "/" ? c.groupPath : null;

  return (
    <button
      type="button"
      className={`qcb-item${isActive ? " active" : ""}`}
      onMouseDown={(e) => e.preventDefault()}
      onClick={onSelect}
      onMouseEnter={onMouseEnter}
    >
      <span className={`qcb-dot${item.isConnected ? " online" : ""}`} />
      <span className="qcb-item-body">
        <span className="qcb-item-name">{highlight(c.name, keyword)}</span>
        {groupLabel && <span className="qcb-item-group">{groupLabel}</span>}
      </span>
      <span className="qcb-item-host">
        {highlight(c.host, keyword)}
        <span className="qcb-item-port">:{c.port}</span>
      </span>
      <span className="qcb-item-action" title="新建终端连接" aria-label="新建终端连接">
        <i className="ri-terminal-box-line" aria-hidden="true" />
      </span>
    </button>
  );
};

interface QuickConnectSessionItemProps {
  item: SessionResultItem;
  isActive: boolean;
  keyword: string;
  onSelect: () => void;
  onMouseEnter: () => void;
}

const QuickConnectSessionItem = ({
  item,
  isActive,
  keyword,
  onSelect,
  onMouseEnter
}: QuickConnectSessionItemProps) => {
  const { session, connection } = item;
  const isDisconnected = session.status === "disconnected" || session.status === "failed";
  const suffix = connection ? `${connection.username}@${connection.host}` : null;

  return (
    <button
      type="button"
      className={`qcb-item${isActive ? " active" : ""}`}
      onMouseDown={(e) => e.preventDefault()}
      onClick={onSelect}
      onMouseEnter={onMouseEnter}
      style={isDisconnected ? { opacity: 0.6 } : undefined}
    >
      <span className="qcb-dot-quick-input">
        <i className="ri-window-2-line" aria-hidden="true" />
      </span>
      <span className="qcb-item-body">
        <span className="qcb-item-name">{highlight(session.title, keyword)}</span>
        {suffix && <span className="qcb-item-group">{highlight(suffix, keyword)}</span>}
      </span>
      {isDisconnected && (
        <span className="qcb-item-host" style={{ color: "var(--t3)" }}>
          已断开
        </span>
      )}
      <span className="qcb-item-action" title="切换到该会话" aria-label="切换到该会话">
        <i className="ri-arrow-right-line" aria-hidden="true" />
      </span>
    </button>
  );
};

function highlight(text: string, keyword: string): React.ReactNode {
  if (!keyword.trim()) return text;
  const lower = keyword.trim().toLowerCase();
  const idx = text.toLowerCase().indexOf(lower);
  if (idx === -1) return text;
  return (
    <>
      {text.slice(0, idx)}
      <mark className="qcb-hl">{text.slice(idx, idx + lower.length)}</mark>
      {text.slice(idx + lower.length)}
    </>
  );
}
