import type { ChatMessage, LlmAdapter, LlmOptions } from "./types";
import { fetchWithRetry } from "./request-utils";

interface GeminiConfig {
  baseUrl: string;
  apiKey: string;
  model: string;
}

export class GeminiAdapter implements LlmAdapter {
  private readonly config: GeminiConfig;
  private readonly providerKey: string;

  constructor(config: GeminiConfig) {
    this.config = config;
    this.providerKey = `gemini:${config.baseUrl}:${config.model}`;
  }

  private buildContents(messages: ChatMessage[]): {
    systemInstruction?: { parts: Array<{ text: string }> };
    contents: Array<{ role: string; parts: Array<{ text: string }> }>;
  } {
    const systemMsg = messages.find((m) => m.role === "system");
    const conversationMsgs = messages.filter((m) => m.role !== "system");

    return {
      ...(systemMsg ? { systemInstruction: { parts: [{ text: systemMsg.content }] } } : {}),
      contents: conversationMsgs.map((m) => ({
        role: m.role === "assistant" ? "model" : "user",
        parts: [{ text: m.content }],
      })),
    };
  }

  async chat(messages: ChatMessage[], options?: LlmOptions): Promise<string> {
    const baseUrl = this.config.baseUrl.replace(/\/+$/, "");
    const url = `${baseUrl}/v1beta/models/${this.config.model}:generateContent?key=${this.config.apiKey}`;
    const { contents, systemInstruction } = this.buildContents(messages);

    const response = await fetchWithRetry("Gemini", url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        contents,
        ...(systemInstruction ? { systemInstruction } : {}),
        generationConfig: {
          temperature: options?.temperature ?? 0.7,
          maxOutputTokens: options?.maxTokens ?? 4096,
        },
      }),
    }, {
      signal: options?.signal,
      timeoutMs: options?.timeoutMs,
      maxRetries: options?.maxRetries ?? 1,
      providerKey: options?.providerKey ?? this.providerKey,
    });

    const data = await response.json() as {
      candidates: Array<{ content: { parts: Array<{ text: string }> } }>;
    };
    return data.candidates?.[0]?.content?.parts
      ?.map((p) => p.text)
      .join("") ?? "";
  }

  async streamChat(
    messages: ChatMessage[],
    onToken: (token: string) => void,
    options?: LlmOptions
  ): Promise<string> {
    const baseUrl = this.config.baseUrl.replace(/\/+$/, "");
    const url = `${baseUrl}/v1beta/models/${this.config.model}:streamGenerateContent?key=${this.config.apiKey}&alt=sse`;
    const { contents, systemInstruction } = this.buildContents(messages);

    const response = await fetchWithRetry("Gemini", url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        contents,
        ...(systemInstruction ? { systemInstruction } : {}),
        generationConfig: {
          temperature: options?.temperature ?? 0.7,
          maxOutputTokens: options?.maxTokens ?? 4096,
        },
      }),
    }, {
      signal: options?.signal,
      timeoutMs: options?.timeoutMs,
      maxRetries: options?.maxRetries ?? 1,
      providerKey: options?.providerKey ?? this.providerKey,
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
            candidates?: Array<{ content?: { parts?: Array<{ text?: string }> } }>;
          };
          const text = parsed.candidates?.[0]?.content?.parts?.[0]?.text;
          if (text) {
            fullContent += text;
            onToken(text);
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
