import type { WebContents } from "electron";
import { BrowserWindow } from "electron";
import type {
  AiConversation,
  AiChatMessage,
  AiExecutionPlan,
  AiProviderConfig,
  AppPreferences,
  CommandExecutionResult,
} from "../../../../../../packages/core/src/index";
import type {
  AiChatInput,
  AiApproveInput,
  AiAbortInput,
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
import { OutputCollector, stripAnsi } from "./output-capture";
import { logger } from "../../logger";

interface AiServiceDeps {
  writeSession: (sessionId: string, data: string) => { ok: true };
  execCommand: (connectionId: string, cmd: string) => Promise<CommandExecutionResult>;
  subscribeSessionData?: (sessionId: string, cb: (data: string) => void) => () => void;
  vault: EncryptedSecretVault;
  getPreferences: () => AppPreferences;
}

export class AiService {
  private readonly deps: AiServiceDeps;
  private readonly router = new LlmRouter();
  private readonly conversations = new Map<string, AiConversation>();
  private readonly abortControllers = new Map<string, AbortController>();
  private readonly apiKeys = new Map<string, string>();

  constructor(deps: AiServiceDeps) {
    this.deps = deps;
  }

  private broadcastToAll(channel: string, payload: unknown): void {
    for (const win of BrowserWindow.getAllWindows()) {
      if (!win.isDestroyed()) win.webContents.send(channel, payload);
    }
  }

  private getActiveProvider(): AiProviderConfig | undefined {
    const prefs = this.deps.getPreferences();
    if (!prefs.ai.enabled) return undefined;
    const activeId = prefs.ai.activeProviderId;
    return prefs.ai.providers.find((p) => p.id === activeId && p.enabled);
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

  async chat(input: AiChatInput): Promise<{ conversationId: string }> {
    const provider = this.getActiveProvider();
    if (!provider) throw new Error("未启用 AI 助手或未配置提供商");

    const apiKey = await this.resolveApiKey(provider);
    const adapter = this.router.getAdapter(provider, apiKey);

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
    }

    const userMessage: AiChatMessage = {
      id: crypto.randomUUID(),
      role: "user",
      content: input.message,
      timestamp: new Date().toISOString(),
    };
    conversation.messages.push(userMessage);

    const prefs = this.deps.getPreferences();
    const systemPrompt = prefs.ai.systemPromptOverride?.trim() || SYSTEM_PROMPT;

    const chatMessages: ChatMessage[] = [
      { role: "system", content: systemPrompt },
      ...conversation.messages.map((m) => ({
        role: m.role as "user" | "assistant",
        content: m.content,
      })),
    ];

    const abortController = new AbortController();
    this.abortControllers.set(conversation.id, abortController);

    const conversationId = conversation.id;

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
            this.broadcastToAll(IPCChannel.AiStreamEvent, event);
          },
          { signal: abortController.signal }
        );

        const plan = extractPlanFromResponse(fullContent);

        const assistantMessage: AiChatMessage = {
          id: crypto.randomUUID(),
          role: "assistant",
          content: fullContent,
          timestamp: new Date().toISOString(),
          plan: plan ?? undefined,
        };
        conversation!.messages.push(assistantMessage);
        conversation!.updatedAt = new Date().toISOString();

        const doneEvent: AiStreamEvent = {
          conversationId,
          type: plan ? "plan" : "done",
          fullContent,
          ...(plan ? { plan } : {}),
        };
        this.broadcastToAll(IPCChannel.AiStreamEvent, doneEvent);
      } catch (err) {
        if (abortController.signal.aborted) return;
        const errorMsg = err instanceof Error ? err.message : String(err);
        logger.error("[AI] chat error", err);
        const errorEvent: AiStreamEvent = {
          conversationId,
          type: "error",
          error: errorMsg,
        };
        this.broadcastToAll(IPCChannel.AiStreamEvent, errorEvent);
      } finally {
        this.abortControllers.delete(conversationId);
      }
    })();

    return { conversationId };
  }

  async approve(input: AiApproveInput): Promise<{ ok: true }> {
    const conversation = this.conversations.get(input.conversationId);
    if (!conversation) throw new Error("对话不存在");

    const lastAssistant = [...conversation.messages]
      .reverse()
      .find((m) => m.role === "assistant" && m.plan);
    if (!lastAssistant?.plan) throw new Error("没有待执行的计划");

    const sessionId = conversation.sessionId;
    const connectionId = conversation.connectionId;
    if (!sessionId && !connectionId) throw new Error("没有关联的会话或连接");

    void this.executePlan(conversation, lastAssistant.plan, sessionId, connectionId);

    return { ok: true as const };
  }

  private async executePlan(
    conversation: AiConversation,
    plan: AiExecutionPlan,
    sessionId?: string,
    connectionId?: string
  ): Promise<void> {
    const conversationId = conversation.id;
    const prefs = this.deps.getPreferences();
    const timeoutMs = (prefs.ai.executionTimeoutSec ?? 30) * 1000;

    for (let i = 0; i < plan.steps.length; i++) {
      const step = plan.steps[i];
      if (!step) continue;

      // Notify step start
      const startEvent: AiProgressEvent = {
        conversationId,
        type: "step_start",
        step: step.step,
        command: step.command,
        status: "running",
      };
      this.broadcastToAll(IPCChannel.AiProgressEvent, startEvent);

      try {
        let output: string;
        let exitCode: number | null = null;

        if (connectionId) {
          // Use exec for clean output capture
          const result = await this.deps.execCommand(connectionId, step.command);
          output = result.stdout + (result.stderr ? `\n${result.stderr}` : "");
          exitCode = result.exitCode;

          // Also write to terminal for visibility if sessionId exists
          if (sessionId) {
            try {
              this.deps.writeSession(sessionId, `${step.command}\r`);
            } catch {
              // terminal write is best-effort for visibility
            }
          }
        } else if (sessionId) {
          // Fallback: write to terminal and capture output
          this.deps.writeSession(sessionId, `${step.command}\r`);
          const collector = new OutputCollector();
          if (this.deps.subscribeSessionData) {
            const unsub = this.deps.subscribeSessionData(sessionId, (data) => {
              collector.push(data);
            });
            output = await collector.collect(timeoutMs);
            unsub();
          } else {
            await new Promise((r) => setTimeout(r, 2000));
            output = "(终端输出捕获不可用，请查看终端)";
          }
        } else {
          throw new Error("无可用的执行通道");
        }

        // Notify step output
        const outputEvent: AiProgressEvent = {
          conversationId,
          type: "step_output",
          step: step.step,
          output: output.slice(0, 2000),
          status: "success",
        };
        this.broadcastToAll(IPCChannel.AiProgressEvent, outputEvent);

        // Notify step done
        const doneEvent: AiProgressEvent = {
          conversationId,
          type: "step_done",
          step: step.step,
          status: exitCode === 0 || exitCode === null ? "success" : "failed",
          output: output.slice(0, 2000),
        };
        this.broadcastToAll(IPCChannel.AiProgressEvent, doneEvent);

        // Add execution result to conversation for analysis
        const resultMessage: AiChatMessage = {
          id: crypto.randomUUID(),
          role: "user",
          content: buildAnalysisPrompt(step.command, output, exitCode),
          timestamp: new Date().toISOString(),
        };
        conversation.messages.push(resultMessage);

      } catch (err) {
        const errorMsg = err instanceof Error ? err.message : String(err);
        const errorEvent: AiProgressEvent = {
          conversationId,
          type: "error",
          step: step.step,
          error: errorMsg,
          status: "failed",
        };
        this.broadcastToAll(IPCChannel.AiProgressEvent, errorEvent);
        logger.error(`[AI] step ${step.step} failed`, err);
        break;
      }
    }

    // Send analysis request to LLM
    try {
      const provider = this.getActiveProvider();
      if (provider) {
        const apiKey = await this.resolveApiKey(provider);
        const adapter = this.router.getAdapter(provider, apiKey);

        const prefs = this.deps.getPreferences();
        const systemPrompt = prefs.ai.systemPromptOverride?.trim() || SYSTEM_PROMPT;

        const chatMessages: ChatMessage[] = [
          { role: "system", content: systemPrompt },
          ...conversation.messages.map((m) => ({
            role: m.role as "user" | "assistant",
            content: m.content,
          })),
        ];

        const analysis = await adapter.streamChat(
          chatMessages,
          (token) => {
            this.broadcastToAll(IPCChannel.AiStreamEvent, {
              conversationId,
              type: "token",
              token,
            } satisfies AiStreamEvent);
          }
        );

        const newPlan = extractPlanFromResponse(analysis);
        const analysisMsg: AiChatMessage = {
          id: crypto.randomUUID(),
          role: "assistant",
          content: analysis,
          timestamp: new Date().toISOString(),
          plan: newPlan ?? undefined,
        };
        conversation.messages.push(analysisMsg);

        this.broadcastToAll(IPCChannel.AiStreamEvent, {
          conversationId,
          type: newPlan ? "plan" : "done",
          fullContent: analysis,
          ...(newPlan ? { plan: newPlan } : {}),
        } satisfies AiStreamEvent);
      }
    } catch (err) {
      logger.error("[AI] post-execution analysis failed", err);
    }

    const allDoneEvent: AiProgressEvent = {
      conversationId,
      type: "all_done",
      summary: plan.summary,
    };
    this.broadcastToAll(IPCChannel.AiProgressEvent, allDoneEvent);
  }

  abort(input: AiAbortInput): { ok: true } {
    const controller = this.abortControllers.get(input.conversationId);
    if (controller) {
      controller.abort();
      this.abortControllers.delete(input.conversationId);
    }
    return { ok: true as const };
  }

  history(): AiConversation[] {
    return Array.from(this.conversations.values())
      .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
      .slice(0, 50);
  }

  async testProvider(input: AiProviderTestInput): Promise<{ ok: boolean; error?: string }> {
    const adapter = this.router.createTemporary(
      input.type,
      input.baseUrl,
      input.model,
      input.apiKey
    );
    return adapter.testConnection();
  }

  dispose(): void {
    for (const controller of this.abortControllers.values()) {
      controller.abort();
    }
    this.abortControllers.clear();
    this.router.clearCache();
  }
}
