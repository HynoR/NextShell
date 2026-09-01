import { describe, expect, test } from "vitest";
import { buildScopeKey } from "@nextshell/core";
import type {
  CloudSyncWorkspaceProfile,
  ConnectionProfile,
  ProxyProfile,
  RecycleBinEntry,
  SshKeyProfile,
  WorkspaceCommandItem,
  WorkspaceRepoLocalState,
  WorkspaceRepoSnapshot
} from "@nextshell/core";
import { CloudSyncManager, type CloudSyncManagerDeps } from "./cloud-sync-manager";

type WorkspaceSyncResult = WorkspaceRepoLocalState & {
  syncState: "synced" | "diverged";
};

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
  lastError: null
});

const createDeps = (
  workspace: CloudSyncWorkspaceProfile,
  password: string | undefined
): CloudSyncManagerDeps => ({
  listConnections: (): ConnectionProfile[] => [],
  saveConnection: (_conn): void => undefined,
  removeConnection: (_id): void => undefined,
  listSshKeys: (): SshKeyProfile[] => [],
  saveSshKey: (_key): void => undefined,
  removeSshKey: (_id): void => undefined,
  listProxies: (): ProxyProfile[] => [],
  saveProxy: (_proxy): void => undefined,
  removeProxy: (_id): void => undefined,
  readCredential: async (_ref): Promise<string | undefined> => undefined,
  storeCredential: async (_name, _secret): Promise<string> => "secret://test",
  deleteCredential: async (_ref): Promise<void> => undefined,
  listWorkspaces: (): CloudSyncWorkspaceProfile[] => [workspace],
  saveWorkspace: (_ws): void => undefined,
  removeWorkspace: (_id): void => undefined,
  getWorkspaceRepoLocalState: (_workspaceId: string): WorkspaceRepoLocalState | undefined =>
    undefined,
  saveWorkspaceRepoLocalState: (_state: WorkspaceRepoLocalState): void => undefined,
  listWorkspaceCommands: (_workspaceId: string): WorkspaceCommandItem[] => [],
  replaceWorkspaceCommands: (_workspaceId: string, _commands: WorkspaceCommandItem[]): void =>
    undefined,
  saveRecycleBinEntry: (_entry: RecycleBinEntry): void => undefined,
  storeWorkspacePassword: async (_workspaceId, _nextPassword): Promise<void> => undefined,
  getWorkspacePassword: async (_workspaceId): Promise<string | undefined> => password,
  deleteWorkspacePassword: async (_workspaceId): Promise<void> => undefined,
  getJsonSetting: <T>(_key: string): T | undefined => undefined,
  saveJsonSetting: (_key: string, _value: unknown): void => undefined,
  broadcastStatus: (_status): void => undefined,
  broadcastApplied: (_workspaceId): void => undefined
});

const now = "2026-03-15T00:00:00.000Z";

const snapshotConnection = (
  uuid: string,
  name: string,
  host: string
): WorkspaceRepoSnapshot["connections"][number] => ({
  uuid,
  name,
  host,
  port: 22,
  username: "root",
  authType: "agent",
  strictHostKeyChecking: false,
  terminalEncoding: "utf-8",
  backspaceMode: "ascii-backspace",
  deleteMode: "vt220-delete",
  groupPath: "/workspace/prod-team",
  tags: [],
  favorite: false,
  createdAt: now,
  updatedAt: now
});

const repoSnapshot = (
  workspaceId: string,
  snapshotId: string,
  connections: WorkspaceRepoSnapshot["connections"][number][]
): WorkspaceRepoSnapshot => ({
  workspaceId,
  snapshotId,
  createdAt: now,
  connections,
  sshKeys: [],
  proxies: []
});

interface MutableCloudSyncState {
  workspace: CloudSyncWorkspaceProfile;
  password?: string;
  connections: ConnectionProfile[];
  sshKeys: SshKeyProfile[];
  proxies: ProxyProfile[];
  commands: WorkspaceCommandItem[];
  localState?: WorkspaceRepoLocalState;
  recycleBin: RecycleBinEntry[];
  credentials: Map<string, string>;
}

