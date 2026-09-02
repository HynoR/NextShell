import { mkdir, readdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";

export const ENDPOINT_DISCOVERY_VERSION = 1;
export const ENDPOINT_FILE_NAME = "endpoint.json";

const INSTANCE_FILE_RE = /^endpoint-(\d+)\.json$/;

export interface EndpointDiscoveryRecord {
  version: number;
  pid: number;
  socketPath: string | null;
  appVersion: string;
  startedAt: string;
}

export interface EndpointDiscoveryOptions {
  /** `<userData>`; the discovery directory is `<userData>/mcp`. */
  userDataDir: string;
  appVersion: string;
  pid?: number;
  isProcessAlive?: (pid: number) => boolean;
}

const defaultIsProcessAlive = (pid: number): boolean => {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return (error as NodeJS.ErrnoException).code === "EPERM";
  }
};

const parseRecord = (raw: string): EndpointDiscoveryRecord | null => {
  try {
    const parsed = JSON.parse(raw) as Partial<EndpointDiscoveryRecord>;
    if (typeof parsed.pid !== "number" || !Number.isInteger(parsed.pid)) {
      return null;
    }
    return {
      version: typeof parsed.version === "number" ? parsed.version : ENDPOINT_DISCOVERY_VERSION,
      pid: parsed.pid,
      socketPath: typeof parsed.socketPath === "string" ? parsed.socketPath : null,
      appVersion: typeof parsed.appVersion === "string" ? parsed.appVersion : "unknown",
      startedAt: typeof parsed.startedAt === "string" ? parsed.startedAt : new Date(0).toISOString()
    };
  } catch {
    return null;
  }
};

/**
 * `<userData>/mcp/endpoint-<pid>.json` per instance, plus `endpoint.json`
 * pointing at the newest live one. Files stay 0600: no secrets any more, but
 * the socket path is instance-private by convention.
 */
export class EndpointDiscoveryFile {
  private readonly directory: string;
  private readonly appVersion: string;
  private readonly pid: number;
  private readonly isProcessAlive: (pid: number) => boolean;

  constructor(options: EndpointDiscoveryOptions) {
    this.directory = path.join(options.userDataDir, "mcp");
    this.appVersion = options.appVersion;
    this.pid = options.pid ?? process.pid;
    this.isProcessAlive = options.isProcessAlive ?? defaultIsProcessAlive;
  }

  /** Our own pid is alive by definition — probing it would race our own write. */
  private isAlive(pid: number): boolean {
    return pid === this.pid || this.isProcessAlive(pid);
  }

  get directoryPath(): string {
    return this.directory;
  }

  /** Path clients read by default (`NEXTSHELL_MCP_ENDPOINT` overrides it). */
  get primaryPath(): string {
    return path.join(this.directory, ENDPOINT_FILE_NAME);
  }

  get instancePath(): string {
    return path.join(this.directory, `endpoint-${this.pid}.json`);
  }

  async write(input: {
    socketPath: string | null;
    startedAt?: string;
  }): Promise<EndpointDiscoveryRecord> {
    const record: EndpointDiscoveryRecord = {
      version: ENDPOINT_DISCOVERY_VERSION,
      pid: this.pid,
      socketPath: input.socketPath,
      appVersion: this.appVersion,
      startedAt: input.startedAt ?? new Date().toISOString()
    };

    await mkdir(this.directory, { recursive: true, mode: 0o700 });
    const payload = `${JSON.stringify(record, null, 2)}\n`;
    await writeFile(this.instancePath, payload, { mode: 0o600 });
    await writeFile(this.primaryPath, payload, { mode: 0o600 });
    await this.pruneStale();
    return record;
  }

  async remove(): Promise<void> {
    await rm(this.instancePath, { force: true }).catch(() => undefined);
    const primary = await this.readPrimary();
    if (!primary || primary.pid === this.pid) {
      await rm(this.primaryPath, { force: true }).catch(() => undefined);
    }
    await this.pruneStale();
  }

  async readPrimary(): Promise<EndpointDiscoveryRecord | null> {
    try {
      return parseRecord(await readFile(this.primaryPath, "utf8"));
    } catch {
      return null;
    }
  }

  /**
   * Drops instance files whose process is gone and repoints `endpoint.json` at
   * the newest survivor — stale discovery files are the failure mode that killed
   * port-probing proxies.
   */
  async pruneStale(): Promise<EndpointDiscoveryRecord | null> {
    let names: string[];
    try {
      names = await readdir(this.directory);
    } catch {
      return null;
    }

    const live: EndpointDiscoveryRecord[] = [];
    for (const name of names) {
      const match = INSTANCE_FILE_RE.exec(name);
      if (!match) {
        continue;
      }
      const filePath = path.join(this.directory, name);
      const record = parseRecord(await readFile(filePath, "utf8").catch(() => ""));
      if (!record || !this.isAlive(record.pid)) {
        await rm(filePath, { force: true }).catch(() => undefined);
        continue;
      }
      live.push(record);
    }

    const primary = await this.readPrimary();
    if (primary && this.isAlive(primary.pid)) {
      return primary;
    }

    const newest = live.sort((a, b) => b.startedAt.localeCompare(a.startedAt))[0] ?? null;
    if (!newest) {
      await rm(this.primaryPath, { force: true }).catch(() => undefined);
      return null;
    }
    await writeFile(this.primaryPath, `${JSON.stringify(newest, null, 2)}\n`, { mode: 0o600 });
    return newest;
  }
}
