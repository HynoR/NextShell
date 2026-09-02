import http from "node:http";

import type { EndpointTarget } from "./endpoint.js";
import {
  isRecord,
  type JsonRpcErrorBody,
  type JsonRpcId,
  type JsonRpcNotification,
  type JsonRpcRequest,
  type JsonRpcResponse
} from "./json-rpc.js";
import { SseParser } from "./sse.js";

const MAX_RESPONSE_BYTES = 32 * 1024 * 1024;
const MAX_ERROR_BODY_CHARS = 512;

/**
 * Failure codes are the only upstream detail allowed to reach stdout: unlike
 * the underlying Node error messages they never carry a socket path.
 */
export type UpstreamErrorCode =
  | "UNREACHABLE"
  | "REFUSED"
  | "TIMEOUT"
  | "UNAUTHORIZED"
  | "SESSION_EXPIRED"
  | "HTTP_ERROR"
  | "PROTOCOL"
  | "RPC_ERROR";

export class UpstreamError extends Error {
  readonly code: UpstreamErrorCode;
  readonly status: number | null;

  constructor(message: string, code: UpstreamErrorCode, status: number | null = null) {
    super(message);
    this.name = "UpstreamError";
    this.code = code;
    this.status = status;
  }
}

export class UpstreamRpcError extends UpstreamError {
  readonly body: JsonRpcErrorBody;

  constructor(body: JsonRpcErrorBody) {
    super(body.message, "RPC_ERROR");
    this.name = "UpstreamRpcError";
    this.body = body;
  }
}

/** Transport-level failures make the cached session unusable and force a re-dial. */
export const isTransportFailure = (error: unknown): boolean =>
  error instanceof UpstreamError &&
  (error.code === "UNREACHABLE" ||
    error.code === "REFUSED" ||
    error.code === "TIMEOUT" ||
    error.code === "SESSION_EXPIRED" ||
    error.code === "PROTOCOL");

const toUpstreamError = (error: unknown): UpstreamError => {
  if (error instanceof UpstreamError) {
    return error;
  }
  const code = isRecord(error) && typeof error.code === "string" ? error.code : "";
  const message = error instanceof Error ? error.message : String(error);
  if (code === "ECONNREFUSED") {
    return new UpstreamError(message, "REFUSED");
  }
  if (code === "ETIMEDOUT" || code === "ECONNRESET" || code === "EPIPE") {
    return new UpstreamError(message, "TIMEOUT");
  }
  return new UpstreamError(message, "UNREACHABLE");
};

const headerValue = (value: string | string[] | undefined): string | null => {
  if (typeof value === "string") {
    return value;
  }
  if (Array.isArray(value)) {
    return value[0] ?? null;
  }
  return null;
};

const DEFAULT_HTTP_PATH = "/mcp";

const buildRequestOptions = (
  target: EndpointTarget,
  method: string,
  headers: Record<string, string>
): http.RequestOptions => ({
  method,
  path: DEFAULT_HTTP_PATH,
  socketPath: target.socketPath,
  headers
});

const buildHeaders = (
  sessionId: string | null,
  protocolVersion: string | null,
  extra: Record<string, string> = {}
): Record<string, string> => {
  const headers: Record<string, string> = {
    accept: "application/json, text/event-stream",
    ...extra
  };
  if (sessionId !== null) {
    headers["mcp-session-id"] = sessionId;
  }
  if (protocolVersion !== null) {
    headers["mcp-protocol-version"] = protocolVersion;
  }
  return headers;
};

interface PostOutcome {
  status: number;
  sessionId: string | null;
  response: JsonRpcResponse | null;
}

interface PostArgs {
  target: EndpointTarget;
  message: JsonRpcRequest | JsonRpcNotification;
  awaitId: JsonRpcId | null;
  sessionId: string | null;
  protocolVersion: string | null;
  timeoutMs: number;
  onNotification?: (notification: JsonRpcNotification) => void;
  log?: (message: string) => void;
}

