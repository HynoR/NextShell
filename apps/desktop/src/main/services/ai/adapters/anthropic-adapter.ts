import type { ChatMessage, LlmAdapter, LlmOptions } from "./types";
import { fetchWithRetry } from "./request-utils";

interface AnthropicConfig {
  baseUrl: string;
  apiKey: string;
  model: string;
}

export class AnthropicAdapter implements LlmAdapter {
  private readonly config: AnthropicConfig;

  constructor(config: AnthropicConfig) {
    this.config = config;
  }

  private buildPayload(messages: ChatMessage[], options?: LlmOptions, stream = false): {
    url: string;
    body: Record<string, unknown>;
    headers: Record<string, string>;
  } {
    const systemMsg = messages.find((m) => m.role === "system");
    const nonSystemMsgs = messages
      .filter((m) => m.role !== "system")
      .map((m) => ({ role: m.role, content: m.content }));

    return {
      url: `${this.config.baseUrl.replace(/\/+$/, "")}/v1/messages`,
      headers: {
        "Content-Type": "application/json",
        "x-api-key": this.config.apiKey,
        "anthropic-version": "2023-06-01",
      },
      body: {
        model: this.config.model,
        max_tokens: options?.maxTokens ?? 4096,
        ...(systemMsg ? { system: systemMsg.content } : {}),
        messages: nonSystemMsgs,
        stream,
      },
    };
  }

  async chat(messages: ChatMessage[], options?: LlmOptions): Promise<string> {
    const { url, headers, body } = this.buildPayload(messages, options, false);
    const response = await fetchWithRetry("Anthropic", url, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
    }, {
      signal: options?.signal,
      timeoutMs: options?.timeoutMs,
      maxRetries: options?.maxRetries ?? 1,
    });

    const data = await response.json() as {
      content: Array<{ type: string; text?: string }>;
    };
    return data.content
      .filter((b) => b.type === "text")
      .map((b) => b.text ?? "")
      .join("");
  }

  async streamChat(
    messages: ChatMessage[],
    onToken: (token: string) => void,
    options?: LlmOptions
  ): Promise<string> {
    const { url, headers, body } = this.buildPayload(messages, options, true);
    const response = await fetchWithRetry("Anthropic", url, {
      method: "POST",
      headers,
      body: JSON.stringify(body),
    }, {
      signal: options?.signal,
      timeoutMs: options?.timeoutMs,
      maxRetries: options?.maxRetries ?? 1,
    });

    const reader = response.body?.getReader();
    if (!reader) throw new Error("No response body");

    const decoder = new TextDecoder();
    let fullContent = "";
    let buffer = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";

      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed.startsWith("data: ")) continue;
        const payload = trimmed.slice(6);

        try {
          const parsed = JSON.parse(payload) as {
            type: string;
            delta?: { type: string; text?: string };
          };
          if (parsed.type === "content_block_delta" && parsed.delta?.text) {
            fullContent += parsed.delta.text;
            onToken(parsed.delta.text);
          }
        } catch {
          // skip
        }
      }
    }

    return fullContent;
  }

  async testConnection(): Promise<{ ok: boolean; error?: string }> {
    try {
      const result = await this.chat(
        [{ role: "user", content: "Reply with OK" }],
        { maxTokens: 10, timeoutMs: 10_000, maxRetries: 1 }
      );
      return { ok: result.length > 0 };
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : String(err) };
    }
  }
}
