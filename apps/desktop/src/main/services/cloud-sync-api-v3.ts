import { Buffer } from "node:buffer";
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { z } from "zod";

const workspaceSecretEnvelopeSchema = z.object({
  v: z.literal(1),
  alg: z.string(),
  kdf: z.literal("scrypt"),
  salt: z.string(),
  iv: z.string(),
  aad: z.string().optional(),
  ciphertext: z.string(),
  tag: z.string(),
});

const repoConnectionSchema = z.object({
  uuid: z.string(),
  name: z.string(),
  host: z.string(),
  port: z.number().int(),
  username: z.string(),
  authType: z.enum(["password", "privateKey", "agent", "interactive"]),
  password: workspaceSecretEnvelopeSchema.optional(),
  sshKeyUuid: z.string().optional(),
  hostFingerprint: z.string().optional(),
  strictHostKeyChecking: z.boolean(),
  proxyUuid: z.string().optional(),
  keepAliveEnabled: z.boolean().optional(),
  keepAliveIntervalSec: z.number().int().optional(),
  terminalEncoding: z.enum(["utf-8", "gb18030", "gbk", "big5"]),
  backspaceMode: z.enum(["ascii-backspace", "ascii-delete"]),
  deleteMode: z.enum(["vt220-delete", "ascii-delete", "ascii-backspace"]),
  groupPath: z.string(),
  tags: z.array(z.string()).default([]),
  notes: z.string().optional(),
  favorite: z.boolean(),
  createdAt: z.string(),
  updatedAt: z.string(),
});

const repoSshKeySchema = z.object({
  uuid: z.string(),
  name: z.string(),
  privateKey: workspaceSecretEnvelopeSchema,
  passphrase: workspaceSecretEnvelopeSchema.optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
});

const repoProxySchema = z.object({
  uuid: z.string(),
  name: z.string(),
  proxyType: z.enum(["socks4", "socks5"]),
  host: z.string(),
  port: z.number().int(),
  username: z.string().optional(),
  password: workspaceSecretEnvelopeSchema.optional(),
  createdAt: z.string(),
  updatedAt: z.string(),
});

const repoSnapshotSchema = z.object({
  workspaceId: z.string().optional(),
  snapshotId: z.string(),
  createdAt: z.string(),
  connections: z.array(repoConnectionSchema).default([]),
  sshKeys: z.array(repoSshKeySchema).default([]),
  proxies: z.array(repoProxySchema).default([]),
});

const commitMetaSchema = z.object({
  workspaceId: z.string().optional(),
  commitId: z.string(),
  parentCommitId: z.string().optional(),
  snapshotId: z.string(),
  authorName: z.string(),
  authorKind: z.enum(["system", "user", "reconcile"]),
  message: z.string(),
  createdAt: z.string(),
});

const workspaceCommandItemSchema = z.object({
  id: z.string(),
  workspaceId: z.string().optional(),
  name: z.string(),
  description: z.string().optional(),
  group: z.string(),
  command: z.string(),
  isTemplate: z.boolean(),
  createdAt: z.string(),
  updatedAt: z.string(),
});

const resolveResponseSchema = z.object({
  workspaceId: z.string().optional(),
  displayName: z.string().optional(),
  headCommitId: z.string().nullable().optional(),
  commandsVersion: z.string().nullable().optional(),
  serverTime: z.string().optional(),
});

const repoPullResponseSchema = z.object({
  unchanged: z.boolean().optional(),
  headCommitId: z.string().nullable().optional(),
  headCommit: commitMetaSchema.optional(),
  snapshot: repoSnapshotSchema.nullable().optional(),
  recentCommits: z.array(commitMetaSchema).default([]),
  serverTime: z.string().optional(),
});

const repoPushAcceptedSchema = z.object({
  status: z.literal("accepted"),
  headCommitId: z.string(),
  headCommit: commitMetaSchema.optional(),
  recentCommits: z.array(commitMetaSchema).default([]),
});

const repoPushDivergedSchema = z.object({
  status: z.literal("diverged"),
  headCommitId: z.string().nullable().optional(),
  headCommit: commitMetaSchema.optional(),
  snapshot: repoSnapshotSchema,
  recentCommits: z.array(commitMetaSchema).default([]),
});

