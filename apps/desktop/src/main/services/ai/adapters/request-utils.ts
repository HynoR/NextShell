import type { AiProviderType } from "../../../../../../../packages/core/src/index";

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_RETRY_DELAY_MS = 400;

export interface ProviderRequestOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
  maxRetries?: number;
}

export class ProviderCapabilityError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ProviderCapabilityError";
  }
}

export class ProviderHttpError extends Error {
  readonly status: number;
  readonly retryable: boolean;

  constructor(message: string, status: number, retryable = false) {
    super(message);
    this.name = "ProviderHttpError";
    this.status = status;
    this.retryable = retryable;
  }
}

const sleep = async (ms: number, signal?: AbortSignal): Promise<void> => {
  if (ms <= 0) return;
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);

    const onAbort = () => {
      clearTimeout(timer);
      reject(signal?.reason ?? new DOMException("Aborted", "AbortError"));
    };

    signal?.addEventListener("abort", onAbort, { once: true });
  });
};

const createRequestSignal = (
  sourceSignal?: AbortSignal,
  timeoutMs = DEFAULT_TIMEOUT_MS
): { signal: AbortSignal; cleanup: () => void; didTimeout: () => boolean } => {
  const controller = new AbortController();
  let timeoutTriggered = false;

  const onAbort = () => {
    controller.abort(sourceSignal?.reason ?? new DOMException("Aborted", "AbortError"));
  };

  if (sourceSignal?.aborted) {
    onAbort();
  } else {
    sourceSignal?.addEventListener("abort", onAbort, { once: true });
  }

  const timer = setTimeout(() => {
    timeoutTriggered = true;
    controller.abort(new Error(`AI provider request timed out after ${timeoutMs}ms`));
  }, timeoutMs);

  return {
    signal: controller.signal,
    cleanup: () => {
      clearTimeout(timer);
      sourceSignal?.removeEventListener("abort", onAbort);
    },
    didTimeout: () => timeoutTriggered,
  };
};

const shouldRetryStatus = (status: number): boolean => {
  return status === 408 || status === 409 || status === 425 || status === 429 || status >= 500;
};

const readErrorText = async (response: Response): Promise<string> => {
  const text = await response.text().catch(() => "");
  return text.length > 300 ? `${text.slice(0, 300)}...` : text;
};

export const formatProviderTimeoutMessage = (providerLabel: string, timeoutMs: number): string => {
  return `${providerLabel} 请求超时（${Math.ceil(timeoutMs / 1000)} 秒）`;
};

export const validateProviderCapability = (
  type: AiProviderType,
  baseUrl: string,
  model: string
): void => {
  const normalizedBaseUrl = baseUrl.trim();
  const normalizedModel = model.trim();

  if (!/^https?:\/\//.test(normalizedBaseUrl)) {
    throw new ProviderCapabilityError("Provider Base URL 必须以 http:// 或 https:// 开头");
  }

  if (!normalizedModel) {
    throw new ProviderCapabilityError("Provider Model 不能为空");
  }

  const lowerBaseUrl = normalizedBaseUrl.toLowerCase();
  switch (type) {
    case "openai":
      if (lowerBaseUrl.includes("/chat/completions")) {
        throw new ProviderCapabilityError(
          "OpenAI Base URL 不应包含 /chat/completions，请填写 API 根地址，例如 https://api.openai.com/v1"
        );
      }
      break;
    case "anthropic":
      if (lowerBaseUrl.includes("/v1/messages")) {
        throw new ProviderCapabilityError(
          "Anthropic Base URL 不应包含 /v1/messages，请填写 API 根地址，例如 https://api.anthropic.com"
        );
      }
      break;
    case "gemini":
      if (lowerBaseUrl.includes("/models/") || lowerBaseUrl.includes(":generatecontent")) {
        throw new ProviderCapabilityError(
          "Gemini Base URL 不应包含 /models/... 或 :generateContent，请填写 API 根地址，例如 https://generativelanguage.googleapis.com"
        );
      }
      break;
  }
};

export const fetchWithRetry = async (
  providerLabel: string,
  input: string,
  init: RequestInit,
  options?: ProviderRequestOptions
): Promise<Response> => {
  const timeoutMs = options?.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const maxRetries = Math.max(0, options?.maxRetries ?? 0);

  let lastError: unknown;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    const requestSignal = createRequestSignal(options?.signal, timeoutMs);

    try {
      const response = await fetch(input, {
        ...init,
        signal: requestSignal.signal,
      });

      if (!response.ok) {
        const text = await readErrorText(response);
        const message = `${providerLabel} API error ${response.status}${text ? `: ${text}` : ""}`;
        const error = new ProviderHttpError(message, response.status, shouldRetryStatus(response.status));
        if (error.retryable && attempt < maxRetries) {
          await sleep(DEFAULT_RETRY_DELAY_MS * (attempt + 1), options?.signal);
          continue;
        }
        throw error;
      }

      return response;
    } catch (error) {
      lastError = error;

      if (requestSignal.didTimeout()) {
        throw new Error(formatProviderTimeoutMessage(providerLabel, timeoutMs));
      }

      if (options?.signal?.aborted) {
        throw error;
      }

      const retryable =
        error instanceof ProviderHttpError
          ? error.retryable
          : error instanceof TypeError;

      if (!retryable || attempt >= maxRetries) {
        throw error;
      }

      await sleep(DEFAULT_RETRY_DELAY_MS * (attempt + 1), options?.signal);
    } finally {
      requestSignal.cleanup();
    }
  }

  throw lastError instanceof Error ? lastError : new Error(`${providerLabel} 请求失败`);
};
