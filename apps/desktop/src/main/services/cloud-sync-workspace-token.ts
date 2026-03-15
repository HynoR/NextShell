import {
  cloudSyncWorkspaceTokenDraftSchema,
  type CloudSyncWorkspaceTokenDraft,
} from "@nextshell/shared";

const CLOUD_SYNC_WORKSPACE_TOKEN_PREFIX = "nshell-csv1:";
const INVALID_TOKEN_ERROR = "无效的云同步工作区 token";

const normalizeDraft = (draft: CloudSyncWorkspaceTokenDraft): CloudSyncWorkspaceTokenDraft => {
  return cloudSyncWorkspaceTokenDraftSchema.parse(draft);
};

export const encodeCloudSyncWorkspaceToken = (draft: CloudSyncWorkspaceTokenDraft): string => {
  const normalized = normalizeDraft(draft);
  const json = JSON.stringify({
    apiBaseUrl: normalized.apiBaseUrl,
    workspaceName: normalized.workspaceName,
    displayName: normalized.displayName,
    workspacePassword: normalized.workspacePassword,
    pullIntervalSec: normalized.pullIntervalSec,
    ignoreTlsErrors: normalized.ignoreTlsErrors,
    enabled: normalized.enabled,
  });
  const payload = Buffer.from(json, "utf8").toString("base64");
  return `${CLOUD_SYNC_WORKSPACE_TOKEN_PREFIX}${payload}`;
};

export const parseCloudSyncWorkspaceToken = (token: string): CloudSyncWorkspaceTokenDraft => {
  try {
    if (!token.startsWith(CLOUD_SYNC_WORKSPACE_TOKEN_PREFIX)) {
      throw new Error(INVALID_TOKEN_ERROR);
    }

    const encoded = token.slice(CLOUD_SYNC_WORKSPACE_TOKEN_PREFIX.length);
    if (!encoded) {
      throw new Error(INVALID_TOKEN_ERROR);
    }

    const json = Buffer.from(encoded, "base64").toString("utf8");
    const parsed = JSON.parse(json) as CloudSyncWorkspaceTokenDraft;
    return normalizeDraft(parsed);
  } catch {
    throw new Error(INVALID_TOKEN_ERROR);
  }
};