const createMutableState = (
  workspace: CloudSyncWorkspaceProfile,
  overrides: Partial<MutableCloudSyncState> = {}
): MutableCloudSyncState => ({
  workspace,
  password: "workspace-password",
  connections: [],
  sshKeys: [],
  proxies: [],
  commands: [],
  recycleBin: [],
  credentials: new Map(),
  ...overrides
});

const createMutableDeps = (state: MutableCloudSyncState): CloudSyncManagerDeps => ({
  listConnections: (): ConnectionProfile[] => state.connections,
  saveConnection: (conn): void => {
    state.connections = [...state.connections.filter((item) => item.id !== conn.id), conn];
  },
  removeConnection: (id): void => {
    state.connections = state.connections.filter((item) => item.id !== id);
  },
  listSshKeys: (): SshKeyProfile[] => state.sshKeys,
  saveSshKey: (key): void => {
    state.sshKeys = [...state.sshKeys.filter((item) => item.id !== key.id), key];
  },
  removeSshKey: (id): void => {
    state.sshKeys = state.sshKeys.filter((item) => item.id !== id);
  },
  listProxies: (): ProxyProfile[] => state.proxies,
  saveProxy: (proxy): void => {
    state.proxies = [...state.proxies.filter((item) => item.id !== proxy.id), proxy];
  },
  removeProxy: (id): void => {
    state.proxies = state.proxies.filter((item) => item.id !== id);
  },
  readCredential: async (ref): Promise<string | undefined> => state.credentials.get(ref),
  storeCredential: async (name, secret): Promise<string> => {
    const ref = `secret://${name}`;
    state.credentials.set(ref, secret);
    return ref;
  },
  deleteCredential: async (ref): Promise<void> => {
    state.credentials.delete(ref);
  },
  listWorkspaces: (): CloudSyncWorkspaceProfile[] => [state.workspace],
  saveWorkspace: (ws): void => {
    state.workspace = ws;
  },
  removeWorkspace: (_id): void => undefined,
  getWorkspaceRepoLocalState: (_workspaceId: string): WorkspaceRepoLocalState | undefined =>
    state.localState,
  saveWorkspaceRepoLocalState: (nextState): void => {
    state.localState = nextState;
  },
  listWorkspaceCommands: (_workspaceId: string): WorkspaceCommandItem[] => state.commands,
  replaceWorkspaceCommands: (_workspaceId: string, commands: WorkspaceCommandItem[]): void => {
    state.commands = commands;
  },
  saveRecycleBinEntry: (entry): void => {
    state.recycleBin.push(entry);
  },
  storeWorkspacePassword: async (_workspaceId, password): Promise<void> => {
    state.password = password;
  },
  getWorkspacePassword: async (_workspaceId): Promise<string | undefined> => state.password,
  deleteWorkspacePassword: async (_workspaceId): Promise<void> => {
    state.password = undefined;
  },
  getJsonSetting: <T>(_key: string): T | undefined => undefined,
  saveJsonSetting: (_key: string, _value: unknown): void => undefined,
  broadcastStatus: (_status): void => undefined,
  broadcastApplied: (_workspaceId): void => undefined
});

const testCredentials = (workspace: CloudSyncWorkspaceProfile) => ({
  apiBaseUrl: workspace.apiBaseUrl,
  workspaceName: workspace.workspaceName,
  workspacePassword: "workspace-password",
  ignoreTlsErrors: false,
  clientId: "test-client",
  clientVersion: "test"
});

