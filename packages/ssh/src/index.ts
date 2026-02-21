import { once } from "node:events";
import { createHash } from "node:crypto";
import fs from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import type { Duplex } from "node:stream";
import type {
  Client,
  ClientChannel,
  ConnectConfig,
  SFTPWrapper,
  Stats
} from "ssh2";

type AuthType = "password" | "privateKey" | "agent";
type ProxyType = "socks4" | "socks5";

const DEFAULT_READY_TIMEOUT_MS = 10000;
const CONNECTION_CLOSE_TIMEOUT_MS = 2000;
const require = createRequire(import.meta.url);

interface RawSftpEntry {
  filename: string;
  longname: string;
  attrs: {
    size?: number;
    mode?: number;
    uid?: number;
    gid?: number;
    atime?: number;
    mtime?: number;
  };
}

interface Ssh2Module {
  Client: new () => Client;
}

interface SocksCreateConnectionOptions {
  command: "connect";
  destination: {
    host: string;
    port: number;
  };
  proxy: {
    host: string;
    port: number;
    type: 4 | 5;
    userId?: string;
    password?: string;
  };
  timeout?: number;
}

interface SocksCreateConnectionResult {
  socket: Duplex;
}

interface SocksModule {
  SocksClient: {
    createConnection: (options: SocksCreateConnectionOptions) => Promise<SocksCreateConnectionResult>;
  };
}

const loadSsh2 = (): Ssh2Module => {
  const moduleName = `ssh${2}`;
  return require(moduleName) as Ssh2Module;
};

const loadSocks = (): SocksModule => {
  return require("socks") as SocksModule;
};

const normalizeProxyError = (error: unknown): Error => {
  const message = error instanceof Error ? error.message : "Unknown proxy error";
  const lower = message.toLowerCase();

  if (lower.includes("timed out") || lower.includes("timeout")) {
    return new Error("Proxy handshake timed out.");
  }

  if (lower.includes("auth") || lower.includes("username") || lower.includes("password")) {
    return new Error("Proxy authentication failed.");
  }

  return new Error(`Proxy is unreachable: ${message}`);
};

export interface SshProxyOptions {
  type: ProxyType;
  host: string;
  port: number;
  username?: string;
  password?: string;
}

export interface SshConnectOptions {
  host: string;
  port: number;
  username: string;
  authType: AuthType;
  password?: string;
  privateKey?: string;
  privateKeyPath?: string;
  passphrase?: string;
  agentSock?: string;
  hostFingerprint?: string;
  strictHostKeyChecking?: boolean;
  proxy?: SshProxyOptions;
}

export interface ShellOpenOptions {
  cols: number;
  rows: number;
  term?: string;
}

export interface ExecResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

export interface SshDirectoryEntry {
  name: string;
  longname: string;
  size: number;
  mode?: number;
  uid?: number;
  gid?: number;
  atime?: number;
  mtime?: number;
}

export type SshShellChannel = ClientChannel;
export type RemotePathType = "file" | "directory" | "link";

const expandHomePath = (rawPath: string): string => {
  if (rawPath === "~") {
    return os.homedir();
  }

  if (rawPath.startsWith("~/")) {
    return path.join(os.homedir(), rawPath.slice(2));
  }

  return rawPath;
};

export class SshConnection {
  private readonly client: Client;
  private readonly readyPromise: Promise<void>;
  private closed = false;

  private constructor(private readonly options: SshConnectOptions) {
    const ssh2 = loadSsh2();
    this.client = new ssh2.Client();
    this.readyPromise = this.connect();
  }

  static async connect(options: SshConnectOptions): Promise<SshConnection> {
    const connection = new SshConnection(options);
    await connection.readyPromise;
    return connection;
  }

  private async connect(): Promise<void> {
    const config = await this.buildConfig();

    await new Promise<void>((resolve, reject) => {
      const onReady = () => {
        cleanup();
        resolve();
      };

      const onError = (error: Error) => {
        cleanup();
        reject(error);
      };

      const onEnd = () => {
        cleanup();
        reject(new Error("SSH connection ended before ready"));
      };

      const cleanup = () => {
        this.client.off("ready", onReady);
        this.client.off("error", onError);
        this.client.off("end", onEnd);
      };

      this.client.once("ready", onReady);
      this.client.once("error", onError);
      this.client.once("end", onEnd);
      this.client.connect(config);
    });
  }

