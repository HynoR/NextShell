export interface ChatMessage {
  role: "system" | "user" | "assistant";
  content: string;
}

export interface LlmOptions {
  temperature?: number;
  maxTokens?: number;
  signal?: AbortSignal;
}

export interface LlmAdapter {
  chat(messages: ChatMessage[], options?: LlmOptions): Promise<string>;
  streamChat(
    messages: ChatMessage[],
    onToken: (token: string) => void,
    options?: LlmOptions
  ): Promise<string>;
  testConnection(): Promise<{ ok: boolean; error?: string }>;
}
