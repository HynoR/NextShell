import { useEffect } from "react";
import { Empty } from "antd";
import type { AiConversation } from "@nextshell/core";

interface AiConversationHistoryProps {
  conversations: AiConversation[];
  activeConversationId?: string;
  onSelect: (conversationId: string) => void;
  onBack: () => void;
  onLoad: () => void;
}

const formatTime = (iso: string): string => {
  const d = new Date(iso);
  const now = new Date();
  const diffMs = now.getTime() - d.getTime();
  const diffMin = Math.floor(diffMs / 60_000);

  if (diffMin < 1) return "刚刚";
  if (diffMin < 60) return `${diffMin} 分钟前`;

  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;

  const diffDay = Math.floor(diffHour / 24);
  if (diffDay < 7) return `${diffDay} 天前`;

  return d.toLocaleDateString("zh-CN", { month: "short", day: "numeric" });
};

const getPreviewText = (conv: AiConversation): string => {
  const lastMsg = [...conv.messages].reverse().find((m) => m.role !== "system");
  if (!lastMsg) return "暂无消息";
  const text = lastMsg.content.replace(/```[\s\S]*?```/g, "[代码]").replace(/\n+/g, " ");
  return text.length > 60 ? `${text.slice(0, 60)}...` : text;
};

const getMessageStats = (conv: AiConversation): string => {
  const userCount = conv.messages.filter((m) => m.role === "user").length;
  const hasPlan = conv.messages.some((m) => m.plan);
  const parts: string[] = [`${userCount} 条对话`];
  if (hasPlan) parts.push("含执行计划");
  return parts.join(" · ");
};

export const AiConversationHistory = ({
  conversations,
  activeConversationId,
  onSelect,
  onBack,
  onLoad,
}: AiConversationHistoryProps) => {
  useEffect(() => {
    onLoad();
  }, [onLoad]);

  const sorted = [...conversations].sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));

  return (
    <div className="ai-history-panel">
      <div className="ai-history-header">
        <button type="button" className="ai-header-btn" onClick={onBack} title="返回对话">
          <i className="ri-arrow-left-line" />
        </button>
        <span>历史对话</span>
        <span className="ai-history-count">{sorted.length}</span>
      </div>
      <div className="ai-history-list">
        {sorted.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="暂无历史对话"
            style={{ marginTop: 40 }}
          />
        ) : (
          sorted.map((conv) => (
            <button
              key={conv.id}
              type="button"
              className={`ai-history-item ${conv.id === activeConversationId ? "active" : ""}`}
              onClick={() => onSelect(conv.id)}
            >
              <div className="ai-history-item-header">
                <span className="ai-history-item-title">{conv.title || "未命名对话"}</span>
                <span className="ai-history-item-time">{formatTime(conv.updatedAt)}</span>
              </div>
              <div className="ai-history-item-preview">{getPreviewText(conv)}</div>
              <div className="ai-history-item-meta">{getMessageStats(conv)}</div>
            </button>
          ))
        )}
      </div>
    </div>
  );
};
