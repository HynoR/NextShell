import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState
} from "react";
import { App as AntdApp } from "antd";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { ClipboardAddon } from "@xterm/addon-clipboard";
import { WebglAddon } from "@xterm/addon-webgl";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";
import type { SessionAuthOverrideInput } from "@nextshell/shared";
import {
  MAX_SESSION_OUTPUT_BYTES,
  appendWithLimit,
  createEmptyBuffer,
  toReplayChunks,
  type SessionOutputBuffer
} from "../utils/sessionOutputBuffer";
import { formatErrorMessage } from "../utils/errorMessage";
import { usePreferencesStore } from "../store/usePreferencesStore";
import {
  buildTerminalAuthIntro,
  buildTerminalAuthRetryNotice,
  consumeTerminalAuthInput,
  createTerminalAuthState,
  isAuthFailureReason,
  resetTerminalAuthForRetry,
  stripAuthFailurePrefix,
  type TerminalAuthState
} from "../utils/terminal-auth-flow";

interface TerminalPaneProps {
  connection?: ConnectionProfile;
  session?: SessionDescriptor;
  sessionIds: string[];
  onRetrySessionAuth: (
    sessionId: string,
    authOverride: SessionAuthOverrideInput
  ) => Promise<{ ok: true } | { ok: false; authRequired: boolean; reason: string }>;
  onRequestSearchMode?: () => void;
}

export interface TerminalPaneHandle {
  setSearchTerm: (value: string) => void;
  findNext: () => void;
  findPrevious: () => void;
  fit: () => void;
}

interface FrozenTerminalOptions {
  backspaceMode: ConnectionProfile["backspaceMode"];
  deleteMode: ConnectionProfile["deleteMode"];
}

const DEFAULT_TERMINAL_OPTIONS: FrozenTerminalOptions = {
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete"
};

const CUSTOM_FONT_PREFIX = "NextShell Custom";
const customFontCache = new Map<string, string>();

const getFontFileStem = (filePath: string): string => {
  const normalized = filePath.replace(/\\/g, "/");
  const base = normalized.split("/").pop() ?? "";
  return base.replace(/\.[^.]+$/, "").trim();
};

const deriveCustomFontFamily = (filePath: string): string => {
  const stem = getFontFileStem(filePath);
  if (!stem) {
    return CUSTOM_FONT_PREFIX;
  }
  return `${CUSTOM_FONT_PREFIX} ${stem}`;
};

const buildAssetUrl = (filePath: string): string => {
  return `nextshell-asset://local${encodeURI(filePath)}`;
};

const ensureCustomFontLoaded = async (filePath: string): Promise<string> => {
  const cached = customFontCache.get(filePath);
  if (cached) {
    return cached;
  }
  const family = deriveCustomFontFamily(filePath);
  try {
    const face = new FontFace(family, `url("${buildAssetUrl(filePath)}")`);
    await face.load();
    document.fonts.add(face);
  } catch {
    // ignore loading errors; fall back to system fonts
  }
  customFontCache.set(filePath, family);
  return family;
};

const buildTerminalFontFamily = (
  fontFamily: string,
  customFontPath?: string
): string => {
  if (customFontPath) {
    return deriveCustomFontFamily(customFontPath);
  }
  const trimmed = fontFamily.trim();
  return trimmed.length > 0 ? trimmed : "monospace";
};

const sequenceByBackspaceMode = (
  mode: ConnectionProfile["backspaceMode"]
): string => {
  if (mode === "ascii-delete") {
    return "\x7f";
  }

  return "\x08";
};

const sequenceByDeleteMode = (
  mode: ConnectionProfile["deleteMode"]
): string => {
  if (mode === "ascii-delete") {
    return "\x7f";
  }

  if (mode === "ascii-backspace") {
    return "\x08";
  }

  return "\x1b[3~";
};