describe("CloudSyncManager workspace token", () => {
  test("exports a v1 token and parses it back into a workspace draft", async () => {
    const workspace = createWorkspace();
    const manager = new CloudSyncManager(createDeps(workspace, "super-secret"));

    expect(
      typeof (manager as unknown as { exportWorkspaceToken?: unknown }).exportWorkspaceToken
    ).toBe("function");
    expect(
      typeof (manager as unknown as { parseWorkspaceToken?: unknown }).parseWorkspaceToken
    ).toBe("function");

    const { token } = await (
      manager as unknown as {
        exportWorkspaceToken: (workspaceId: string) => Promise<{ token: string }>;
      }
    ).exportWorkspaceToken(workspace.id);

    expect(token.startsWith("nshell-csv1:")).toBe(true);

    const draft = await (
      manager as unknown as {
        parseWorkspaceToken: (token: string) => {
          apiBaseUrl: string;
          workspaceName: string;
          displayName: string;
          workspacePassword: string;
          pullIntervalSec: number;
          ignoreTlsErrors: boolean;
          enabled: boolean;
        };
      }
    ).parseWorkspaceToken(token);

    expect(draft).toEqual({
      apiBaseUrl: "https://sync.example.com",
      workspaceName: workspace.workspaceName,
      displayName: workspace.displayName,
      workspacePassword: "super-secret",
      pullIntervalSec: workspace.pullIntervalSec,
      ignoreTlsErrors: workspace.ignoreTlsErrors,
      enabled: workspace.enabled
    });
  });

  test("rejects tokens without the nshell-csv1 prefix", async () => {
    const manager = new CloudSyncManager(createDeps(createWorkspace(), "super-secret"));

    expect(
      typeof (manager as unknown as { parseWorkspaceToken?: unknown }).parseWorkspaceToken
    ).toBe("function");

    await expect(
      (
        manager as unknown as {
          parseWorkspaceToken: (token: string) => Promise<unknown>;
        }
      ).parseWorkspaceToken("token=abc")
    ).rejects.toThrow("无效的云同步工作区 token");
  });

  test("rejects malformed token payloads", async () => {
    const manager = new CloudSyncManager(createDeps(createWorkspace(), "super-secret"));

    expect(
      typeof (manager as unknown as { parseWorkspaceToken?: unknown }).parseWorkspaceToken
    ).toBe("function");

    const invalidJson = `nshell-csv1:${Buffer.from("{bad json", "utf8").toString("base64")}`;
    const missingPassword = `nshell-csv1:${Buffer.from(
      JSON.stringify({
        apiBaseUrl: "https://sync.example.com/",
        workspaceName: "prod-team",
        displayName: "生产环境",
        pullIntervalSec: 120,
        ignoreTlsErrors: false,
        enabled: true
      }),
      "utf8"
    ).toString("base64")}`;

    await expect(
      (
        manager as unknown as {
          parseWorkspaceToken: (token: string) => Promise<unknown>;
        }
      ).parseWorkspaceToken(invalidJson)
    ).rejects.toThrow("无效的云同步工作区 token");

    await expect(
      (
        manager as unknown as {
          parseWorkspaceToken: (token: string) => Promise<unknown>;
        }
      ).parseWorkspaceToken(missingPassword)
    ).rejects.toThrow("无效的云同步工作区 token");
  });

  test("fails export when the workspace password is unavailable", async () => {
    const workspace = createWorkspace();
    const manager = new CloudSyncManager(createDeps(workspace, undefined));

    expect(
      typeof (manager as unknown as { exportWorkspaceToken?: unknown }).exportWorkspaceToken
    ).toBe("function");

    await expect(
      (
        manager as unknown as {
          exportWorkspaceToken: (workspaceId: string) => Promise<{ token: string }>;
        }
      ).exportWorkspaceToken(workspace.id)
    ).rejects.toThrow("该工作区缺少可导出的完整配置");
  });
});

