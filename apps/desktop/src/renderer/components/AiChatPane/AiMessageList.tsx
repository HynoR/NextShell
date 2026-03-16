import React, { useEffect, useRef } from "react";
import type { AiChatMessage } from "@nextshell/core";

interface AiMessageListProps {
  messages: AiChatMessage[];
  streamingContent: string;
  isStreaming: boolean;
}

const renderMarkdownSimple = (content: string) => {
  const blocks = content.split(/```(\w*)\n([\s\S]*?)```/g);
  const elements: React.ReactNode[] = [];

  for (let i = 0; i < blocks.length; i++) {
    if (i % 3 === 0) {
      const text = blocks[i] ?? "";
      if (text.trim()) {
        const paragraphs = text.split("\n\n");
        for (let p = 0; p < paragraphs.length; p++) {
          const para = paragraphs[p]?.trim();
          if (para) {
            elements.push(
              <p key={`p-${i}-${p}`} className="ai-msg-text">
                {para.split("\n").map((line, li) => (
                  <span key={li}>
                    {line}
                    {li < (para.split("\n").length - 1) && <br />}
                  </span>
                ))}
              </p>
            );
          }
        }
      }
    } else if (i % 3 === 2) {
      const code = blocks[i] ?? "";
      const lang = blocks[i - 1] ?? "";
      elements.push(
        <pre key={`code-${i}`} className="ai-msg-code">
          {lang && <span className="ai-msg-code-lang">{lang}</span>}
          <code>{code}</code>
        </pre>
      );
    }
  }

  return <>{elements}</>;
};

export const AiMessageList = ({
  messages,
  streamingContent,
  isStreaming,
}: AiMessageListProps) => {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages.length, streamingContent]);

  return (
    <div className="ai-message-list">
      {messages.map((msg) => (
        <div key={msg.id} className={`ai-message ai-message-${msg.role}`}>
          <div className="ai-message-avatar">
            <i className={msg.role === "user" ? "ri-user-3-line" : "ri-robot-2-line"} />
          </div>
          <div className="ai-message-body">
            {renderMarkdownSimple(msg.content)}
          </div>
        </div>
      ))}
      {isStreaming && streamingContent && (
        <div className="ai-message ai-message-assistant">
          <div className="ai-message-avatar">
            <i className="ri-robot-2-line" />
          </div>
          <div className="ai-message-body">
            {renderMarkdownSimple(streamingContent)}
            <span className="ai-streaming-cursor" />
          </div>
        </div>
      )}
      {isStreaming && !streamingContent && (
        <div className="ai-message ai-message-assistant">
          <div className="ai-message-avatar">
            <i className="ri-robot-2-line" />
          </div>
          <div className="ai-message-body">
            <span className="ai-typing-indicator">
              <span /><span /><span />
            </span>
          </div>
        </div>
      )}
      <div ref={bottomRef} />
    </div>
  );
};