const swallowSessionActionError = (error: unknown): void => {
  const reason = formatErrorMessage(error, "会话不存在");
  if (reason.includes("Session not found")) {
    return;
  }
};

const runSessionAction = (action: Promise<unknown>): void => {
  action.catch(swallowSessionActionError);
};

const statusMessage = (
  status: SessionDescriptor["status"],
  reason?: string
): string | undefined => {
  if (status === "connecting") {
    return "正在建立 SSH 会话...";
  }

  if (status === "connected") {
    return reason
      ? `SSH 会话已连接：${formatErrorMessage(reason, "连接成功")}`
      : "SSH 会话已连接。";
  }

  if (status === "disconnected") {
    return "SSH 会话已断开。";
  }

  if (status === "failed") {
    const displayReason = stripAuthFailurePrefix(reason);
    return `SSH 会话连接失败：${formatErrorMessage(displayReason, "未知原因")}`;
  }

  return undefined;
};

const isAuthRetryInProgress = (
  status: SessionDescriptor["status"],
  reason?: string
): boolean =>
  status === "failed" && isAuthFailureReason(reason);

const formatStatusOutput = (
  status: SessionDescriptor["status"],
  reason?: string
): string | undefined => {
  if (status === "connected") {
    return undefined;
  }
  if (isAuthRetryInProgress(status, reason)) {
    return undefined;
  }
  const msg = statusMessage(status, reason);
  if (!msg) {
    return undefined;
  }
  return `${msg}\r\n`;
};

