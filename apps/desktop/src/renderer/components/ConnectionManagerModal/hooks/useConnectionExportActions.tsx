import { useCallback } from "react";
import { App as AntdApp, Input, Radio } from "antd";
import type { ConnectionProfile } from "@nextshell/core";
import { formatErrorMessage } from "../../../utils/errorMessage";

interface UseConnectionExportActionsOptions {
  connections: ConnectionProfile[];
  selectedIds: Set<string>;
  modal: ReturnType<typeof AntdApp.useApp>["modal"];
  message: ReturnType<typeof AntdApp.useApp>["message"];
}

export const useConnectionExportActions = ({
  connections,
  selectedIds,
  modal,
  message
}: UseConnectionExportActionsOptions) => {
  const getCachedMasterPassword = useCallback(async (): Promise<string> => {
    try {
      const result = await window.nextshell.masterPassword.getCached();
      return result.password ?? "";
    } catch {
      return "";
    }
  }, []);

  const promptExportMode = useCallback((): Promise<"plain" | "encrypted" | null> => {
    return new Promise((resolve) => {
      let mode: "plain" | "encrypted" = "plain";
      let settled = false;
      const settle = (value: "plain" | "encrypted" | null): void => {
        if (settled) return;
        settled = true;
        resolve(value);
      };

      modal.confirm({
        title: "导出选项",
        okText: "继续",
        cancelText: "取消",
        content: (
          <Radio.Group
            defaultValue="plain"
            onChange={(event) => {
              mode = event.target.value;
            }}
          >
            <Radio value="plain">普通导出（JSON）</Radio>
            <Radio value="encrypted">加密导出（AES + b64##）</Radio>
          </Radio.Group>
        ),
        onOk: () => settle(mode),
        onCancel: () => settle(null)
      });
    });
  }, [modal]);

  const promptExportEncryptionPassword = useCallback((defaultPassword?: string): Promise<string | null> => {
    return new Promise((resolve) => {
      let password = defaultPassword ?? "";
      let confirmPassword = defaultPassword ?? "";
      let settled = false;
      const settle = (value: string | null): void => {
        if (settled) return;
        settled = true;
        resolve(value);
      };

      modal.confirm({
        title: "输入导出加密密码",
        okText: "确认",
        cancelText: "取消",
        content: (
          <div style={{ display: "grid", gap: 8 }}>
            {defaultPassword ? (
              <div style={{ fontSize: 12, color: "var(--t3)" }}>
                已自动填充主密码，可按需修改。
              </div>
            ) : null}
            <Input.Password
              placeholder="请输入密码（至少 6 位）"
              defaultValue={defaultPassword}
              onChange={(event) => {
                password = event.target.value;
              }}
            />
            <Input.Password
              placeholder="请再次输入密码"
              defaultValue={defaultPassword}
              onChange={(event) => {
                confirmPassword = event.target.value;
              }}
            />
          </div>
        ),
        onOk: async () => {
          const trimmedPassword = password.trim();
          const trimmedConfirm = confirmPassword.trim();
          if (trimmedPassword.length < 6) {
            message.warning("导出加密密码至少需要 6 个字符。");
            throw new Error("invalid-export-password-length");
          }
          if (trimmedPassword !== trimmedConfirm) {
            message.warning("两次输入的密码不一致。");
            throw new Error("invalid-export-password-confirm");
          }
          settle(trimmedPassword);
        },
        onCancel: () => settle(null)
      });
    });
  }, [message, modal]);

  const runSingleExport = useCallback(
    async (exportIds: string[]): Promise<void> => {
      if (exportIds.length === 0) return;

      const mode = await promptExportMode();
      if (!mode) return;

      let encryptionPassword: string | undefined;
      if (mode === "encrypted") {
        const defaultPassword = await getCachedMasterPassword();
        const password = await promptExportEncryptionPassword(defaultPassword);
        if (!password) return;
        encryptionPassword = password;
      }

      try {
        const result = await window.nextshell.connection.exportToFile({
          connectionIds: exportIds,
          encryptionPassword
        });
        if (result.ok) {
          message.success(
            mode === "encrypted"
              ? `已加密导出 ${exportIds.length} 个连接`
              : `已导出 ${exportIds.length} 个连接`
          );
        }
      } catch (error) {
        message.error(`导出失败：${formatErrorMessage(error, "请稍后重试")}`);
      }
    },
    [getCachedMasterPassword, message, promptExportEncryptionPassword, promptExportMode]
  );

  const handleExportAll = useCallback(async () => {
    if (connections.length === 0) return;
    await runSingleExport(connections.map((connection) => connection.id));
  }, [connections, runSingleExport]);

  const handleExportSelected = useCallback(async () => {
    if (selectedIds.size === 0) return;
    const exportIds = connections
      .map((connection) => connection.id)
      .filter((id) => selectedIds.has(id));
    if (exportIds.length === 0) return;

    const mode = await promptExportMode();
    if (!mode) return;

    let encryptionPassword: string | undefined;
    if (mode === "encrypted") {
      const defaultPassword = await getCachedMasterPassword();
      const password = await promptExportEncryptionPassword(defaultPassword);
      if (!password) return;
      encryptionPassword = password;
    }

    const directory = await window.nextshell.dialog.openDirectory({
      title: "选择导出目录"
    });
    if (directory.canceled || !directory.filePath) {
      return;
    }

    try {
      const result = await window.nextshell.connection.exportBatch({
        connectionIds: exportIds,
        directoryPath: directory.filePath,
        encryptionPassword
      });

      if (result.failed === 0) {
        message.success(
          mode === "encrypted"
            ? `已加密导出 ${result.exported} 个连接到目录：${result.directoryPath}`
            : `已导出 ${result.exported} 个连接到目录：${result.directoryPath}`
        );
        return;
      }

      if (result.exported > 0) {
        message.warning(`已导出 ${result.exported}/${result.total}，失败 ${result.failed}`);
      } else {
        message.error(`导出失败：共 ${result.failed} 个连接导出失败`);
      }

      const maxWarnings = 5;
      result.errors.slice(0, maxWarnings).forEach((errorText) => {
        message.warning(formatErrorMessage(errorText, "导出失败"));
      });
      if (result.errors.length > maxWarnings) {
        message.warning(`其余 ${result.errors.length - maxWarnings} 项导出失败`);
      }
    } catch (error) {
      message.error(`导出失败：${formatErrorMessage(error, "请稍后重试")}`);
    }
  }, [
    connections,
    getCachedMasterPassword,
    message,
    promptExportEncryptionPassword,
    promptExportMode,
    selectedIds
  ]);

  return {
    handleExportAll,
    handleExportSelected
  };
};
