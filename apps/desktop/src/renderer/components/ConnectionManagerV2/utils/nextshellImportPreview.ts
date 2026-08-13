import type { ConnectionImportEntry } from "../../../../../../../packages/core/src/index";
import { CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX } from "../../../../../../../packages/shared/src/index";
import { formatErrorMessage } from "../../../utils/errorMessage";
import type { ImportPreviewBatch } from "../types";

interface BuildNextShellImportPreviewQueueOptions {
  filePaths: string[];
  importPreview: (payload: {
    filePath: string;
    decryptionPassword?: string;
  }) => Promise<ConnectionImportEntry[]>;
  promptImportDecryptionPassword: (fileName: string, promptText: string) => Promise<string | null>;
}

export const getImportFileName = (filePath: string): string => {
  const normalized = filePath.replace(/\\/g, "/");
  const splitIndex = normalized.lastIndexOf("/");
  if (splitIndex < 0) {
    return normalized;
  }
  return normalized.slice(splitIndex + 1);
};

export const buildNextShellImportPreviewQueue = async ({
  filePaths,
  importPreview,
  promptImportDecryptionPassword
}: BuildNextShellImportPreviewQueueOptions): Promise<{
  queue: ImportPreviewBatch[];
  warnings: string[];
}> => {
  const queue: ImportPreviewBatch[] = [];
  const warnings: string[] = [];

  for (const filePath of filePaths) {
    const fileName = getImportFileName(filePath);
    let decryptionPassword: string | undefined;
    let handled = false;

    while (!handled) {
      try {
        const entries = await importPreview({
          filePath,
          decryptionPassword
        });
        if (entries.length === 0) {
          warnings.push(`${fileName}：文件中没有可导入的连接`);
        } else {
          queue.push({ fileName, entries });
        }
        handled = true;
      } catch (error) {
        const reason = formatErrorMessage(error, "导入预览失败");
        if (reason.startsWith(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX)) {
          const promptText =
            reason.slice(CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX.length).trim() ||
            "该导入文件已加密，请输入密码";
          const inputPassword = await promptImportDecryptionPassword(fileName, promptText);
          if (!inputPassword) {
            warnings.push(`${fileName}：用户取消解密，已跳过该文件`);
            handled = true;
            continue;
          }
          decryptionPassword = inputPassword;
          continue;
        }

        warnings.push(`${fileName}：${formatErrorMessage(reason, "导入预览失败")}`);
        handled = true;
      }
    }
  }

  return {
    queue,
    warnings
  };
};
