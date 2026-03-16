import { useCallback, useEffect } from "react";
import { App as AntdApp } from "antd";
import { useAiChatStore } from "../../store/useAiChatStore";
import { usePreferencesStore } from "../../store/usePreferencesStore";
import { AiMessageList } from "./AiMessageList";
import { AiChatInput } from "./AiChatInput";
import { AiExecutionPlanCard } from "./AiExecutionPlan";
import { AiExecutionProgressCard } from "./AiExecutionProgress";

interface AiChatPaneProps {
  sessionId?: string;
  connectionId?: string;
}

export const AiChatPane = ({ sessionId, connectionId }: AiChatPaneProps) => {
  const { message } = AntdApp.useApp();
  const aiEnabled = usePreferencesStore((s) => s.preferences.ai.enabled);
  const hasProvider = usePreferencesStore((s) => s.preferences.ai.providers.length > 0);

  const {
    conversations,
    activeConversationId,
    isStreaming,
    streamingContent,
    executionProgress,
    pendingPlan,
    pendingPlanUserRequest,
    sendMessage,
    approvePlan,
    abortExecution,
    newConversation,
    initListeners,
  } = useAiChatStore();

  useEffect(() => {
    const cleanup = initListeners();
    return cleanup;
  }, [initListeners]);

  const activeConversation = conversations.find((c) => c.id === activeConversationId);
  const messages = activeConversation?.messages ?? [];

  const handleSend = useCallback(
    async (content: string) => {
      try {
        await sendMessage(content, sessionId, connectionId);
      } catch (err) {
        message.error(`发送失败：${err instanceof Error ? err.message : "未知错误"}`);
      }
    },
    [sendMessage, sessionId, connectionId, message]
  );

  const handleApprove = useCallback(async () => {
    try {
      await approvePlan();
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

  return (
    <div className="ai-chat-pane">
      <div className="ai-chat-header">
        <div className="ai-chat-header-left">
          <i className="ri-robot-2-line" />
          <span>AI 助手</span>
        </div>
        <div className="ai-chat-header-actions">
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
              onApprove={() => void handleApprove()}
              onReject={handleReject}
            />
          )}

          {executionProgress && (
            <AiExecutionProgressCard progress={executionProgress} />
          )}

          <AiChatInput
            disabled={!isConfigured}
            isStreaming={isStreaming}
            onSend={(msg) => void handleSend(msg)}
            onAbort={() => void handleAbort()}
          />
        </>
      )}
    </div>
  );
};
