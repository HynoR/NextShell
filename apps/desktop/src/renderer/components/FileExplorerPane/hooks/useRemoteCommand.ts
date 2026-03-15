import { useCallback } from "react";
import { App as AntdApp } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";
import { isPermissionDenied } from "../shared";

type AppMessage = ReturnType<typeof AntdApp.useApp>["message"];

interface UseRemoteCommandParams {
  connection?: ConnectionProfile;
  message: AppMessage;
}

export const useRemoteCommand = ({ connection, message }: UseRemoteCommandParams) =>
  useCallback(
    async (command: string): Promise<{ ok: boolean; stderr: string }> => {
      if (!connection) return { ok: false, stderr: "no connection" };
      try {
        const result = await window.nextshell.command.exec({
          connectionId: connection.id,
          command
        });
        if (result.exitCode !== 0) {
          if (isPermissionDenied(result.stderr)) {
            message.error(`权限不足，无法执行操作：${result.stderr.trim()}`);
          } else {
            message.error(
              `命令执行失败（exit ${result.exitCode}）：${result.stderr.trim() || result.stdout.trim()}`
            );
          }
          return { ok: false, stderr: result.stderr };
        }
        return { ok: true, stderr: "" };
      } catch (error) {
        const reason = formatErrorMessage(error, "远端命令执行失败");
        message.error(reason);
        return { ok: false, stderr: reason };
      }
    },
    [connection, message]
  );