export const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(({
  connection,
  session,
  sessionIds,
  onRetrySessionAuth,
  onRequestSearchMode
}, ref) => {
  const { message } = AntdApp.useApp();
  const terminalPreferences = usePreferencesStore((state) => state.preferences.terminal);
  const terminalFontFamily = buildTerminalFontFamily(
    terminalPreferences.fontFamily,
    terminalPreferences.customFontPath
  );
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const searchAddonRef = useRef<SearchAddon | null>(null);
  const searchTermRef = useRef<string>("");
  const sessionIdRef = useRef<string | undefined>(undefined);
  const bufferBySessionRef = useRef<Map<string, SessionOutputBuffer>>(new Map());
  const lastStatusKeyBySessionRef = useRef<Map<string, string>>(new Map());
  const knownSessionIdsRef = useRef<Set<string>>(new Set());
  const frozenSessionIdRef = useRef<string | undefined>(undefined);
  const terminalOptionsRef = useRef<FrozenTerminalOptions>(DEFAULT_TERMINAL_OPTIONS);
  const onRequestSearchModeRef = useRef<TerminalPaneProps["onRequestSearchMode"]>(onRequestSearchMode);
  const onRetrySessionAuthRef = useRef<TerminalPaneProps["onRetrySessionAuth"]>(onRetrySessionAuth);
  const findNextRef = useRef<() => void>(() => {});
  const findPreviousRef = useRef<() => void>(() => {});
  const authStateBySessionRef = useRef<Map<string, TerminalAuthState>>(new Map());
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number } | null>(null);
  const ctxMenuRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    onRequestSearchModeRef.current = onRequestSearchMode;
  }, [onRequestSearchMode]);

  useEffect(() => {
    onRetrySessionAuthRef.current = onRetrySessionAuth;
  }, [onRetrySessionAuth]);
  const appendSessionOutput = useCallback((targetSessionId: string, text: string) => {
    if (!knownSessionIdsRef.current.has(targetSessionId) || !text) {
      return;
    }

    const existing = bufferBySessionRef.current.get(targetSessionId) ?? createEmptyBuffer();
    const next = appendWithLimit(existing, text, MAX_SESSION_OUTPUT_BYTES);
    bufferBySessionRef.current.set(targetSessionId, next);
  }, []);

  const writeLocalOutput = useCallback(
    (
      targetSessionId: string,
      text: string,
      options?: { persist?: boolean }
    ) => {
      if (!text) {
        return;
      }
      if (options?.persist !== false) {
        appendSessionOutput(targetSessionId, text);
      }
      if (sessionIdRef.current === targetSessionId) {
        terminalRef.current?.write(text);
      }
    },
    [appendSessionOutput]
  );

  const beginLocalAuthPrompt = useCallback(
    (targetSessionId: string, reason?: string) => {
      const existing = authStateBySessionRef.current.get(targetSessionId);
      if (existing) {
        return;
      }
      authStateBySessionRef.current.set(targetSessionId, createTerminalAuthState());
      writeLocalOutput(targetSessionId, buildTerminalAuthIntro(reason));
    },
    [writeLocalOutput]
  );

  const handleLocalAuthInput = useCallback(
    (targetSessionId: string, data: string) => {
      const current = authStateBySessionRef.current.get(targetSessionId);
      if (!current) {
        return false;
      }

      const consumed = consumeTerminalAuthInput(current, data);
      authStateBySessionRef.current.set(targetSessionId, consumed.nextState);
      if (consumed.output) {
        writeLocalOutput(targetSessionId, consumed.output);
      }

      if (!consumed.submit) {
        return true;
      }

      const { username, password, nonce } = consumed.submit;
      const authType = connection?.authType === "interactive" ? "interactive" : "password";
      void onRetrySessionAuthRef.current(targetSessionId, {
        username,
        authType,
        password
      }).then((result) => {
        const latest = authStateBySessionRef.current.get(targetSessionId);
        if (!latest || latest.nonce !== nonce) {
          return;
        }

        if (result.ok) {
          authStateBySessionRef.current.delete(targetSessionId);
          return;
        }

        if (!result.authRequired) {
          authStateBySessionRef.current.delete(targetSessionId);
          return;
        }

        const retried = resetTerminalAuthForRetry(latest);
        authStateBySessionRef.current.set(targetSessionId, retried);
        writeLocalOutput(targetSessionId, buildTerminalAuthRetryNotice(result.reason));
      }).finally(() => {
        // Ensure no stale password remains if user closes before retry completes.
        const latest = authStateBySessionRef.current.get(targetSessionId);
        if (latest?.stage === "submitting") {
          authStateBySessionRef.current.set(targetSessionId, {
            ...latest,
            passwordBuffer: ""
          });
        }
      });

      return true;
    },
    [connection?.authType, writeLocalOutput]
  );

  const replaySessionOutput = useCallback((targetSessionId: string) => {
    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }

    terminal.reset();

    const buffer = bufferBySessionRef.current.get(targetSessionId);
    if (!buffer) {
      return;
    }

    const replay = toReplayChunks(buffer).join("");
    if (replay) {
      terminal.write(replay);
    }
  }, []);

  const findNext = useCallback(() => {
    const nextTerm = searchTermRef.current.trim();
    if (!nextTerm) {
      return;
    }
    searchAddonRef.current?.findNext(nextTerm);
  }, []);

  const findPrevious = useCallback(() => {
    const nextTerm = searchTermRef.current.trim();
    if (!nextTerm) {
      return;
    }
    searchAddonRef.current?.findPrevious(nextTerm);
  }, []);

  useEffect(() => {
    findNextRef.current = findNext;
    findPreviousRef.current = findPrevious;
  }, [findNext, findPrevious]);

  // Context menu outside-click and Escape dismissal
  useEffect(() => {
    if (!ctxMenu) {
      return;
    }

    const handleMouseDown = (e: MouseEvent) => {
      if (ctxMenuRef.current && !ctxMenuRef.current.contains(e.target as Node)) {
        setCtxMenu(null);
      }
    };

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setCtxMenu(null);
      }
    };

    document.addEventListener("mousedown", handleMouseDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handleMouseDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [ctxMenu]);

  const handleContextMenu = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    const menuWidth = 200;
    const menuHeight = 200;
    const x = Math.min(e.clientX, window.innerWidth - menuWidth);
    const y = Math.min(e.clientY, window.innerHeight - menuHeight);
    setCtxMenu({ x, y });
  }, []);

  const handleCtxCopy = useCallback(() => {
    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }
    const selection = terminal.getSelection();
    if (selection) {
      void navigator.clipboard.writeText(selection);
    }
    setCtxMenu(null);
  }, []);

  const handleCtxPaste = useCallback(() => {
    const sessionId = sessionIdRef.current;
    if (!sessionId) {
      return;
    }
    runSessionAction(
      navigator.clipboard.readText().then((text) => {
        if (!text) {
          return;
        }
        if (authStateBySessionRef.current.has(sessionId)) {
          handleLocalAuthInput(sessionId, text);
          return;
        }
        return window.nextshell.session.write({ sessionId, data: text });
      })
    );
    setCtxMenu(null);
  }, [handleLocalAuthInput]);

  const handleCtxPasteSelection = useCallback(() => {
    const terminal = terminalRef.current;
    const sessionId = sessionIdRef.current;
    if (!terminal || !sessionId) {
      return;
    }
    const selection = terminal.getSelection();
    if (selection) {
      void navigator.clipboard.writeText(selection);
      if (authStateBySessionRef.current.has(sessionId)) {
        handleLocalAuthInput(sessionId, selection);
        setCtxMenu(null);
        return;
      }
      runSessionAction(
        window.nextshell.session.write({ sessionId, data: selection })
      );
    }
    setCtxMenu(null);
  }, [handleLocalAuthInput]);

  const handleCtxClear = useCallback(() => {
    const terminal = terminalRef.current;
    const sessionId = sessionIdRef.current;
    if (!terminal) {
      return;
    }
    terminal.reset();
    if (sessionId) {
      bufferBySessionRef.current.set(sessionId, createEmptyBuffer());
    }
    setCtxMenu(null);
  }, []);

  useImperativeHandle(
    ref,
    () => ({
      setSearchTerm: (value: string) => {
        searchTermRef.current = value;
        const nextTerm = value.trim();
        if (!nextTerm) {
          return;
        }
        searchAddonRef.current?.findNext(nextTerm, { incremental: true });
      },
      findNext,
      findPrevious,
      fit: () => {
        fitRef.current?.fit();
      }
    }),
    [findNext, findPrevious]
  );

  useEffect(() => {
    const knownSessionIds = new Set(sessionIds);
    knownSessionIdsRef.current = knownSessionIds;

    for (const sessionId of Array.from(bufferBySessionRef.current.keys())) {
      if (!knownSessionIds.has(sessionId)) {
        bufferBySessionRef.current.delete(sessionId);
      }
    }

    for (const sessionId of Array.from(lastStatusKeyBySessionRef.current.keys())) {
      if (!knownSessionIds.has(sessionId)) {
        lastStatusKeyBySessionRef.current.delete(sessionId);
      }
    }

    for (const sessionId of Array.from(authStateBySessionRef.current.keys())) {
      if (!knownSessionIds.has(sessionId)) {
        authStateBySessionRef.current.delete(sessionId);
      }
    }
  }, [sessionIds]);

  useEffect(() => {
    if (!containerRef.current || terminalRef.current) {
      return;
    }

    if (terminalPreferences.customFontPath) {
      void ensureCustomFontLoaded(terminalPreferences.customFontPath);
    }

    const terminalBg = terminalPreferences.backgroundColor;
    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: terminalPreferences.fontSize,
      lineHeight: terminalPreferences.lineHeight,
      fontFamily: terminalFontFamily,
      theme: {
        background: terminalBg,
        foreground: terminalPreferences.foregroundColor,
        cursor: terminalPreferences.foregroundColor
      }
    });

    const fitAddon = new FitAddon();
    const searchAddon = new SearchAddon();
    const clipboardAddon = new ClipboardAddon();

    terminal.loadAddon(fitAddon);
    terminal.loadAddon(searchAddon);
    terminal.loadAddon(clipboardAddon);

    try {
      const webglAddon = new WebglAddon();
      terminal.loadAddon(webglAddon);
      webglAddon.onContextLoss(() => {
        webglAddon.dispose();
      });
    } catch {
      // webgl acceleration is optional
    }

    terminal.open(containerRef.current);
    fitAddon.fit();

    terminal.attachCustomKeyEventHandler((event: KeyboardEvent) => {
      if (event.type !== "keydown") {
        return true;
      }

      const ctrlOrMeta = event.ctrlKey || event.metaKey;
      const key = event.key.toLowerCase();

      const searchPressed = ctrlOrMeta && event.shiftKey && key === "f";
      if (searchPressed) {
        onRequestSearchModeRef.current?.();
        return false;
      }

      if (event.key === "F3") {
        if (event.shiftKey) {
          findPreviousRef.current();
        } else {
          findNextRef.current();
        }
        return false;
      }

      const copyPressed = ctrlOrMeta && event.shiftKey && key === "c";
      if (copyPressed) {
        const selection = terminal.getSelection();
        if (selection) {
          void navigator.clipboard.writeText(selection);
        }
        return false;
      }

      const pastePressed = ctrlOrMeta && event.shiftKey && key === "v";
      if (pastePressed) {
        const sessionId = sessionIdRef.current;
        if (!sessionId) {
          return false;
        }

        runSessionAction(
          navigator.clipboard.readText().then((text) => {
            if (!text) {
              return;
            }

            if (authStateBySessionRef.current.has(sessionId)) {
              handleLocalAuthInput(sessionId, text);
              return;
            }

            return window.nextshell.session.write({
              sessionId,
              data: text
            });
          })
        );
        return false;
      }

      if (event.key === "Backspace") {
        const sessionId = sessionIdRef.current;
        if (!sessionId) {
          return false;
        }

        if (authStateBySessionRef.current.has(sessionId)) {
          handleLocalAuthInput(sessionId, "\x7f");
          return false;
        }

        runSessionAction(window.nextshell.session.write({
          sessionId,
          data: sequenceByBackspaceMode(terminalOptionsRef.current.backspaceMode)
        }));
        return false;
      }

      if (event.key === "Delete") {
        const sessionId = sessionIdRef.current;
        if (!sessionId) {
          return false;
        }

        if (authStateBySessionRef.current.has(sessionId)) {
          handleLocalAuthInput(sessionId, "\x7f");
          return false;
        }

        runSessionAction(window.nextshell.session.write({
          sessionId,
          data: sequenceByDeleteMode(terminalOptionsRef.current.deleteMode)
        }));
        return false;
      }

      return true;
    });

    const dataSub = terminal.onData((data) => {
      const sessionId = sessionIdRef.current;
      if (!sessionId) {
        return;
      }

      if (authStateBySessionRef.current.has(sessionId)) {
        handleLocalAuthInput(sessionId, data);
        return;
      }

      runSessionAction(window.nextshell.session.write({
        sessionId,
        data
      }));
    });

    const resizeSub = terminal.onResize(({ cols, rows }) => {
      const sessionId = sessionIdRef.current;
      if (!sessionId) {
        return;
      }

      runSessionAction(window.nextshell.session.resize({
        sessionId,
        cols,
        rows
      }));
    });

    let resizeRafId = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(resizeRafId);
      resizeRafId = requestAnimationFrame(() => {
        fitAddon.fit();
        const sessionId = sessionIdRef.current;
        if (!sessionId) {
          return;
        }

        runSessionAction(window.nextshell.session.resize({
          sessionId,
          cols: terminal.cols,
          rows: terminal.rows
        }));
      });
    });

    observer.observe(containerRef.current);

    terminalRef.current = terminal;
    fitRef.current = fitAddon;
    searchAddonRef.current = searchAddon;

    return () => {
      cancelAnimationFrame(resizeRafId);
      observer.disconnect();
      dataSub.dispose();
      resizeSub.dispose();
      terminal.dispose();
      terminalRef.current = null;
      fitRef.current = null;
      searchAddonRef.current = null;
    };
  }, [handleLocalAuthInput]);

  useEffect(() => {
    if (terminalPreferences.customFontPath) {
      void ensureCustomFontLoaded(terminalPreferences.customFontPath);
    }

    const terminal = terminalRef.current;
    if (!terminal) {
      return;
    }

    const terminalBg = terminalPreferences.backgroundColor;
    terminal.options.theme = {
      ...terminal.options.theme,
      background: terminalBg,
      foreground: terminalPreferences.foregroundColor,
      cursor: terminalPreferences.foregroundColor
    };
    terminal.options.fontFamily = terminalFontFamily;
    terminal.options.fontSize = terminalPreferences.fontSize;
    terminal.options.lineHeight = terminalPreferences.lineHeight;

    fitRef.current?.fit();
    const sessionId = sessionIdRef.current;
    if (!sessionId) {
      return;
    }

    runSessionAction(window.nextshell.session.resize({
      sessionId,
      cols: terminal.cols,
      rows: terminal.rows
    }));
  }, [
    terminalPreferences.backgroundColor,
    terminalPreferences.foregroundColor,
    terminalPreferences.fontFamily,
    terminalPreferences.customFontPath,
    terminalFontFamily,
    terminalPreferences.fontSize,
    terminalPreferences.lineHeight
  ]);

  useEffect(() => {
    const offData = window.nextshell.session.onData((event) => {
      if (!knownSessionIdsRef.current.has(event.sessionId)) {
        return;
      }

      appendSessionOutput(event.sessionId, event.data);
      if (event.sessionId === sessionIdRef.current) {
        terminalRef.current?.write(event.data);
      }
    });

    const offStatus = window.nextshell.session.onStatus((event) => {
      if (!knownSessionIdsRef.current.has(event.sessionId)) {
        return;
      }

      if (event.status === "connected" || event.status === "disconnected") {
        authStateBySessionRef.current.delete(event.sessionId);
      }

      const eventKey = `${event.sessionId}:${event.status}:${event.reason ?? ""}`;
      const previousEventKey = lastStatusKeyBySessionRef.current.get(event.sessionId);
      if (previousEventKey === eventKey) {
        return;
      }
      lastStatusKeyBySessionRef.current.set(event.sessionId, eventKey);

      if (event.status === "connected") {
        const text = event.reason
          ? `连接已建立，${event.reason}`
          : "连接已建立。";
        message.success(text);
      }

      if (event.status === "failed" && isAuthFailureReason(event.reason)) {
        beginLocalAuthPrompt(event.sessionId, event.reason);
        return;
      }

      const output = formatStatusOutput(event.status, event.reason);
      if (!output) {
        return;
      }

      appendSessionOutput(event.sessionId, output);
      if (event.sessionId === sessionIdRef.current) {
        terminalRef.current?.write(output);
      }
    });

    return () => {
      offData();
      offStatus();
    };
  }, [appendSessionOutput, beginLocalAuthPrompt, message]);

  useEffect(() => {
    const previousSessionId = sessionIdRef.current;
    const currentSessionId = session?.id;
    sessionIdRef.current = currentSessionId;

    if (previousSessionId !== currentSessionId) {
      if (!currentSessionId) {
        terminalRef.current?.reset();
      } else {
        if (session?.status === "connecting") {
          const connectingEventKey = `${currentSessionId}:connecting:`;
          if (lastStatusKeyBySessionRef.current.get(currentSessionId) !== connectingEventKey) {
            lastStatusKeyBySessionRef.current.set(currentSessionId, connectingEventKey);
            const output = formatStatusOutput("connecting");
            if (output) {
              appendSessionOutput(currentSessionId, output);
            }
          }
        }

        replaySessionOutput(currentSessionId);
      }
    }

    if (currentSessionId && session?.status === "failed" && isAuthFailureReason(session.reason)) {
      beginLocalAuthPrompt(currentSessionId, session.reason);
    }

    if (currentSessionId && (session?.status === "connected" || session?.status === "disconnected")) {
      authStateBySessionRef.current.delete(currentSessionId);
    }

    if (frozenSessionIdRef.current !== currentSessionId) {
      frozenSessionIdRef.current = currentSessionId;
      terminalOptionsRef.current = connection
        ? {
            backspaceMode: connection.backspaceMode,
            deleteMode: connection.deleteMode
          }
        : DEFAULT_TERMINAL_OPTIONS;
    }

    if (!terminalRef.current) {
      return;
    }

    if (session && connection && session.status === "connected") {
      fitRef.current?.fit();
      runSessionAction(window.nextshell.session.resize({
        sessionId: session.id,
        cols: terminalRef.current.cols,
        rows: terminalRef.current.rows
      }));
    }
  }, [appendSessionOutput, beginLocalAuthPrompt, connection, replaySessionOutput, session]);

  const prevSessionStatusRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    const currentSessionId = session?.id;
    const currentStatus = session?.status;
    const prevStatus = prevSessionStatusRef.current;
    prevSessionStatusRef.current = currentStatus;

    if (
      currentSessionId &&
      currentStatus === "failed" &&
      prevStatus !== undefined &&
      prevStatus !== "failed"
    ) {
      if (isAuthFailureReason(session?.reason)) {
        beginLocalAuthPrompt(currentSessionId, session.reason);
        return;
      }

      // Skip if the IPC onStatus event already wrote this failure to the terminal
      const lastKey = lastStatusKeyBySessionRef.current.get(currentSessionId);
      if (lastKey?.includes(":failed:")) {
        return;
      }
      const output = formatStatusOutput("failed", session?.reason);
      if (output) {
        appendSessionOutput(currentSessionId, output);
        if (currentSessionId === sessionIdRef.current) {
          terminalRef.current?.write(output);
        }
      }
    }
  }, [appendSessionOutput, beginLocalAuthPrompt, session?.id, session?.reason, session?.status]);

  const hasSelection = ctxMenu ? !!terminalRef.current?.getSelection() : false;
  const hasSession = !!sessionIdRef.current;

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div
        className="flex-1 min-h-0 py-1.5 px-1"
        ref={containerRef}
        onContextMenu={handleContextMenu}
      />
      {ctxMenu && (
        <div
          ref={ctxMenuRef}
          className="fe-ctx-menu"
          style={{ left: ctxMenu.x, top: ctxMenu.y }}
        >
          <button
            type="button"
            className="fe-ctx-item"
            disabled={!hasSelection}
            onClick={handleCtxCopy}
          >
            <span className="fe-ctx-icon"><i className="ri-file-copy-line" /></span>
            复制选中内容
          </button>
          <button
            type="button"
            className="fe-ctx-item"
            disabled={!hasSession}
            onClick={handleCtxPaste}
          >
            <span className="fe-ctx-icon"><i className="ri-clipboard-line" /></span>
            粘贴
          </button>
          <button
            type="button"
            className="fe-ctx-item"
            disabled={!hasSelection || !hasSession}
            onClick={handleCtxPasteSelection}
          >
            <span className="fe-ctx-icon"><i className="ri-file-copy-2-line" /></span>
            粘贴选中
          </button>
          <div className="fe-ctx-divider" />
          <button
            type="button"
            className="fe-ctx-item fe-ctx-danger"
            onClick={handleCtxClear}
          >
            <span className="fe-ctx-icon"><i className="ri-delete-bin-line" /></span>
            清空界面
          </button>
        </div>
      )}
    </div>
  );
});

TerminalPane.displayName = "TerminalPane";
