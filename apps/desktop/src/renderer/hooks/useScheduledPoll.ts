import { useEffect, useRef } from "react";
import {
  pollingScheduler,
  type PollingSubscriptionOptions
} from "../utils/pollingScheduler";

export interface UseScheduledPollOptions {
  enabled: boolean;
  intervalMs: number;
  runImmediately?: boolean;
  task: () => void | Promise<void>;
}

export const useScheduledPoll = ({
  enabled,
  intervalMs,
  runImmediately,
  task
}: UseScheduledPollOptions): void => {
  const taskRef = useRef<PollingSubscriptionOptions["task"]>(task);

  useEffect(() => {
    taskRef.current = task;
  }, [task]);

  useEffect(() => {
    return pollingScheduler.subscribe({
      enabled,
      intervalMs,
      runImmediately,
      task: () => taskRef.current()
    });
  }, [enabled, intervalMs, runImmediately]);
};
