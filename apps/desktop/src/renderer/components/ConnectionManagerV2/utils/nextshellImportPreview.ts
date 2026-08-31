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

/**
 * 这一批 entry 的 `groupPath` 该按哪种格式解释,决定主进程物化目录链前剥不剥前缀:
 * - 目录扫描导入(`sourceKind === "directory"`)的路径是磁盘上的相对目录,**一段都不能剥**——
 *   用户把顶层文件夹取名叫 `server` 完全合法,按线格式剥掉会静默吞掉一整层目录;
 * - 其余来源(导出文件、云快照、FinalShell 的 `/import/finalshell`)都是线格式,前缀必须剥。
 *
 * 省略这个字段主进程按 `wire` 走,也就是现行为;所以只有目录那条路径需要显式改口。
 */
export const resolveImportGroupPathFormat = (
  sourceKind: ImportPreviewBatch["sourceKind"]
): "wire" | "literal" => (sourceKind === "directory" ? "literal" : "wire");

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
