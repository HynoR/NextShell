/**
 * CloudSyncApiV2Client — HTTP client for cloud sync server API v2.
 *
 * Implements:
 * - POST /api/v2/workspaces/resolve
 * - POST /api/v2/workspaces/pull
 * - POST /api/v2/workspaces/push
 */

import { Buffer } from "node:buffer";
import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { z } from "zod";

// ── Response schemas ────────────────────────────────────────────────────────

const resolveResponseSchema = z.object({
  ok: z.literal(true),
  workspaceId: z.string(),
  serverTime: z.string(),
  currentVersion: z.number().int().min(0),
  capabilities: z.object({
    resourceTypes: z.array(z.string()),
    supportsBatchPush: z.boolean(),
  }),
});

const serverPayloadSchema = z.record(z.string(), z.unknown());

const serverSnapshotItemSchema = z.object({
  uuid: z.string(),
  revision: z.number().int().min(1),
  updatedAt: z.string(),
  payload: serverPayloadSchema,
});

const deletedItemSchema = z.object({
  resourceType: z.enum(["server", "sshKey"]),
  uuid: z.string(),
  revision: z.number().int().min(1),
  deletedAt: z.string(),
});

const pullResponseSchema = z.object({
  ok: z.literal(true),
  workspaceVersion: z.number().int().min(0),
  serverTime: z.string(),
  servers: z.array(serverSnapshotItemSchema).default([]),
  sshKeys: z.array(serverSnapshotItemSchema).default([]),
  deleted: z.array(deletedItemSchema).default([]),
});

const pushResultItemSchema = z.object({
  type: z.string(),
  uuid: z.string(),
  revision: z.number().int().min(1),
});

const pushSuccessResponseSchema = z.object({
  ok: z.literal(true),
  workspaceVersion: z.number().int().min(0),
  results: z.array(pushResultItemSchema),
});

const conflictItemSchema = z.object({
  type: z.string(),
  resourceType: z.enum(["server", "sshKey"]),
  uuid: z.string(),
  serverRevision: z.number().int().min(1),
  serverDeleted: z.boolean(),
  serverUpdatedAt: z.string().nullable().optional(),
  serverPayload: z.record(z.string(), z.unknown()).optional(),
});

const pushConflictResponseSchema = z.object({
  ok: z.literal(false),
  error: z.literal("conflict"),
  workspaceVersion: z.number().int().min(0),
  conflicts: z.array(conflictItemSchema),
});

// ── Exported types ──────────────────────────────────────────────────────────

export type ResolveResponse = z.infer<typeof resolveResponseSchema>;

export type PullResponse = z.infer<typeof pullResponseSchema>;
export type ServerSnapshotItem = z.infer<typeof serverSnapshotItemSchema>;
export type DeletedItem = z.infer<typeof deletedItemSchema>;

export type PushSuccessResponse = z.infer<typeof pushSuccessResponseSchema>;
export type PushConflictResponse = z.infer<typeof pushConflictResponseSchema>;
export type PushConflictItem = z.infer<typeof conflictItemSchema>;
export type PushResultItem = z.infer<typeof pushResultItemSchema>;

export type PushResponse = PushSuccessResponse | PushConflictResponse;

export interface PushOperation {
  type: "upsertServer" | "upsertSshKey" | "deleteServer" | "deleteSshKey";
  uuid: string;
  baseRevision: number | null;
  payload?: Record<string, unknown>;
}

export interface CloudSyncApiCredentials {
  apiBaseUrl: string;
  workspaceName: string;
  workspacePassword: string;
  ignoreTlsErrors: boolean;
}

// ── Client ──────────────────────────────────────────────────────────────────

export class CloudSyncApiV2Client {
  async resolve(creds: CloudSyncApiCredentials): Promise<ResolveResponse> {
    return this.post(creds, "/api/v2/workspaces/resolve", {
      workspaceName: creds.workspaceName,
    }, resolveResponseSchema);
  }

