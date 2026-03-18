import { create } from "zustand";
import type {
  AiConversation,
  AiChatMessage,
  AiExecutionPlan,
  AiExecutionProgress,
  AiStepResult,
} from "@nextshell/core";
import {
  isAiAssistantReplyMessage,
  isAiUserPromptMessage,
} from "@nextshell/core";
import type { AiStreamEvent, AiProgressEvent } from "@nextshell/shared";
import { formatAiErrorMessage, summarizeAiError } from "../utils/ai-error-message";

const AI_PANEL_STORAGE_KEY = "nextshell.workspace.aiPanelCollapsed";
const AI_CLIENT_ID_STORAGE_KEY = "nextshell.workspace.aiClientId";

let memoryAiClientId: string | undefined;

const createAiClientId = (): string => {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `ai-client-${Date.now()}-${Math.random().toString(16).slice(2)}`;
};

const getAiClientId = (): string => {
  if (memoryAiClientId) {
    return memoryAiClientId;
  }
  try {
    const stored = sessionStorage.getItem(AI_CLIENT_ID_STORAGE_KEY);
    if (stored) {
      memoryAiClientId = stored;
      return stored;
    }
    const created = createAiClientId();
    sessionStorage.setItem(AI_CLIENT_ID_STORAGE_KEY, created);
    memoryAiClientId = created;
    return created;
  } catch {
    memoryAiClientId = createAiClientId();
    return memoryAiClientId;
  }
};

export type ExecutionPhase = "executing" | "collecting" | "analyzing" | "receiving";

/** 当前 AI 面板的活动状态提示 */
export interface StatusHint {
  icon: string;
  text: string;
  /** 是否显示动画 */
  animate?: boolean;
}

interface AiChatState {
  panelOpen: boolean;

  /** 当前 AI 面板绑定的连接 */
  boundConnectionId?: string;
  boundSessionId?: string;
  boundConnectionLabel?: string;

  conversations: AiConversation[];
  activeConversationId?: string;
  isStreaming: boolean;
  streamingContent: string;
  executionProgress?: AiExecutionProgress;
  executionPhase?: ExecutionPhase;
  pendingPlan?: AiExecutionPlan;
  pendingPlanUserRequest?: string;
  showHistory: boolean;
  statusHint?: StatusHint;

  togglePanel: () => void;
  setPanelOpen: (open: boolean) => void;
  /** 当活动终端 tab 切换时调用，将 AI 面板绑定到新连接 */
  setConnection: (connectionId?: string, sessionId?: string, label?: string) => void;
  sendMessage: (content: string) => Promise<void>;
  approvePlan: (editedPlan?: AiExecutionPlan) => Promise<void>;
  abortExecution: () => Promise<void>;
  newConversation: () => void;
  setShowHistory: (show: boolean) => void;
  switchConversation: (conversationId: string) => void;
  loadHistory: () => Promise<void>;
  handleStreamEvent: (event: AiStreamEvent) => void;
  handleProgressEvent: (event: AiProgressEvent) => void;
  initListeners: () => () => void;
}

const readPanelState = (): boolean => {
  try {
    return localStorage.getItem(AI_PANEL_STORAGE_KEY) !== "true";
  } catch {
    return false;
  }
};

/**
 * 从对话消息中恢复未审批的执行计划。
 * 判定规则：从后往前找到第一条 assistant 消息，如果它带有 plan 且后面没有新的真实用户输入，
 * 则该计划仍处于待审批状态。
 */
export const restorePendingPlan = (
  conv: AiConversation
): { plan: AiExecutionPlan; userRequest?: string } | undefined => {
  const msgs = conv.messages;
  if (msgs.length === 0) return undefined;

  for (let i = msgs.length - 1; i >= 0; i--) {
    const msg = msgs[i]!;
    if (isAiAssistantReplyMessage(msg) && msg.plan) {
      const hasFollowUp = msgs.slice(i + 1).some((m) => isAiUserPromptMessage(m));
      if (hasFollowUp) return undefined;

      let userRequest: string | undefined;
      for (let j = i - 1; j >= 0; j--) {
        if (isAiUserPromptMessage(msgs[j]!)) {
          userRequest = msgs[j]!.content;
          break;
        }
      }
      return { plan: msg.plan, userRequest };
    }
    if (isAiUserPromptMessage(msg)) return undefined;
  }
  return undefined;
};

