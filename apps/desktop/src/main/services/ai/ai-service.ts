import fs from "node:fs";
import fsp from "node:fs/promises";
import path from "node:path";
import type { WebContents } from "electron";
import type {
  AiConversation,
  AiChatMessage,
  AiExecutionPlan,
  AiProviderConfig,
  AppPreferences,
  CommandExecutionResult,
} from "../../../../../../packages/core/src/index";
import {
  getAiMessageCanonicalRole,
  getAiMessageModelRole,
  isAiAssistantReplyMessage,
  resolveAiMessageType,
} from "../../../../../../packages/core/src/index";
import type {
  AiChatInput,
  AiApproveInput,
  AiAbortInput,
  AiHistoryInput,
  AiProviderTestInput,
  AiStreamEvent,
  AiProgressEvent,
} from "../../../../../../packages/shared/src/index";
import { IPCChannel } from "../../../../../../packages/shared/src/index";
import type { EncryptedSecretVault } from "../../../../../../packages/security/src/index";
import type { ChatMessage } from "./adapters/types";
import { LlmRouter } from "./llm-router";
import { SYSTEM_PROMPT, buildAnalysisPrompt } from "./prompt-templates";
import { extractPlanFromResponse } from "./plan-parser";
import { normalizeApprovedPlan } from "./plan-guard";
import { AiExecutionCoordinator } from "./ai-execution-coordinator";
import { logger } from "../../logger";

/**
 * 估算消息的 token 数（粗略：1 token ≈ 3 中文字符或 4 英文字符）。
 * 不需要精确，只要能防止明显超限即可。
 */
const estimateTokens = (text: string): number => {
  const cjk = text.match(/[\u4e00-\u9fff\u3000-\u303f\uff00-\uffef]/g)?.length ?? 0;
  const rest = text.length - cjk;
  return Math.ceil(cjk / 1.5 + rest / 4);
};

/** 上下文窗口限制（保守值，给模型输出留空间） */
const CONTEXT_TOKEN_BUDGET = 24000;

/**
 * 裁剪对话消息以适应模型上下文窗口：
 * - 始终保留 system prompt
 * - 始终保留最新的用户消息
 * - 从最早的消息开始丢弃，直到总 token 量在预算内
 * - 丢弃时在开头插入一条摘要提示
 */
const trimMessagesForContext = (
  messages: ChatMessage[],
  budget = CONTEXT_TOKEN_BUDGET
): ChatMessage[] => {
  const totalTokens = messages.reduce((sum, m) => sum + estimateTokens(m.content), 0);
  if (totalTokens <= budget) return messages;

  const system = messages[0]?.role === "system" ? messages[0] : undefined;
  const rest = system ? messages.slice(1) : [...messages];

  let currentTokens = system ? estimateTokens(system.content) : 0;
  const lastMsg = rest[rest.length - 1];
  if (lastMsg) {
    currentTokens += estimateTokens(lastMsg.content);
  }

  const kept: ChatMessage[] = [];
  let droppedCount = 0;

  for (let i = rest.length - 2; i >= 0; i--) {
    const msg = rest[i]!;
    const msgTokens = estimateTokens(msg.content);
    if (currentTokens + msgTokens <= budget - 200) {
      kept.unshift(msg);
      currentTokens += msgTokens;
    } else {
      droppedCount = i + 1;
      break;
    }
  }

  const result: ChatMessage[] = [];
  if (system) result.push(system);

  if (droppedCount > 0) {
    result.push({
      role: "system",
      content: `[上下文管理] 为避免超出模型上下文窗口，已省略最早的 ${droppedCount} 条消息。请基于保留的上下文继续分析。`,
    });
  }

  result.push(...kept);
  if (lastMsg) result.push(lastMsg);

  return result;
};

class AiTaskAbortedError extends Error {
  constructor(message = "AI 执行已中止") {
    super(message);
    this.name = "AiTaskAbortedError";
  }
}

const AI_HISTORY_FILE = "ai-conversations.json";
const MAX_HISTORY_ITEMS = 100;