  private async buildConfig(): Promise<ConnectConfig> {
    const config: ConnectConfig = {
      host: this.options.host,
      port: this.options.port,
      username: this.options.username,
      readyTimeout: DEFAULT_READY_TIMEOUT_MS,
      keepaliveInterval: 15000,
      keepaliveCountMax: 3
    };

    const proxySocket = await this.createProxySocket();
    if (proxySocket) {
      config.sock = proxySocket;
    }

    if (this.options.strictHostKeyChecking) {
      const expected = this.options.hostFingerprint?.trim();
      if (!expected) {
        throw new Error("Strict host key checking requires host fingerprint");
      }

      config.hostVerifier = (key: Buffer | string) => {
        const keyBuffer = Buffer.isBuffer(key) ? key : Buffer.from(key, "binary");
        const sha256Base64 = createHash("sha256").update(keyBuffer).digest("base64");
        const sha256Hex = createHash("sha256").update(keyBuffer).digest("hex");
        const md5Hex = createHash("md5").update(keyBuffer).digest("hex");
        const md5Colon = md5Hex.match(/.{2}/g)?.join(":") ?? md5Hex;

        const normalizedExpected = expected.toLowerCase();
        if (normalizedExpected.startsWith("sha256:")) {
          return normalizedExpected.slice("sha256:".length) === sha256Base64.toLowerCase();
        }

        if (normalizedExpected.includes(":")) {
          return normalizedExpected === md5Colon.toLowerCase();
        }

        return normalizedExpected === sha256Hex.toLowerCase() || normalizedExpected === md5Hex.toLowerCase();
      };
    }

    if (this.options.authType === "password") {
      if (!this.options.password) {
        throw new Error("Password auth requires password");
      }
      config.password = this.options.password;
      return config;
    }

    if (this.options.authType === "privateKey") {
      let privateKey = this.options.privateKey;
      if (!privateKey && this.options.privateKeyPath) {
        const privateKeyPath = expandHomePath(this.options.privateKeyPath);
        privateKey = await fs.readFile(privateKeyPath, "utf-8");
      }
      if (!privateKey) {
        throw new Error("Private key auth requires privateKeyPath or imported private key");
      }

      config.privateKey = privateKey;
      if (this.options.passphrase) {
        config.passphrase = this.options.passphrase;
      }
      return config;
    }

    config.agent = this.options.agentSock ?? process.env.SSH_AUTH_SOCK;
    if (!config.agent) {
      throw new Error("SSH agent auth requires SSH_AUTH_SOCK");
    }

    return config;
  }

  private async createProxySocket(): Promise<Duplex | undefined> {
    const proxy = this.options.proxy;
    if (!proxy) {
      return undefined;
    }

    const socks = loadSocks();
    const proxyType = proxy.type === "socks4" ? 4 : 5;

    const connectionOptions: SocksCreateConnectionOptions = {
      command: "connect",
      destination: {
        host: this.options.host,
        port: this.options.port
      },
      proxy: {
        host: proxy.host,
        port: proxy.port,
        type: proxyType,
        userId: proxy.username,
        password: proxy.type === "socks5" ? proxy.password : undefined
      },
      timeout: DEFAULT_READY_TIMEOUT_MS
    };

    try {
      const result = await socks.SocksClient.createConnection(connectionOptions);
      return result.socket;
    } catch (error) {
      throw normalizeProxyError(error);
    }
  }

  onError(listener: (error: Error) => void): void {
    this.client.on("error", listener);
  }

  onClose(listener: () => void): void {
    this.client.on("close", listener);
  }

  async openShell(options: ShellOpenOptions): Promise<ClientChannel> {
    await this.readyPromise;

    return new Promise((resolve, reject) => {
      this.client.shell(
        {
          term: options.term ?? "xterm-256color",
          cols: options.cols,
          rows: options.rows
        },
        (error, channel) => {
          if (error || !channel) {
            reject(error ?? new Error("Failed to open SSH shell"));
            return;
          }
          resolve(channel);
        }
      );
    });
  }

