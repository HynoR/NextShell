import { create } from "zustand";
import type { SftpTransferStatusEvent } from "@nextshell/shared";

export type TransferDirection = "upload" | "download";
export type TransferTaskStatus = "queued" | "running" | "success" | "failed";

export interface TransferTask {
  id: string;
  direction: TransferDirection;
  connectionId: string;
  localPath: string;
  remotePath: string;
  status: TransferTaskStatus;
  progress: number;
  createdAt: string;
  updatedAt: string;
  error?: string;
  message?: string;
  retryOfTaskId?: string;
}

interface EnqueueTransferInput {
  id?: string;
  direction: TransferDirection;
  connectionId: string;
  localPath: string;
  remotePath: string;
  retryOfTaskId?: string;
}

interface TransferQueueState {
  tasks: TransferTask[];
  enqueueTask: (input: EnqueueTransferInput) => TransferTask;
  applyEvent: (event: SftpTransferStatusEvent) => void;
  markFailed: (taskId: string, error: string) => void;
  markSuccess: (taskId: string) => void;
  getTask: (taskId: string) => TransferTask | undefined;
  clearFinished: () => void;
  removeTask: (taskId: string) => void;
}

const MAX_TASKS = 200;

const nowIso = (): string => new Date().toISOString();

const createTask = (input: EnqueueTransferInput): TransferTask => {
  const now = nowIso();
  return {
    id: input.id ?? crypto.randomUUID(),
    direction: input.direction,
    connectionId: input.connectionId,
    localPath: input.localPath,
    remotePath: input.remotePath,
    status: "queued",
    progress: 0,
    createdAt: now,
    updatedAt: now,
    retryOfTaskId: input.retryOfTaskId
  };
};

const upsertTask = (tasks: TransferTask[], next: TransferTask): TransferTask[] => {
  const index = tasks.findIndex((item) => item.id === next.id);
  if (index < 0) {
    return [next, ...tasks].slice(0, MAX_TASKS);
  }

  const cloned = [...tasks];
  cloned[index] = next;
  return cloned;
};

const matchTaskByPayload = (
  tasks: TransferTask[],
  event: SftpTransferStatusEvent
): TransferTask | undefined => {
  return tasks.find(
    (task) =>
      task.direction === event.direction &&
      task.connectionId === event.connectionId &&
      task.localPath === event.localPath &&
      task.remotePath === event.remotePath &&
      (task.status === "queued" || task.status === "running")
  );
};

export const useTransferQueueStore = create<TransferQueueState>((set, get) => ({
  tasks: [],
  enqueueTask: (input) => {
    const task = createTask(input);
    set((state) => ({
      tasks: upsertTask(state.tasks, task)
    }));
    return task;
  },
  applyEvent: (event) => {
    set((state) => {
      const current = event.taskId
        ? state.tasks.find((item) => item.id === event.taskId)
        : matchTaskByPayload(state.tasks, event);

      const fallback = current ?? createTask({
        id: event.taskId,
        direction: event.direction,
        connectionId: event.connectionId,
        localPath: event.localPath,
        remotePath: event.remotePath
      });

      const next: TransferTask = {
        ...fallback,
        status: event.status,
        progress: event.progress,
        error: event.error,
        message: event.message,
        updatedAt: nowIso()
      };

      return {
        tasks: upsertTask(state.tasks, next)
      };
    });
  },
  markFailed: (taskId, error) => {
    set((state) => {
      const current = state.tasks.find((item) => item.id === taskId);
      if (!current) {
        return {};
      }

      return {
        tasks: upsertTask(state.tasks, {
          ...current,
          status: "failed",
          progress: 100,
          error,
          updatedAt: nowIso()
        })
      };
    });
  },
  markSuccess: (taskId) => {
    set((state) => {
      const current = state.tasks.find((item) => item.id === taskId);
      if (!current) {
        return {};
      }

      return {
        tasks: upsertTask(state.tasks, {
          ...current,
          status: "success",
          progress: 100,
          updatedAt: nowIso()
        })
      };
    });
  },
  getTask: (taskId) => get().tasks.find((item) => item.id === taskId),
  clearFinished: () => {
    set((state) => ({
      tasks: state.tasks.filter((task) => task.status !== "success")
    }));
  },
  removeTask: (taskId) => {
    set((state) => ({
      tasks: state.tasks.filter((task) => task.id !== taskId)
    }));
  }
}));
