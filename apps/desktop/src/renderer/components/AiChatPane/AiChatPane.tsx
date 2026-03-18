import { useCallback, useEffect, useState, useRef } from "react";
import { App as AntdApp } from "antd";
import { isAiSystemNoteMessage } from "@nextshell/core";
import type { AiExecutionPlan } from "@nextshell/core";
import { getAiClientId, useAiChatStore } from "../../store/useAiChatStore";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { AiMessageList } from "./AiMessageList";
import { AiChatInput } from "./AiChatInput";
import { AiExecutionPlanCard } from "./AiExecutionPlan";
import { AiExecutionProgressCard } from "./AiExecutionProgress";
import { AiConversationHistory } from "./AiConversationHistory";

interface AiChatPaneProps {
  sessionId?: string;
  connectionId?: string;
  connectionLabel?: string;
}

export const AiChatPane = ({ sessionId, connectionId, connectionLabel }: AiChatPaneProps) => {
  const { message } = AntdApp.useApp();
  const aiEnabled = usePreferencesStore((s) => s.preferences.ai.enabled);
  const hasProvider = usePreferencesStore((s) => s.preferences.ai.providers.length > 0);

  // 计划窗口高度状态（默认 40vh，最小 200px，最大 80vh）
  const [planHeight, setPlanHeight] = useState<number>(() => {
    try {
      const saved = localStorage.getItem("nextshell.ai.planHeight");
      return saved ? Math.max(200, Math.min(window.innerHeight * 0.8, parseInt(saved, 10))) : Math.max(200, window.innerHeight * 0.4);
    } catch {
      return Math.max(200, window.innerHeight * 0.4);
    }
  });

  const isResizingRef = useRef(false);
  const startYRef = useRef(0);
  const startHeightRef = useRef(0);
  const currentHeightRef = useRef(planHeight);
  const contentRef = useRef<HTMLDivElement>(null);

  // 同步 ref 和 state
  useEffect(() => {
    currentHeightRef.current = planHeight;
  }, [planHeight]);

  const {
    conversations,
    activeConversationId,
    boundConnectionId,
    boundConnectionLabel,
    isStreaming,
    streamingContent,
    executionProgress,
    executionPhase,
    pendingPlan,
    pendingPlanUserRequest,
    showHistory,
    statusHint,
    setConnection,
    sendMessage,
    approvePlan,
    abortExecution,
    newConversation,
    setShowHistory,
    switchConversation,
    loadHistory,
    initListeners,
  } = useAiChatStore();

  useEffect(() => {
    const cleanup = initListeners();
    return cleanup;
  }, [initListeners]);

  // 当活动终端 tab 切换时，同步绑定连接
  useEffect(() => {
    setConnection(connectionId, sessionId, connectionLabel);
  }, [connectionId, sessionId, connectionLabel, setConnection]);

  const activeConversation = conversations.find((c) => c.id === activeConversationId);
  const messages = activeConversation?.messages ?? [];

  // 只显示当前连接的对话
  const connectionConversations = boundConnectionId
    ? conversations.filter((c) => c.connectionId === boundConnectionId)
    : [];

  const handleSend = useCallback(
    async (content: string) => {
      try {
        await sendMessage(content);
      } catch (err) {
        message.error(`发送失败：${err instanceof Error ? err.message : "未知错误"}`);
      }
    },
    [sendMessage, message]
  );

  const handleApprove = useCallback(async (plan?: AiExecutionPlan) => {
    try {
      await approvePlan(plan);
    } catch (err) {
      message.error(`批准执行失败：${err instanceof Error ? err.message : "未知错误"}`);
    }
  }, [approvePlan, message]);

  const handleAbort = useCallback(async () => {
    try {
      await abortExecution();
    } catch (err) {
      message.error(`停止失败：${err instanceof Error ? err.message : "未知错误"}`);
    }
  }, [abortExecution, message]);

  const handleReject = useCallback(() => {
    useAiChatStore.setState({ pendingPlan: undefined });
  }, []);

  const handleExportConversation = useCallback(async (conversationId: string) => {
    try {
      const result = await window.nextshell.ai.exportConversation({
        conversationId,
        clientId: getAiClientId(),
      });
      if (result.ok) {
        message.success(`已导出对话记录：${result.filePath}`);
      }
    } catch (err) {
      message.error(`导出失败：${err instanceof Error ? err.message : "未知错误"}`);
    }
  }, [message]);

  // 拖动开始
  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isResizingRef.current = true;
    startYRef.current = e.clientY;
    startHeightRef.current = planHeight;
    document.addEventListener("mousemove", handleResizeMove);
    document.addEventListener("mouseup", handleResizeEnd);
    document.body.style.cursor = "row-resize";
    document.body.style.userSelect = "none";
  }, [planHeight]);

  // 拖动中
  const handleResizeMove = useCallback((e: MouseEvent) => {
    if (!isResizingRef.current || !contentRef.current) return;
    const deltaY = startYRef.current - e.clientY; // 向上拖动为正
    const newHeight = Math.max(200, Math.min(window.innerHeight * 0.8, startHeightRef.current + deltaY));
    setPlanHeight(newHeight);
  }, []);

  // 拖动结束
  const handleResizeEnd = useCallback(() => {
    if (!isResizingRef.current) return;
    isResizingRef.current = false;
    document.removeEventListener("mousemove", handleResizeMove);
    document.removeEventListener("mouseup", handleResizeEnd);
    document.body.style.cursor = "";
    document.body.style.userSelect = "";
    
    // 保存当前高度（使用 ref 确保是最新值）
    try {
      localStorage.setItem("nextshell.ai.planHeight", currentHeightRef.current.toString());
    } catch {
      // ignore
    }
  }, [handleResizeMove]);

  useEffect(() => {
    return () => {
      document.removeEventListener("mousemove", handleResizeMove);
      document.removeEventListener("mouseup", handleResizeEnd);
    };
  }, [handleResizeMove, handleResizeEnd]);

  const isConfigured = aiEnabled && hasProvider;
  const hasConnection = !!boundConnectionId;
  const showPlanCard = !!(pendingPlan && !executionProgress);
  const showProgressCard = !!executionProgress;

  return (
    <div className="ai-chat-pane">
      <div className="ai-chat-header">
        <div className="ai-chat-header-left">
          <i className="ri-robot-2-line" />
          <span>AI 助手</span>
          {boundConnectionLabel && (
            <span className="ai-connection-badge" title={`目标机器：${boundConnectionLabel}`}>
              <i className="ri-server-line" />
              {boundConnectionLabel}
            </span>
          )}
        </div>
        <div className="ai-chat-header-actions">
          <button
            type="button"
            className="ai-header-btn"
            onClick={() => setShowHistory(!showHistory)}
            title="历史对话"
          >
            <i className="ri-chat-history-line" />
          </button>
          <button
            type="button"
            className="ai-header-btn"
            onClick={newConversation}
            title="新对话"
          >
            <i className="ri-add-line" />
          </button>
        </div>
      </div>

      {!isConfigured ? (
        <div className="ai-chat-empty">
          <i className="ri-robot-2-line" style={{ fontSize: 32, opacity: 0.3 }} />
          <p>请先在设置中启用 AI 助手并配置提供商</p>
        </div>
      ) : !hasConnection ? (
        <div className="ai-chat-empty">
          <i className="ri-server-line" style={{ fontSize: 32, opacity: 0.3 }} />
          <p>请先连接一个 SSH 终端会话</p>
        </div>
      ) : showHistory ? (
        <AiConversationHistory
          conversations={connectionConversations}
          activeConversationId={activeConversationId}
          connectionLabel={boundConnectionLabel}
          onSelect={switchConversation}
          onExport={handleExportConversation}
          onBack={() => setShowHistory(false)}
          onLoad={loadHistory}
        />
      ) : (
        <>
          <div className="ai-chat-content" ref={contentRef}>
            <div className="ai-messages-container">
              <AiMessageList
                messages={messages.filter((m) => !isAiSystemNoteMessage(m))}
                streamingContent={streamingContent}
                isStreaming={isStreaming}
              />
            </div>

            {(showPlanCard || showProgressCard) && (
              <>
                <div
                  className="ai-plan-resizer"
                  onMouseDown={handleResizeStart}
                  title="拖动调整计划窗口大小"
                >
                  <div className="ai-plan-resizer-handle" />
                </div>
                <div
                  className="ai-plan-container"
                  style={{ height: `${planHeight}px` }}
                >
                  {showPlanCard && (
                    <AiExecutionPlanCard
                      plan={pendingPlan}
                      userRequest={pendingPlanUserRequest}
                      onApprove={(plan) => void handleApprove(plan)}
                      onReject={handleReject}
                      onAbort={() => void handleAbort()}
                    />
                  )}
                  {showProgressCard && (
                    <AiExecutionProgressCard progress={executionProgress} phase={executionPhase} />
                  )}
                </div>
              </>
            )}
          </div>

          {statusHint && (
            <div className="ai-status-bar">
              <i className={`ai-status-icon ${statusHint.animate ? "ai-spin" : ""} ${statusHint.icon}`} />
              <span className="ai-status-text">{statusHint.text}</span>
            </div>
          )}

          <AiChatInput
            disabled={!isConfigured || !hasConnection}
            isStreaming={isStreaming}
            onSend={(msg) => void handleSend(msg)}
            onAbort={() => void handleAbort()}
          />
        </>
      )}
    </div>
  );
};