  async pull(creds: CloudSyncApiCredentials, knownVersion: number): Promise<PullResponse> {
    return this.post(creds, "/api/v2/workspaces/pull", {
      knownVersion,
    }, pullResponseSchema);
  }

  async push(
    creds: CloudSyncApiCredentials,
    baseWorkspaceVersion: number,
    operations: PushOperation[],
  ): Promise<PushResponse> {
    const body = { baseWorkspaceVersion, operations };
    const rawJson = await this.postRaw(creds, "/api/v2/workspaces/push", body);

    // Determine if it's a success or conflict response
    if (rawJson.ok === true) {
      return pushSuccessResponseSchema.parse(rawJson);
    }
    if (rawJson.ok === false && rawJson.error === "conflict") {
      return pushConflictResponseSchema.parse(rawJson);
    }
    throw new Error(`Unexpected push response: ${JSON.stringify(rawJson).slice(0, 500)}`);
  }

  // ── Internal HTTP transport ───────────────────────────────────────────────

  private async post<T>(
    creds: CloudSyncApiCredentials,
    pathname: string,
    payload: unknown,
    schema: z.ZodType<T>,
  ): Promise<T> {
    const raw = await this.postRaw(creds, pathname, payload);
    return schema.parse(raw);
  }

  private async postRaw(
    creds: CloudSyncApiCredentials,
    pathname: string,
    payload: unknown,
  ): Promise<Record<string, unknown>> {
    const requestUrl = new URL(`${creds.apiBaseUrl}${pathname}`);
    const body = JSON.stringify(payload);
    const isHttps = requestUrl.protocol === "https:";
    const transport = isHttps ? httpsRequest : httpRequest;

    const TIMEOUT_MS = 30_000;
    const MAX_BODY_BYTES = 10 * 1024 * 1024; // 10MB

    const { statusCode, bodyText } = await new Promise<{ statusCode: number; bodyText: string }>((resolve, reject) => {
      const abortController = new AbortController();
      const timer = setTimeout(() => {
        abortController.abort();
        clientRequest.destroy();
        reject(new Error("请求超时（30s）"));
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
          },
          rejectUnauthorized: isHttps ? !creds.ignoreTlsErrors : undefined,
          signal: abortController.signal,
        },
        (response) => {
          const chunks: Buffer[] = [];
          let totalBytes = 0;
          response.on("data", (chunk) => {
            const buf = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
            totalBytes += buf.length;
            if (totalBytes > MAX_BODY_BYTES) {
              clearTimeout(timer);
              clientRequest.destroy();
              reject(new Error("响应体超过 10MB 限制"));
              return;
            }
            chunks.push(buf);
          });
          response.on("end", () => {
            clearTimeout(timer);
            resolve({
              statusCode: response.statusCode ?? 0,
              bodyText: Buffer.concat(chunks).toString("utf8"),
            });
          });
        },
      );

      clientRequest.on("error", (err) => {
        clearTimeout(timer);
        reject(err);
      });
      clientRequest.write(body);
      clientRequest.end();
    });

    if (statusCode < 200 || statusCode >= 300) {
      let message: string | undefined;
      if (bodyText.trim()) {
        try {
          const parsed = JSON.parse(bodyText) as Record<string, unknown>;
          if (typeof parsed.error === "string") message = parsed.error;
        } catch {
          message = bodyText.trim().slice(0, 500);
        }
      }
      // For 409 conflict, pass the raw body through so caller can parse it
      if (statusCode === 409 && bodyText.trim()) {
        try {
          return JSON.parse(bodyText) as Record<string, unknown>;
        } catch {
          throw new Error(message ?? `HTTP ${statusCode}`);
        }
      }
      throw new Error(message ?? `HTTP ${statusCode}`);
    }

    return JSON.parse(bodyText) as Record<string, unknown>;
  }
}
