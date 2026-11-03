import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from "react";
import { App as AntdApp } from "antd";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { WebglAddon } from "@xterm/addon-webgl";
import type { ConnectionProfile, SessionDescriptor } from "@nextshell/core";
import type { SessionAuthOverrideInput } from "@nextshell/shared";
import {
  MAX_SESSION_OUTPUT_BYTES,
  REPLAY_MAX_BYTES,
  appendWithLimit,
  buildReplayPayload,
  createEmptyBuffer,
  toReplayChunks,
  type ReplayPayload,
  type SessionOutputBuffer
} from "../utils/sessionOutputBuffer";
import {
  retainSessionsInCollections,
  setBoundedSessionMapEntry
} from "../utils/sessionScopedCollections";
import { formatErrorMessage } from "../utils/errorMessage";
import { useWorkspaceStore } from "../store/useWorkspaceStore";
import { usePreferencesStore } from "../store/usePreferencesStore";
import { shouldTrackTerminalSessionMetadata } from "../utils/terminalSessionMonitoring";
import { shouldReconnectOnInput } from "../utils/terminal-reconnect";
import { resolveTerminalWallpaperRendering } from "../utils/terminalWallpaper";
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
import {
  consumeTerminalQueryReplyChunk,
  createTerminalQueryReplyFilterState,
  installTerminalQueryCompatibilityGuards
} from "../utils/terminalControlSequenceCompat";
import { installOscRuntime, type OscRuntimeHandle } from "../terminal/oscRuntime";
import { installParserHandlerGuards } from "../terminal/parserGuards";
import { openExternalLink } from "../terminal/osc/linkOpening";
import { clampScrollLinesFromBottom, shouldRememberSessionScroll } from "./TerminalPane.switching";
import { setSessionBacklogProvider } from "../terminal/sessionBacklogProvider";
import { createSwitchFreezeFrame, type SwitchFreezeFrame } from "../terminal/switchFreezeFrame";

type LocalAwareSessionDescriptor = SessionDescriptor & {
  target?: "remote" | "local";
};

const isLocalSession = (session?: SessionDescriptor): boolean =>
  (session as LocalAwareSessionDescriptor | undefined)?.target === "local";

const isRemoteSession = (session?: SessionDescriptor): boolean => !isLocalSession(session);

interface TerminalPaneProps {
  connection?: ConnectionProfile;
  session?: SessionDescriptor;
  sessionIds: string[];
  onReconnectSession: (sessionId: string) => Promise<void> | void;
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
  focus: () => void;
}

interface FrozenTerminalOptions {
  backspaceMode: ConnectionProfile["backspaceMode"];
  deleteMode: ConnectionProfile["deleteMode"];
}

const DEFAULT_TERMINAL_OPTIONS: FrozenTerminalOptions = {
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete"
};
const MAX_BUFFERED_SESSION_COUNT = 32;
/** Theme background used when the terminal lets the app wallpaper through. */
const TRANSPARENT_TERMINAL_BACKGROUND = "rgba(0, 0, 0, 0)";

/**
 * Output can reach the renderer before React has committed the session into
 * `sessionIds` — the main process starts streaming as soon as the channel is
 * up, while the store update that makes the session "known" here lands a tick
 * later. Those bytes are parked per session until the session shows up, which
 * is what keeps the opening banner/prompt from silently disappearing.
 *
 * Both bounds exist because a session id may never arrive at all (opened and
 * closed again before the renderer heard about it): the map holds at most a
 * handful of sessions and a modest slice of each one's head-of-stream, so a
 * stream that is never claimed cannot grow without limit.
 */
const MAX_PENDING_SESSION_COUNT = 8;
const MAX_PENDING_SESSION_BYTES = 256 * 1024;

/**
 * Delivery acks are batched per session: instead of one IPC invoke per frame
 * (~60/s at the dispatcher's flush cadence), consumed bytes accumulate and a
 * single cumulative ack is flushed when either the byte threshold is crossed
 * (synchronously, from the xterm write callback — hidden windows throttle
 * timers, so bulk throughput must never depend on one) or the short timer
 * fires. The threshold must stay well below the dispatcher's send window
 * (512KB) or a bulk stream would stall waiting for a throttled timer.
 */
const ACK_FLUSH_THRESHOLD_BYTES = 128 * 1024;
const ACK_FLUSH_INTERVAL_MS = 50;

interface SessionAckAccumulator {
  /** Delta of consumed bytes since the last flushed ack. */
  bytes: number;
  /** Highest deliveryId processed so far. */
  lastDeliveryId: number;
  timer: ReturnType<typeof setTimeout> | undefined;
}

/**
 * Replay instrumentation switch. Vite statically replaces `import.meta.env.DEV`,
 * so a production bundle drops both helpers below and every call site with them
 * — the only cost left in production is the two `performance.now()` reads being
 * eliminated too.
 */
const REPLAY_DEBUG = import.meta.env.DEV;
/** Measure name to look for in the DevTools Performance timings track. */
const REPLAY_MEASURE_NAME = "nextshell:replay";

const beginReplayMeasurement = (requestId: number): string => {
  // Per-request mark name: two rapid switches can have replays in flight at
  // once, and a shared mark would let the second one close the first's measure.
  const startMark = `${REPLAY_MEASURE_NAME}:start:${requestId}`;
  performance.mark(startMark);
  return startMark;
};

const endReplayMeasurement = (
  sessionId: string,
  payload: ReplayPayload,
  startMark: string,
  startedAt: number
): void => {
  const parseMs = performance.now() - startedAt;
  try {
    performance.measure(REPLAY_MEASURE_NAME, startMark);
  } catch {
    // The start mark is gone (entry buffer cleared mid-replay); the console
    // line below still carries the duration.
  }
  performance.clearMarks(startMark);
  console.debug(
    `[${REPLAY_MEASURE_NAME}] session=${sessionId} bytes=${payload.writtenBytes}/${payload.totalBytes} chunks=${payload.chunkCount} truncated=${payload.truncated} parse=${parseMs.toFixed(1)}ms`
  );
};