interface AiTaskContext {
  chatController?: AbortController;
  executionController?: AbortController;
  analysisController?: AbortController;
  aborted: boolean;
}

interface AiServiceDeps {
  execCommand: (
    connectionId: string,
    cmd: string,
    options?: { signal?: AbortSignal; timeoutMs?: number; skipAudit?: boolean }
  ) => Promise<CommandExecutionResult>;
  execInSession: (
    sessionId: string,
    cmd: string,
    options?: { signal?: AbortSignal; timeoutMs?: number }
  ) => Promise<CommandExecutionResult>;
  vault: EncryptedSecretVault;
  getPreferences: () => AppPreferences;
  dataDir: string;
  appendAuditLog: (payload: {
    action: string;
    level: "info" | "warn" | "error";
    connectionId?: string;
    message: string;
    metadata?: Record<string, unknown>;
  }) => void;
}

interface ProviderRuntimeOptions {
  timeoutMs: number;
  maxRetries: number;
}

const isRecord = (value: unknown): value is Record<string, unknown> => {
  return typeof value === "object" && value !== null;
};

const normalizeLoadedPlan = (value: unknown): AiExecutionPlan | undefined => {
  if (!isRecord(value) || typeof value.summary !== "string" || !Array.isArray(value.steps)) {
    return undefined;
  }

  const steps = value.steps
    .map((step): AiExecutionPlan["steps"][number] | undefined => {
      if (!isRecord(step)) return undefined;
      if (
        typeof step.step !== "number" ||
        typeof step.command !== "string" ||
        typeof step.description !== "string" ||
        typeof step.risky !== "boolean"
      ) {
        return undefined;
      }
      return {
        step: step.step,
        command: step.command,
        description: step.description,
        risky: step.risky,
      };
    })
    .filter((step): step is AiExecutionPlan["steps"][number] => Boolean(step));

  return { summary: value.summary, steps };
};

const normalizeLoadedMessage = (value: unknown): AiChatMessage | undefined => {
  if (!isRecord(value)) return undefined;
  if (
    typeof value.id !== "string" ||
    typeof value.content !== "string" ||
    typeof value.timestamp !== "string" ||
    (value.role !== "user" && value.role !== "assistant" && value.role !== "system")
  ) {
    return undefined;
  }

  const type = resolveAiMessageType({
    role: value.role,
    type:
      value.type === "user_prompt" ||
      value.type === "assistant_reply" ||
      value.type === "execution_result" ||
      value.type === "system_note"
        ? value.type
        : undefined,
    kind: value.kind === "execution_result" ? "execution_result" : "chat",
  });

  return {
    id: value.id,
    role: getAiMessageCanonicalRole({ role: value.role, type }),
    type,
    content: value.content,
    timestamp: value.timestamp,
    plan: normalizeLoadedPlan(value.plan),
  };
};

const normalizeLoadedConversation = (value: unknown): AiConversation | undefined => {
  if (!isRecord(value)) return undefined;
  if (
    typeof value.id !== "string" ||
    typeof value.title !== "string" ||
    typeof value.createdAt !== "string" ||
    typeof value.updatedAt !== "string" ||
    !Array.isArray(value.messages)
  ) {
    return undefined;
  }

  return {
    id: value.id,
    title: value.title,
    messages: value.messages
      .map((message) => normalizeLoadedMessage(message))
      .filter((message): message is AiChatMessage => Boolean(message)),
    sessionId: typeof value.sessionId === "string" ? value.sessionId : undefined,
    connectionId: typeof value.connectionId === "string" ? value.connectionId : undefined,
    createdAt: value.createdAt,
    updatedAt: value.updatedAt,
  };
};

export class AiService {
  private readonly deps: AiServiceDeps;
  private readonly router = new LlmRouter();
  private readonly executionCoordinator: AiExecutionCoordinator;
  private readonly conversations = new Map<string, AiConversation>();
  private readonly conversationOwners = new Map<string, WebContents>();
  private readonly apiKeys = new Map<string, string>();
  private readonly taskContexts = new Map<string, AiTaskContext>();
  private readonly historyFilePath: string;
  private persistQueue: Promise<void> = Promise.resolve();

