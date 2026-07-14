import { app, ipcMain } from "electron";
import { ZodError, z } from "zod";
import { logger } from "../logger";
import { IPCChannel } from "../../../../../packages/shared/src/index";
import type { ServiceContainer } from "../services/container";
import { isTrustedRendererUrl } from "../navigation-security";
import { ipcInvokeRegistry } from "./registry";

const channels = Object.values(IPCChannel);

const formatValidationError = (error: ZodError): string => {
  return error.issues
    .map((issue) => {
      const path = issue.path.length > 0 ? issue.path.map(String).join(".") : "payload";
      return `${path}: ${issue.message}`;
    })
    .join("; ");
};

const parsePayload = <T>(
  schema: z.ZodType<T>,
  payload: unknown,
  actionLabel: string
): T => {
  try {
    return schema.parse(payload);
  } catch (error) {
    if (error instanceof ZodError) {
      logger.error(`[IPC Validation] ${actionLabel}`, error);
      throw new Error(`${actionLabel} 参数无效：${formatValidationError(error)}`);
    }

    throw error;
  }
};

export const registerIpcHandlers = (services: ServiceContainer): void => {
  for (const channel of channels) {
    ipcMain.removeHandler(channel);
  }

  for (const entry of ipcInvokeRegistry) {
    ipcMain.handle(entry.channel, (event, payload) => {
      if (
        event.senderFrame !== event.sender.mainFrame ||
        !isTrustedRendererUrl(
          event.senderFrame?.url ?? "",
          app.getAppPath(),
          process.env.VITE_DEV_SERVER_URL
        )
      ) {
        logger.warn(`[IPC Security] blocked untrusted sender for ${entry.channel}`);
        throw new Error("IPC 调用来源不可信");
      }
      const input = entry.schema
        ? parsePayload(entry.schema, entry.coerceEmptyPayload ? (payload ?? {}) : payload, entry.label)
        : undefined;
      return entry.dispatch(services, input, event);
    });
  }
};
