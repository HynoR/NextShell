import { session as electronSession } from "electron";
import type { AppPreferences, ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import type {
  CloudSyncConflictItem,
  CloudSyncConfigureInput,
  CloudSyncResolveConflictInput,
  CloudSyncStatus,
  SettingsUpdateInput,
} from "@nextshell/shared";
import { IPCChannel } from "@nextshell/shared";
import type { EncryptedSecretVault } from "@nextshell/security";
import type { CachedConnectionRepository } from "@nextshell/storage";
import {
  CloudSyncService,
  CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY,
  type CloudSyncApplyConnectionInput,
  type CloudSyncApplyProxyInput,
  type CloudSyncApplySshKeyInput,
} from "./cloud-sync-service";

export interface CloudSyncBridgeOptions {
  keytarServiceName: string;
  getAppPreferences: () => AppPreferences;
  saveAppPreferencesPatch: (patch: SettingsUpdateInput, options?: { reconfigureCloudSync?: boolean }) => AppPreferences;
  vault: EncryptedSecretVault;
  listConnections: () => ConnectionProfile[];
  listSshKeys: () => SshKeyProfile[];
  listProxies: () => ProxyProfile[];
  applyConnectionFromCloudSync: (input: CloudSyncApplyConnectionInput) => Promise<void>;
  applySshKeyFromCloudSync: (input: CloudSyncApplySshKeyInput) => Promise<void>;
  applyProxyFromCloudSync: (input: CloudSyncApplyProxyInput) => Promise<void>;
  removeConnectionFromCloudSync: (id: string) => Promise<void>;
  removeSshKeyFromCloudSync: (id: string) => Promise<void>;
  removeProxyFromCloudSync: (id: string) => Promise<void>;
  broadcastToAllWindows: (channel: string, payload: unknown) => void;
  connections: CachedConnectionRepository;
}

export class CloudSyncBridge {
  private readonly cloudSyncService: CloudSyncService;
  private readonly cloudSyncNetworkSession: Electron.Session;

  constructor(private readonly options: CloudSyncBridgeOptions) {
    this.cloudSyncNetworkSession = electronSession.fromPartition("persist:nextshell-cloud-sync");

    const { connections } = options;

    this.cloudSyncService = new CloudSyncService({
      keytarServiceName: options.keytarServiceName,
      getPreferences: options.getAppPreferences,
      savePreferencesPatch: options.saveAppPreferencesPatch,
      vault: options.vault,
      listConnections: options.listConnections,
      listSshKeys: options.listSshKeys,
      listProxies: options.listProxies,
      applyConnectionFromCloudSync: options.applyConnectionFromCloudSync,
      applySshKeyFromCloudSync: options.applySshKeyFromCloudSync,
      applyProxyFromCloudSync: options.applyProxyFromCloudSync,
      removeConnectionFromCloudSync: options.removeConnectionFromCloudSync,
      removeSshKeyFromCloudSync: options.removeSshKeyFromCloudSync,
      removeProxyFromCloudSync: options.removeProxyFromCloudSync,
      emitStatus: (status) => {
        options.broadcastToAllWindows(IPCChannel.CloudSyncStatusEvent, status);
      },
      emitApplied: (event) => {
        options.broadcastToAllWindows(IPCChannel.CloudSyncAppliedEvent, event);
      },
      loadPendingQueueState: () => connections.getJsonSetting(CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY),
      savePendingQueueState: (state) => {
        if (state === undefined) {
          connections.removeSetting(CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY);
          return;
        }
        connections.saveJsonSetting(CLOUD_SYNC_PENDING_QUEUE_SETTING_KEY, state);
      },
      listResourceStates: () => connections.listCloudSyncResourceStates(),
      getResourceState: (resourceType, resourceId) =>
        connections.getCloudSyncResourceState(resourceType, resourceId),
      saveResourceState: (state) => {
        connections.saveCloudSyncResourceState(state);
      },
      removeResourceState: (resourceType, resourceId) => {
        connections.removeCloudSyncResourceState(resourceType, resourceId);
      },
      requestJson: (request, schema) => this.cloudSyncRequestJson(request, schema),
    });

    this.cloudSyncService.initialize();
  }

  async configure(input: CloudSyncConfigureInput): Promise<CloudSyncStatus> {
    return this.cloudSyncService.configure(input);
  }

  async disable(): Promise<{ ok: true }> {
    return this.cloudSyncService.disable();
  }

  async status(): Promise<CloudSyncStatus> {
    return this.cloudSyncService.status();
  }

  async syncNow(): Promise<{ ok: true }> {
    return this.cloudSyncService.syncNow();
  }

  async listConflicts(): Promise<CloudSyncConflictItem[]> {
    return this.cloudSyncService.listConflicts();
  }

  async resolveConflict(input: CloudSyncResolveConflictInput): Promise<{ ok: true }> {
    return this.cloudSyncService.resolveConflict(input);
  }

  getService(): CloudSyncService {
    return this.cloudSyncService;
  }

  dispose(): void {
    this.cloudSyncService.dispose();
  }

  private configureCloudSyncTlsVerification(apiBaseUrl: string, ignoreTlsErrors: boolean): void {
    if (!ignoreTlsErrors) {
      this.cloudSyncNetworkSession.setCertificateVerifyProc(null);
      return;
    }
    void apiBaseUrl;
    this.cloudSyncNetworkSession.setCertificateVerifyProc((_request, callback) => {
      callback(0);
    });
  }

  private async cloudSyncRequestJson<T>(
    request: {
      apiBaseUrl: string;
      workspaceName: string;
      workspacePassword: string;
      pathname: string;
      payload: unknown;
      ignoreTlsErrors: boolean;
    },
    schema: { parse: (value: unknown) => T },
  ): Promise<T> {
    this.configureCloudSyncTlsVerification(request.apiBaseUrl, request.ignoreTlsErrors);

    const response = await this.cloudSyncNetworkSession.fetch(
      `${request.apiBaseUrl}${request.pathname}`,
      {
        method: "POST",
        headers: {
          Accept: "application/json",
          Authorization: `Basic ${Buffer.from(`${request.workspaceName}:${request.workspacePassword}`, "utf8").toString("base64")}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(request.payload),
      },
    );

    if (!response.ok) {
      const bodyText = await response.text();
      if (response.status === 409 && bodyText.trim()) {
        throw new Error(bodyText);
      }
      let message: string | undefined;
      if (bodyText.trim()) {
        try {
          const parsed = JSON.parse(bodyText) as { error?: unknown };
          if (typeof parsed.error === "string" && parsed.error.trim()) {
            message = parsed.error;
          }
        } catch {
          message = bodyText.trim();
        }
        message ??= bodyText.trim();
      }
      throw new Error(message ?? `HTTP ${response.status}`);
    }

    return schema.parse(await response.json());
  }
}