  constructor(deps: AiServiceDeps) {
    this.deps = deps;
    this.executionCoordinator = new AiExecutionCoordinator({
      execCommand: deps.execCommand,
      execInSession: deps.execInSession,
      appendAuditLog: deps.appendAuditLog,
      isAbortError: (error) => this.isAbortError(error),
    });
    this.historyFilePath = path.join(deps.dataDir, AI_HISTORY_FILE);
    this.loadPersistedConversations();
  }

  private loadPersistedConversations(): void {
    try {
      const raw = fs.readFileSync(this.historyFilePath, "utf-8");
      const parsed = JSON.parse(raw);
      if (!Array.isArray(parsed)) {
        throw new Error("历史文件格式不正确");
      }

      for (const item of parsed) {
        const conversation = normalizeLoadedConversation(item);
        if (conversation) {
          this.conversations.set(conversation.id, conversation);
        }
      }
    } catch (error) {
      const nodeError = error as NodeJS.ErrnoException;
      if (nodeError?.code !== "ENOENT") {
        logger.warn("[AI] 加载历史会话失败，已降级为空历史", error);
      }
    }
  }

  private getConversationOwner(conversationId: string): WebContents | undefined {
    const owner = this.conversationOwners.get(conversationId);
    if (owner?.isDestroyed()) {
      this.conversationOwners.delete(conversationId);
      return undefined;
    }
    return owner;
  }

  private bindConversationOwner(conversationId: string, sender: WebContents): void {
    if (!sender.isDestroyed()) {
      this.conversationOwners.set(conversationId, sender);
    }
  }

  private canAccessConversation(
    conversation: AiConversation,
    sender: WebContents,
    connectionId?: string
  ): boolean {
    const owner = this.getConversationOwner(conversation.id);
    if (owner) {
      return owner.id === sender.id;
    }
    return Boolean(connectionId && conversation.connectionId === connectionId);
  }

  private emitToSender(sender: WebContents, channel: string, payload: unknown): void {
    if (!sender.isDestroyed()) {
      sender.send(channel, payload);
    }
  }

  private queuePersist(): void {
    const content = JSON.stringify(
      Array.from(this.conversations.values())
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
        .slice(0, MAX_HISTORY_ITEMS),
      null,
      2
    );

    this.persistQueue = this.persistQueue
      .catch(() => undefined)
      .then(async () => {
        try {
          await fsp.mkdir(path.dirname(this.historyFilePath), { recursive: true });
          const tempPath = `${this.historyFilePath}.tmp`;
          await fsp.writeFile(tempPath, content, "utf-8");
          await fsp.rename(tempPath, this.historyFilePath);
        } catch (error) {
          logger.error("[AI] 持久化历史会话失败", error);
        }
      });
  }

  private getTaskContext(conversationId: string): AiTaskContext {
    const existing = this.taskContexts.get(conversationId);
    if (existing) {
      return existing;
    }
    const created: AiTaskContext = { aborted: false };
    this.taskContexts.set(conversationId, created);
    return created;
  }

  private resetTaskContext(conversationId: string): AiTaskContext {
    const current = this.getTaskContext(conversationId);
    current.chatController?.abort(new AiTaskAbortedError());
    current.executionController?.abort(new AiTaskAbortedError());
    current.analysisController?.abort(new AiTaskAbortedError());
    const next: AiTaskContext = { aborted: false };
    this.taskContexts.set(conversationId, next);
    return next;
  }

  private ensureTaskNotAborted(conversationId: string): void {
    if (this.taskContexts.get(conversationId)?.aborted) {
      throw new AiTaskAbortedError();
    }
  }

  private isAbortError(error: unknown): boolean {
    if (error instanceof AiTaskAbortedError) {
      return true;
    }
    if (error instanceof DOMException && error.name === "AbortError") {
      return true;
    }
    return error instanceof Error && error.name === "AbortError";
  }

  private getActiveProvider(): AiProviderConfig | undefined {
    const prefs = this.deps.getPreferences();
    if (!prefs.ai.enabled) return undefined;
    const activeId = prefs.ai.activeProviderId;
    return prefs.ai.providers.find((p) => p.id === activeId && p.enabled);
  }