describe("CloudSyncManager workspace repo sync", () => {
  test("pulls when only the remote head changed", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const remote = repoSnapshot(workspace.id, "remote-snapshot", [
      snapshotConnection("remote-conn", "Remote", "remote.example.com")
    ]);
    const localState: WorkspaceRepoLocalState = {
      workspaceId: workspace.id,
      remoteVersion: "base-version",
      localFingerprint: "empty-local"
    };
    const state = createMutableState(workspace, { localState });

    const manager = new CloudSyncManager(createMutableDeps(state));
    localState.localFingerprint = (
      manager as unknown as { workspaceFingerprint: (workspaceId: string) => string }
    ).workspaceFingerprint(workspace.id);
    let pulledKnownVersion: string | null | undefined;
    (manager as unknown as { api: unknown }).api = {
      pull: async (_credentials: unknown, knownVersion?: string | null) => {
        pulledKnownVersion = knownVersion;
        return { unchanged: false, headCommitId: "remote-version", snapshot: remote };
      }
    };

    const result = await (
      manager as unknown as {
        syncWorkspaceRepo: (
          workspace: CloudSyncWorkspaceProfile,
          credentials: ReturnType<typeof testCredentials>,
          localState: WorkspaceRepoLocalState,
          remoteVersion?: string
        ) => Promise<WorkspaceSyncResult>;
      }
    ).syncWorkspaceRepo(workspace, testCredentials(workspace), localState, "remote-version");

    expect(pulledKnownVersion).toBe("base-version");
    expect(result.syncState).toBe("synced");
    expect(result.remoteVersion).toBe("remote-version");
    expect(state.connections.map((connection) => connection.host)).toEqual(["remote.example.com"]);
  });

  test("pushes when only the local fingerprint changed", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName
    });
    const localConnection: ConnectionProfile = {
      id: "local-connection",
      name: "Local",
      host: "local.example.com",
      port: 22,
      username: "root",
      authType: "agent",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      resourceId: `${scopeKey}-local-connection`,
      uuidInScope: "local-connection",
      originKind: "cloud",
      originScopeKey: scopeKey,
      originWorkspaceId: workspace.id
    };
    const localState: WorkspaceRepoLocalState = {
      workspaceId: workspace.id,
      remoteVersion: "base-version",
      localFingerprint: "old-local"
    };
    const state = createMutableState(workspace, { localState, connections: [localConnection] });

    const manager = new CloudSyncManager(createMutableDeps(state));
    const fingerprintBeforePush = (
      manager as unknown as { workspaceFingerprint: (workspaceId: string) => string }
    ).workspaceFingerprint(workspace.id);
    let pushedBaseHead: string | null | undefined;
    (manager as unknown as { api: unknown }).api = {
      push: async (_credentials: unknown, payload: { baseHeadCommitId?: string | null }) => {
        pushedBaseHead = payload.baseHeadCommitId;
        state.connections = [{ ...localConnection, updatedAt: "2026-03-15T02:00:00.000Z" }];
        return { status: "accepted" as const, headCommitId: "local-version" };
      }
    };

    const result = await (
      manager as unknown as {
        syncWorkspaceRepo: (
          workspace: CloudSyncWorkspaceProfile,
          credentials: ReturnType<typeof testCredentials>,
          localState: WorkspaceRepoLocalState,
          remoteVersion?: string
        ) => Promise<WorkspaceSyncResult>;
      }
    ).syncWorkspaceRepo(workspace, testCredentials(workspace), localState, "base-version");

    expect(pushedBaseHead).toBe("base-version");
    expect(result.syncState).toBe("synced");
    expect(result.remoteVersion).toBe("local-version");
    expect(result.localFingerprint).toBe(fingerprintBeforePush);
    expect(result.localFingerprint).not.toBe(
      (
        manager as unknown as { workspaceFingerprint: (workspaceId: string) => string }
      ).workspaceFingerprint(workspace.id)
    );
  });

  test("marks both-side changes as diverged without changing local data in auto mode", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const connection: ConnectionProfile = {
      id: "local-connection",
      name: "Local",
      host: "local.example.com",
      port: 22,
      username: "root",
      authType: "agent",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      uuidInScope: "same-connection",
      originKind: "cloud",
      originScopeKey: "cloud-scope",
      originWorkspaceId: workspace.id
    };
    const state = createMutableState(workspace, {
      localState: {
        workspaceId: workspace.id,
        remoteVersion: "base-version",
        localFingerprint: "old-local"
      },
      connections: [connection]
    });
    const manager = new CloudSyncManager(createMutableDeps(state));
    let apiCalls = 0;
    (manager as unknown as { api: unknown }).api = {
      pull: async () => {
        apiCalls += 1;
        throw new Error("pull should not run in auto divergence");
      },
      push: async () => {
        apiCalls += 1;
        throw new Error("push should not run in auto divergence");
      }
    };

    const result = await (
      manager as unknown as {
        syncWorkspaceRepo: (
          workspace: CloudSyncWorkspaceProfile,
          credentials: ReturnType<typeof testCredentials>,
          localState: WorkspaceRepoLocalState,
          remoteVersion?: string
        ) => Promise<WorkspaceSyncResult>;
      }
    ).syncWorkspaceRepo(workspace, testCredentials(workspace), state.localState!, "remote-version");

    expect(result.syncState).toBe("diverged");
    expect(result.remoteVersion).toBe("base-version");
    expect(state.connections[0]?.host).toBe("local.example.com");
    expect(apiCalls).toBe(0);
  });

  test("cloud-wins applies the snapshot returned by a push 409", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName
    });
    const localConnection: ConnectionProfile = {
      id: "local-connection",
      name: "Local",
      host: "local.example.com",
      port: 22,
      username: "root",
      authType: "agent",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      resourceId: `${scopeKey}-same-connection`,
      uuidInScope: "same-connection",
      originKind: "cloud",
      originScopeKey: scopeKey,
      originWorkspaceId: workspace.id
    };
    const remote = repoSnapshot(workspace.id, "remote-snapshot", [
      snapshotConnection("same-connection", "Remote", "remote.example.com")
    ]);
    const state = createMutableState(workspace, {
      localState: {
        workspaceId: workspace.id,
        remoteVersion: "base-version",
        localFingerprint: "old-local"
      },
      connections: [localConnection]
    });
    const manager = new CloudSyncManager(createMutableDeps(state));
    (manager as unknown as { api: unknown }).api = {
      push: async () => ({
        status: "diverged" as const,
        headCommitId: "remote-version",
        snapshot: remote
      })
    };

    const result = await (
      manager as unknown as {
        syncWorkspaceRepo: (
          workspace: CloudSyncWorkspaceProfile,
          credentials: ReturnType<typeof testCredentials>,
          localState: WorkspaceRepoLocalState,
          remoteVersion?: string,
          mode?: "cloud-wins" | "local-wins"
        ) => Promise<WorkspaceSyncResult>;
      }
    ).syncWorkspaceRepo(
      workspace,
      testCredentials(workspace),
      state.localState!,
      "base-version",
      "cloud-wins"
    );

    expect(result.syncState).toBe("synced");
    expect(state.connections[0]?.host).toBe("remote.example.com");
  });

  test("local-wins retries against each 409 head and stops after three attempts", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const connection: ConnectionProfile = {
      id: "local-connection",
      name: "Local",
      host: "local.example.com",
      port: 22,
      username: "root",
      authType: "agent",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      uuidInScope: "same-connection",
      originKind: "cloud",
      originScopeKey: "cloud-scope",
      originWorkspaceId: workspace.id
    };
    const state = createMutableState(workspace, {
      localState: {
        workspaceId: workspace.id,
        remoteVersion: "base-version",
        localFingerprint: "old-local"
      },
      connections: [connection]
    });
    const manager = new CloudSyncManager(createMutableDeps(state));
    const baseHeads: Array<string | null | undefined> = [];
    (manager as unknown as { api: unknown }).api = {
      push: async (_credentials: unknown, payload: { baseHeadCommitId?: string | null }) => {
        baseHeads.push(payload.baseHeadCommitId);
        return {
          status: "diverged" as const,
          headCommitId: `head-${baseHeads.length}`,
          snapshot: repoSnapshot(workspace.id, `remote-${baseHeads.length}`, [])
        };
      }
    };

    await expect(
      (
        manager as unknown as {
          syncWorkspaceRepo: (
            workspace: CloudSyncWorkspaceProfile,
            credentials: ReturnType<typeof testCredentials>,
            localState: WorkspaceRepoLocalState,
            remoteVersion?: string,
            mode?: "cloud-wins" | "local-wins"
          ) => Promise<WorkspaceSyncResult>;
        }
      ).syncWorkspaceRepo(
        workspace,
        testCredentials(workspace),
        state.localState!,
        "base-version",
        "local-wins"
      )
    ).rejects.toThrow("停止本地优先重试");

    expect(baseHeads).toEqual(["base-version", "head-1", "head-2"]);
    expect(state.connections[0]?.host).toBe("local.example.com");
  });

  test("uses stable workspace-scope AAD for encrypted snapshot credentials", async () => {
    const firstWorkspace = { ...createWorkspace(), id: "client-a" };
    const secondWorkspace = { ...createWorkspace(), id: "client-b" };
    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: firstWorkspace.apiBaseUrl,
      workspaceName: firstWorkspace.workspaceName
    });

    const buildSnapshotFor = async (workspace: CloudSyncWorkspaceProfile) => {
      const state = createMutableState(workspace);
      state.credentials.set("secret://ssh-key", "PRIVATE KEY");
      state.sshKeys = [
        {
          id: `key-${workspace.id}`,
          name: "Deploy key",
          keyContentRef: "secret://ssh-key",
          createdAt: now,
          updatedAt: now,
          resourceId: `${scopeKey}-deploy-key`,
          uuidInScope: "deploy-key",
          originKind: "cloud",
          originScopeKey: scopeKey,
          originWorkspaceId: workspace.id
        }
      ];
      const manager = new CloudSyncManager(createMutableDeps(state));
      return (
        manager as unknown as {
          buildWorkspaceSnapshot: (
            workspace: CloudSyncWorkspaceProfile,
            workspacePassword: string
          ) => Promise<WorkspaceRepoSnapshot>;
        }
      ).buildWorkspaceSnapshot(workspace, "workspace-password");
    };

    const firstSnapshot = await buildSnapshotFor(firstWorkspace);
    const secondSnapshot = await buildSnapshotFor(secondWorkspace);
    expect(firstSnapshot.sshKeys[0]?.privateKey.aad).toBe(
      `${scopeKey}:sshKey:deploy-key:privateKey`
    );
    expect(secondSnapshot.sshKeys[0]?.privateKey.aad).toBe(
      firstSnapshot.sshKeys[0]?.privateKey.aad
    );
  });

  test("keeps the asset fingerprint stable when only an envelope is reencrypted", () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const connection: ConnectionProfile = {
      id: "conn-1",
      name: "Prod",
      host: "prod.example.com",
      port: 22,
      username: "root",
      authType: "password",
      credentialRef: "secret://password",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      uuidInScope: "conn-1",
      originKind: "cloud",
      originScopeKey: "cloud-scope",
      originWorkspaceId: workspace.id
    };
    const state = createMutableState(workspace, { connections: [connection] });
    state.credentials.set("secret://password", "first-password");
    const manager = new CloudSyncManager(createMutableDeps(state));
    const fingerprint = (
      manager as unknown as { workspaceFingerprint: (workspaceId: string) => string }
    ).workspaceFingerprint(workspace.id);

    state.credentials.set("secret://password", "second-password");
    const reencryptedFingerprint = (
      manager as unknown as { workspaceFingerprint: (workspaceId: string) => string }
    ).workspaceFingerprint(workspace.id);

    expect(reencryptedFingerprint).toBe(fingerprint);
  });
});

