import type { AiProviderType } from "../../../../../../../packages/core/src/index";

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_RETRY_DELAY_MS = 400;
const DEFAULT_MAX_CONCURRENT_REQUESTS = 2;
const MAX_RETRY_AFTER_MS = 30_000;

export interface ProviderRequestOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
  maxRetries?: number;
  providerKey?: string;
  maxConcurrentRequests?: number;
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
  readonly retryAfterMs?: number;

  constructor(message: string, status: number, retryable = false, retryAfterMs?: number) {
    super(message);
    this.name = "ProviderHttpError";
    this.status = status;
    this.retryable = retryable;
    this.retryAfterMs = retryAfterMs;
  }
}

interface ProviderRuntimeState {
  activeCount: number;
  nextAllowedAt: number;
  waiters: Array<() => void>;
}

const providerRuntimeStates = new Map<string, ProviderRuntimeState>();

const getProviderRuntimeState = (providerKey: string): ProviderRuntimeState => {
  const existing = providerRuntimeStates.get(providerKey);
  if (existing) return existing;
  const created: ProviderRuntimeState = {
    activeCount: 0,
    nextAllowedAt: 0,
    waiters: [],
  };
  providerRuntimeStates.set(providerKey, created);
  return created;
};

const notifyProviderWaiters = (state: ProviderRuntimeState): void => {
  const waiters = state.waiters.splice(0, state.waiters.length);
  for (const wake of waiters) wake();
};

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

const waitForProviderWindow = async (
  state: ProviderRuntimeState,
  waitMs: number,
  signal?: AbortSignal
): Promise<void> => {
  await new Promise<void>((resolve, reject) => {
    let done = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const finish = (): void => {
      if (done) return;
      done = true;
      if (timer) clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      resolve();
    };

    const onAbort = (): void => {
      if (done) return;
      done = true;
      if (timer) clearTimeout(timer);
      state.waiters = state.waiters.filter((wake) => wake !== finish);
      reject(signal?.reason ?? new DOMException("Aborted", "AbortError"));
    };

    if (waitMs > 0) {
      timer = setTimeout(finish, waitMs);
    }

    state.waiters.push(finish);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
};

const acquireProviderSlot = async (
  providerKey: string,
  signal?: AbortSignal,
  maxConcurrentRequests = DEFAULT_MAX_CONCURRENT_REQUESTS
): Promise<() => void> => {
  const state = getProviderRuntimeState(providerKey);

  while (true) {
    if (signal?.aborted) {
      throw signal.reason ?? new DOMException("Aborted", "AbortError");
    }

    const now = Date.now();
    const waitMs = Math.max(0, state.nextAllowedAt - now);
    if (state.activeCount < maxConcurrentRequests && waitMs === 0) {
      state.activeCount += 1;
      return () => {
        state.activeCount = Math.max(0, state.activeCount - 1);
        notifyProviderWaiters(state);
      };
    }

    await waitForProviderWindow(state, waitMs, signal);
  }
};

const setProviderBackoff = (providerKey: string, delayMs: number): void => {
  if (delayMs <= 0) return;
  const state = getProviderRuntimeState(providerKey);
  state.nextAllowedAt = Math.max(state.nextAllowedAt, Date.now() + delayMs);
  notifyProviderWaiters(state);
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

const parseRetryAfterMs = (value: string | null): number | undefined => {
  if (!value) return undefined;

  const seconds = Number(value);
  if (Number.isFinite(seconds) && seconds >= 0) {
    return Math.min(seconds * 1000, MAX_RETRY_AFTER_MS);
  }

  const dateMs = Date.parse(value);
  if (Number.isFinite(dateMs)) {
    return Math.min(Math.max(0, dateMs - Date.now()), MAX_RETRY_AFTER_MS);
  }

  return undefined;
};

const getRetryDelayMs = (
  attempt: number,
  retryAfterMs?: number
): number => {
  if (retryAfterMs !== undefined) return retryAfterMs;
  return Math.min(DEFAULT_RETRY_DELAY_MS * 2 ** attempt, MAX_RETRY_AFTER_MS);
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
  const providerKey = options?.providerKey ?? providerLabel;
  const maxConcurrentRequests = Math.max(1, options?.maxConcurrentRequests ?? DEFAULT_MAX_CONCURRENT_REQUESTS);

  let lastError: unknown;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    const requestSignal = createRequestSignal(options?.signal, timeoutMs);
    const releaseProviderSlot = await acquireProviderSlot(providerKey, options?.signal, maxConcurrentRequests);

    try {
      const response = await fetch(input, {
        ...init,
        signal: requestSignal.signal,
      });

      if (!response.ok) {
        const text = await readErrorText(response);
        const retryAfterMs = parseRetryAfterMs(response.headers.get("retry-after"));
        const message = `${providerLabel} API error ${response.status}${text ? `: ${text}` : ""}`;
        const error = new ProviderHttpError(
          message,
          response.status,
          shouldRetryStatus(response.status),
          retryAfterMs
        );
        if (error.retryable && attempt < maxRetries) {
          const delayMs = getRetryDelayMs(attempt, retryAfterMs);
          setProviderBackoff(providerKey, delayMs);
          await sleep(delayMs, options?.signal);
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

      const delayMs = getRetryDelayMs(
        attempt,
        error instanceof ProviderHttpError ? error.retryAfterMs : undefined
      );
      setProviderBackoff(providerKey, delayMs);
      await sleep(delayMs, options?.signal);
    } finally {
      releaseProviderSlot();
      requestSignal.cleanup();
    }
  }

  throw lastError instanceof Error ? lastError : new Error(`${providerLabel} 请求失败`);
};