  private getProviderRuntimeOptions(): ProviderRuntimeOptions {
    const prefs = this.deps.getPreferences();
    return {
      timeoutMs: (prefs.ai.providerRequestTimeoutSec ?? 30) * 1000,
      maxRetries: prefs.ai.providerMaxRetries ?? 1,
    };
  }

  private async resolveApiKey(provider: AiProviderConfig): Promise<string> {
    const cached = this.apiKeys.get(provider.id);
    if (cached) return cached;

    if (provider.apiKeyRef) {
      try {
        const key = await this.deps.vault.readCredential(provider.apiKeyRef);
        if (!key) throw new Error("API Key 为空");
        this.apiKeys.set(provider.id, key);
        return key;
      } catch {
        throw new Error("无法读取 API Key，请在设置中重新配置");
      }
    }
    throw new Error("未配置 API Key，请在设置中添加");
  }

  async setApiKey(providerId: string, apiKey: string): Promise<void> {
    const ref = `ai-provider-${providerId}`;
    await this.deps.vault.storeCredential(ref, apiKey);
    this.apiKeys.set(providerId, apiKey);
    this.router.clearCache();
  }

  async chat(sender: WebContents, input: AiChatInput): Promise<{ conversationId: string }> {
    const provider = this.getActiveProvider();
    if (!provider) throw new Error("未启用 AI 助手或未配置提供商");

    const apiKey = await this.resolveApiKey(provider);
    const adapter = this.router.getAdapter(provider, apiKey);
    const providerRuntimeOptions = this.getProviderRuntimeOptions();

    let conversation = input.conversationId
      ? this.conversations.get(input.conversationId)
      : undefined;

    if (!conversation) {
      const id = input.conversationId ?? crypto.randomUUID();
      conversation = {
        id,
        title: input.message.slice(0, 50),
        messages: [],
        sessionId: input.sessionId,
        connectionId: input.connectionId,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      };
      this.conversations.set(id, conversation);
    } else {
      // 同步最新的 session/connection（用户可能重连或切换了终端 tab）
      conversation.sessionId = input.sessionId;
      conversation.connectionId = input.connectionId;
    }

    this.bindConversationOwner(conversation.id, sender);

    const userMessage: AiChatMessage = {
      id: crypto.randomUUID(),
      role: "user",
      type: "user_prompt",
      content: input.message,
      timestamp: new Date().toISOString(),
    };
    conversation.messages.push(userMessage);
    conversation.updatedAt = userMessage.timestamp;
    this.queuePersist();

    const prefs = this.deps.getPreferences();
    const systemPrompt = prefs.ai.systemPromptOverride?.trim() || SYSTEM_PROMPT;

    const chatMessages = trimMessagesForContext([
      { role: "system", content: systemPrompt },
      ...conversation.messages.map((m) => ({
        role: getAiMessageModelRole(m) as "user" | "assistant" | "system",
        content: m.content,
      })),
    ]);

    const conversationId = conversation.id;
    const taskContext = this.resetTaskContext(conversationId);
    const chatController = new AbortController();
    taskContext.chatController = chatController;

    // Stream in background
    void (async () => {
      let fullContent = "";
      try {
        fullContent = await adapter.streamChat(
          chatMessages,
          (token) => {
            const event: AiStreamEvent = {
              conversationId,
              type: "token",
              token,
            };
            this.emitToSender(sender, IPCChannel.AiStreamEvent, event);
          },
          {
            signal: chatController.signal,
            timeoutMs: providerRuntimeOptions.timeoutMs,
            maxRetries: providerRuntimeOptions.maxRetries,
          }
        );

        const plan = extractPlanFromResponse(fullContent);

        const assistantMessage: AiChatMessage = {
          id: crypto.randomUUID(),
          role: "assistant",
          type: "assistant_reply",
          content: fullContent,
          timestamp: new Date().toISOString(),
          plan: plan ?? undefined,
        };
        conversation!.messages.push(assistantMessage);
        conversation!.updatedAt = assistantMessage.timestamp;
        this.queuePersist();

        const doneEvent: AiStreamEvent = {
          conversationId,
          type: plan ? "plan" : "done",
          fullContent,
          ...(plan ? { plan } : {}),
        };
        this.emitToSender(sender, IPCChannel.AiStreamEvent, doneEvent);
      } catch (err) {
        if (this.isAbortError(err) || chatController.signal.aborted) {
          return;
        }
        const errorMsg = err instanceof Error ? err.message : String(err);
        logger.error("[AI] chat error", err);
        const errorEvent: AiStreamEvent = {
          conversationId,
          type: "error",
          error: errorMsg,
        };
        this.emitToSender(sender, IPCChannel.AiStreamEvent, errorEvent);
      } finally {
        const currentTask = this.taskContexts.get(conversationId);
        if (currentTask?.chatController === chatController) {
          currentTask.chatController = undefined;
        }
      }
    })();

    return { conversationId };
  }

