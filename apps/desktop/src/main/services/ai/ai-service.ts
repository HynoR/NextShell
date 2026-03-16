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
import { truncateOutput } from "./output-capture";
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

/** 将字符串安全包裹为 shell 单引号参数 */
const escapeShellArg = (arg: string): string => {
  return "'" + arg.replace(/'/g, "'\\''") + "'";
};

interface AiServiceDeps {
  writeSession: (sessionId: string, data: string) => { ok: true };
  execCommand: (connectionId: string, cmd: string) => Promise<CommandExecutionResult>;
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
    } else {
      // 同步最新的 session/connection（用户可能重连或切换了终端 tab）
      conversation.sessionId = input.sessionId;
      conversation.connectionId = input.connectionId;
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

    const chatMessages = trimMessagesForContext([
      { role: "system", content: systemPrompt },
      ...conversation.messages.map((m) => ({
        role: m.role as "user" | "assistant",
        content: m.content,
      })),
    ]);

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

    let planToExecute = input.plan;

    if (!planToExecute) {
      const lastAssistant = [...conversation.messages]
        .reverse()
        .find((m) => m.role === "assistant" && m.plan);
      if (!lastAssistant?.plan) throw new Error("没有待执行的计划");
      planToExecute = lastAssistant.plan;
    }

    const sessionId = conversation.sessionId;
    const connectionId = conversation.connectionId;
    if (!sessionId && !connectionId) throw new Error("没有关联的会话或连接");

    void this.executePlan(conversation, planToExecute, sessionId, connectionId);

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
          // 通过 exec 通道执行，包裹在交互式登录 shell 中以加载完整用户环境
          // 在命令末尾追加 exit code 标记，因为 bash -lic 的外层退出码不可靠
          const EXIT_MARKER = "__NEXTSHELL_EXIT__";
          const innerCmd = `${step.command}; echo "${EXIT_MARKER}$?"`;
          const wrappedCmd = `bash -lic ${escapeShellArg(innerCmd)}`;
          const result = await this.deps.execCommand(connectionId, wrappedCmd);

          // 从 stdout 中提取真实退出码并移除标记行
          const stdoutLines = result.stdout.split("\n");
          let markerIdx = -1;
          for (let k = stdoutLines.length - 1; k >= 0; k--) {
            if (stdoutLines[k]!.startsWith(EXIT_MARKER)) { markerIdx = k; break; }
          }
          if (markerIdx >= 0) {
            const code = parseInt(stdoutLines[markerIdx]!.slice(EXIT_MARKER.length), 10);
            exitCode = Number.isFinite(code) ? code : null;
            stdoutLines.splice(markerIdx, 1);
          }
          const cleanStdout = stdoutLines.join("\n");

          // 过滤 bash -i 产生的无害 stderr 噪声
          const cleanStderr = (result.stderr ?? "")
            .split("\n")
            .filter((l) => !l.includes("cannot set terminal process group") && !l.includes("no job control"))
            .join("\n")
            .trim();
          output = cleanStdout + (cleanStderr ? `\n${cleanStderr}` : "");

          // 同时写入终端让用户看到命令（最终用户看到的和 AI 分析的是同一条命令）
          if (sessionId) {
            try {
              this.deps.writeSession(sessionId, `${step.command}\r`);
            } catch {
              // terminal write is best-effort for visibility
            }
          }
        } else if (sessionId) {
          // 无 exec 通道时通过终端执行并等待输出
          this.deps.writeSession(sessionId, `${step.command}\r`);
          await new Promise((r) => setTimeout(r, 3000));
          output = "(仅终端执行，请查看终端窗口中的输出)";
        } else {
          throw new Error("无可用的执行通道");
        }

        const truncated = truncateOutput(output);

        // Notify step output (前端展示用，限 2000 字符)
        const displayOutput = output.length > 2000
          ? output.slice(0, 1000) + `\n... [省略 ${output.length - 2000} 字符] ...\n` + output.slice(-1000)
          : output;
        const outputEvent: AiProgressEvent = {
          conversationId,
          type: "step_output",
          step: step.step,
          output: displayOutput,
          status: "success",
        };
        this.broadcastToAll(IPCChannel.AiProgressEvent, outputEvent);

        // Notify step done
        const doneEvent: AiProgressEvent = {
          conversationId,
          type: "step_done",
          step: step.step,
          status: exitCode === 0 || exitCode === null ? "success" : "failed",
          output: displayOutput,
        };
        this.broadcastToAll(IPCChannel.AiProgressEvent, doneEvent);

        // Add execution result to conversation for analysis
        const resultMessage: AiChatMessage = {
          id: crypto.randomUUID(),
          role: "user",
          content: buildAnalysisPrompt({
            command: step.command,
            output: truncated.text,
            exitCode,
            wasTruncated: truncated.wasTruncated,
            totalLines: truncated.totalLines,
            totalChars: truncated.totalChars,
          }),
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

    // Notify frontend: entering analysis phase
    const analysisStartEvent: AiProgressEvent = {
      conversationId,
      type: "analysis_start",
      status: "running",
    };
    this.broadcastToAll(IPCChannel.AiProgressEvent, analysisStartEvent);

    // Send analysis request to LLM
    try {
      const provider = this.getActiveProvider();
      if (provider) {
        const apiKey = await this.resolveApiKey(provider);
        const adapter = this.router.getAdapter(provider, apiKey);

        const prefs = this.deps.getPreferences();
        const systemPrompt = prefs.ai.systemPromptOverride?.trim() || SYSTEM_PROMPT;

        const chatMessages = trimMessagesForContext([
          { role: "system", content: systemPrompt },
          ...conversation.messages.map((m) => ({
            role: m.role as "user" | "assistant",
            content: m.content,
          })),
        ]);

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
