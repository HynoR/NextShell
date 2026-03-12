import { spawn, execFileSync, type ChildProcess } from "node:child_process";
import type { WebContents } from "electron";
import type { PingResult, TracerouteEvent, UpdateCheckResult } from "@nextshell/shared";
import { IPCChannel } from "@nextshell/shared";
import type { CachedConnectionRepository } from "@nextshell/storage";
import {
  normalizeGithubRepo,
  parseComparableVersion,
  compareCoreSegments,
  comparePrerelease,
} from "./container-utils";
import { logger } from "../logger";

interface NetworkToolServiceOptions {
  connections: CachedConnectionRepository;
}

export class NetworkToolService {
  private readonly connections: CachedConnectionRepository;
  private activeTracerouteProcess: ChildProcess | null = null;

  constructor(options: NetworkToolServiceOptions) {
    this.connections = options.connections;
  }

  compareVersions(a: string, b: string): number {
    const parsedA = parseComparableVersion(a);
    const parsedB = parseComparableVersion(b);
    if (parsedA && parsedB) {
      const coreCompare = compareCoreSegments(parsedA.core, parsedB.core);
      if (coreCompare !== 0) {
        return coreCompare;
      }
      return comparePrerelease(parsedA.prerelease, parsedB.prerelease);
    }
    return a.localeCompare(b, "en", { numeric: true, sensitivity: "base" });
  }

  async checkForUpdate(): Promise<UpdateCheckResult> {
    const githubRepo = normalizeGithubRepo(process.env["VITE_GITHUB_REPO"] ?? "");
    const currentVersion = process.env["VITE_APP_VERSION"] ?? "0.0.0";
    if (!githubRepo) {
      return { currentVersion, latestVersion: null, hasUpdate: false, releaseUrl: null, error: "未配置或配置了无效的 GitHub 仓库" };
    }
    try {
      const response = await fetch(`https://api.github.com/repos/${githubRepo}/releases/latest`, {
        headers: { Accept: "application/vnd.github.v3+json", "User-Agent": "NextShell-UpdateChecker" },
      });
      if (!response.ok) {
        if (response.status === 404) {
          return { currentVersion, latestVersion: null, hasUpdate: false, releaseUrl: `https://github.com/${githubRepo}/releases`, error: "未找到任何 Release" };
        }
        throw new Error(`GitHub API 返回 ${response.status}`);
      }
      const data = (await response.json()) as { tag_name?: string; html_url?: string };
      const latestVersion = data.tag_name ?? null;
      if (!latestVersion) {
        return { currentVersion, latestVersion: null, hasUpdate: false, releaseUrl: `https://github.com/${githubRepo}/releases`, error: "Release 缺少 tag" };
      }
      return {
        currentVersion,
        latestVersion,
        hasUpdate: this.compareVersions(latestVersion, currentVersion) > 0,
        releaseUrl: data.html_url ?? `https://github.com/${githubRepo}/releases`,
        error: null,
      };
    } catch (error) {
      logger.error("[UpdateCheck]", error);
      return { currentVersion, latestVersion: null, hasUpdate: false, releaseUrl: null, error: error instanceof Error ? error.message : "检查更新失败" };
    }
  }

  async pingHost(host: string): Promise<PingResult> {
    try {
      const ping = await import("ping");
      const res = await ping.promise.probe(host, { timeout: 3 });
      if (res.alive && typeof res.time === "number") {
        return { ok: true, avgMs: res.time };
      }
      return { ok: false, error: (res as { output?: string }).output ?? "不可达" };
    } catch (err) {
      const message = err instanceof Error ? err.message : "Ping 失败";
      return { ok: false, error: message };
    }
  }

  resolveNexttrace(): string {
    const prefs = this.connections.getAppPreferences();
    const configured = prefs.traceroute.nexttracePath.trim();
    if (configured) {
      return configured;
    }
    const cmd = process.platform === "win32" ? "where" : "which";
    try {
      return execFileSync(cmd, ["nexttrace"], { encoding: "utf-8" }).trim().split(/\r?\n/)[0]!;
    } catch {
      throw new Error("未找到 nexttrace，请在设置 > 网络工具中配置路径，或确保 nexttrace 已安装到 PATH。");
    }
  }

