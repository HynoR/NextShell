import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";

interface QuickConnectBarProps {
  connections: ConnectionProfile[];
  sessions: SessionDescriptor[];
  onConnect: (connectionId: string) => void;
}

interface ResultItem {
  connection: ConnectionProfile;
  isConnected: boolean;
}

const MAX_RECENT = 6;

export const QuickConnectBar = ({
  connections,
  sessions,
  onConnect,
}: QuickConnectBarProps) => {
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [activeIndex, setActiveIndex] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  const connectedIds = useMemo(
    () =>
      new Set(
        sessions
          .filter((s) => s.status === "connected" && s.type === "terminal")
          .map((s) => s.connectionId),
      ),
    [sessions],
  );

  const recentConnections = useMemo<ResultItem[]>(() => {
    return [...connections]
      .filter((c) => c.lastConnectedAt)
      .sort(
        (a, b) =>
          new Date(b.lastConnectedAt!).getTime() -
          new Date(a.lastConnectedAt!).getTime(),
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

  const handleOpen = useCallback(() => {
    setOpen(true);
    setActiveIndex(-1);
  }, []);

  const handleClose = useCallback(() => {
    setOpen(false);
    setKeyword("");
    setActiveIndex(-1);
    inputRef.current?.blur();
  }, []);

  const handleSelect = useCallback(
    (connectionId: string) => {
      onConnect(connectionId);
      handleClose();
    },
    [onConnect, handleClose],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!open) return;
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setActiveIndex((i) => Math.min(i + 1, filteredResults.length - 1));
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setActiveIndex((i) => Math.max(i - 1, 0));
      } else if (e.key === "Enter") {
        e.preventDefault();
        const item = filteredResults[activeIndex];
        if (item) handleSelect(item.connection.id);
      } else if (e.key === "Escape") {
        e.preventDefault();
        handleClose();
      }
    },
    [open, filteredResults, activeIndex, handleSelect, handleClose],
  );

  // Close on outside click
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (
        containerRef.current &&
        !containerRef.current.contains(e.target as Node)
      ) {
        handleClose();
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open, handleClose]);

  const sectionLabel = keyword.trim()
    ? `${filteredResults.length} 个结果`
    : "最近连接";

  return (
    <div
      ref={containerRef}
      className={`qcb-wrap${open ? " qcb-open" : ""}`}
    >
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
        {!open && (
          <kbd className="qcb-shortcut">⌘K</kbd>
        )}
      </div>

      {open && (
        <div className="qcb-dropdown">
          {filteredResults.length === 0 ? (
            <div className="qcb-empty">
              <i className="ri-server-line" aria-hidden="true" />
              <span>
                {keyword.trim() ? "未找到匹配的服务器" : "暂无最近连接记录"}
              </span>
            </div>
          ) : (
            <>
              <div className="qcb-section-label">{sectionLabel}</div>
              {filteredResults.map((item, idx) => (
                <QuickConnectItem
                  key={item.connection.id}
                  item={item}
                  isActive={idx === activeIndex}
                  keyword={keyword}
                  onSelect={() => handleSelect(item.connection.id)}
                  onMouseEnter={() => setActiveIndex(idx)}
                />
              ))}
              <div className="qcb-footer">
                <span>↑↓ 导航</span>
                <span>↵ 连接</span>
                <span>Esc 关闭</span>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
};

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
  onMouseEnter,
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
        <span className="qcb-item-name">
          {highlight(c.name, keyword)}
        </span>
        {groupLabel && (
          <span className="qcb-item-group">{groupLabel}</span>
        )}
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
