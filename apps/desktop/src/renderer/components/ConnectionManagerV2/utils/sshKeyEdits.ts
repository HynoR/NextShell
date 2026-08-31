import type { SshKeyProfile } from "@nextshell/core";
import type { SshKeyUpsertInput } from "@nextshell/shared";

/**
 * 密钥详情区的两种编辑动作,都落在同一个 `sshKey.upsert` 通道上。差别全在 payload 里带不带
 * `keyContent`——契约(`sshKeyUpsertSchema`)把它标成可选,主进程 `upsertSshKey` 只有在拿到
 * 内容时才重新解析并换掉 vault 里的私钥,否则沿用旧的 keyContentRef 与解析出的类型/指纹。
 * 所以"改名不动内容"不需要回传原文,也不需要改契约。
 */

/** 改名。名字没变或被清空时返回 undefined——不发一次没有意义的写。 */
export const planSshKeyRename = (input: {
  key: Pick<SshKeyProfile, "id" | "name">;
  name: string;
  workspaceId?: string;
}): SshKeyUpsertInput | undefined => {
  const name = input.name.trim();
  if (!name || name === input.key.name) {
    return undefined;
  }
  // 不带 keyContent 也不带 passphrase：主进程见到 passphrase !== undefined 会把已存的口令删掉，
  // 改个名字不该有这种副作用。
  return { id: input.key.id, name, workspaceId: input.workspaceId };
};

/** 换私钥内容。内容留空 = 不更新,返回 undefined。 */
export const planSshKeyReplace = (input: {
  key: Pick<SshKeyProfile, "id" | "name">;
  keyContent: string | undefined;
  passphrase?: string;
  workspaceId?: string;
}): SshKeyUpsertInput | undefined => {
  const keyContent = (input.keyContent ?? "").trim();
  if (!keyContent) {
    return undefined;
  }
  const passphrase = (input.passphrase ?? "").trim();
  return {
    id: input.key.id,
    name: input.key.name,
    keyContent,
    // 同上：留空要传 undefined 而不是空串，否则等于"把口令清掉"。
    passphrase: passphrase || undefined,
    workspaceId: input.workspaceId
  };
};

/** 私钥文件名 → 默认密钥名。`id_rsa.pem` → `id_rsa`;没有扩展名就原样用。 */
export const suggestKeyNameFromFile = (fileName: string): string => {
  const base = fileName.split(/[\\/]/).pop() ?? fileName;
  return base.replace(/\.[^.]+$/, "") || base;
};

/**
 * 读私钥文件。渲染进程在 sandbox 下拿不到 `File.path`,只能把文本读出来贴进表单。
 * 用 FileReader 而不是 `File.text()`:前者是第一包已经在跑的路径,收尾包不换实现。
 */
export const readPrivateKeyFile = (file: File): Promise<string> =>
  new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = (loaded) => {
      const text = loaded.target?.result;
      if (typeof text !== "string") {
        reject(new Error("读取文件失败，请重试"));
        return;
      }
      resolve(text);
    };
    reader.onerror = () => reject(new Error("读取文件失败，请重试"));
    reader.readAsText(file);
  });