describe("CloudSyncManager applyWorkspaceSnapshot", () => {
  type ApplySnapshot = (
    workspace: CloudSyncWorkspaceProfile,
    workspacePassword: string,
    snapshot: WorkspaceRepoSnapshot
  ) => Promise<void>;

  // folderId 不在线协议里,是本地的目录归属;saveConnection 是全行 upsert,重建 profile 时
  // 漏掉这个键就等于每次 pull 都把云连接的目录清空。
  test("keeps the local folderId of an existing cloud connection across a pull", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName
    });
    const existing: ConnectionProfile = {
      id: "local-id-1",
      name: "Prod",
      host: "old.example.com",
      port: 22,
      username: "root",
      authType: "agent",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team/asia",
      folderId: "folder-asia",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      resourceId: `${scopeKey}-conn-1`,
      uuidInScope: "conn-1",
      originKind: "cloud",
      originScopeKey: scopeKey,
      originWorkspaceId: workspace.id
    } as ConnectionProfile;
    const state = createMutableState(workspace, { connections: [existing] });
    const manager = new CloudSyncManager(createMutableDeps(state));

    await (manager as unknown as { applyWorkspaceSnapshot: ApplySnapshot }).applyWorkspaceSnapshot(
      workspace,
      "workspace-password",
      repoSnapshot(workspace.id, "remote-snapshot", [
        snapshotConnection("conn-1", "Prod", "new.example.com")
      ])
    );

    const applied = state.connections.find((connection) => connection.uuidInScope === "conn-1");
    expect(applied?.host).toBe("new.example.com");
    expect(applied?.folderId).toBe("folder-asia");
  });

  // agentAccess 同样不在线协议里:全行 upsert 绑的是 `agentAccess ?? "off"`,漏掉这个键
  // 等于每次 pull 都把用户授予 agent 的访问权限悄悄降回 off。
  test("keeps the local agentAccess of an existing cloud connection across a pull", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const scopeKey = buildScopeKey({
      kind: "cloud",
      apiBaseUrl: workspace.apiBaseUrl,
      workspaceName: workspace.workspaceName
    });
    const existing: ConnectionProfile = {
      id: "local-id-2",
      name: "Prod",
      host: "old.example.com",
      port: 22,
      username: "root",
      authType: "agent",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team",
      agentAccess: "full",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      resourceId: `${scopeKey}-conn-2`,
      uuidInScope: "conn-2",
      originKind: "cloud",
      originScopeKey: scopeKey,
      originWorkspaceId: workspace.id
    } as ConnectionProfile;
    const state = createMutableState(workspace, { connections: [existing] });
    const manager = new CloudSyncManager(createMutableDeps(state));

    await (manager as unknown as { applyWorkspaceSnapshot: ApplySnapshot }).applyWorkspaceSnapshot(
      workspace,
      "workspace-password",
      repoSnapshot(workspace.id, "remote-snapshot", [
        snapshotConnection("conn-2", "Prod", "new.example.com")
      ])
    );

    const applied = state.connections.find((connection) => connection.uuidInScope === "conn-2");
    expect(applied?.host).toBe("new.example.com");
    expect(applied?.agentAccess).toBe("full");
  });

  test("leaves folderId empty for a connection this device has never seen", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const state = createMutableState(workspace);
    const manager = new CloudSyncManager(createMutableDeps(state));

    await (manager as unknown as { applyWorkspaceSnapshot: ApplySnapshot }).applyWorkspaceSnapshot(
      workspace,
      "workspace-password",
      repoSnapshot(workspace.id, "remote-snapshot", [
        snapshotConnection("conn-new", "New", "new.example.com")
      ])
    );

    expect(state.connections[0]?.folderId).toBeUndefined();
  });

  test("moves resources deleted remotely into the recycle bin", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const existing: ConnectionProfile = {
      id: "local-id-3",
      name: "Old server",
      host: "old.example.com",
      port: 22,
      username: "root",
      authType: "agent",
      strictHostKeyChecking: false,
      groupPath: "/workspace/prod-team",
      tags: [],
      favorite: false,
      monitorSession: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      createdAt: now,
      updatedAt: now,
      resourceId: "cloud-scope-old-server",
      uuidInScope: "old-server",
      originKind: "cloud",
      originScopeKey: "cloud-scope",
      originWorkspaceId: workspace.id
    };
    const state = createMutableState(workspace, { connections: [existing] });
    const manager = new CloudSyncManager(createMutableDeps(state));

    await (manager as unknown as { applyWorkspaceSnapshot: ApplySnapshot }).applyWorkspaceSnapshot(
      workspace,
      "workspace-password",
      repoSnapshot(workspace.id, "empty-remote", [])
    );

    expect(state.connections).toHaveLength(0);
    expect(state.recycleBin).toHaveLength(1);
    expect(state.recycleBin[0]).toMatchObject({
      resourceType: "server",
      displayName: "Old server",
      originalResourceId: "cloud-scope-old-server",
      reason: "delete"
    });
  });
});

