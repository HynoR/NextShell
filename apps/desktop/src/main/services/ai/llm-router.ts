import type { AiProviderConfig, AiProviderType } from "../../../../../../packages/core/src/index";
import type { LlmAdapter } from "./adapters/types";
import { OpenAiAdapter } from "./adapters/openai-adapter";
import { AnthropicAdapter } from "./adapters/anthropic-adapter";
import { GeminiAdapter } from "./adapters/gemini-adapter";
import { validateProviderCapability } from "./adapters/request-utils";

const createAdapter = (type: AiProviderType, baseUrl: string, model: string, apiKey: string): LlmAdapter => {
  const normalizedBaseUrl = baseUrl.trim();
  const normalizedModel = model.trim();
  validateProviderCapability(type, normalizedBaseUrl, normalizedModel);

  switch (type) {
    case "openai":
      return new OpenAiAdapter({ baseUrl: normalizedBaseUrl, apiKey, model: normalizedModel });
    case "anthropic":
      return new AnthropicAdapter({ baseUrl: normalizedBaseUrl, apiKey, model: normalizedModel });
    case "gemini":
      return new GeminiAdapter({ baseUrl: normalizedBaseUrl, apiKey, model: normalizedModel });
  }
};

export class LlmRouter {
  private cache = new Map<string, LlmAdapter>();

  getAdapter(
    provider: AiProviderConfig,
    apiKey: string
  ): LlmAdapter {
    const cacheKey = `${provider.id}:${provider.type}:${provider.baseUrl}:${provider.model}`;
    const cached = this.cache.get(cacheKey);
    if (cached) return cached;

    const adapter = createAdapter(provider.type, provider.baseUrl, provider.model, apiKey);
    this.cache.set(cacheKey, adapter);
    return adapter;
  }

  /** 用于测试连接时创建临时适配器，不缓存 */
  createTemporary(
    type: AiProviderType,
    baseUrl: string,
    model: string,
    apiKey: string
  ): LlmAdapter {
    return createAdapter(type, baseUrl, model, apiKey);
  }

  clearCache(): void {
    this.cache.clear();
  }
}