const repoHistoryResponseSchema = z.object({
  commits: z.array(commitMetaSchema).default([]),
  nextCursor: z.string().nullable().optional(),
});

const repoSnapshotResponseSchema = z.object({
  commit: commitMetaSchema.optional(),
  snapshot: repoSnapshotSchema,
});

const commandsPullUnchangedSchema = z.object({
  status: z.literal("unchanged"),
  version: z.string(),
});

const commandsPullChangedSchema = z.object({
  status: z.literal("changed"),
  version: z.string(),
  commands: z.array(workspaceCommandItemSchema).default([]),
});

const commandsPushResponseSchema = z.object({
  version: z.string(),
});

export type RepoResolveResponse = z.infer<typeof resolveResponseSchema>;
export type RepoPullResponse = z.infer<typeof repoPullResponseSchema>;
export type RepoPushAcceptedResponse = z.infer<typeof repoPushAcceptedSchema>;
export type RepoPushDivergedResponse = z.infer<typeof repoPushDivergedSchema>;
export type RepoPushResponse = RepoPushAcceptedResponse | RepoPushDivergedResponse;
export type RepoHistoryResponse = z.infer<typeof repoHistoryResponseSchema>;
export type RepoSnapshotResponse = z.infer<typeof repoSnapshotResponseSchema>;
export type CommandsPullResponse =
  | z.infer<typeof commandsPullUnchangedSchema>
  | z.infer<typeof commandsPullChangedSchema>;
export type CommandsPushResponse = z.infer<typeof commandsPushResponseSchema>;

export interface CloudSyncApiV3Credentials {
  apiBaseUrl: string;
  workspaceName: string;
  workspacePassword: string;
  ignoreTlsErrors: boolean;
  clientId: string;
  clientVersion: string;
}

export class CloudSyncApiV3Client {
  async resolve(creds: CloudSyncApiV3Credentials): Promise<RepoResolveResponse> {
    return this.post(creds, "/api/v3/workspaces/resolve", {}, resolveResponseSchema);
  }

  async pull(
    creds: CloudSyncApiV3Credentials,
    knownHeadCommitId?: string | null,
  ): Promise<RepoPullResponse> {
    return this.post(
      creds,
      "/api/v3/repo/pull",
      { knownHeadCommitId: knownHeadCommitId ?? null },
      repoPullResponseSchema,
    );
  }

  async push(
    creds: CloudSyncApiV3Credentials,
    payload: {
      baseHeadCommitId?: string | null;
      commitMeta: unknown;
      snapshot: unknown;
    },
  ): Promise<RepoPushResponse> {
    const raw = await this.postRaw(creds, "/api/v3/repo/push", payload);
    if (raw.status === "accepted") {
      return repoPushAcceptedSchema.parse(raw);
    }
    if (raw.status === "diverged") {
      return repoPushDivergedSchema.parse(raw);
    }
    throw new Error(`Unexpected repo push response: ${JSON.stringify(raw).slice(0, 500)}`);
  }

  async history(
    creds: CloudSyncApiV3Credentials,
    cursor: string | undefined,
    limit: number,
  ): Promise<RepoHistoryResponse> {
    return this.post(
      creds,
      "/api/v3/repo/history",
      { cursor: cursor ?? null, limit },
      repoHistoryResponseSchema,
    );
  }

  async snapshot(
    creds: CloudSyncApiV3Credentials,
    commitId: string,
  ): Promise<RepoSnapshotResponse> {
    return this.post(
      creds,
      "/api/v3/repo/snapshot",
      { commitId },
      repoSnapshotResponseSchema,
    );
  }

  async pullCommands(
    creds: CloudSyncApiV3Credentials,
    knownVersion?: string | null,
  ): Promise<CommandsPullResponse> {
    const raw = await this.postRaw(
      creds,
      "/api/v3/commands/pull",
      { knownVersion: knownVersion ?? null },
    );
    if (raw.status === "unchanged") {
      return commandsPullUnchangedSchema.parse(raw);
    }
    if (raw.status === "changed") {
      return commandsPullChangedSchema.parse(raw);
    }
    throw new Error(`Unexpected commands pull response: ${JSON.stringify(raw).slice(0, 500)}`);
  }