export const useAiChatStore = create<AiChatState>((set, get) => ({
  panelOpen: readPanelState(),
  boundConnectionId: undefined,
  boundSessionId: undefined,
  boundConnectionLabel: undefined,
  conversations: [],
  activeConversationId: undefined,
  isStreaming: false,
  streamingContent: "",
  executionProgress: undefined,
  executionPhase: undefined,
  pendingPlan: undefined,
  pendingPlanUserRequest: undefined,
  showHistory: false,
  statusHint: undefined,

  togglePanel: () => {
    const next = !get().panelOpen;
    set({ panelOpen: next });
    try {
      localStorage.setItem(AI_PANEL_STORAGE_KEY, next ? "false" : "true");
    } catch { /* ignore */ }
  },

  setPanelOpen: (open) => {
    set({ panelOpen: open });
    try {
      localStorage.setItem(AI_PANEL_STORAGE_KEY, open ? "false" : "true");
    } catch { /* ignore */ }
  },

  setConnection: (connectionId, sessionId, label) => {
    const state = get();

    // 同一连接只更新 session（重连场景）
    if (state.boundConnectionId === connectionId) {
      set({ boundSessionId: sessionId, boundConnectionLabel: label });
      return;
    }

    // 连接切换 - 自动切到新连接最近的对话
    const latestConv = connectionId
      ? state.conversations
          .filter((c) => c.connectionId === connectionId)
          .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))[0]
      : undefined;

    const restored = latestConv ? restorePendingPlan(latestConv) : undefined;

    set({
      boundConnectionId: connectionId,
      boundSessionId: sessionId,
      boundConnectionLabel: label,
      activeConversationId: latestConv?.id,
      isStreaming: false,
      streamingContent: "",
      executionProgress: undefined,
      executionPhase: undefined,
      pendingPlan: restored?.plan,
      pendingPlanUserRequest: restored?.userRequest,
      showHistory: false,
      statusHint: restored ? { icon: "ri-file-list-3-line", text: "有待审批的执行计划" } : undefined,
    });
  },

  sendMessage: async (content) => {
    const { boundConnectionId, boundSessionId } = get();
    if (!boundConnectionId) throw new Error("未选择终端连接");

    set({
      isStreaming: true,
      streamingContent: "",
      pendingPlan: undefined,
      pendingPlanUserRequest: content,
      statusHint: { icon: "ri-loader-4-line", text: "正在连接 AI 模型...", animate: true },
    });

    const state = get();
    const activeId = state.activeConversationId;

    const userMessage: AiChatMessage = {
      id: crypto.randomUUID(),
      role: "user",
      type: "user_prompt",
      content,
      timestamp: new Date().toISOString(),
    };

    if (activeId) {
      set((s) => ({
        conversations: s.conversations.map((c) =>
          c.id === activeId
            ? { ...c, messages: [...c.messages, userMessage], updatedAt: new Date().toISOString() }
            : c
        ),
      }));
    }

    try {
      const result = await window.nextshell.ai.chat({
        conversationId: activeId,
        message: content,
        sessionId: boundSessionId,
        connectionId: boundConnectionId,
        clientId: getAiClientId(),
      });

      if (!activeId) {
        const newConv: AiConversation = {
          id: result.conversationId,
          title: content.slice(0, 50),
          messages: [userMessage],
          sessionId: boundSessionId,
          connectionId: boundConnectionId,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        };
        set((s) => ({
          conversations: [newConv, ...s.conversations],
          activeConversationId: result.conversationId,
        }));
      }
    } catch (err) {
      set({ isStreaming: false });
      throw err;
    }
  },

  approvePlan: async (editedPlan) => {
    const state = get();
    if (!state.activeConversationId) return;

    const planToExecute = editedPlan ?? state.pendingPlan;
    if (!planToExecute) return;

    set({
      pendingPlan: planToExecute,
    });

    set({
      pendingPlan: undefined,
      executionPhase: "executing",
      executionProgress: {
        planSummary: planToExecute.summary ?? "",
        steps: planToExecute.steps.map((s) => ({
          step: s.step,
          status: "pending",
        })),
        currentStep: 0,
        completed: false,
      },
      statusHint: { icon: "ri-terminal-box-line", text: "正在执行计划...", animate: true },
    });

    await window.nextshell.ai.approve({
      conversationId: state.activeConversationId,
      clientId: getAiClientId(),
      plan: {
        steps: planToExecute.steps,
        summary: planToExecute.summary,
      },
    });
  },

  abortExecution: async () => {
    const state = get();
    if (!state.activeConversationId) return;
    await window.nextshell.ai.abort({
      conversationId: state.activeConversationId,
      clientId: getAiClientId(),
    });
    set({ isStreaming: false, executionProgress: undefined, executionPhase: undefined, pendingPlan: undefined, statusHint: undefined });
  },

  newConversation: () => {
    set({
      activeConversationId: undefined,
      isStreaming: false,
      streamingContent: "",
      executionProgress: undefined,
      executionPhase: undefined,
      pendingPlan: undefined,
      pendingPlanUserRequest: undefined,
      showHistory: false,
      statusHint: undefined,
    });
  },

  setShowHistory: (show) => {
    set({ showHistory: show });
  },

  switchConversation: (conversationId) => {
    const conv = get().conversations.find((c) => c.id === conversationId);
    const restored = conv ? restorePendingPlan(conv) : undefined;

    set({
      activeConversationId: conversationId,
      isStreaming: false,
      streamingContent: "",
      executionProgress: undefined,
      executionPhase: undefined,
      pendingPlan: restored?.plan,
      pendingPlanUserRequest: restored?.userRequest,
      showHistory: false,
      statusHint: restored ? { icon: "ri-file-list-3-line", text: "有待审批的执行计划" } : undefined,
    });
  },

  loadHistory: async () => {
    try {
      const history = await window.nextshell.ai.history({
        connectionId: get().boundConnectionId,
        clientId: getAiClientId(),
      });
      if (Array.isArray(history)) {
        set((s) => {
          const mergedById = new Map(s.conversations.map((c) => [c.id, c]));
          for (const conv of history) {
            mergedById.set(conv.id, conv);
          }
          const merged = Array.from(mergedById.values());
          merged.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
          return { conversations: merged };
        });
      }
    } catch {
      // history load is best-effort
    }
  },

  handleStreamEvent: (event) => {
    const state = get();
    const isActiveConv = event.conversationId === state.activeConversationId;

    // 无论是否活跃，都将消息追加到对应对话
    if (event.type === "token" && event.token && isActiveConv) {
      const updates: Partial<AiChatState> = {
        streamingContent: state.streamingContent + event.token,
        isStreaming: true,
        statusHint: { icon: "ri-robot-2-line", text: "AI 正在回复...", animate: true },
      };
      if (state.executionPhase === "analyzing") {
        updates.executionPhase = "receiving";
        updates.statusHint = { icon: "ri-robot-2-line", text: "正在接收分析结论...", animate: true };
      }
      set(updates);
    }

    if (event.type === "plan" && event.plan) {
      const plan: AiExecutionPlan = {
        steps: event.plan.steps,
        summary: event.plan.summary,
      };
      if (isActiveConv) {
        set({
          pendingPlan: plan,
          isStreaming: false,
          executionProgress: undefined,
          executionPhase: undefined,
          statusHint: { icon: "ri-file-list-3-line", text: "已生成执行计划，等待审批" },
        });
      } else {
        const backgroundConv = state.conversations.find((conv) => conv.id === event.conversationId);
        if (
          backgroundConv?.connectionId === state.boundConnectionId
          && !state.executionProgress
          && !state.isStreaming
        ) {
          set({
            statusHint: { icon: "ri-notification-3-line", text: "其他对话已生成待审批计划，点击历史查看" },
          });
        }
      }

      if (event.fullContent) {
        addAssistantMessage(event.conversationId, event.fullContent, plan);
      }
    }

    if (event.type === "done") {
      if (isActiveConv) {
        set({ isStreaming: false, executionProgress: undefined, executionPhase: undefined, statusHint: undefined });
      }
      if (event.fullContent) {
        addAssistantMessage(event.conversationId, event.fullContent);
      }
    }

    if (event.type === "error") {
      const readableError = formatAiErrorMessage(event.error, "AI 请求失败");
      if (isActiveConv) {
        set({
          isStreaming: false,
          statusHint: { icon: "ri-error-warning-line", text: summarizeAiError(event.error, "AI 请求失败") },
        });
      }
      addAssistantMessage(event.conversationId, `错误：${readableError}`);
    }

    function addAssistantMessage(convId: string, content: string, plan?: AiExecutionPlan): void {
      const msg: AiChatMessage = {
        id: crypto.randomUUID(),
        role: "assistant",
        type: "assistant_reply",
        content,
        timestamp: new Date().toISOString(),
        plan,
      };

      set((s) => ({
        streamingContent: isActiveConv ? "" : s.streamingContent,
        conversations: s.conversations.map((c) =>
          c.id === convId
            ? { ...c, messages: [...c.messages, msg], updatedAt: new Date().toISOString() }
            : c
        ),
      }));
    }
  },

  handleProgressEvent: (event) => {
    const state = get();
    const isActiveConv = event.conversationId === state.activeConversationId;

    // 非活跃对话的进度事件忽略 UI 更新（后端仍在执行）
    if (!isActiveConv) return;

    set((s) => {
      const progress = s.executionProgress;
      if (!progress) return {};

      const updatedSteps: AiStepResult[] = [...progress.steps];

      if (event.type === "step_start" && event.step !== undefined) {
        const idx = updatedSteps.findIndex((st) => st.step === event.step);
        if (idx >= 0) {
          updatedSteps[idx] = { ...updatedSteps[idx]!, status: "running" };
        }
        return {
          executionPhase: "executing" as const,
          executionProgress: {
            ...progress,
            steps: updatedSteps,
            currentStep: event.step,
          },
          statusHint: {
            icon: "ri-terminal-box-line",
            text: `正在执行步骤 ${event.step}/${progress.steps.length}...`,
            animate: true,
          },
        };
      }

      if (event.type === "step_done" && event.step !== undefined) {
        const idx = updatedSteps.findIndex((st) => st.step === event.step);
        if (idx >= 0) {
          updatedSteps[idx] = {
            ...updatedSteps[idx]!,
            status: event.status === "success" ? "success" : "failed",
            output: event.output,
          };
        }
        return {
          executionPhase: "collecting" as const,
          executionProgress: { ...progress, steps: updatedSteps },
          statusHint: {
            icon: event.status === "success" ? "ri-check-line" : "ri-close-line",
            text: `步骤 ${event.step} ${event.status === "success" ? "执行成功" : "执行失败"}`,
          },
        };
      }

      if (event.type === "step_output" && event.step !== undefined) {
        const idx = updatedSteps.findIndex((st) => st.step === event.step);
        if (idx >= 0) {
          updatedSteps[idx] = {
            ...updatedSteps[idx]!,
            output: event.output,
          };
        }
        return {
          executionPhase: "collecting" as const,
          executionProgress: { ...progress, steps: updatedSteps },
        };
      }

      if (event.type === "analysis_start") {
        for (let i = 0; i < updatedSteps.length; i++) {
          const st = updatedSteps[i]!;
          if (st.status === "running" || st.status === "pending") {
            updatedSteps[i] = { ...st, status: "success" };
          }
        }
        return {
          executionPhase: "analyzing" as const,
          executionProgress: { ...progress, steps: updatedSteps },
          statusHint: { icon: "ri-brain-line", text: "正在将执行结果提交 AI 分析...", animate: true },
        };
      }

      if (event.type === "all_done") {
        return { executionProgress: undefined, executionPhase: undefined, statusHint: undefined };
      }

      if (event.type === "error") {
        const readableError = formatAiErrorMessage(event.error, "执行出错");
        if (event.step !== undefined) {
          const idx = updatedSteps.findIndex((st) => st.step === event.step);
          if (idx >= 0) {
            updatedSteps[idx] = { ...updatedSteps[idx]!, status: "failed", error: readableError };
          }
        }
        return {
          executionPhase: undefined,
          executionProgress: {
            ...progress,
            steps: updatedSteps,
            completed: true,
          },
          statusHint: { icon: "ri-error-warning-line", text: summarizeAiError(event.error, "执行出错") },
        };
      }

      return {};
    });
  },

  initListeners: () => {
    const unsubStream = window.nextshell.ai.onStream((event) => {
      get().handleStreamEvent(event);
    });

    const unsubProgress = window.nextshell.ai.onProgress((event) => {
      get().handleProgressEvent(event);
    });

    return () => {
      unsubStream();
      unsubProgress();
    };
  },
}));
