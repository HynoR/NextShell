import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent
} from "react";
import { message } from "antd";
import type { SessionDescriptor } from "@nextshell/core";
import {
  useCommandHistory,
  type CommandHistoryEntry
} from "../hooks/useCommandHistory";

interface CommandInputBarProps {
  session?: SessionDescriptor;
  searchMode: boolean;
  onSearchModeChange: (enabled: boolean) => void;
  terminalSearchTerm: string;
  onTerminalSearchTermChange: (value: string) => void;
  onTerminalSearchNext: () => void;
  onTerminalSearchPrevious: () => void;
}

type PanelMode = "history" | "search";

export const CommandInputBar = ({
  session,
  searchMode,
  onSearchModeChange,
  terminalSearchTerm,
  onTerminalSearchTermChange,
  onTerminalSearchNext,
  onTerminalSearchPrevious
}: CommandInputBarProps) => {
  const {
    entries,
    push,
    remove,
    clear,
    search,
    navigateUp,
    navigateDown,
    resetNavigation
  } = useCommandHistory();

  const [commandInput, setCommandInput] = useState("");
  const [panelOpen, setPanelOpen] = useState(false);
  const [panelMode, setPanelMode] = useState<PanelMode>("history");
  const [highlightIndex, setHighlightIndex] = useState(-1);

  const inputRef = useRef<HTMLInputElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo(
    () => (panelOpen && panelMode === "history" ? search(commandInput) : []),
    [panelOpen, panelMode, search, commandInput]
  );

  useEffect(() => {
    setHighlightIndex(-1);
  }, [filtered.length, commandInput, panelMode]);

  useEffect(() => {
    if (!panelOpen) {
      return;
    }

    const handler = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) {
        setPanelOpen(false);
      }
    };

    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [panelOpen]);

  const sendCommand = useCallback(
    (command: string) => {
      const trimmed = command.trim();
      if (!trimmed) {
        return;
      }

      if (!session || session.status !== "connected") {
        message.warning("当前没有活跃的 SSH 会话，请先连接。");
        return;
      }

      window.nextshell.session
        .write({ sessionId: session.id, data: `${trimmed}\r` })
        .catch(() => message.error("发送命令失败"));

      void push(trimmed);
      setCommandInput("");
      setPanelOpen(false);
      resetNavigation();
    },
    [session, push, resetNavigation]
  );

  const openHistoryPanel = useCallback(() => {
    setPanelMode("history");
    setPanelOpen((prev) => (panelMode === "history" ? !prev : true));
    onSearchModeChange(false);
    inputRef.current?.focus();
  }, [onSearchModeChange, panelMode]);

  const openSearchMode = useCallback(() => {
    onSearchModeChange(true);
    setPanelMode("search");
    setPanelOpen(true);
    requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
  }, [onSearchModeChange]);

  const closeSearchMode = useCallback(() => {
    onSearchModeChange(false);
    setPanelMode("history");
    setPanelOpen(false);
    requestAnimationFrame(() => {
      inputRef.current?.focus();
    });
  }, [onSearchModeChange]);

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (searchMode) {
      if (event.key === "Enter") {
        event.preventDefault();
        if (event.shiftKey) {
          onTerminalSearchPrevious();
        } else {
          onTerminalSearchNext();
        }
        return;
      }

      if (event.key === "Escape") {
        event.preventDefault();
        closeSearchMode();
      }
      return;
    }

    if (event.key === "Enter") {
      event.preventDefault();
      if (
        panelOpen &&
        panelMode === "history" &&
        highlightIndex >= 0 &&
        highlightIndex < filtered.length
      ) {
        const entry = filtered[highlightIndex];
        if (entry) {
          setCommandInput(entry.command);
          setPanelOpen(false);
          sendCommand(entry.command);
        }
        return;
      }
      sendCommand(commandInput);
      return;
    }

    if (event.key === "Escape") {
      if (panelOpen) {
        setPanelOpen(false);
        event.preventDefault();
      }
      return;
    }

    if (event.key === "ArrowUp") {
      event.preventDefault();
      if (panelOpen && panelMode === "history" && filtered.length > 0) {
        setHighlightIndex((prev) => (prev <= 0 ? filtered.length - 1 : prev - 1));
        return;
      }

      const prev = navigateUp();
      if (prev !== undefined) {
        setCommandInput(prev);
      }
      return;
    }

    if (event.key === "ArrowDown") {
      event.preventDefault();
      if (panelOpen && panelMode === "history" && filtered.length > 0) {
        setHighlightIndex((prev) => (prev >= filtered.length - 1 ? 0 : prev + 1));
        return;
      }

      const next = navigateDown();
      if (next !== undefined) {
        setCommandInput(next);
      }
      return;
    }
  };

  const handleInputChange = (value: string) => {
    if (searchMode) {
      onTerminalSearchTermChange(value);
      return;
    }

    setCommandInput(value);
    resetNavigation();
  };

  const handleSelectEntry = (entry: CommandHistoryEntry) => {
    setCommandInput(entry.command);
    setPanelOpen(false);
    sendCommand(entry.command);
    inputRef.current?.focus();
  };

  const handleRemoveEntry = (event: React.MouseEvent, command: string) => {
    event.stopPropagation();
    void remove(command);
  };

  const scrollHighlightIntoView = useCallback((index: number) => {
    if (!panelRef.current) {
      return;
    }

    const items = panelRef.current.querySelectorAll(".cib-history-item");
    items[index]?.scrollIntoView({ block: "nearest" });
  }, []);

  useEffect(() => {
    if (highlightIndex >= 0) {
      scrollHighlightIntoView(highlightIndex);
    }
  }, [highlightIndex, scrollHighlightIntoView]);

  const isConnected = session?.status === "connected";
  const canSearch = Boolean(session);

  return (
    <div className="cib-root" ref={rootRef}>
      {panelOpen && (
        <div className="cib-panel" ref={panelRef}>
          <div className="cib-panel-header">
            <div className="cib-panel-modes">
              <button
                type="button"
                className={`cib-panel-mode${panelMode === "history" ? " active" : ""}`}
                onClick={() => {
                  onSearchModeChange(false);
                  setPanelMode("history");
                }}
              >
                命令历史
              </button>
              <button
                type="button"
                className={`cib-panel-mode${panelMode === "search" ? " active" : ""}`}
                onClick={() => {
                  if (!canSearch) {
                    return;
                  }
                  openSearchMode();
                }}
                disabled={!canSearch}
              >
                终端搜索
              </button>
              {panelMode === "history" ? (
                <span className="cib-panel-count">{entries.length}</span>
              ) : null}
            </div>
            {panelMode === "history" && entries.length > 0 && (
              <button
                type="button"
                className="cib-clear-btn"
                onClick={() => {
                  void clear();
                  setPanelOpen(false);
                }}
              >
                清空
              </button>
            )}
          </div>
          {panelMode === "history" ? (
            <div className="cib-panel-list">
              {filtered.map((entry, index) => (
                <div
                  key={entry.command}
                  className={`cib-history-item${index === highlightIndex ? " highlight" : ""}`}
                  onClick={() => handleSelectEntry(entry)}
                  onMouseEnter={() => setHighlightIndex(index)}
                >
                  <span className="cib-cmd-text">{entry.command}</span>
                  <span className="cib-cmd-meta">
                    <span className="cib-cmd-count" title="使用次数">
                      ×{entry.useCount}
                    </span>
                    <button
                      type="button"
                      className="cib-cmd-remove"
                      title="删除"
                      onClick={(event) => handleRemoveEntry(event, entry.command)}
                    >
                      ×
                    </button>
                  </span>
                </div>
              ))}
              {filtered.length === 0 ? (
                <div className="cib-empty-tip">暂无匹配命令</div>
              ) : null}
            </div>
          ) : (
            <div className="cib-search-panel">
              <div className="cib-search-tip">
                输入关键词搜索终端输出。`Enter` 下一条，`Shift+Enter` 上一条。
              </div>
              <div className="cib-search-actions">
                <button
                  type="button"
                  className="cib-search-action-btn"
                  onClick={onTerminalSearchPrevious}
                  disabled={!terminalSearchTerm.trim()}
                >
                  上一条
                </button>
                <button
                  type="button"
                  className="cib-search-action-btn"
                  onClick={onTerminalSearchNext}
                  disabled={!terminalSearchTerm.trim()}
                >
                  下一条
                </button>
                <button
                  type="button"
                  className="cib-search-action-btn"
                  onClick={() => onTerminalSearchTermChange("")}
                >
                  清空关键词
                </button>
                <button
                  type="button"
                  className="cib-search-action-btn"
                  onClick={closeSearchMode}
                >
                  返回命令
                </button>
              </div>
            </div>
          )}
        </div>
      )}

      <div className="cib-bar">
        <button
          type="button"
          className={`cib-history-toggle${panelOpen && panelMode === "history" ? " active" : ""}`}
          onClick={openHistoryPanel}
          title="命令历史"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="10" />
            <polyline points="12 6 12 12 16 14" />
          </svg>
          {entries.length > 0 && (
            <span className="cib-badge">{entries.length}</span>
          )}
        </button>

        <div className="cib-input-wrap">
          <span className={`cib-prompt${searchMode ? " search" : ""}`}>
            {searchMode ? <i className="ri-search-line" aria-hidden="true" /> : "$"}
          </span>
          <input
            ref={inputRef}
            className="cib-input"
            type="text"
            value={searchMode ? terminalSearchTerm : commandInput}
            onChange={(event) => handleInputChange(event.target.value)}
            onKeyDown={handleKeyDown}
            onFocus={() => {
              if (searchMode) {
                setPanelMode("search");
                setPanelOpen(true);
                return;
              }

              if (entries.length > 0 && !panelOpen) {
                setPanelOpen(true);
              }
            }}
            placeholder={
              searchMode
                ? "搜索终端输出，Enter 下一条 / Shift+Enter 上一条…"
                : isConnected
                  ? "输入命令，回车发送到终端…"
                  : "请先建立 SSH 连接"
            }
            disabled={searchMode ? !canSearch : !isConnected}
            spellCheck={false}
            autoComplete="off"
          />
        </div>

        <button
          type="button"
          className={`cib-send-btn${searchMode || !isConnected || !commandInput.trim() ? " disabled" : ""}`}
          disabled={searchMode || !isConnected || !commandInput.trim()}
          onClick={() => sendCommand(commandInput)}
          title="发送命令"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <line x1="22" y1="2" x2="11" y2="13" />
            <polygon points="22 2 15 22 11 13 2 9 22 2" />
          </svg>
        </button>

        <button
          type="button"
          className={`cib-search-btn${searchMode ? " active" : ""}${!canSearch ? " disabled" : ""}`}
          disabled={!canSearch}
          onClick={() => {
            if (searchMode) {
              closeSearchMode();
            } else {
              openSearchMode();
            }
          }}
          title="终端搜索"
        >
          <i className="ri-search-line" aria-hidden="true" />
        </button>
      </div>
    </div>
  );
};