  async pushCommands(
    creds: CloudSyncApiV3Credentials,
    commands: Array<Record<string, unknown>>,
  ): Promise<CommandsPushResponse> {
    return this.post(
      creds,
      "/api/v3/commands/push",
      { commands },
      commandsPushResponseSchema,
    );
  }

  private async post<T>(
    creds: CloudSyncApiV3Credentials,
    pathname: string,
    payload: unknown,
    schema: z.ZodType<T>,
  ): Promise<T> {
    const raw = await this.postRaw(creds, pathname, payload);
    return schema.parse(raw);
  }

  private async postRaw(
    creds: CloudSyncApiV3Credentials,
    pathname: string,
    payload: unknown,
  ): Promise<Record<string, unknown>> {
    const requestUrl = new URL(`${creds.apiBaseUrl}${pathname}`);
    const body = JSON.stringify(payload);
    const isHttps = requestUrl.protocol === "https:";
    const transport = isHttps ? httpsRequest : httpRequest;

    const TIMEOUT_MS = 30_000;
    const MAX_BODY_BYTES = 20 * 1024 * 1024;

    const { statusCode, bodyText } = await new Promise<{ statusCode: number; bodyText: string }>((resolve, reject) => {
      let settled = false;
      const abortController = new AbortController();
      const timer = setTimeout(() => {
        abortController.abort();
        clientRequest.destroy();
        if (!settled) {
          settled = true;
          reject(new Error("请求超时（30s）"));
        }
      }, TIMEOUT_MS);

      const clientRequest = transport(
        requestUrl,
        {
          method: "POST",
          headers: {
            Accept: "application/json",
            Authorization: `Basic ${Buffer.from(`${creds.workspaceName}:${creds.workspacePassword}`, "utf8").toString("base64")}`,
            "Content-Length": Buffer.byteLength(body),
            "Content-Type": "application/json",
            "X-NextShell-Client-Id": creds.clientId,
            "X-NextShell-Client-Version": creds.clientVersion,
          },
          rejectUnauthorized: isHttps ? !creds.ignoreTlsErrors : undefined,
          signal: abortController.signal,
        },
        (response) => {
          const chunks: Buffer[] = [];
          let totalBytes = 0;
          response.on("data", (chunk) => {
            const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
            totalBytes += buffer.length;
            if (totalBytes > MAX_BODY_BYTES) {
              clearTimeout(timer);
              clientRequest.destroy();
              if (!settled) {
                settled = true;
                reject(new Error("响应体超过 20MB 限制"));
              }
              return;
            }
            chunks.push(buffer);
          });
          response.on("end", () => {
            clearTimeout(timer);
            if (settled) {
              return;
            }
            settled = true;
            resolve({
              statusCode: response.statusCode ?? 0,
              bodyText: Buffer.concat(chunks).toString("utf8"),
            });
          });
        },
      );

      clientRequest.on("error", (error) => {
        clearTimeout(timer);
        if (settled) {
          return;
        }
        settled = true;
        reject(error);
      });

      clientRequest.write(body);
      clientRequest.end();
    });

    if (statusCode < 200 || statusCode >= 300) {
      let message: string | undefined;
      if (bodyText.trim()) {
        try {
          const parsed = JSON.parse(bodyText) as Record<string, unknown>;
          if (typeof parsed.error === "string") {
            message = parsed.error;
          } else if (typeof parsed.message === "string") {
            message = parsed.message;
          }
        } catch {
          message = bodyText.trim().slice(0, 500);
        }
      }

      if (statusCode === 409 && bodyText.trim()) {
        return JSON.parse(bodyText) as Record<string, unknown>;
      }

      throw new Error(message ?? `HTTP ${statusCode}`);
    }

    if (!bodyText.trim()) {
      return {};
    }

    return JSON.parse(bodyText) as Record<string, unknown>;
  }
}
