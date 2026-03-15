import { describe, expect, test } from "bun:test";
import type {
  CloudSyncPendingOp,
  CloudSyncResourceStateV2,
  CloudSyncWorkspaceProfile,
  ConnectionProfile,
  RecycleBinEntry,
  SshKeyProfile,
} from "@nextshell/core";
import { CloudSyncManager, type CloudSyncManagerDeps } from "./cloud-sync-manager";

const createWorkspace = (): CloudSyncWorkspaceProfile => ({
  id: "ws-1",
  apiBaseUrl: "https://sync.example.com/",
  workspaceName: "prod-team",
  displayName: "生产环境",
  pullIntervalSec: 120,
  ignoreTlsErrors: true,
  enabled: false,
  createdAt: "2026-03-15T00:00:00.000Z",
  updatedAt: "2026-03-15T00:00:00.000Z",
  lastSyncAt: null,
  lastError: null,
});

const createDeps = (
  workspace: CloudSyncWorkspaceProfile,
  password: string | undefined,
): CloudSyncManagerDeps => ({
  listConnections: (): ConnectionProfile[] => [],
  saveConnection: (_conn): void => undefined,
  removeConnection: (_id): void => undefined,
  listSshKeys: (): SshKeyProfile[] => [],
  saveSshKey: (_key): void => undefined,
  removeSshKey: (_id): void => undefined,
  readCredential: async (_ref): Promise<string | undefined> => undefined,
  listWorkspaces: (): CloudSyncWorkspaceProfile[] => [workspace],
  saveWorkspace: (_ws): void => undefined,
  removeWorkspace: (_id): void => undefined,
  listPendingOps: (_workspaceId): CloudSyncPendingOp[] => [],
  savePendingOp: (_op): number => 1,
  upsertPendingOp: (_op): number => 1,
  updatePendingOp: (_op): void => undefined,
  removePendingOp: (_id): void => undefined,
  clearPendingOps: (_workspaceId): void => undefined,
  listResourceStates: (_workspaceId): CloudSyncResourceStateV2[] => [],
  getResourceState: (_workspaceId, _resourceType, _resourceId): CloudSyncResourceStateV2 | undefined => undefined,
  saveResourceState: (_state): void => undefined,
  removeResourceState: (_workspaceId, _resourceType, _resourceId): void => undefined,
  clearResourceStates: (_workspaceId): void => undefined,
  saveRecycleBinEntry: (_entry: RecycleBinEntry): void => undefined,
  listRecycleBinEntries: (): RecycleBinEntry[] => [],
  removeRecycleBinEntry: (_id): void => undefined,
  storeWorkspacePassword: async (_workspaceId, _nextPassword): Promise<void> => undefined,
  getWorkspacePassword: async (_workspaceId): Promise<string | undefined> => password,
  deleteWorkspacePassword: async (_workspaceId): Promise<void> => undefined,
  getRuntimeCurrentVersion: (_workspaceId): number | null => null,
  saveRuntimeCurrentVersion: (_workspaceId, _currentVersion): void => undefined,
  broadcastStatus: (_status): void => undefined,
  broadcastApplied: (_workspaceId): void => undefined,
});

describe("CloudSyncManager workspace token", () => {
  test("exports a v1 token and parses it back into a workspace draft", async () => {
    const workspace = createWorkspace();
    const manager = new CloudSyncManager(createDeps(workspace, "super-secret"));

    expect(typeof (manager as unknown as { exportWorkspaceToken?: unknown }).exportWorkspaceToken).toBe("function");
    expect(typeof (manager as unknown as { parseWorkspaceToken?: unknown }).parseWorkspaceToken).toBe("function");

    const { token } = await (manager as unknown as {
      exportWorkspaceToken: (workspaceId: string) => Promise<{ token: string }>;
    }).exportWorkspaceToken(workspace.id);

    expect(token.startsWith("nshell-csv1:")).toBe(true);

    const draft = await (manager as unknown as {
      parseWorkspaceToken: (token: string) => {
        apiBaseUrl: string;
        workspaceName: string;
        displayName: string;
        workspacePassword: string;
        pullIntervalSec: number;
        ignoreTlsErrors: boolean;
        enabled: boolean;
      };
    }).parseWorkspaceToken(token);

    expect(draft).toEqual({
      apiBaseUrl: "https://sync.example.com",
      workspaceName: workspace.workspaceName,
      displayName: workspace.displayName,
      workspacePassword: "super-secret",
      pullIntervalSec: workspace.pullIntervalSec,
      ignoreTlsErrors: workspace.ignoreTlsErrors,
      enabled: workspace.enabled,
    });
  });

  test("rejects tokens without the nshell-csv1 prefix", async () => {
    const manager = new CloudSyncManager(createDeps(createWorkspace(), "super-secret"));

    expect(typeof (manager as unknown as { parseWorkspaceToken?: unknown }).parseWorkspaceToken).toBe("function");

    await expect(
      (manager as unknown as {
        parseWorkspaceToken: (token: string) => Promise<unknown>;
      }).parseWorkspaceToken("token=abc"),
    ).rejects.toThrow("无效的云同步工作区 token");
  });

  test("rejects malformed token payloads", async () => {
    const manager = new CloudSyncManager(createDeps(createWorkspace(), "super-secret"));

    expect(typeof (manager as unknown as { parseWorkspaceToken?: unknown }).parseWorkspaceToken).toBe("function");

    const invalidJson = `nshell-csv1:${Buffer.from("{bad json", "utf8").toString("base64")}`;
    const missingPassword = `nshell-csv1:${Buffer.from(JSON.stringify({
      apiBaseUrl: "https://sync.example.com/",
      workspaceName: "prod-team",
      displayName: "生产环境",
      pullIntervalSec: 120,
      ignoreTlsErrors: false,
      enabled: true,
    }), "utf8").toString("base64")}`;

    await expect(
      (manager as unknown as {
        parseWorkspaceToken: (token: string) => Promise<unknown>;
      }).parseWorkspaceToken(invalidJson),
    ).rejects.toThrow("无效的云同步工作区 token");

    await expect(
      (manager as unknown as {
        parseWorkspaceToken: (token: string) => Promise<unknown>;
      }).parseWorkspaceToken(missingPassword),
    ).rejects.toThrow("无效的云同步工作区 token");
  });

  test("fails export when the workspace password is unavailable", async () => {
    const workspace = createWorkspace();
    const manager = new CloudSyncManager(createDeps(workspace, undefined));

    expect(typeof (manager as unknown as { exportWorkspaceToken?: unknown }).exportWorkspaceToken).toBe("function");

    await expect(
      (manager as unknown as {
        exportWorkspaceToken: (workspaceId: string) => Promise<{ token: string }>;
      }).exportWorkspaceToken(workspace.id),
    ).rejects.toThrow("该工作区缺少可导出的完整配置");
  });
});