  async exec(command: string): Promise<ExecResult> {
    await this.readyPromise;

    return new Promise((resolve, reject) => {
      this.client.exec(command, (error, channel) => {
        if (error || !channel) {
          reject(error ?? new Error("Failed to execute command"));
          return;
        }

        let stdout = "";
        let stderr = "";

        channel.on("data", (chunk: Buffer | string) => {
          stdout += chunk.toString();
        });

        channel.stderr.on("data", (chunk: Buffer | string) => {
          stderr += chunk.toString();
        });

        channel.once("close", (exitCode?: number) => {
          resolve({
            stdout,
            stderr,
            exitCode: exitCode ?? 0
          });
        });

        channel.once("error", reject);
      });
    });
  }

  private async openSftp(): Promise<SFTPWrapper> {
    await this.readyPromise;

    return new Promise((resolve, reject) => {
      this.client.sftp((error, sftp) => {
        if (error || !sftp) {
          reject(error ?? new Error("Failed to open SFTP subsystem"));
          return;
        }
        resolve(sftp);
      });
    });
  }

  private async withSftp<T>(work: (sftp: SFTPWrapper) => Promise<T>): Promise<T> {
    const sftp = await this.openSftp();
    try {
      return await work(sftp);
    } finally {
      sftp.end();
    }
  }

  private async statPath(sftp: SFTPWrapper, pathName: string): Promise<Stats> {
    return new Promise((resolve, reject) => {
      sftp.stat(pathName, (error, stats) => {
        if (error || !stats) {
          reject(error ?? new Error("Failed to stat remote path"));
          return;
        }
        resolve(stats);
      });
    });
  }

  private async listRawEntries(sftp: SFTPWrapper, pathName: string): Promise<RawSftpEntry[]> {
    return new Promise((resolve, reject) => {
      sftp.readdir(pathName, (error, list) => {
        if (error || !list) {
          reject(error ?? new Error("Failed to list remote directory"));
          return;
        }

        resolve(list as unknown as RawSftpEntry[]);
      });
    });
  }

  private async fastGet(sftp: SFTPWrapper, remotePath: string, localPath: string): Promise<void> {
    await new Promise<void>((resolve, reject) => {
      sftp.fastGet(remotePath, localPath, (error) => {
        if (error) {
          reject(error);
          return;
        }
        resolve();
      });
    });
  }

  private async mkdirSingle(sftp: SFTPWrapper, pathName: string): Promise<void> {
    await new Promise<void>((resolve, reject) => {
      sftp.mkdir(pathName, (error) => {
        if (error) {
          reject(error);
          return;
        }
        resolve();
      });
    });
  }

  private async ensureRemoteDir(sftp: SFTPWrapper, pathName: string): Promise<void> {
    const normalized = path.posix.normalize(pathName);
    if (normalized === "/" || normalized === ".") {
      return;
    }

    const absolute = normalized.startsWith("/");
    const parts = normalized.split("/").filter(Boolean);
    let current = absolute ? "/" : "";

    for (const part of parts) {
      current = current === "/" ? `/${part}` : current ? `${current}/${part}` : part;

      try {
        await this.mkdirSingle(sftp, current);
      } catch (error) {
        try {
          const stats = await this.statPath(sftp, current);
          if (!stats.isDirectory()) {
            throw error;
          }
        } catch {
          throw error;
        }
      }
    }
  }

  private async downloadDirectoryRecursive(
    sftp: SFTPWrapper,
    remoteDir: string,
    localDir: string
  ): Promise<void> {
    await fs.mkdir(localDir, { recursive: true });
    const entries = await this.listRawEntries(sftp, remoteDir);

    for (const entry of entries) {
      if (entry.filename === "." || entry.filename === "..") {
        continue;
      }

      const remotePath = path.posix.join(remoteDir, entry.filename);
      const localPath = path.join(localDir, entry.filename);
      const isDirectory = entry.longname.startsWith("d");

      if (isDirectory) {
        await this.downloadDirectoryRecursive(sftp, remotePath, localPath);
      } else {
        await fs.mkdir(path.dirname(localPath), { recursive: true });
        await this.fastGet(sftp, remotePath, localPath);
      }
    }
  }