const postJsonRpc = (args: PostArgs): Promise<PostOutcome> =>
  new Promise<PostOutcome>((resolve, reject) => {
    const payload = Buffer.from(JSON.stringify(args.message), "utf8");
    const headers = buildHeaders(args.sessionId, args.protocolVersion, {
      "content-type": "application/json",
      "content-length": String(payload.byteLength)
    });
    const request = http.request(buildRequestOptions(args.target, "POST", headers));

    let settled = false;
    const timer = setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      request.destroy();
      reject(new UpstreamError("upstream request timed out", "TIMEOUT"));
    }, args.timeoutMs);
    timer.unref();

    const succeed = (outcome: PostOutcome): void => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      resolve(outcome);
    };
    const fail = (error: unknown): void => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      reject(toUpstreamError(error));
    };

    request.on("error", fail);

    request.on("response", (response) => {
      const status = response.statusCode ?? 0;
      const sessionId = headerValue(response.headers["mcp-session-id"]);
      const contentType = (headerValue(response.headers["content-type"]) ?? "").toLowerCase();
      response.setEncoding("utf8");

      if (status >= 400) {
        let body = "";
        response.on("data", (chunk: string) => {
          if (body.length < MAX_ERROR_BODY_CHARS) {
            body += chunk;
          }
        });
        response.on("end", () => {
          const detail = body.slice(0, MAX_ERROR_BODY_CHARS).replace(/\s+/g, " ").trim();
          if (status === 401 || status === 403) {
            fail(
              new UpstreamError(`upstream rejected the bridge: ${detail}`, "UNAUTHORIZED", status)
            );
            return;
          }
          if (status === 404) {
            fail(new UpstreamError("upstream session is gone", "SESSION_EXPIRED", status));
            return;
          }
          fail(
            new UpstreamError(`upstream returned HTTP ${status}: ${detail}`, "HTTP_ERROR", status)
          );
        });
        response.on("error", fail);
        return;
      }

      if (args.awaitId === null) {
        response.resume();
        response.on("end", () => succeed({ status, sessionId, response: null }));
        response.on("error", fail);
        return;
      }

      const consume = (message: unknown): boolean => {
        if (!isRecord(message)) {
          return false;
        }
        if (typeof message.method === "string") {
          if (message.id === undefined) {
            args.onNotification?.(message as unknown as JsonRpcNotification);
          } else {
            args.log?.(`ignoring unsupported upstream request: ${message.method}`);
          }
          return false;
        }
        if (message.id === args.awaitId) {
          succeed({ status, sessionId, response: message as unknown as JsonRpcResponse });
          response.destroy();
          request.destroy();
          return true;
        }
        return false;
      };

      let received = 0;
      if (contentType.startsWith("text/event-stream")) {
        const parser = new SseParser();
        response.on("data", (chunk: string) => {
          received += chunk.length;
          if (received > MAX_RESPONSE_BYTES) {
            fail(new UpstreamError("upstream response exceeded the size limit", "PROTOCOL"));
            response.destroy();
            request.destroy();
            return;
          }
          for (const message of parser.push(chunk)) {
            if (consume(message)) {
              return;
            }
          }
        });
      } else {
        let body = "";
        response.on("data", (chunk: string) => {
          received += chunk.length;
          if (received > MAX_RESPONSE_BYTES) {
            fail(new UpstreamError("upstream response exceeded the size limit", "PROTOCOL"));
            response.destroy();
            request.destroy();
            return;
          }
          body += chunk;
        });
        response.on("end", () => {
          if (settled) {
            return;
          }
          let parsed: unknown;
          try {
            parsed = JSON.parse(body);
          } catch {
            fail(new UpstreamError("upstream returned malformed JSON", "PROTOCOL"));
            return;
          }
          const entries = Array.isArray(parsed) ? parsed : [parsed];
          for (const entry of entries) {
            if (consume(entry)) {
              return;
            }
          }
          fail(new UpstreamError("upstream response carried no matching reply", "PROTOCOL"));
        });
      }

      response.on("end", () => {
        if (!settled) {
          fail(new UpstreamError("upstream closed the stream before replying", "PROTOCOL"));
        }
      });
      response.on("error", fail);
    });

    request.end(payload);
  });