  async tracerouteRun(host: string, sender: WebContents): Promise<{ ok: true }> {
    if (this.activeTracerouteProcess) {
      this.activeTracerouteProcess.kill();
      this.activeTracerouteProcess = null;
    }
    const bin = this.resolveNexttrace();
    const prefs = this.connections.getAppPreferences().traceroute;
    const args: string[] = [];
    if (prefs.protocol === "tcp") {
      args.push("--tcp");
    } else if (prefs.protocol === "udp") {
      args.push("--udp");
    }
    if ((prefs.protocol === "tcp" || prefs.protocol === "udp") && prefs.port > 0) {
      args.push("--port", String(prefs.port));
    }
    if (prefs.ipVersion === "ipv4") {
      args.push("--ipv4");
    } else if (prefs.ipVersion === "ipv6") {
      args.push("--ipv6");
    }
    if (prefs.queries !== 3) {
      args.push("--queries", String(prefs.queries));
    }
    if (prefs.maxHops !== 30) {
      args.push("--max-hops", String(prefs.maxHops));
    }
    if (prefs.dataProvider !== "LeoMoeAPI") {
      args.push("--data-provider", prefs.dataProvider);
    }
    if (prefs.noRdns) {
      args.push("--no-rdns");
    }
    if (prefs.language !== "cn") {
      args.push("--language", prefs.language);
    }
    if (prefs.powProvider !== "api.nxtrace.org") {
      args.push("--pow-provider", prefs.powProvider);
    }
    args.push(host);

    const child = spawn(bin, args, { stdio: ["ignore", "pipe", "pipe"] });
    this.activeTracerouteProcess = child;

    const pendingTracerouteEvents: TracerouteEvent[] = [];
    let tracerouteFlushTimer: ReturnType<typeof setTimeout> | undefined;

    const flushTracerouteEvents = (): void => {
      tracerouteFlushTimer = undefined;
      if (pendingTracerouteEvents.length === 0 || sender.isDestroyed()) return;
      const batch = pendingTracerouteEvents.splice(0);
      for (const event of batch) {
        sender.send(IPCChannel.TracerouteData, event);
      }
    };

    const sendEvent = (event: TracerouteEvent): void => {
      if (sender.isDestroyed()) return;
      if (event.type === "done" || event.type === "error") {
        if (tracerouteFlushTimer) {
          clearTimeout(tracerouteFlushTimer);
          tracerouteFlushTimer = undefined;
        }
        const batch = pendingTracerouteEvents.splice(0);
        for (const e of batch) {
          sender.send(IPCChannel.TracerouteData, e);
        }
        sender.send(IPCChannel.TracerouteData, event);
        return;
      }
      pendingTracerouteEvents.push(event);
      if (!tracerouteFlushTimer) {
        tracerouteFlushTimer = setTimeout(flushTracerouteEvents, 50);
      }
    };

    let stdoutBuffer = "";
    child.stdout?.on("data", (chunk: Buffer) => {
      stdoutBuffer += chunk.toString("utf-8");
      const lines = stdoutBuffer.split(/\r?\n/);
      stdoutBuffer = lines.pop() ?? "";
      for (const line of lines) {
        sendEvent({ type: "data", line });
      }
    });

    let stderrBuffer = "";
    child.stderr?.on("data", (chunk: Buffer) => {
      stderrBuffer += chunk.toString("utf-8");
      const lines = stderrBuffer.split(/\r?\n/);
      stderrBuffer = lines.pop() ?? "";
      for (const line of lines) {
        sendEvent({ type: "data", line });
      }
    });

    child.on("error", (err) => {
      sendEvent({ type: "error", message: err.message });
      child.removeAllListeners();
      if (this.activeTracerouteProcess === child) {
        this.activeTracerouteProcess = null;
      }
    });

    child.on("close", (code) => {
      if (stdoutBuffer) {
        sendEvent({ type: "data", line: stdoutBuffer });
      }
      if (stderrBuffer) {
        sendEvent({ type: "data", line: stderrBuffer });
      }
      sendEvent({ type: "done", exitCode: code });
      child.removeAllListeners();
      if (this.activeTracerouteProcess === child) {
        this.activeTracerouteProcess = null;
      }
    });

    return { ok: true };
  }

  tracerouteStop(): { ok: true } {
    if (this.activeTracerouteProcess) {
      this.activeTracerouteProcess.removeAllListeners();
      this.activeTracerouteProcess.kill();
      this.activeTracerouteProcess = null;
    }
    return { ok: true };
  }
}
