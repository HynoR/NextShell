/**
 * 会话状态枚举 → 中文展示文案的集中映射。
 * 所有面向用户的会话状态文本(侧栏、头部、标题)都应经此函数,
 * 避免在界面里直接渲染原始英文枚举(connected/disconnected/...)。
 *
 * 注意:CSS class 仍使用原始枚举值(connected/disconnected 等)以保留配色,
 * 本函数只负责文本内容的本地化。
 */

import type { SessionStatus } from "@nextshell/core";

type SessionStatusKey = SessionStatus | "no-session";

const STATUS_LABEL_MAP: Record<SessionStatusKey, string> = {
  connected: "已连接",
  connecting: "连接中",
  disconnected: "已断开",
  failed: "连接失败",
  "no-session": "未选择会话"
};

/**
 * 把会话状态枚举值转换为面向用户的中文标签。
 *
 * @param status 会话状态枚举(connected/connecting/disconnected/failed)或 "no-session"
 * @returns 中文状态文案;遇到未知值回退为「未选择会话」
 */
export function sessionStatusLabel(status: SessionStatusKey): string {
  return STATUS_LABEL_MAP[status] ?? STATUS_LABEL_MAP["no-session"];
}