export interface UpstreamSessionOptions {
  clientInfo: { name: string; version: string };
  protocolVersion: string;
  connectTimeoutMs: number;
  requestTimeoutMs: number;
  onNotification?: (notification: JsonRpcNotification) => void;
  log?: (message: string) => void;
}

export interface UpstreamLike {
  readonly transport: "socket";
  readonly serverInfo: { name: string; version: string } | null;
  request(method: string, params?: unknown, timeoutMs?: number): Promise<unknown>;
  close(): Promise<void>;
}

export class UpstreamSession implements UpstreamLike {
  private nextId = 1;
  private closed = false;

  private constructor(
    private readonly target: EndpointTarget,
    private readonly options: UpstreamSessionOptions,
    private readonly sessionId: string | null,
    readonly protocolVersion: string,
    readonly serverInfo: { name: string; version: string } | null
  ) {}

  get transport(): "socket" {
    return this.target.transport;
  }

  static async open(
    target: EndpointTarget,
    options: UpstreamSessionOptions
  ): Promise<UpstreamSession> {
    const outcome = await postJsonRpc({
      target,
      message: {
        jsonrpc: "2.0",
        id: 0,
        method: "initialize",
        params: {
          protocolVersion: options.protocolVersion,
          capabilities: {},
          clientInfo: options.clientInfo
        }
      },
      awaitId: 0,
      sessionId: null,
      protocolVersion: null,
      timeoutMs: options.connectTimeoutMs,
      onNotification: options.onNotification,
      log: options.log
    });

    const response = outcome.response;
    if (response === null) {
      throw new UpstreamError("upstream did not answer initialize", "PROTOCOL");
    }
    if (response.error !== undefined) {
      throw new UpstreamRpcError(response.error);
    }

    const result = isRecord(response.result) ? response.result : {};
    const protocolVersion =
      typeof result.protocolVersion === "string" ? result.protocolVersion : options.protocolVersion;
    const info = isRecord(result.serverInfo) ? result.serverInfo : null;
    const serverInfo =
      info !== null && typeof info.name === "string"
        ? { name: info.name, version: typeof info.version === "string" ? info.version : "" }
        : null;

    const session = new UpstreamSession(
      target,
      options,
      outcome.sessionId,
      protocolVersion,
      serverInfo
    );
    await session.notify("notifications/initialized");
    return session;
  }

  async notify(method: string, params?: unknown): Promise<void> {
    await postJsonRpc({
      target: this.target,
      message: { jsonrpc: "2.0", method, ...(params === undefined ? {} : { params }) },
      awaitId: null,
      sessionId: this.sessionId,
      protocolVersion: this.protocolVersion,
      timeoutMs: this.options.connectTimeoutMs,
      log: this.options.log
    });
  }

  async request(method: string, params?: unknown, timeoutMs?: number): Promise<unknown> {
    if (this.closed) {
      throw new UpstreamError("upstream session is closed", "SESSION_EXPIRED");
    }
    const id = this.nextId++;
    const outcome = await postJsonRpc({
      target: this.target,
      message: { jsonrpc: "2.0", id, method, ...(params === undefined ? {} : { params }) },
      awaitId: id,
      sessionId: this.sessionId,
      protocolVersion: this.protocolVersion,
      timeoutMs: timeoutMs ?? this.options.requestTimeoutMs,
      onNotification: this.options.onNotification,
      log: this.options.log
    });

    const response = outcome.response;
    if (response === null) {
      throw new UpstreamError(`upstream did not answer ${method}`, "PROTOCOL");
    }
    if (response.error !== undefined) {
      throw new UpstreamRpcError(response.error);
    }
    return response.result;
  }

  async close(): Promise<void> {
    if (this.closed) {
      return;
    }
    this.closed = true;
    if (this.sessionId === null) {
      return;
    }
    await new Promise<void>((resolve) => {
      const headers = buildHeaders(this.sessionId, this.protocolVersion);
      const request = http.request(buildRequestOptions(this.target, "DELETE", headers));
      const done = (): void => resolve();
      request.on("error", done);
      request.on("response", (response) => {
        response.resume();
        response.on("end", done);
        response.on("error", done);
      });
      request.setTimeout(this.options.connectTimeoutMs, () => request.destroy());
      request.end();
    });
  }
}