const sequenceByBackspaceMode = (mode: ConnectionProfile["backspaceMode"]): string => {
  if (mode === "ascii-delete") {
    return "\x7f";
  }

  return "\x08";
};

const sequenceByDeleteMode = (mode: ConnectionProfile["deleteMode"]): string => {
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

const sendSessionAck = (sessionId: string, deliveryId: number, consumedBytes: number): void => {
  runSessionAction(
    window.nextshell.session.ackData({
      streamKind: "session",
      streamId: sessionId,
      deliveryId,
      consumedBytes
    })
  );
};

const statusMessage = (
  session: SessionDescriptor | undefined,
  status: SessionDescriptor["status"],
  reason?: string
): string | undefined => {
  if (isLocalSession(session)) {
    if (status === "connecting") {
      return "正在启动本地终端...";
    }

    if (status === "connected") {
      return reason
        ? `本地终端已启动：${formatErrorMessage(reason, "启动成功")}`
        : "本地终端已启动。";
    }

    if (status === "disconnected") {
      return "本地终端已断开。按回车键重新打开。";
    }

    if (status === "failed") {
      return `本地终端启动失败：${formatErrorMessage(reason, "未知原因")}。按回车键重试。`;
    }

    return undefined;
  }

  if (status === "connecting") {
    return "正在建立 SSH 会话...";
  }

  if (status === "connected") {
    return reason
      ? `SSH 会话已连接：${formatErrorMessage(reason, "连接成功")}`
      : "SSH 会话已连接。";
  }

  if (status === "disconnected") {
    return "SSH 会话已断开。按回车键尝试重连。";
  }

  if (status === "failed") {
    const displayReason = stripAuthFailurePrefix(reason);
    return `SSH 会话连接失败：${formatErrorMessage(displayReason, "未知原因")}。按回车键尝试重连。`;
  }

  return undefined;
};

const isAuthRetryInProgress = (
  session: SessionDescriptor | undefined,
  status: SessionDescriptor["status"],
  reason?: string
): boolean => isRemoteSession(session) && status === "failed" && isAuthFailureReason(reason);

const formatStatusOutput = (
  session: SessionDescriptor | undefined,
  status: SessionDescriptor["status"],
  reason?: string
): string | undefined => {
  if (status === "connected") {
    return undefined;
  }
  if (isAuthRetryInProgress(session, status, reason)) {
    return undefined;
  }
  const msg = statusMessage(session, status, reason);
  if (!msg) {
    return undefined;
  }
  return `${msg}\r\n`;
};

export const TerminalPane = forwardRef<TerminalPaneHandle, TerminalPaneProps>(
  (
    {
      connection,
      session,
      sessionIds,
      onReconnectSession,
      onRetrySessionAuth,
      onRequestSearchMode
    },
    ref
  ) => {
    const { message } = AntdApp.useApp();
    const terminalPreferences = usePreferencesStore((state) => state.preferences.terminal);
    const appBackgroundImagePath = usePreferencesStore(
      (state) => state.preferences.window.backgroundImagePath
    );
    const { transparent: transparencyEnabled, webgl: webglEnabled } =
      resolveTerminalWallpaperRendering(appBackgroundImagePath, terminalPreferences.wallpaper);
    const containerRef = useRef<HTMLDivElement | null>(null);
    const terminalRef = useRef<Terminal | null>(null);
    const fitRef = useRef<FitAddon | null>(null);
    const searchAddonRef = useRef<SearchAddon | null>(null);
    const searchTermRef = useRef<string>("");
    const sessionIdRef = useRef<string | undefined>(undefined);
    const bufferBySessionRef = useRef<Map<string, SessionOutputBuffer>>(new Map());
    const pendingDataBySessionRef = useRef<Map<string, SessionOutputBuffer>>(new Map());
    const lastStatusKeyBySessionRef = useRef<Map<string, string>>(new Map());
    const knownSessionIdsRef = useRef<Set<string>>(new Set());
    const frozenSessionIdRef = useRef<string | undefined>(undefined);
    const oscRuntimeRef = useRef<OscRuntimeHandle | null>(null);
    /**
     * Holds the outgoing session's pixels on screen while the incoming replay
     * parses, so a switch does not flash the bare background.
     */
    const switchFreezeFrameRef = useRef<SwitchFreezeFrame | null>(null);
    /**
     * Monotonic id of the newest requested replay. A replay repaints only if it
     * is still the newest one by the time xterm's write queue has drained.
     */
    const replayRequestIdRef = useRef(0);
    const terminalQueryReplyStateBySessionRef = useRef<
      Map<string, ReturnType<typeof createTerminalQueryReplyFilterState>>
    >(new Map());
    const terminalQuerySuppressionCountBySessionRef = useRef<Map<string, number>>(new Map());
    const terminalCompatEnabledRef = useRef(false);
    const terminalOptionsRef = useRef<FrozenTerminalOptions>(DEFAULT_TERMINAL_OPTIONS);
    const onRequestSearchModeRef =
      useRef<TerminalPaneProps["onRequestSearchMode"]>(onRequestSearchMode);
    const onReconnectSessionRef =
      useRef<TerminalPaneProps["onReconnectSession"]>(onReconnectSession);
    const onRetrySessionAuthRef =
      useRef<TerminalPaneProps["onRetrySessionAuth"]>(onRetrySessionAuth);
    const findNextRef = useRef<() => void>(() => {});
    const findPreviousRef = useRef<() => void>(() => {});
    const authStateBySessionRef = useRef<Map<string, TerminalAuthState>>(new Map());
    const sessionStatusBySessionRef = useRef<Map<string, SessionDescriptor["status"]>>(new Map());
    const reconnectPendingSessionIdsRef = useRef<Set<string>>(new Set());
    const ackAccumulatorBySessionRef = useRef<Map<string, SessionAckAccumulator>>(new Map());
    /**
     * Per-session viewport position, kept as "lines above the bottom" rather
     * than an absolute row: the replayed buffer is a different height than the
     * one the user scrolled in (output kept arriving while the tab was
     * inactive, and the byte cap drops old lines), so the distance from the
     * bottom is the only offset that survives a replay. 0 means pinned to the
     * bottom, which is also the state of every session that was never scrolled.
     */
    const scrollLinesFromBottomBySessionRef = useRef<Map<string, number>>(new Map());
    /**
     * Which session's content the shared xterm buffer is currently painted with.
     * Only a replay (or a clear) that actually ran may move it — a replay runs
     * behind the write queue, so a switch that supersedes a still-queued replay
     * leaves the previous session's screen in place, and the scroll snapshot has
     * to know.
     */
    const displayedSessionIdRef = useRef<string | undefined>(undefined);
    const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number } | null>(null);
    const ctxMenuRef = useRef<HTMLDivElement | null>(null);

    const flushSessionAck = useCallback((targetSessionId: string) => {
      const accumulator = ackAccumulatorBySessionRef.current.get(targetSessionId);
      if (!accumulator) {
        return;
      }

      if (accumulator.timer !== undefined) {
        clearTimeout(accumulator.timer);
      }
      // Deleting the entry doubles as the accumulator reset: disconnect,
      // session removal, and unmount all flush here, so a reconnected stream
      // always starts from a clean delta.
      ackAccumulatorBySessionRef.current.delete(targetSessionId);
      if (accumulator.bytes <= 0 || accumulator.lastDeliveryId <= 0) {
        return;
      }

      sendSessionAck(targetSessionId, accumulator.lastDeliveryId, accumulator.bytes);
    }, []);

    const flushAllSessionAcks = useCallback(() => {
      for (const targetSessionId of Array.from(ackAccumulatorBySessionRef.current.keys())) {
        flushSessionAck(targetSessionId);
      }
    }, [flushSessionAck]);

    const accumulateSessionAck = useCallback(
      (targetSessionId: string, deliveryId: number, byteLength: number) => {
        const accumulators = ackAccumulatorBySessionRef.current;
        let accumulator = accumulators.get(targetSessionId);
        if (!accumulator) {
          accumulator = { bytes: 0, lastDeliveryId: 0, timer: undefined };
          accumulators.set(targetSessionId, accumulator);
        }

        accumulator.bytes += byteLength;
        if (deliveryId > accumulator.lastDeliveryId) {
          accumulator.lastDeliveryId = deliveryId;
        }

        if (accumulator.bytes >= ACK_FLUSH_THRESHOLD_BYTES) {
          // Synchronous flush: bulk throughput must never wait on a (possibly
          // throttled) timer to reopen the dispatcher's send window.
          flushSessionAck(targetSessionId);
          return;
        }

        if (accumulator.timer === undefined) {
          accumulator.timer = setTimeout(() => {
            const current = ackAccumulatorBySessionRef.current.get(targetSessionId);
            if (current) {
              current.timer = undefined;
            }
            flushSessionAck(targetSessionId);
          }, ACK_FLUSH_INTERVAL_MS);
        }
      },
      [flushSessionAck]
    );

    useEffect(() => {
      onRequestSearchModeRef.current = onRequestSearchMode;
    }, [onRequestSearchMode]);

    useEffect(() => {
      onReconnectSessionRef.current = onReconnectSession;
    }, [onReconnectSession]);

    useEffect(() => {
      onRetrySessionAuthRef.current = onRetrySessionAuth;
    }, [onRetrySessionAuth]);

    /**
     * The only path bytes take into xterm. Every write is tagged with the
     * session that produced it so the OSC runtime can attribute sequences to
     * the right session even while an older session's replay is still being
     * parsed — reading a "current session" ref inside an OSC handler would
     * credit whichever tab happens to be in front by then.
     */
    const writeSessionText = useCallback(
      (
        targetSessionId: string,
        text: string,
        options?: { replay?: boolean; onParsed?: () => void }
      ) => {
        const runtime = oscRuntimeRef.current;
        if (!runtime) {
          // The runtime and the terminal are created and torn down together, so
          // a missing runtime means there is nothing to write into; the
          // completion hook still has to run (it releases the delivery ack).
          options?.onParsed?.();
          return;
        }

        if (options?.replay) {
          runtime.replaySessionData(targetSessionId, text, options.onParsed);
          return;
        }
        runtime.writeSessionData(targetSessionId, text, options?.onParsed);
      },
      []
    );

    const appendSessionOutput = useCallback((targetSessionId: string, text: string) => {
      if (!knownSessionIdsRef.current.has(targetSessionId) || !text) {
        return;
      }

      const existing = bufferBySessionRef.current.get(targetSessionId) ?? createEmptyBuffer();
      const next = appendWithLimit(existing, text, MAX_SESSION_OUTPUT_BYTES);
      setBoundedSessionMapEntry(
        bufferBySessionRef.current,
        targetSessionId,
        next,
        MAX_BUFFERED_SESSION_COUNT,
        sessionIdRef.current ? [sessionIdRef.current] : []
      );
    }, []);

    // 把会话缓冲以只读方式暴露给监视网格等旁观者(模块级注册表,
    // 不经过 React 状态)。组件卸载时撤销,避免悬挂引用。
    useEffect(() => {
      setSessionBacklogProvider((targetSessionId) => {
        const buffer = bufferBySessionRef.current.get(targetSessionId);
        return buffer ? toReplayChunks(buffer).join("") : undefined;
      });
      return () => setSessionBacklogProvider(undefined);
    }, []);

    /** Park output for a session this component has not been told about yet. */
    const bufferPendingSessionData = useCallback((targetSessionId: string, text: string) => {
      if (!text) {
        return;
      }

      const pending = pendingDataBySessionRef.current;
      const existing = pending.get(targetSessionId) ?? createEmptyBuffer();
      const next = appendWithLimit(existing, text, MAX_PENDING_SESSION_BYTES);
      setBoundedSessionMapEntry(pending, targetSessionId, next, MAX_PENDING_SESSION_COUNT);
    }, []);

    /**
     * Hand parked output over to sessions that just became known. Runs before
     * any newly arriving data for those sessions is appended, so the stream
     * stays in order, and before the session-switch effect, so a session that
     * becomes known and active in the same commit gets its parked bytes into
     * the buffer in time for the replay.
     */
    const adoptPendingSessionData = useCallback(
      (knownSessionIds: ReadonlySet<string>) => {
        const pending = pendingDataBySessionRef.current;
        if (pending.size === 0) {
          return;
        }

        for (const [targetSessionId, buffer] of Array.from(pending.entries())) {
          if (!knownSessionIds.has(targetSessionId)) {
            continue;
          }

          pending.delete(targetSessionId);
          const text = toReplayChunks(buffer).join("");
          if (!text) {
            continue;
          }

          appendSessionOutput(targetSessionId, text);
          // Normally false — a session cannot be the foreground one before it
          // is known — but if it ever is, the replay path will not run and the
          // bytes have to reach the screen from here.
          if (targetSessionId === sessionIdRef.current) {
            writeSessionText(targetSessionId, text);
          }
        }
      },
      [appendSessionOutput, writeSessionText]
    );

    const writeLocalOutput = useCallback(
      (targetSessionId: string, text: string, options?: { persist?: boolean }) => {
        if (!text) {
          return;
        }
        if (options?.persist !== false) {
          appendSessionOutput(targetSessionId, text);
        }
        if (sessionIdRef.current === targetSessionId) {
          writeSessionText(targetSessionId, text);
        }
      },
      [appendSessionOutput, writeSessionText]
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
        void onRetrySessionAuthRef
          .current(targetSessionId, {
            username,
            authType,
            password
          })
          .then((result) => {
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
          })
          .finally(() => {
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

    const tryReconnectOnEnter = useCallback((targetSessionId: string, data: string): boolean => {
      const status = sessionStatusBySessionRef.current.get(targetSessionId);
      if (!shouldReconnectOnInput(status, data)) {
        return false;
      }

      if (reconnectPendingSessionIdsRef.current.has(targetSessionId)) {
        return true;
      }

      reconnectPendingSessionIdsRef.current.add(targetSessionId);
      Promise.resolve(onReconnectSessionRef.current(targetSessionId))
        .catch(swallowSessionActionError)
        .finally(() => {
          reconnectPendingSessionIdsRef.current.delete(targetSessionId);
        });
      return true;
    }, []);

    /**
     * Run a screen-replacing operation (reset, replay) once xterm has parsed
     * everything queued before it, and only if no newer one was requested in
     * the meantime.
     *
     * xterm's `Terminal.reset()` clears the buffers but leaves the write queue
     * untouched, so resetting the instant a tab switch happens lets the
     * outgoing session's unparsed backlog paint into the incoming session's
     * freshly cleared screen. Ordering the reset behind the queue is what makes
     * it actually discard that backlog; the request id keeps two rapid switches
     * from stacking the first one's scrollback above the second's.
     */
    const runLatestScreenChange = useCallback((apply: () => void) => {
      const requestId = replayRequestIdRef.current + 1;
      replayRequestIdRef.current = requestId;

      const guarded = (): void => {
        if (!terminalRef.current || replayRequestIdRef.current !== requestId) {
          return;
        }
        apply();
      };

      const runtime = oscRuntimeRef.current;
      if (!runtime) {
        guarded();
        return;
      }
      runtime.runAfterPendingWrites(guarded);
    }, []);

    /** Blank the screen (no session attached) without stranding OSC state. */
    const clearTerminalScreen = useCallback(() => {
      if (!terminalRef.current) {
        return;
      }
      runLatestScreenChange(() => {
        terminalRef.current?.reset();
        displayedSessionIdRef.current = undefined;
        // A blank screen is the intended result here, so any frame still frozen
        // from an earlier switch has to go instead of hiding it.
        switchFreezeFrameRef.current?.release();
        // No incoming session: markers and decorations the reset just
        // invalidated still have to be dropped.
        oscRuntimeRef.current?.notifyReplayStart(undefined);
      });
    }, [runLatestScreenChange]);

    /**
     * Snapshot where the outgoing session was looking, so switching back does
     * not dump the user at the bottom of the stream they had scrolled away
     * from.
     */
    const rememberSessionScroll = useCallback((targetSessionId: string) => {
      const terminal = terminalRef.current;
      if (!terminal) {
        return;
      }

      // The buffer may still show a session the user left two tabs ago (its
      // replay was superseded on the write queue before it ever painted), and
      // that screen's offset is not this session's to store.
      if (!shouldRememberSessionScroll(targetSessionId, displayedSessionIdRef.current)) {
        return;
      }

      const buffer = terminal.buffer.active;
      const linesFromBottom = Math.max(0, buffer.baseY - buffer.viewportY);
      setBoundedSessionMapEntry(
        scrollLinesFromBottomBySessionRef.current,
        targetSessionId,
        linesFromBottom,
        MAX_BUFFERED_SESSION_COUNT,
        [targetSessionId]
      );
    }, []);

    /**
     * Runs once the replayed backlog has been parsed, so the buffer it scrolls
     * in is the finished one. `requestId` is the replay this restore belongs
     * to: a newer switch bumps `replayRequestIdRef`, and scrolling then would
     * move a screen that now belongs to a different session.
     */
    const restoreSessionScroll = useCallback((targetSessionId: string, requestId: number) => {
      const terminal = terminalRef.current;
      if (!terminal || replayRequestIdRef.current !== requestId) {
        return;
      }

      const stored = scrollLinesFromBottomBySessionRef.current.get(targetSessionId);
      if (!stored) {
        return;
      }

      const lines = clampScrollLinesFromBottom(stored, terminal.buffer.active.baseY);
      if (lines <= 0) {
        return;
      }

      terminal.scrollLines(-lines);
    }, []);

    const replaySessionOutput = useCallback(
      (targetSessionId: string) => {
        if (!terminalRef.current) {
          return;
        }

        runLatestScreenChange(() => {
          // Read inside the guarded body: `runLatestScreenChange` only calls it
          // while this replay is still the newest one, so the current value is
          // this replay's own id.
          const requestId = replayRequestIdRef.current;
          terminalRef.current?.reset();
          // From here the screen belongs to the incoming session, even if it has
          // nothing buffered to paint.
          displayedSessionIdRef.current = targetSessionId;

          // Fired before the early returns: reset() already invalidated every
          // marker and decoration the OSC runtime was holding, so the
          // replay-start hooks must run even when the incoming session has
          // nothing buffered.
          oscRuntimeRef.current?.notifyReplayStart(targetSessionId);

          const buffer = bufferBySessionRef.current.get(targetSessionId);
          if (!buffer) {
            // Nothing to paint: the blank screen *is* this session's content,
            // so a frozen frame of the previous one may not outlive this point.
            switchFreezeFrameRef.current?.release();
            return;
          }

          setBoundedSessionMapEntry(
            bufferBySessionRef.current,
            targetSessionId,
            buffer,
            MAX_BUFFERED_SESSION_COUNT,
            [targetSessionId]
          );

          // Only the newest slice of the ring buffer is re-parsed; see
          // REPLAY_MAX_BYTES for why the rest is dropped on the floor here.
          const payload = buildReplayPayload(buffer.chunks, REPLAY_MAX_BYTES);
          if (!payload.text) {
            switchFreezeFrameRef.current?.release();
            return;
          }

          const startedAt = REPLAY_DEBUG ? performance.now() : 0;
          const startMark = REPLAY_DEBUG ? beginReplayMeasurement(requestId) : "";

          // Buffered output may contain OSC sequences with side effects; tagging
          // the write as a replay for this session keeps them silent and, when
          // rapid tab switches leave two replays queued at once, keeps each
          // one's sequences credited to its own session.
          writeSessionText(targetSessionId, payload.text, {
            replay: true,
            onParsed: () => {
              restoreSessionScroll(targetSessionId, requestId);
              // The incoming frame is on screen now — but only this replay's
              // own frozen frame may be dropped: a newer switch has already
              // captured a fresh one that its own replay will release.
              if (replayRequestIdRef.current === requestId) {
                switchFreezeFrameRef.current?.release();
              }
              if (REPLAY_DEBUG) {
                endReplayMeasurement(targetSessionId, payload, startMark, startedAt);
              }
            }
          });
        });
      },
      [restoreSessionScroll, runLatestScreenChange, writeSessionText]
    );

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
        void navigator.clipboard
          .writeText(selection)
          .then(() => message.success("已复制"))
          .catch(() => message.error("复制失败"));
      }
      setCtxMenu(null);
    }, [message]);

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
      terminalRef.current?.focus();
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
          terminal.focus();
          return;
        }
        runSessionAction(window.nextshell.session.write({ sessionId, data: selection }));
      }
      setCtxMenu(null);
      terminal.focus();
    }, [handleLocalAuthInput]);

    const handleCtxClear = useCallback(() => {
      const terminal = terminalRef.current;
      const sessionId = sessionIdRef.current;
      if (!terminal) {
        return;
      }
      // Ordered behind the queue, so output still waiting to be parsed is
      // cleared too instead of reappearing right after the clear.
      clearTerminalScreen();
      if (sessionId) {
        setBoundedSessionMapEntry(
          bufferBySessionRef.current,
          sessionId,
          createEmptyBuffer(),
          MAX_BUFFERED_SESSION_COUNT,
          [sessionId]
        );
      }
      setCtxMenu(null);
    }, [clearTerminalScreen]);

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
        },
        focus: () => {
          terminalRef.current?.focus();
        }
      }),
      [findNext, findPrevious]
    );

    useEffect(() => {
      const knownSessionIds = new Set(sessionIds);
      knownSessionIdsRef.current = knownSessionIds;

      // Sessions that just left the workspace get their final cumulative ack
      // now (flushing also clears the entry and its timer), mirroring the
      // immediate acks the unknown-session data path performs.
      for (const targetSessionId of Array.from(ackAccumulatorBySessionRef.current.keys())) {
        if (!knownSessionIds.has(targetSessionId)) {
          flushSessionAck(targetSessionId);
        }
      }

      retainSessionsInCollections(knownSessionIds, [
        bufferBySessionRef.current,
        lastStatusKeyBySessionRef.current,
        authStateBySessionRef.current,
        sessionStatusBySessionRef.current,
        terminalQueryReplyStateBySessionRef.current,
        terminalQuerySuppressionCountBySessionRef.current,
        reconnectPendingSessionIdsRef.current,
        scrollLinesFromBottomBySessionRef.current
      ]);

      // Deliberately after the retain pass and deliberately not part of it: the
      // pending map is keyed by sessions that are unknown *by definition*, so
      // pruning it against the known set would throw away exactly the bytes it
      // exists to protect. Entries leave it only by being adopted here or by
      // the size bound.
      adoptPendingSessionData(knownSessionIds);
    }, [adoptPendingSessionData, flushSessionAck, sessionIds]);

    useEffect(() => {
      if (!containerRef.current || terminalRef.current) {
        return;
      }

      // A fully transparent canvas — the dimming that keeps text readable comes
      // from `.terminal-shell`'s tinted background, so the cell grid and the
      // padding around it share one surface with no seam.
      const terminalBg = transparencyEnabled
        ? TRANSPARENT_TERMINAL_BACKGROUND
        : terminalPreferences.backgroundColor;
      const terminal = new Terminal({
        cursorBlink: true,
        // registerDecoration (OSC 133 exit-code marks) is proposed API and
        // throws without this flag — from inside the parse loop, which kills
        // xterm's write queue for good and froze every session sharing this
        // terminal the moment shell integration reported a finished command.
        allowProposedApi: true,
        allowTransparency: transparencyEnabled,
        fontSize: terminalPreferences.fontSize,
        lineHeight: terminalPreferences.lineHeight,
        fontFamily: terminalPreferences.fontFamily,
        theme: {
          background: terminalBg,
          foreground: terminalPreferences.foregroundColor,
          cursor: terminalPreferences.foregroundColor
        }
      });
      // Before any handler registration (compat guards, OSC runtime, addons):
      // every parser handler registered from here on is exception-contained.
      installParserHandlerGuards(terminal);

      const fitAddon = new FitAddon();
      const searchAddon = new SearchAddon();
      const webLinksAddon = new WebLinksAddon((_event, uri) => {
        void openExternalLink(uri, {
          confirm: usePreferencesStore.getState().preferences.terminal.hyperlinkConfirm
        });
      });

      terminal.loadAddon(fitAddon);
      terminal.loadAddon(searchAddon);
      terminal.loadAddon(webLinksAddon);

      // Skipping the addon leaves xterm on its built-in DOM renderer (6.0
      // dropped the canvas renderer, so DOM is the only other option).
      if (webglEnabled) {
        try {
          const webglAddon = new WebglAddon();
          terminal.loadAddon(webglAddon);
          webglAddon.onContextLoss(() => {
            webglAddon.dispose();
          });
        } catch {
          // webgl acceleration is optional
        }
      }

      const compatibilityGuard = installTerminalQueryCompatibilityGuards(terminal, {
        isEnabled: () => terminalCompatEnabledRef.current,
        onSuppressed: () => {
          const sessionId = sessionIdRef.current;
          if (!sessionId) {
            return;
          }
          const currentCount =
            terminalQuerySuppressionCountBySessionRef.current.get(sessionId) ?? 0;
          terminalQuerySuppressionCountBySessionRef.current.set(sessionId, currentCount + 1);
        }
      });

      oscRuntimeRef.current = installOscRuntime(terminal, {
        // Fallback only: the runtime prefers the session tagged onto the write
        // currently in the parser, and consults this for anything happening
        // outside a parse (prompt jumps, link clicks).
        getSessionId: () => sessionIdRef.current,
        writeToRemote: (sessionId, data) => {
          // Tagged as protocol traffic (OSC query replies, clipboard answers)
          // so the main process does not mistake it for user keystrokes.
          void window.nextshell.session
            .write({ sessionId, data, origin: "protocol" })
            .catch(() => undefined);
        }
      });

      terminal.open(containerRef.current);
      fitAddon.fit();

      switchFreezeFrameRef.current = createSwitchFreezeFrame(containerRef.current, {
        onCaptureFailure: (reason) => {
          if (REPLAY_DEBUG) {
            console.debug(`[nextshell:switch-freeze] capture unavailable — ${reason}`);
          }
        }
      });

      terminal.attachCustomKeyEventHandler((event: KeyboardEvent) => {
        if (oscRuntimeRef.current?.handleKeyEvent(event)) {
          return false;
        }

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
            void navigator.clipboard
              .writeText(selection)
              .then(() => message.success("已复制"))
              .catch(() => message.error("复制失败"));
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

          runSessionAction(
            window.nextshell.session.write({
              sessionId,
              data: sequenceByBackspaceMode(terminalOptionsRef.current.backspaceMode)
            })
          );
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

          runSessionAction(
            window.nextshell.session.write({
              sessionId,
              data: sequenceByDeleteMode(terminalOptionsRef.current.deleteMode)
            })
          );
          return false;
        }

        return true;
      });

      const dataSub = terminal.onData((data) => {
        const sessionId = sessionIdRef.current;
        if (!sessionId) {
          return;
        }

        let nextData = data;
        if (terminalCompatEnabledRef.current) {
          const currentState =
            terminalQueryReplyStateBySessionRef.current.get(sessionId) ??
            createTerminalQueryReplyFilterState();
          const filtered = consumeTerminalQueryReplyChunk(currentState, data);
          terminalQueryReplyStateBySessionRef.current.set(sessionId, filtered.state);
          if (!filtered.text) {
            return;
          }
          nextData = filtered.text;
        }

        if (authStateBySessionRef.current.has(sessionId)) {
          handleLocalAuthInput(sessionId, nextData);
          return;
        }

        if (tryReconnectOnEnter(sessionId, nextData)) {
          return;
        }

        runSessionAction(
          window.nextshell.session.write({
            sessionId,
            data: nextData
          })
        );
      });

      const resizeSub = terminal.onResize(({ cols, rows }) => {
        const sessionId = sessionIdRef.current;
        if (!sessionId) {
          return;
        }

        runSessionAction(
          window.nextshell.session.resize({
            sessionId,
            cols,
            rows
          })
        );
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

          runSessionAction(
            window.nextshell.session.resize({
              sessionId,
              cols: terminal.cols,
              rows: terminal.rows
            })
          );
        });
      });

      observer.observe(containerRef.current);

      terminalRef.current = terminal;
      fitRef.current = fitAddon;
      searchAddonRef.current = searchAddon;

      // `allowTransparency` and the renderer choice are construction-time only,
      // so toggling either re-runs this effect and rebuilds the terminal. The
      // output buffers live in refs and survive that, so the visible session is
      // restored here instead of flashing empty until the next write.
      const attachedSessionId = sessionIdRef.current;
      if (attachedSessionId) {
        replaySessionOutput(attachedSessionId);
        fitAddon.fit();
        runSessionAction(
          window.nextshell.session.resize({
            sessionId: attachedSessionId,
            cols: terminal.cols,
            rows: terminal.rows
          })
        );
      }

      return () => {
        cancelAnimationFrame(resizeRafId);
        observer.disconnect();
        dataSub.dispose();
        resizeSub.dispose();
        oscRuntimeRef.current?.dispose();
        oscRuntimeRef.current = null;
        switchFreezeFrameRef.current?.dispose();
        switchFreezeFrameRef.current = null;
        compatibilityGuard.dispose();
        terminal.dispose();
        terminalRef.current = null;
        fitRef.current = null;
        searchAddonRef.current = null;
      };
      // terminalPreferences values other than the two below are applied by the
      // hot-update effect right after; only construction-time options belong in
      // this dependency list, or every font tweak would rebuild the terminal.
      // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [
      handleLocalAuthInput,
      message,
      replaySessionOutput,
      transparencyEnabled,
      tryReconnectOnEnter,
      webglEnabled
    ]);

    useEffect(() => {
      const terminal = terminalRef.current;
      if (!terminal) {
        return;
      }

      const terminalBg = transparencyEnabled
        ? TRANSPARENT_TERMINAL_BACKGROUND
        : terminalPreferences.backgroundColor;
      terminal.options.theme = {
        ...terminal.options.theme,
        background: terminalBg,
        foreground: terminalPreferences.foregroundColor,
        cursor: terminalPreferences.foregroundColor
      };
      terminal.options.fontSize = terminalPreferences.fontSize;
      terminal.options.lineHeight = terminalPreferences.lineHeight;
      terminal.options.fontFamily = terminalPreferences.fontFamily;

      fitRef.current?.fit();
      const sessionId = sessionIdRef.current;
      if (!sessionId) {
        return;
      }

      runSessionAction(
        window.nextshell.session.resize({
          sessionId,
          cols: terminal.cols,
          rows: terminal.rows
        })
      );
    }, [
      terminalPreferences.backgroundColor,
      terminalPreferences.foregroundColor,
      terminalPreferences.fontSize,
      terminalPreferences.lineHeight,
      terminalPreferences.fontFamily,
      transparencyEnabled
    ]);

    useEffect(() => {
      const offData = window.nextshell.session.onData((event) => {
        if (!knownSessionIdsRef.current.has(event.sessionId)) {
          // "Unknown" covers two cases and they must not be conflated: a stream
          // that is dying, and one whose session simply has not been committed
          // into `sessionIds` yet. Dropping the bytes here is what made a
          // freshly opened session occasionally start blank, so park them for
          // the session to claim. The ack still goes out immediately so the
          // dispatcher can drain a dying stream without waiting on the
          // batching timer.
          bufferPendingSessionData(event.sessionId, event.data);
          accumulateSessionAck(event.sessionId, event.deliveryId, event.byteLength);
          flushSessionAck(event.sessionId);
          return;
        }

        // OSC sequences (cwd reports, ...) stay in the raw stream: the xterm
        // parser consumes them via oscRuntime handlers, both live and on
        // replay from the per-session buffer.
        appendSessionOutput(event.sessionId, event.data);
        if (event.sessionId === sessionIdRef.current) {
          writeSessionText(event.sessionId, event.data, {
            onParsed: () => {
              accumulateSessionAck(event.sessionId, event.deliveryId, event.byteLength);
            }
          });
          return;
        }

        accumulateSessionAck(event.sessionId, event.deliveryId, event.byteLength);
      });

      const offStatus = window.nextshell.session.onStatus((event) => {
        if (event.status === "disconnected" || event.status === "failed") {
          // Push the final delta out right away so the main-process dispatcher
          // can drain and finalize the stream without waiting on the batching
          // timer; this also resets the accumulator before any reconnect.
          flushSessionAck(event.sessionId);
        }

        if (!knownSessionIdsRef.current.has(event.sessionId)) {
          return;
        }

        sessionStatusBySessionRef.current.set(event.sessionId, event.status);
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
          const text = event.reason ? `连接已建立，${event.reason}` : "连接已建立。";
          message.success(text);
        }

        const targetSession = useWorkspaceStore
          .getState()
          .sessions.find((session: SessionDescriptor) => session.id === event.sessionId);

        if (
          isRemoteSession(targetSession) &&
          event.status === "failed" &&
          isAuthFailureReason(event.reason)
        ) {
          beginLocalAuthPrompt(event.sessionId, event.reason);
          return;
        }

        const output = formatStatusOutput(targetSession, event.status, event.reason);
        if (!output) {
          return;
        }

        appendSessionOutput(event.sessionId, output);
        if (event.sessionId === sessionIdRef.current) {
          writeSessionText(event.sessionId, output);
        }
      });

      return () => {
        offData();
        offStatus();
        // Final acks for everything already processed: without this, teardown
        // (or an effect re-run) could strand a delta until the main-process
        // stall timeout fires.
        flushAllSessionAcks();
      };
    }, [
      accumulateSessionAck,
      appendSessionOutput,
      beginLocalAuthPrompt,
      bufferPendingSessionData,
      flushAllSessionAcks,
      flushSessionAck,
      message,
      writeSessionText
    ]);

    useEffect(() => {
      const previousSessionId = sessionIdRef.current;
      const currentSessionId = session?.id;
      terminalCompatEnabledRef.current = shouldTrackTerminalSessionMetadata(session, connection);
      sessionIdRef.current = currentSessionId;
      if (currentSessionId && session?.status) {
        sessionStatusBySessionRef.current.set(currentSessionId, session.status);
      }

      if (previousSessionId !== currentSessionId) {
        if (previousSessionId) {
          // Don't leave the outgoing session's delta to the batching timer:
          // its stream keeps flowing in the background and the dispatcher's
          // window should reopen promptly.
          flushSessionAck(previousSessionId);
          rememberSessionScroll(previousSessionId);
        }

        if (!currentSessionId) {
          // Same ordering as a replay: a reset that jumps the write queue would
          // let the outgoing session keep painting into the empty pane, and it
          // must also supersede any replay still waiting on the queue.
          clearTerminalScreen();
        } else {
          if (previousSessionId) {
            // Synchronously, before anything can enqueue the reset: this reads
            // the pixels that are on screen right now and pins them over the
            // viewport until the incoming replay has parsed.
            switchFreezeFrameRef.current?.capture();
          }

          if (session?.status === "connecting") {
            const connectingEventKey = `${currentSessionId}:connecting:`;
            if (lastStatusKeyBySessionRef.current.get(currentSessionId) !== connectingEventKey) {
              lastStatusKeyBySessionRef.current.set(currentSessionId, connectingEventKey);
              const output = formatStatusOutput(session, "connecting");
              if (output) {
                appendSessionOutput(currentSessionId, output);
              }
            }
          }

          // Every activation is now a deliberate landing (Ctrl+Tab cycling only
          // moves the switcher's selection and activates once, on release), so
          // there is no cycling to debounce: replay straight away.
          replaySessionOutput(currentSessionId);
        }
      }

      if (
        currentSessionId &&
        isRemoteSession(session) &&
        session?.status === "failed" &&
        isAuthFailureReason(session.reason)
      ) {
        beginLocalAuthPrompt(currentSessionId, session.reason);
      }

      if (
        currentSessionId &&
        (session?.status === "connected" || session?.status === "disconnected")
      ) {
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
        runSessionAction(
          window.nextshell.session.resize({
            sessionId: session.id,
            cols: terminalRef.current.cols,
            rows: terminalRef.current.rows
          })
        );
      }
    }, [
      appendSessionOutput,
      beginLocalAuthPrompt,
      clearTerminalScreen,
      connection,
      flushSessionAck,
      rememberSessionScroll,
      replaySessionOutput,
      session
    ]);

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
        if (isRemoteSession(session) && isAuthFailureReason(session?.reason)) {
          beginLocalAuthPrompt(currentSessionId, session.reason);
          return;
        }

        // Skip if the IPC onStatus event already wrote this failure to the terminal
        const lastKey = lastStatusKeyBySessionRef.current.get(currentSessionId);
        if (lastKey?.includes(":failed:")) {
          return;
        }
        const output = formatStatusOutput(session, "failed", session?.reason);
        if (output) {
          appendSessionOutput(currentSessionId, output);
          if (currentSessionId === sessionIdRef.current) {
            writeSessionText(currentSessionId, output);
          }
        }
      }
    }, [
      appendSessionOutput,
      beginLocalAuthPrompt,
      session?.id,
      session?.reason,
      session?.status,
      writeSessionText
    ]);

    const hasSelection = ctxMenu ? !!terminalRef.current?.getSelection() : false;
    const hasSession = !!sessionIdRef.current;

    return (
      <div className="flex-1 min-h-0 flex flex-col pb-1">
        <div
          className="flex-1 min-h-0 box-border overflow-hidden py-1.5 px-1"
          ref={containerRef}
          onContextMenu={handleContextMenu}
        />
        {ctxMenu && (
          <div ref={ctxMenuRef} className="fe-ctx-menu" style={{ left: ctxMenu.x, top: ctxMenu.y }}>
            <button
              type="button"
              className="fe-ctx-item"
              disabled={!hasSelection}
              onClick={handleCtxCopy}
            >
              <span className="fe-ctx-icon">
                <i className="ri-file-copy-line" />
              </span>
              复制选中内容
            </button>
            <button
              type="button"
              className="fe-ctx-item"
              disabled={!hasSession}
              onClick={handleCtxPaste}
            >
              <span className="fe-ctx-icon">
                <i className="ri-clipboard-line" />
              </span>
              粘贴
            </button>
            <button
              type="button"
              className="fe-ctx-item"
              disabled={!hasSelection || !hasSession}
              onClick={handleCtxPasteSelection}
            >
              <span className="fe-ctx-icon">
                <i className="ri-file-copy-2-line" />
              </span>
              粘贴选中
            </button>
            <div className="fe-ctx-divider" />
            <button type="button" className="fe-ctx-item fe-ctx-danger" onClick={handleCtxClear}>
              <span className="fe-ctx-icon">
                <i className="ri-delete-bin-line" />
              </span>
              清空界面
            </button>
          </div>
        )}
      </div>
    );
  }
);

TerminalPane.displayName = "TerminalPane";
