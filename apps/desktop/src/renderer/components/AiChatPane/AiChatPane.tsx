import { useCallback, useEffect } from "react";
import { App as AntdApp } from "antd";
import type { AiExecutionPlan } from "@nextshell/core";
import { useAiChatStore } from "../../store/useAiChatStore";
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

  const isConfigured = aiEnabled && hasProvider;
  const hasConnection = !!boundConnectionId;

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
          onBack={() => setShowHistory(false)}
          onLoad={loadHistory}
        />
      ) : (
        <>
          <AiMessageList
            messages={messages.filter((m) => m.role !== "system")}
            streamingContent={streamingContent}
            isStreaming={isStreaming}
          />

          {pendingPlan && !executionProgress && (
            <AiExecutionPlanCard
              plan={pendingPlan}
              userRequest={pendingPlanUserRequest}
              onApprove={(plan) => void handleApprove(plan)}
              onReject={handleReject}
              onAbort={() => void handleAbort()}
            />
          )}

          {executionProgress && (
            <AiExecutionProgressCard progress={executionProgress} phase={executionPhase} />
          )}

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
