export const AUTH_REQUIRED_PREFIX = "AUTH_REQUIRED::";
export const CONNECTION_IMPORT_DECRYPT_PROMPT_PREFIX = "CONNECTION_IMPORT_DECRYPT_PROMPT::";
export const SUPPORTED_BACKGROUND_IMAGE_EXTENSIONS = ["png", "jpg", "jpeg", "gif", "bmp", "webp"];

// ─── Group path ───────────────────────────────────────────────────────────────

/**
 * `groupPath` 的规范化。zone 分类法(`server` / `workspace` / `import` 三个硬编码顶层区)已随
 * 目录实体化删除:隔离域由 `originScopeKey` 承担,用户目录由 `connection_folders` 承担,
 * `groupPath` 只剩下"给云同步线协议、MCP 工具 schema 与导出文件读的派生投影"这一个身份
 * (投影函数见 `folder-path.ts`)。
 *
 * 这里只做纯字符串清理,不再改写任何人的路径语义。
 */
export const normalizeGroupPath = (value: string | undefined): string => {
  if (!value) return "/";
  let path = value.trim().replace(/\\/g, "/");
  if (!path.startsWith("/")) path = `/${path}`;
  path = path.replace(/\/+/g, "/");
  if (path.length > 1 && path.endsWith("/")) path = path.slice(0, -1);
  return path || "/";
};