  async approve(sender: WebContents, input: AiApproveInput): Promise<{ ok: true }> {
    const conversation = this.conversations.get(input.conversationId);
    if (!conversation) throw new Error("对话不存在");
    this.bindConversationOwner(conversation.id, sender);

    let planToExecute = input.plan;

    if (!planToExecute) {
      const lastAssistant = [...conversation.messages]
        .reverse()
        .find((m) => isAiAssistantReplyMessage(m) && m.plan);
      if (!lastAssistant?.plan) throw new Error("没有待执行的计划");
      planToExecute = lastAssistant.plan;
    }

    planToExecute = normalizeApprovedPlan(planToExecute);

    const connectionId = conversation.connectionId;
    if (!connectionId) throw new Error("没有关联的远端连接");

    void this.executePlan(sender, conversation, planToExecute, connectionId);

    return { ok: true as const };
  }

  private async executePlan(
    sender: WebContents,
    conversation: AiConversation,
    plan: AiExecutionPlan,
    connectionId: string
  ): Promise<void> {
    const conversationId = conversation.id;
    const prefs = this.deps.getPreferences();
    const timeoutMs = (prefs.ai.executionTimeoutSec ?? 30) * 1000;
    const taskContext = this.getTaskContext(conversationId);
    taskContext.aborted = false;
    const executionController = new AbortController();
    taskContext.executionController = executionController;

    try {
      const executionResult = await this.executionCoordinator.executePlan({
        conversationId,
        connectionId,
        sessionId: conversation.sessionId,
        plan,
        timeoutMs,
        signal: executionController.signal,
        ensureNotAborted: () => this.ensureTaskNotAborted(conversationId),
        onProgress: (event) => this.emitToSender(sender, IPCChannel.AiProgressEvent, event),
        onStepCompleted: ({ step, exitCode, output, sanitizedOutput, truncated }) => {
          const resultMessage: AiChatMessage = {
            id: crypto.randomUUID(),
            role: "system",
            type: "execution_result",
            content: buildAnalysisPrompt({
              command: step.command,
              output: sanitizedOutput,
              exitCode,
              wasTruncated: truncated.wasTruncated,
              totalLines: truncated.totalLines,
              totalChars: truncated.totalChars,
            }),
            timestamp: new Date().toISOString(),
          };
          conversation.messages.push(resultMessage);
          conversation.updatedAt = resultMessage.timestamp;
          this.queuePersist();
        },
      });

      if (executionResult.status === "aborted") {
        return;
      }
      if (executionResult.status === "failed") {
        if (executionResult.error) {
          logger.error("[AI] execution run failed", executionResult.error);
        }
        return;
      }

      this.ensureTaskNotAborted(conversationId);

      const analysisStartEvent: AiProgressEvent = {
        conversationId,
        type: "analysis_start",
        status: "running",
      };
      this.emitToSender(sender, IPCChannel.AiProgressEvent, analysisStartEvent);

      const provider = this.getActiveProvider();
      if (provider) {
        const apiKey = await this.resolveApiKey(provider);
        const adapter = this.router.getAdapter(provider, apiKey);
        const providerRuntimeOptions = this.getProviderRuntimeOptions();

        const prefs = this.deps.getPreferences();
        const systemPrompt = prefs.ai.systemPromptOverride?.trim() || SYSTEM_PROMPT;

        const chatMessages = trimMessagesForContext([
          { role: "system", content: systemPrompt },
          ...conversation.messages.map((m) => ({
            role: getAiMessageModelRole(m) as "user" | "assistant" | "system",
            content: m.content,
          })),
        ]);

        const analysisController = new AbortController();
        taskContext.analysisController = analysisController;

        const analysis = await adapter.streamChat(
          chatMessages,
          (token) => {
            this.emitToSender(sender, IPCChannel.AiStreamEvent, {
              conversationId,
              type: "token",
              token,
            } satisfies AiStreamEvent);
          },
          {
            signal: analysisController.signal,
            timeoutMs: providerRuntimeOptions.timeoutMs,
            maxRetries: providerRuntimeOptions.maxRetries,
          }
        );

        this.ensureTaskNotAborted(conversationId);

        const newPlan = extractPlanFromResponse(analysis);
        const analysisMsg: AiChatMessage = {
          id: crypto.randomUUID(),
          role: "assistant",
          type: "assistant_reply",
          content: analysis,
          timestamp: new Date().toISOString(),
          plan: newPlan ?? undefined,
        };
        conversation.messages.push(analysisMsg);
        conversation.updatedAt = analysisMsg.timestamp;
        this.queuePersist();

        this.emitToSender(sender, IPCChannel.AiStreamEvent, {
          conversationId,
          type: newPlan ? "plan" : "done",
          fullContent: analysis,
          ...(newPlan ? { plan: newPlan } : {}),
        } satisfies AiStreamEvent);
      }

      const allDoneEvent: AiProgressEvent = {
        conversationId,
        type: "all_done",
        summary: plan.summary,
      };
      this.emitToSender(sender, IPCChannel.AiProgressEvent, allDoneEvent);
    } catch (err) {
      if (this.isAbortError(err)) {
        return;
      }
      logger.error("[AI] post-execution analysis failed", err);
    } finally {
      const currentTask = this.taskContexts.get(conversationId);
      if (currentTask === taskContext) {
        currentTask.executionController = undefined;
        currentTask.analysisController = undefined;
        if (!currentTask.aborted) {
          this.taskContexts.delete(conversationId);
        }
      }
    }
  }

