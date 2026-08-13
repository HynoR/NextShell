import { LOCAL_DEFAULT_SCOPE_KEY, buildScopeKey } from "@nextshell/core";
import type { CloudSyncWorkspaceProfile } from "@nextshell/core";

/**
 * 一个作用域 = 一个隔离域。切换器一次只让用户看一个,所以密钥/代理下拉在结构上就不可能
 * 出现别的域的资源——旧管理器"下拉里能选、保存却报跨来源"的分叉在这里不存在。
 */
export interface ManagerScope {
  key: string;
  label: string;
  kind: "local" | "cloud";
  /** 云作用域指向 cloud_sync_workspaces.id。 */
  workspaceId?: string;
  workspaceName?: string;
}

export const LOCAL_SCOPE: ManagerScope = {
  key: LOCAL_DEFAULT_SCOPE_KEY,
  label: "本地",
  kind: "local"
};

export const buildManagerScopes = (
  workspaces: readonly CloudSyncWorkspaceProfile[]
): ManagerScope[] => [
  LOCAL_SCOPE,
  ...workspaces.map((workspace) => ({
    key: buildScopeKey({
      kind: "cloud" as const,
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName
    }),
    label: workspace.displayName || workspace.workspaceName,
    kind: "cloud" as const,
    workspaceId: workspace.id,
    workspaceName: workspace.workspaceName
  }))
];

/**
 * 选中的作用域可能因为 workspace 被移除而消失(退订、删除、令牌失效)。这时必须落回本地而不是
 * 显示一个空壳,否则用户会以为连接丢了。
 */
export const resolveActiveScope = (
  scopes: readonly ManagerScope[],
  selectedKey: string | undefined
): ManagerScope => scopes.find((scope) => scope.key === selectedKey) ?? scopes[0] ?? LOCAL_SCOPE;
