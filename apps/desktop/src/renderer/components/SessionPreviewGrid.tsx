import { useEffect, useMemo, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import type { SessionDescriptor } from "@nextshell/core";
import { readSessionBacklogTail } from "../terminal/sessionBacklogProvider";
import { connectionColor } from "../utils/connectionColor";

/**
 * 监视网格：同屏只读并排盯 2-4 个会话的实时输出(运维盯日志场景)。
 *
 * 刻意不复用主终端的单实例架构——每个格子是一个独立的只读 xterm
 * (DOM 渲染器、禁输入、无 OSC 副作用),初始内容取会话缓冲的尾部,
 * 之后直接消费 session.onData 事件流。ack 仍由主 TerminalPane 统一
 * 负责,这里只旁听,不参与流控。
 */

const PREVIEW_SCROLLBACK_LINES = 2000;
const PREVIEW_BACKLOG_CHARS = 128 * 1024;

interface SessionPreviewGridProps {
  sessions: SessionDescriptor[];
  activeSessionId?: string;
  onActivateSession: (sessionId: string) => void;
  onClose: () => void;
}

interface PreviewCell {
  terminal: Terminal;
  fit: FitAddon;
}

export const SessionPreviewGrid = ({
  sessions,
  activeSessionId,
  onActivateSession,
  onClose
}: SessionPreviewGridProps) => {
  const cellRefs = useRef(new Map<string, HTMLDivElement>());
  const cellsRef = useRef(new Map<string, PreviewCell>());
  const sessionIdsKey = useMemo(() => sessions.map((session) => session.id).join(","), [sessions]);

  useEffect(() => {
    const cells = cellsRef.current;
    const ids = sessionIdsKey ? sessionIdsKey.split(",") : [];

    for (const sessionId of ids) {
      const host = cellRefs.current.get(sessionId);
      if (!host || cells.has(sessionId)) {
        continue;
      }
      const terminal = new Terminal({
        disableStdin: true,
        convertEol: false,
        scrollback: PREVIEW_SCROLLBACK_LINES,
        fontSize: 11,
        fontFamily: "var(--mono, monospace)",
        theme: { background: "#0b141d" }
      });
      const fit = new FitAddon();
      terminal.loadAddon(fit);
      terminal.open(host);
      try {
        fit.fit();
      } catch {
        // 容器尚未量出尺寸时 fit 会抛,首个 ResizeObserver 回调会补上。
      }
      const backlog = readSessionBacklogTail(sessionId, PREVIEW_BACKLOG_CHARS);
      if (backlog) {
        terminal.write(backlog);
      }
      cells.set(sessionId, { terminal, fit });
    }

    // 网格不再展示的会话,回收它的 xterm。
    for (const [sessionId, cell] of cells) {
      if (!ids.includes(sessionId)) {
        cell.terminal.dispose();
        cells.delete(sessionId);
      }
    }

    const unsubscribe = window.nextshell.session.onData((event) => {
      // 只旁听不 ack:交付确认由主 TerminalPane 统一完成,这里重复
      // ack 会污染主进程的流控窗口。
      cells.get(event.sessionId)?.terminal.write(event.data);
    });

    const observer = new ResizeObserver(() => {
      for (const cell of cells.values()) {
        try {
          cell.fit.fit();
        } catch {
          // ignore transient zero-size layouts
        }
      }
    });
    for (const host of cellRefs.current.values()) {
      observer.observe(host);
    }

    return () => {
      unsubscribe();
      observer.disconnect();
    };
  }, [sessionIdsKey]);

  useEffect(
    () => () => {
      for (const cell of cellsRef.current.values()) {
        cell.terminal.dispose();
      }
      cellsRef.current.clear();
    },
    []
  );

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        event.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [onClose]);

  return (
    <div className="session-preview-grid" data-count={sessions.length}>
      {sessions.map((session) => (
        <div
          key={session.id}
          className={`session-preview-cell${session.id === activeSessionId ? " session-preview-cell--active" : ""}`}
        >
          <button
            type="button"
            className="session-preview-cell-head"
            title="切换到该会话"
            onClick={() => onActivateSession(session.id)}
          >
            {session.connectionId ? (
              <span
                className="tab-connection-dot"
                style={{ background: connectionColor(session.connectionId) }}
                aria-hidden="true"
              />
            ) : null}
            <span className="session-preview-cell-title">{session.title}</span>
            <span className="session-preview-cell-status">
              {session.status === "connected"
                ? ""
                : session.status === "connecting"
                  ? "连接中"
                  : "已断开"}
            </span>
          </button>
          <div
            className="session-preview-cell-term"
            ref={(element) => {
              if (element) {
                cellRefs.current.set(session.id, element);
              } else {
                cellRefs.current.delete(session.id);
              }
            }}
          />
        </div>
      ))}
      {sessions.length === 0 ? (
        <div className="session-preview-empty">没有可预览的终端会话</div>
      ) : null}
    </div>
  );
};
