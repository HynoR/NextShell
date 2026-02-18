import { message } from "antd";

/**
 * Utility for optimistic UI updates.
 *
 * 1. Immediately applies `optimisticUpdate` to the UI.
 * 2. Fires the async `ipcCall`.
 * 3. If it fails, calls `rollback` and shows an error toast.
 */
export async function withOptimistic<T>(options: {
  optimisticUpdate: () => void;
  ipcCall: () => Promise<T>;
  rollback: () => void;
  onError?: (error: Error) => void;
}): Promise<T | undefined> {
  const { optimisticUpdate, ipcCall, rollback, onError } = options;

  optimisticUpdate();

  try {
    return await ipcCall();
  } catch (error) {
    rollback();
    const err = error instanceof Error ? error : new Error(String(error));
    if (onError) {
      onError(err);
    } else {
      message.error(err.message);
    }
    return undefined;
  }
}
