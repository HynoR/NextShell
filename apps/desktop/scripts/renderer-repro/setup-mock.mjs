/* eslint-disable */
// Playwright MCP browser_run_code_unsafe script — NOT a normal module.
// FORMAT CONSTRAINT: the whole file must be one bare `async (page) => {...}`
// expression (the runner wraps it as `await (<file>)(page)`): no `export`,
// no statement after the closing brace, no trailing semicolon.
//
// What it does: registers a `window.nextshell` mock (init script, so it exists
// before main.tsx evaluates), then loads the real renderer from the vite dev
// server started with vite.renderer-repro.config.ts. The mock backs sessions
// with an in-page fake shell that echoes input, understands `seq N`, and emits
// OSC 133 / OSC 7 like a shell-integration-enabled remote.
//
// See RENDERER_PLAYWRIGHT_REPRO.md at the repo root for the full workflow.
async (page) => {
  const errors = [];
  page.on("pageerror", (err) => errors.push(`[pageerror] ${err.message}\n${err.stack ?? ""}`));
  // Survives into later browser_run_code_unsafe calls in the same MCP session.
  globalThis.__nsErrors = errors;

  await page.addInitScript(() => {
    const state = {
      shells: new Map(),
      dataListeners: new Set(),
      statusListeners: new Set(),
      // Matches the main-process dispatcher: deliveryIds are global across
      // all streams, not per session.
      nextDeliveryId: 1,
      ackLog: [],
      shellCounter: 0
    };
    window.__fakeState = state;
    const enc = new TextEncoder();

    const OSC = (s) => `\u001b]${s}\u0007`;

    function emitStatus(evt) {
      setTimeout(() => {
        for (const l of Array.from(state.statusListeners)) l(evt);
      }, 0);
    }

    function emitData(sessionId, text) {
      if (!text) return;
      // Async ~4KB frames with exact UTF-8 byteLength, mirroring how the real
      // IPC stream dispatcher carves and delivers chunks.
      setTimeout(() => {
        for (let i = 0; i < text.length; i += 4096) {
          const chunk = text.slice(i, i + 4096);
          const deliveryId = state.nextDeliveryId++;
          const byteLength = enc.encode(chunk).length;
          for (const l of Array.from(state.dataListeners)) {
            l({ sessionId, data: chunk, deliveryId, byteLength });
          }
        }
      }, 0);
    }

    class FakeShell {
      constructor(sessionId) {
        this.id = sessionId;
        this.n = ++state.shellCounter;
        this.lineBuf = "";
        this.promptSeen = false;
      }
      prompt() {
        // OSC 133 D/A/B + OSC 7, like the injected shell-integration script.
        const d = this.promptSeen ? OSC("133;D;0") : "";
        this.promptSeen = true;
        return `${d}${OSC("133;A")}${OSC("7;file://fake/home/user")}\r\n[fake-${this.n}] $ ${OSC("133;B")}`;
      }
      start() {
        emitData(this.id, `Welcome to fake shell #${this.n}\r\n${this.prompt()}`);
      }
      input(data) {
        let out = "";
        for (const ch of data) {
          if (ch === "\r" || ch === "\n") {
            const line = this.lineBuf;
            this.lineBuf = "";
            out += `\r\n${OSC("133;C")}`;
            const m = line.trim().match(/^seq (\d+)$/);
            if (m) {
              const n = Math.min(Number(m[1]), 5000);
              for (let i = 0; i < n; i++) out += `line ${i} of shell#${this.n} ..........\r\n`;
            } else if (line.trim()) {
              out += `shell#${this.n} ran: ${line}\r\n`;
            }
            out += this.prompt();
          } else if (ch === "\u007f") {
            if (this.lineBuf) {
              this.lineBuf = this.lineBuf.slice(0, -1);
              out += "\b \b";
            }
          } else {
            this.lineBuf += ch;
            out += ch;
          }
        }
        emitData(this.id, out);
      }
    }

    const now = new Date().toISOString();
    // monitorSession: true turns on the terminal compat guards / metadata
    // tracking paths for this connection, matching a monitor-enabled profile.
    const fakeConnection = {
      id: "fake-conn",
      name: "fake-local",
      host: "127.0.0.1",
      port: 2323,
      username: "test",
      authType: "agent",
      strictHostKeyChecking: false,
      terminalEncoding: "utf-8",
      backspaceMode: "ascii-backspace",
      deleteMode: "vt220-delete",
      groupPath: "/",
      tags: [],
      favorite: false,
      monitorSession: true,
      createdAt: now,
      updatedAt: now
    };

    const ok = Promise.resolve({ ok: true });
    // Agent endpoint is off in the repro: App.tsx polls `agent.status()` on
    // mount and subscribes to all four event streams, so every one of them has
    // to exist or the renderer throws before the workspace ever renders.
    const agentStatus = {
      enabled: false,
      listening: false,
      socketPath: null,
      tcpPort: null,
      token: null,
      endpointFilePath: "/tmp/fake/mcp/endpoint.json",
      clients: [],
      lastError: null,
      halted: false
    };
    const api = {
      getFilePathForDrop: () => "",
      platform: "darwin",
      ui: { titlebarSafeTop: 0 },
      connection: {
        list: async () => [fakeConnection],
        upsert: async (p) => ({ ...fakeConnection, ...p }),
        batchUpdateAuth: async () => ({ updated: 0, failed: [] }),
        remove: async () => ({ ok: true })
      },
      session: {
        open: async (payload) => {
          const sessionId = payload.sessionId ?? crypto.randomUUID();
          emitStatus({ sessionId, status: "connecting" });
          const shell = new FakeShell(sessionId);
          state.shells.set(sessionId, shell);
          const descriptor = {
            id: sessionId,
            target: "remote",
            connectionId: payload.connectionId ?? "fake-conn",
            title: `fake-local@127.0.0.1`,
            status: "connected",
            type: "terminal",
            createdAt: new Date().toISOString(),
            reconnectable: true
          };
          // The real main process starts streaming as soon as the channel is
          // up — before the renderer store learns the session id; mirror that
          // ordering so the pending-data adoption path gets exercised.
          await new Promise((r) => setTimeout(r, 20));
          shell.start();
          emitStatus({ sessionId, status: "connected" });
          await new Promise((r) => setTimeout(r, 5));
          return descriptor;
        },
        write: async (payload) => {
          const shell = state.shells.get(payload.sessionId);
          if (!shell) throw new Error("Session not found");
          shell.input(payload.data);
          return { ok: true };
        },
        resize: async () => ({ ok: true }),
        close: async (payload) => {
          state.shells.delete(payload.sessionId);
          emitStatus({ sessionId: payload.sessionId, status: "disconnected" });
          return { ok: true };
        },
        getHomeDir: async () => null,
        ackData: async (payload) => {
          // Inspect via window.__fakeState.ackLog to debug backpressure.
          state.ackLog.push(payload);
          return { ok: true };
        },
        onData: (l) => {
          state.dataListeners.add(l);
          return () => state.dataListeners.delete(l);
        },
        onStatus: (l) => {
          state.statusListeners.add(l);
          return () => state.statusListeners.delete(l);
        }
      },
      terminal: {
        showNotification: async () => ({ ok: true }),
        setProgress: async () => ({ ok: true }),
        onNotificationAction: () => () => {}
      },
      monitor: {
        getSystemInfoSnapshot: async () => ({}),
        startSystem: async () => ({ ok: true }),
        stopSystem: async () => ({ ok: true }),
        selectSystemInterface: async () => ({ ok: true }),
        onSystemData: () => () => {},
        startProcess: async () => ({ ok: true }),
        stopProcess: async () => ({ ok: true }),
        onProcessData: () => () => {},
        getProcessDetail: async () => ({}),
        killProcess: async () => ({ ok: true }),
        startNetwork: async () => ({ ok: true }),
        stopNetwork: async () => ({ ok: true }),
        onNetworkData: () => () => {},
        getNetworkConnections: async () => []
      },
      command: {
        exec: async () => ({ ok: true, stdout: "", stderr: "", exitCode: 0 }),
        execBatch: async () => ({ results: [] })
      },
      settings: {
        // Deliberate: the preferences store falls back to
        // DEFAULT_APP_PREFERENCES when settings.get rejects, so the mock does
        // not have to fabricate the full AppPreferences shape.
        get: async () => {
          throw new Error("mock: use defaults");
        },
        update: async () => {
          throw new Error("mock: read-only");
        }
      },
      dialog: {
        openFiles: async () => ({ canceled: true, filePaths: [] }),
        openDirectory: async () => ({ canceled: true }),
        openPath: async () => ({ ok: true })
      },
      sftp: {
        list: async () => [],
        listLocal: async () => [],
        upload: () => ok,
        download: () => ok,
        uploadPacked: () => ok,
        downloadPacked: async () => ({ ok: true, localArchivePath: "" }),
        transferPacked: () => ok,
        cancelTransfer: async () => ({ ok: true, cancelled: false }),
        mkdir: () => ok,
        rename: () => ok,
        remove: () => ok,
        editOpen: async () => ({ editId: "", localPath: "" }),
        editOpenBuiltin: async () => ({ editId: "", content: "" }),
        editSaveBuiltin: () => ok,
        editStop: () => ok,
        editStopAll: () => ok,
        editList: async () => [],
        onEditStatus: () => () => {},
        onTransferStatus: () => () => {}
      },
      commandHistory: {
        list: async () => [],
        push: async (p) => ({ id: "h1", command: p.command, usedAt: new Date().toISOString() }),
        remove: () => ok,
        clear: () => ok
      },
      savedCommand: {
        listScoped: async () => [],
        upsert: async (p) => p,
        remove: () => ok
      },
      cloudSync: {
        workspaceList: async () => [],
        workspaceAdd: async (p) => p,
        workspaceUpdate: async (p) => p,
        workspaceRemove: () => ok,
        workspaceExportToken: async () => ({ token: "" }),
        workspaceParseToken: async () => ({}),
        status: async () => ({ workspaces: [] }),
        syncNow: () => ok,
        listConflicts: async () => [],
        testConnection: async () => ({ ok: true }),
        resolveConflict: () => ok,
        onStatus: () => () => {},
        onApplied: () => () => {}
      },
      agent: {
        status: async () => agentStatus,
        enable: async () => agentStatus,
        disable: async () => agentStatus,
        rotateToken: async () => agentStatus,
        setHalted: async () => agentStatus,
        copyClientConfig: async () => ({ ok: true, command: "", json: "" }),
        installCursor: async () => ({ ok: true, deeplink: "cursor://mock" }),
        installClaudeDesktop: async () => ({ ok: true, configPath: "/tmp/fake/config.json" }),
        exportMcpb: async () => ({ ok: false, canceled: true }),
        respondPrompt: () => ok,
        onPrompt: () => () => {},
        onActivity: () => () => {},
        onSessionControl: () => () => {},
        onSessionFocus: () => () => {}
      },
      sshKey: { list: async () => [], upsert: async (p) => p, remove: () => ok },
      proxy: { list: async () => [], upsert: async (p) => p, remove: () => ok },
      about: { checkUpdate: async () => ({ status: "uptodate", currentVersion: "0.0.0" }) },
      ping: { probe: async () => ({ ok: false, error: "mock" }) },
      traceroute: { run: () => ok, stop: () => ok, onData: () => () => {} },
      debug: {
        enableLog: () => ok,
        disableLog: () => ok,
        reportRendererError: () => ok,
        onLogEvent: () => () => {}
      },
      resourceOps: { copyConnection: async () => fakeConnection },
      recycleBin: {
        list: async () => [],
        restore: async () => fakeConnection,
        purge: () => ok,
        clear: async () => ({ ok: true, deleted: 0 })
      }
    };
    window.nextshell = api;
  });

  await page.goto("http://localhost:5173/");
  await page.waitForTimeout(2500);
  const rootText = await page.evaluate(() => document.body.innerText.slice(0, 400));
  return { rootText, errors };
}
