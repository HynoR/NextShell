import { create } from "zustand";
import type {
  AiConversation,
  AiChatMessage,
  AiExecutionPlan,
  AiExecutionProgress,
  AiStepResult,
} from "@nextshell/core";
import type { AiStreamEvent, AiProgressEvent } from "@nextshell/shared";

const AI_PANEL_STORAGE_KEY = "nextshell.workspace.aiPanelCollapsed";

export type ExecutionPhase = "executing" | "collecting" | "analyzing" | "receiving";

interface AiChatState {
  panelOpen: boolean;
  conversations: AiConversation[];
  activeConversationId?: string;
  isStreaming: boolean;
  streamingContent: string;
  executionProgress?: AiExecutionProgress;
  executionPhase?: ExecutionPhase;
  pendingPlan?: AiExecutionPlan;
  pendingPlanUserRequest?: string;
  showHistory: boolean;

  togglePanel: () => void;
  setPanelOpen: (open: boolean) => void;
  sendMessage: (content: string, sessionId?: string, connectionId?: string) => Promise<void>;
  approvePlan: () => Promise<void>;
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

export const useAiChatStore = create<AiChatState>((set, get) => ({
  panelOpen: readPanelState(),
  conversations: [],
  activeConversationId: undefined,
  isStreaming: false,
  streamingContent: "",
  executionProgress: undefined,
  executionPhase: undefined,
  pendingPlan: undefined,
  pendingPlanUserRequest: undefined,
  showHistory: false,

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

  sendMessage: async (content, sessionId, connectionId) => {
    set({ isStreaming: true, streamingContent: "", pendingPlan: undefined, pendingPlanUserRequest: content });

    const state = get();
    const activeId = state.activeConversationId;

    const userMessage: AiChatMessage = {
      id: crypto.randomUUID(),
      role: "user",
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
        sessionId,
        connectionId,
      });

      if (!activeId) {
        const newConv: AiConversation = {
          id: result.conversationId,
          title: content.slice(0, 50),
          messages: [userMessage],
          sessionId,
          connectionId,
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

  approvePlan: async () => {
    const state = get();
    if (!state.activeConversationId) return;

    set({
      pendingPlan: undefined,
      executionPhase: "executing",
      executionProgress: {
        planSummary: state.pendingPlan?.summary ?? "",
        steps: (state.pendingPlan?.steps ?? []).map((s) => ({
          step: s.step,
          status: "pending",
        })),
        currentStep: 0,
        completed: false,
      },
    });

    await window.nextshell.ai.approve({
      conversationId: state.activeConversationId,
    });
  },

  abortExecution: async () => {
    const state = get();
    if (!state.activeConversationId) return;
    await window.nextshell.ai.abort({
      conversationId: state.activeConversationId,
    });
    set({ isStreaming: false, executionProgress: undefined, executionPhase: undefined, pendingPlan: undefined });
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
    });
  },

  setShowHistory: (show) => {
    set({ showHistory: show });
  },

  switchConversation: (conversationId) => {
    set({
      activeConversationId: conversationId,
      isStreaming: false,
      streamingContent: "",
      executionProgress: undefined,
      executionPhase: undefined,
      pendingPlan: undefined,
      showHistory: false,
    });
  },

  loadHistory: async () => {
    try {
      const history = await window.nextshell.ai.history();
      if (Array.isArray(history)) {
        set((s) => {
          const existingIds = new Set(s.conversations.map((c) => c.id));
          const merged = [...s.conversations];
          for (const conv of history) {
            if (!existingIds.has(conv.id)) {
              merged.push(conv);
            }
          }
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

    if (event.type === "token" && event.token) {
      const updates: Partial<AiChatState> = {
        streamingContent: state.streamingContent + event.token,
        isStreaming: true,
      };
      if (state.executionPhase === "analyzing") {
        updates.executionPhase = "receiving";
      }
      set(updates);
    }

    if (event.type === "plan" && event.plan) {
      const plan: AiExecutionPlan = {
        steps: event.plan.steps,
        summary: event.plan.summary,
      };
      set({ pendingPlan: plan, isStreaming: false, executionProgress: undefined, executionPhase: undefined });

      if (event.fullContent) {
        addAssistantMessage(event.conversationId, event.fullContent, plan);
      }
    }

    if (event.type === "done") {
      set({ isStreaming: false, executionProgress: undefined, executionPhase: undefined });
      if (event.fullContent) {
        addAssistantMessage(event.conversationId, event.fullContent);
      }
    }

    if (event.type === "error") {
      set({ isStreaming: false });
      if (event.error) {
        addAssistantMessage(event.conversationId, `错误：${event.error}`);
      }
    }

    function addAssistantMessage(convId: string, content: string, plan?: AiExecutionPlan): void {
      const msg: AiChatMessage = {
        id: crypto.randomUUID(),
        role: "assistant",
        content,
        timestamp: new Date().toISOString(),
        plan,
      };

      set((s) => ({
        streamingContent: "",
        conversations: s.conversations.map((c) =>
          c.id === convId
            ? { ...c, messages: [...c.messages, msg], updatedAt: new Date().toISOString() }
            : c
        ),
      }));
    }
  },

  handleProgressEvent: (event) => {
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
        return { executionPhase: "analyzing" as const };
      }

      if (event.type === "all_done") {
        return { executionProgress: undefined, executionPhase: undefined };
      }

      if (event.type === "error") {
        return {
          executionPhase: undefined,
          executionProgress: {
            ...progress,
            completed: true,
          },
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
