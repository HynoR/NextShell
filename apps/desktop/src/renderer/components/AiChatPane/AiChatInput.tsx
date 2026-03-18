import { useCallback, useRef, useState } from "react";
import { Button } from "antd";

interface AiChatInputProps {
  disabled: boolean;
  isStreaming: boolean;
  onSend: (message: string) => void;
  onAbort: () => void;
}

export const AiChatInput = ({
  disabled,
  isStreaming,
  onSend,
  onAbort,
}: AiChatInputProps) => {
  const [input, setInput] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = useCallback(() => {
    const trimmed = input.trim();
    if (!trimmed) return;
    onSend(trimmed);
    setInput("");
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  }, [input, onSend]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend]
  );

  const handleInput = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    setInput(e.target.value);
    const el = e.target;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 120)}px`;
  }, []);

  return (
    <div className="ai-chat-input-bar">
      <textarea
        ref={textareaRef}
        className="ai-chat-textarea"
        placeholder={disabled ? "请先在设置中配置 AI 提供商" : "描述你的运维需求..."}
        value={input}
        onChange={handleInput}
        onKeyDown={handleKeyDown}
        disabled={disabled || isStreaming}
        rows={1}
      />
      <div className="ai-chat-input-actions">
        {isStreaming ? (
          <Button size="small" danger onClick={onAbort}>
            <i className="ri-stop-circle-line" /> 停止
          </Button>
        ) : (
          <Button
            size="small"
            type="primary"
            disabled={disabled || !input.trim()}
            onClick={handleSend}
          >
            <i className="ri-send-plane-2-line" /> 发送
          </Button>
        )}
      </div>
    </div>
  );
};
