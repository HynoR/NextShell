import React, { useEffect, useRef, useState } from "react";
import { Tag } from "antd";
import type { AiChatMessage } from "@nextshell/core";

interface AiMessageListProps {
  messages: AiChatMessage[];
  streamingContent: string;
  isStreaming: boolean;
}

interface ParsedPlan {
  plan: Array<{
    step: number;
    command: string;
    description: string;
    risky?: boolean;
  }>;
  summary?: string;
}

const tryParsePlan = (json: string): ParsedPlan | undefined => {
  try {
    const parsed = JSON.parse(json) as ParsedPlan;
    if (Array.isArray(parsed.plan) && parsed.plan.length > 0) return parsed;
  } catch { /* not a valid plan */ }
  return undefined;
};

const InlinePlanBlock = ({ code, blockKey }: { code: string; blockKey: string }) => {
  const [showRaw, setShowRaw] = useState(false);
  const plan = tryParsePlan(code);

  if (!plan) {
    return (
      <pre className="ai-msg-code">
        <span className="ai-msg-code-lang">json</span>
        <code>{code}</code>
      </pre>
    );
  }

  return (
    <div className="ai-inline-plan">
      <div className="ai-inline-plan-header">
        <div className="ai-inline-plan-title">
          <i className="ri-file-list-3-line" />
          <span>执行计划</span>
          {plan.plan.some((s) => s.risky) && (
            <Tag color="red" style={{ marginLeft: 4, fontSize: 10 }}>含危险操作</Tag>
          )}
        </div>
        <button
          type="button"
          className="ai-inline-plan-toggle"
          onClick={() => setShowRaw(!showRaw)}
          title={showRaw ? "查看结构化视图" : "查看原始 JSON"}
        >
          <i className={showRaw ? "ri-layout-grid-line" : "ri-code-s-slash-line"} />
        </button>
      </div>

      {showRaw ? (
        <pre className="ai-inline-plan-raw">
          <code>{code}</code>
        </pre>
      ) : (
        <>
          {plan.summary && (
            <div className="ai-inline-plan-summary">{plan.summary}</div>
          )}
          <div className="ai-inline-plan-steps">
            {plan.plan.map((step, i) => (
              <div key={`${blockKey}-step-${i}`} className={`ai-inline-plan-step ${step.risky ? "risky" : ""}`}>
                <div className="ai-inline-plan-step-head">
                  <Tag color={step.risky ? "red" : "blue"} style={{ fontSize: 10 }}>
                    {step.risky ? "危险" : `#${step.step ?? i + 1}`}
                  </Tag>
                  <span className="ai-inline-plan-step-desc">{step.description}</span>
                </div>
                <code className="ai-inline-plan-step-cmd">{step.command}</code>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
};

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

      if (lang === "json") {
        elements.push(
          <InlinePlanBlock key={`plan-${i}`} code={code} blockKey={`plan-${i}`} />
        );
      } else {
        elements.push(
          <pre key={`code-${i}`} className="ai-msg-code">
            {lang && <span className="ai-msg-code-lang">{lang}</span>}
            <code>{code}</code>
          </pre>
        );
      }
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
  const getMessageIcon = (msg: AiChatMessage): string => {
    if (msg.kind === "execution_result") return "ri-terminal-box-line";
    return msg.role === "user" ? "ri-user-3-line" : "ri-robot-2-line";
  };

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages.length, streamingContent]);

  return (
    <div className="ai-message-list">
      {messages.map((msg) => (
        <div key={msg.id} className={`ai-message ai-message-${msg.role}`}>
          <div className="ai-message-avatar">
            <i className={getMessageIcon(msg)} />
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