  private async removeDirectoryRecursive(sftp: SFTPWrapper, remoteDir: string): Promise<void> {
    const entries = await this.listRawEntries(sftp, remoteDir);

    for (const entry of entries) {
      if (entry.filename === "." || entry.filename === "..") {
        continue;
      }

      const childPath = path.posix.join(remoteDir, entry.filename);
      const isDirectory = entry.longname.startsWith("d");

      if (isDirectory) {
        await this.removeDirectoryRecursive(sftp, childPath);
        continue;
      }

      await new Promise<void>((resolve, reject) => {
        sftp.unlink(childPath, (error) => {
          if (error) {
            reject(error);
            return;
          }
          resolve();
        });
      });
    }

    await new Promise<void>((resolve, reject) => {
      sftp.rmdir(remoteDir, (error) => {
        if (error) {
          reject(error);
          return;
        }
        resolve();
      });
    });
  }

  async list(pathName: string): Promise<SshDirectoryEntry[]> {
    return this.withSftp(async (sftp) => {
      const rows = await this.listRawEntries(sftp, pathName);

      return rows.map((row) => ({
        name: row.filename,
        longname: row.longname,
        size: row.attrs.size ?? 0,
        mode: row.attrs.mode,
        uid: row.attrs.uid,
        gid: row.attrs.gid,
        atime: row.attrs.atime,
        mtime: row.attrs.mtime
      }));
    });
  }

  async upload(localPath: string, remotePath: string): Promise<void> {
    const resolvedLocalPath = expandHomePath(localPath);

    await this.withSftp(
      (sftp) =>
        new Promise<void>((resolve, reject) => {
          sftp.fastPut(resolvedLocalPath, remotePath, (error) => {
            if (error) {
              reject(error);
              return;
            }
            resolve();
          });
        })
    );
  }

  async download(remotePath: string, localPath: string): Promise<void> {
    const resolvedLocalPath = expandHomePath(localPath);

    await this.withSftp(async (sftp) => {
      const stats = await this.statPath(sftp, remotePath);
      if (stats.isDirectory()) {
        await this.downloadDirectoryRecursive(sftp, remotePath, resolvedLocalPath);
        return;
      }

      await fs.mkdir(path.dirname(resolvedLocalPath), { recursive: true });
      await this.fastGet(sftp, remotePath, resolvedLocalPath);
    });
  }

  async mkdir(pathName: string, recursive = false): Promise<void> {
    const normalized = path.posix.normalize(pathName);
    if (normalized === "/" || normalized === ".") {
      return;
    }

    await this.withSftp(async (sftp) => {
      if (recursive) {
        await this.ensureRemoteDir(sftp, normalized);
        return;
      }

      await this.mkdirSingle(sftp, normalized);
    });
  }

  async rename(fromPath: string, toPath: string): Promise<void> {
    await this.withSftp(
      (sftp) =>
        new Promise<void>((resolve, reject) => {
          sftp.rename(fromPath, toPath, (error) => {
            if (error) {
              reject(error);
              return;
            }
            resolve();
          });
        })
    );
  }

  async remove(pathName: string, type: RemotePathType): Promise<void> {
    await this.withSftp(async (sftp) => {
      if (type === "directory") {
        await this.removeDirectoryRecursive(sftp, pathName);
        return;
      }

      await new Promise<void>((resolve, reject) => {
        sftp.unlink(pathName, (error) => {
          if (error) {
            reject(error);
            return;
          }
          resolve();
        });
      });
    });
  }

  async readFileContent(remotePath: string): Promise<Buffer> {
    return this.withSftp(async (sftp) => {
      return new Promise<Buffer>((resolve, reject) => {
        const chunks: Buffer[] = [];
        const stream = sftp.createReadStream(remotePath);
        stream.on("data", (chunk: Buffer) => chunks.push(chunk));
        stream.on("end", () => resolve(Buffer.concat(chunks)));
        stream.on("error", reject);
      });
    });
  }

  async writeFileContent(remotePath: string, content: Buffer): Promise<void> {
    return this.withSftp(async (sftp) => {
      return new Promise<void>((resolve, reject) => {
        const stream = sftp.createWriteStream(remotePath);
        stream.on("close", () => resolve());
        stream.on("error", reject);
        stream.end(content);
      });
    });
  }

  async close(): Promise<void> {
    if (this.closed) {
      return;
    }

    this.closed = true;
    this.client.end();

    await Promise.race([
      once(this.client, "close").then(() => undefined),
      new Promise<void>((resolve) => {
        setTimeout(resolve, CONNECTION_CLOSE_TIMEOUT_MS);
      })
    ]);
  }
}
