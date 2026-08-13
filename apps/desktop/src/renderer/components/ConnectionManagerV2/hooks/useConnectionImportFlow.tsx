import { useCallback, useMemo, useState } from "react";
import { App as AntdApp, Input } from "antd";
import { CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX } from "@nextshell/shared";
import { formatErrorMessage } from "../../../utils/errorMessage";
import type { ImportPreviewBatch } from "../types";
import {
  buildNextShellImportPreviewQueue,
  getImportFileName
} from "../utils/nextshellImportPreview";

interface UseConnectionImportFlowOptions {
  modal: ReturnType<typeof AntdApp.useApp>["modal"];
  message: ReturnType<typeof AntdApp.useApp>["message"];
  onConnectionsImported: () => Promise<void>;
}

export const useConnectionImportFlow = ({
  modal,
  message,
  onConnectionsImported
}: UseConnectionImportFlowOptions) => {
  const [importingPreview, setImportingPreview] = useState(false);
  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importPreviewQueue, setImportPreviewQueue] = useState<ImportPreviewBatch[]>([]);
  const [importQueueIndex, setImportQueueIndex] = useState(0);

  const resetImportFlow = useCallback(() => {
    setImportModalOpen(false);
    setImportPreviewQueue([]);
    setImportQueueIndex(0);
  }, []);

  const promptImportDecryptionPassword = useCallback(
    (fileName: string, promptText: string): Promise<string | null> => {
      return new Promise((resolve) => {
        let password = "";
        let settled = false;
        const settle = (value: string | null): void => {
          if (settled) return;
          settled = true;
          resolve(value);
        };

        modal.confirm({
          title: `${fileName} 需要解密密码`,
          okText: "解密",
          cancelText: "跳过该文件",
          content: (
            <div style={{ display: "grid", gap: 8 }}>
              <div style={{ fontSize: 12, color: "var(--t3)" }}>{promptText}</div>
              <Input.Password
                placeholder="请输入导入密码"
                onChange={(event) => {
                  password = event.target.value;
                }}
              />
            </div>
          ),
          onOk: async () => {
            const trimmed = password.trim();
            if (!trimmed) {
              message.warning("请输入解密密码。");
              throw new Error("empty-import-password");
            }
            settle(trimmed);
          },
          onCancel: () => settle(null)
        });
      });
    },
    [message, modal]
  );

  const openImportPreviewQueue = useCallback(
    (queue: ImportPreviewBatch[], emptyMessage: string) => {
      if (queue.length === 0) {
        message.warning(emptyMessage);
        return;
      }

      setImportPreviewQueue(queue);
      setImportQueueIndex(0);
      setImportModalOpen(true);
      if (queue.length > 1) {
        message.info(`已加载 ${queue.length} 个文件，将按文件逐个导入`);
      }
    },
    [message]
  );

  const showImportWarnings = useCallback(
    (warnings: string[]) => {
      const visibleWarnings = warnings.slice(0, 8);
      visibleWarnings.forEach((item) => {
        message.warning(formatErrorMessage(item, "部分文件导入失败"));
      });
      if (warnings.length > visibleWarnings.length) {
        message.warning(`还有 ${warnings.length - visibleWarnings.length} 条导入警告未显示`);
      }
    },
    [message]
  );

  const previewNextShellImportFiles = useCallback(
    async (filePaths: string[]) => {
      const { queue, warnings } = await buildNextShellImportPreviewQueue({
        filePaths,
        importPreview: (payload) => window.nextshell.connection.importPreview(payload),
        promptImportDecryptionPassword
      });

      if (warnings.length > 0) {
        showImportWarnings(warnings);
      }

      openImportPreviewQueue(queue, "未找到可导入的 NextShell 连接文件");
    },
    [openImportPreviewQueue, promptImportDecryptionPassword, showImportWarnings]
  );

  const loadDroppedNextShellImportPreviewQueue = useCallback(
    async (filePaths: string[]) => {
      if (importingPreview) return;
      try {
        setImportingPreview(true);
        await previewNextShellImportFiles(filePaths);
      } catch (error) {
        message.error(`导入预览失败：${formatErrorMessage(error, "请检查文件格式")}`);
      } finally {
        setImportingPreview(false);
      }
    },
    [importingPreview, message, previewNextShellImportFiles]
  );

  const loadImportPreviewQueue = useCallback(
    async (source: "nextshell" | "finalshell") => {
      if (importingPreview) return;
      try {
        setImportingPreview(true);
        const dialogResult = await window.nextshell.dialog.openFiles({
          title: source === "nextshell" ? "选择 NextShell 导入文件" : "选择 FinalShell 配置文件",
          multi: true
        });
        if (dialogResult.canceled || dialogResult.filePaths.length === 0) return;

        if (source === "nextshell") {
          await previewNextShellImportFiles(dialogResult.filePaths);
          return;
        }

        const queue: ImportPreviewBatch[] = [];
        const warnings: string[] = [];
        for (const filePath of dialogResult.filePaths) {
          const normalized = filePath.replace(/\\/g, "/");
          const fileName = normalized.slice(normalized.lastIndexOf("/") + 1);
          try {
            const entries = await window.nextshell.connection.importFinalShellPreview({
              filePath
            });
            if (entries.length === 0) {
              warnings.push(`${fileName}：文件中没有可导入的连接`);
            } else {
              queue.push({ fileName, entries });
            }
          } catch (error) {
            warnings.push(`${fileName}：${formatErrorMessage(error, "导入预览失败")}`);
          }
        }

        if (warnings.length > 0) {
          showImportWarnings(warnings);
        }

        openImportPreviewQueue(queue, "未找到可导入的 FinalShell 连接文件");
      } catch (error) {
        message.error(`导入预览失败：${formatErrorMessage(error, "请检查文件格式")}`);
      } finally {
        setImportingPreview(false);
      }
    },
    [
      importingPreview,
      message,
      openImportPreviewQueue,
      previewNextShellImportFiles,
      showImportWarnings
    ]
  );

  const loadDirectoryImportPreview = useCallback(
    async (source: "nextshell" | "finalshell") => {
      if (importingPreview) return;
      try {
        setImportingPreview(true);
        const dialogResult = await window.nextshell.dialog.openDirectory({
          title: source === "nextshell" ? "选择 NextShell 导入文件夹" : "选择 FinalShell 配置文件夹"
        });
        if (dialogResult.canceled || !dialogResult.filePath) return;

        const directoryName = getImportFileName(dialogResult.filePath) || dialogResult.filePath;
        let decryptionPassword: string | undefined;
        let handled = false;

        while (!handled) {
          try {
            const result = await window.nextshell.connection.importDirectoryPreview({
              directoryPath: dialogResult.filePath,
              source,
              decryptionPassword
            });

            if (result.warnings.length > 0) {
              showImportWarnings(result.warnings);
            }

            if (result.entries.length === 0) {
              message.warning(
                source === "nextshell"
                  ? "未找到可导入的 NextShell 连接文件"
                  : "未找到可导入的 FinalShell 连接文件"
              );
              handled = true;
              continue;
            }

            const sourceLabel = source === "nextshell" ? "NextShell 文件夹" : "FinalShell 文件夹";
            openImportPreviewQueue(
              [
                {
                  fileName: `${sourceLabel}：${directoryName}`,
                  sourcePath: result.directoryPath,
                  sourceKind: "directory",
                  entries: result.entries
                }
              ],
              "未找到可导入的连接文件"
            );
            message.info(`已从 ${result.importedFiles} 个文件加载 ${result.entries.length} 个连接`);
            handled = true;
          } catch (error) {
            const reason = formatErrorMessage(error, "导入预览失败");
            if (
              source === "nextshell" &&
              reason.startsWith(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX)
            ) {
              const promptText =
                reason.slice(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX.length).trim() ||
                "该导入文件夹包含加密文件，请输入密码";
              const inputPassword = await promptImportDecryptionPassword(directoryName, promptText);
              if (!inputPassword) {
                message.warning(`${directoryName}：用户取消解密，已跳过该文件夹`);
                handled = true;
                continue;
              }
              decryptionPassword = inputPassword;
              continue;
            }

            message.error(`导入预览失败：${formatErrorMessage(reason, "请检查文件夹内容")}`);
            handled = true;
          }
        }
      } catch (error) {
        message.error(`导入预览失败：${formatErrorMessage(error, "请检查文件夹内容")}`);
      } finally {
        setImportingPreview(false);
      }
    },
    [
      importingPreview,
      message,
      openImportPreviewQueue,
      promptImportDecryptionPassword,
      showImportWarnings
    ]
  );

  const handleImportNextShell = useCallback(async () => {
    await loadImportPreviewQueue("nextshell");
  }, [loadImportPreviewQueue]);

  const handleImportFinalShell = useCallback(async () => {
    await loadImportPreviewQueue("finalshell");
  }, [loadImportPreviewQueue]);

  const handleImportNextShellDirectory = useCallback(async () => {
    await loadDirectoryImportPreview("nextshell");
  }, [loadDirectoryImportPreview]);

  const handleImportFinalShellDirectory = useCallback(async () => {
    await loadDirectoryImportPreview("finalshell");
  }, [loadDirectoryImportPreview]);

  const handleImportDroppedNextShellFiles = useCallback(
    async (filePaths: string[]) => {
      await loadDroppedNextShellImportPreviewQueue(filePaths);
    },
    [loadDroppedNextShellImportPreviewQueue]
  );

  const handleImportBatchImported = useCallback(async () => {
    await onConnectionsImported();
    const nextIndex = importQueueIndex + 1;
    if (nextIndex < importPreviewQueue.length) {
      setImportQueueIndex(nextIndex);
      const nextBatch = importPreviewQueue[nextIndex];
      message.info(
        `继续导入 ${nextBatch?.fileName ?? "下一个文件"} (${nextIndex + 1}/${importPreviewQueue.length})`
      );
      return;
    }

    resetImportFlow();
  }, [importPreviewQueue, importQueueIndex, message, onConnectionsImported, resetImportFlow]);

  const currentImportBatch = useMemo(
    () => importPreviewQueue[importQueueIndex],
    [importPreviewQueue, importQueueIndex]
  );

  return {
    currentImportBatch,
    handleImportBatchImported,
    handleImportFinalShell,
    handleImportFinalShellDirectory,
    handleImportDroppedNextShellFiles,
    handleImportNextShell,
    handleImportNextShellDirectory,
    importingPreview,
    importModalOpen,
    importPreviewQueue,
    importQueueIndex,
    resetImportFlow
  };
};