  abort(sender: WebContents, input: AiAbortInput): { ok: true } {
    this.bindConversationOwner(input.conversationId, sender);
    const context = this.taskContexts.get(input.conversationId);
    if (context) {
      context.aborted = true;
      context.chatController?.abort(new AiTaskAbortedError());
      context.executionController?.abort(new AiTaskAbortedError());
      context.analysisController?.abort(new AiTaskAbortedError());
    }
    return { ok: true as const };
  }

  history(sender: WebContents, input: AiHistoryInput): AiConversation[] {
    const connectionId = input.connectionId;
    const visible = Array.from(this.conversations.values()).filter((conversation) =>
      this.canAccessConversation(conversation, sender, connectionId)
    );

    for (const conversation of visible) {
      this.bindConversationOwner(conversation.id, sender);
    }

    return visible
      .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
      .slice(0, 50);
  }

  async testProvider(input: AiProviderTestInput): Promise<{ ok: boolean; error?: string }> {
    try {
      const adapter = this.router.createTemporary(
        input.type,
        input.baseUrl,
        input.model,
        input.apiKey
      );
      return await adapter.testConnection();
    } catch (err) {
      return { ok: false, error: err instanceof Error ? err.message : String(err) };
    }
  }

  dispose(): void {
    for (const context of this.taskContexts.values()) {
      context.chatController?.abort(new AiTaskAbortedError());
      context.executionController?.abort(new AiTaskAbortedError());
      context.analysisController?.abort(new AiTaskAbortedError());
    }
    this.taskContexts.clear();
    this.conversationOwners.clear();
    this.router.clearCache();
  }
}
