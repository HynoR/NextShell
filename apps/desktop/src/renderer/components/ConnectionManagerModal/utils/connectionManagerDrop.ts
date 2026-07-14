import type { ManagerTab } from "../types";

export const canAcceptConnectionManagerExternalDrop = (input: {
  open: boolean;
  activeTab: ManagerTab;
  importingPreview: boolean;
}): boolean => input.open && input.activeTab === "connections" && !input.importingPreview;

export const getConnectionManagerDropPathWarning = (input: { allPathsEmpty: boolean }): string =>
  input.allPathsEmpty ? "无法读取拖入文件的路径，请尝试使用导入按钮选择文件" : "当前仅支持拖入文件";