describe("CloudSyncManager workspace command sync", () => {
  const localCommand: WorkspaceCommandItem = {
    id: "cmd-1",
    workspaceId: "ws-1",
    name: "Deploy",
    group: "ops",
    command: "deploy local",
    isTemplate: false,
    createdAt: now,
    updatedAt: "2026-03-15T01:00:00.000Z"
  };

  test("marks command divergence without creating conflict copies", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const state = createMutableState(workspace, {
      commands: [localCommand],
      localState: {
        workspaceId: workspace.id,
        remoteCommandsVersion: "base-version",
        localCommandsFingerprint: "old-local"
      }
    });
    const manager = new CloudSyncManager(createMutableDeps(state));
    let apiCalls = 0;
    (manager as unknown as { api: unknown }).api = {
      pullCommands: async () => {
        apiCalls += 1;
        throw new Error("pull should not run in auto divergence");
      },
      pushCommands: async () => {
        apiCalls += 1;
        throw new Error("push should not run in auto divergence");
      }
    };

    const result = await (
      manager as unknown as {
        syncWorkspaceCommands: (
          workspace: CloudSyncWorkspaceProfile,
          credentials: ReturnType<typeof testCredentials>,
          localState: WorkspaceRepoLocalState,
          resolvedRemoteVersion?: string,
          mode?: "cloud-wins" | "local-wins"
        ) => Promise<WorkspaceSyncResult>;
      }
    ).syncWorkspaceCommands(
      workspace,
      testCredentials(workspace),
      {
        workspaceId: workspace.id,
        remoteCommandsVersion: "base-version",
        localCommandsFingerprint: "old-local"
      },
      "remote-version"
    );

    expect(result.syncState).toBe("diverged");
    expect(state.commands).toEqual([localCommand]);
    expect(apiCalls).toBe(0);
  });

  test("cloud-wins replaces the complete command set", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const remoteCommand = { ...localCommand, command: "deploy remote", workspaceId: workspace.id };
    const state = createMutableState(workspace, {
      commands: [localCommand],
      localState: {
        workspaceId: workspace.id,
        remoteCommandsVersion: "base-version",
        localCommandsFingerprint: "old-local"
      }
    });
    const manager = new CloudSyncManager(createMutableDeps(state));
    (manager as unknown as { api: unknown }).api = {
      pullCommands: async () => ({
        status: "changed" as const,
        version: "remote-version",
        commands: [remoteCommand]
      })
    };

    const result = await (
      manager as unknown as {
        syncWorkspaceCommands: (
          workspace: CloudSyncWorkspaceProfile,
          credentials: ReturnType<typeof testCredentials>,
          localState: WorkspaceRepoLocalState,
          resolvedRemoteVersion?: string,
          mode?: "cloud-wins" | "local-wins"
        ) => Promise<WorkspaceSyncResult>;
      }
    ).syncWorkspaceCommands(
      workspace,
      testCredentials(workspace),
      state.localState!,
      "remote-version",
      "cloud-wins"
    );

    expect(result.remoteCommandsVersion).toBe("remote-version");
    expect(state.commands).toEqual([remoteCommand]);
  });

  test("local-wins pushes the full local command set", async () => {
    const workspace = { ...createWorkspace(), enabled: true };
    const state = createMutableState(workspace, {
      commands: [localCommand],
      localState: {
        workspaceId: workspace.id,
        remoteCommandsVersion: "base-version",
        localCommandsFingerprint: "old-local"
      }
    });
    const manager = new CloudSyncManager(createMutableDeps(state));
    const fingerprintBeforePush = (
      manager as unknown as { workspaceCommandsFingerprint: (workspaceId: string) => string }
    ).workspaceCommandsFingerprint(workspace.id);
    let pushedCommands: Array<Record<string, unknown>> = [];
    (manager as unknown as { api: unknown }).api = {
      pushCommands: async (_credentials: unknown, commands: Array<Record<string, unknown>>) => {
        pushedCommands = commands;
        state.commands = [{ ...localCommand, updatedAt: "2026-03-15T02:00:00.000Z" }];
        return { version: "local-version" };
      }
    };

    const result = await (
      manager as unknown as {
        syncWorkspaceCommands: (
          workspace: CloudSyncWorkspaceProfile,
          credentials: ReturnType<typeof testCredentials>,
          localState: WorkspaceRepoLocalState,
          resolvedRemoteVersion?: string,
          mode?: "cloud-wins" | "local-wins"
        ) => Promise<WorkspaceSyncResult>;
      }
    ).syncWorkspaceCommands(
      workspace,
      testCredentials(workspace),
      state.localState!,
      "remote-version",
      "local-wins"
    );

    expect(result.remoteCommandsVersion).toBe("local-version");
    expect(pushedCommands).toEqual([{ ...localCommand, workspaceId: workspace.id }]);
    expect(result.localCommandsFingerprint).toBe(fingerprintBeforePush);
    expect(result.localCommandsFingerprint).not.toBe(
      (
        manager as unknown as { workspaceCommandsFingerprint: (workspaceId: string) => string }
      ).workspaceCommandsFingerprint(workspace.id)
    );
  });
});
