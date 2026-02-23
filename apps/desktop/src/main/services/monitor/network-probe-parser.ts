import type { NetworkConnection, NetworkListener } from "../../../../../../packages/core/src/index";

export interface ParsedNetworkProbe {
  listeners: NetworkListener[];
  connections: NetworkConnection[];
}

const CONNECTION_STATES = new Set([
  "ESTAB",
  "ESTABLISHED",
  "TIME-WAIT",
  "TIME_WAIT",
  "CLOSE-WAIT",
  "CLOSE_WAIT",
  "SYN-SENT",
  "SYN_SENT",
  "SYN-RECV",
  "SYN_RECV",
  "FIN-WAIT-1",
  "FIN_WAIT1",
  "FIN-WAIT-2",
  "FIN_WAIT2",
  "LAST-ACK",
  "LAST_ACK",
]);

const parseAddress = (address: string): { ip: string; port: number } => {
  const cut = address.lastIndexOf(":");
  if (cut <= 0) {
    return { ip: "*", port: 0 };
  }

  const host = address.slice(0, cut).replace(/^\[/, "").replace(/\]$/, "");
  const port = Number.parseInt(address.slice(cut + 1) || "0", 10);

  return {
    ip: host || "*",
    port: Number.isFinite(port) ? port : 0,
  };
};

const normalizeLines = (stdout: string): string[] => {
  return stdout
    .replace(/\r\n/g, "\n")
    .replace(/\r(?!\n)/g, "\n")
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
};

const isSsState = (value: string): boolean => {
  return value === "LISTEN" || value === "UNCONN" || CONNECTION_STATES.has(value);
};

export const parseSsOutput = (stdout: string): ParsedNetworkProbe => {
  const lines = normalizeLines(stdout);
  const listeners: NetworkListener[] = [];
  const connections: NetworkConnection[] = [];
  const listenerMap = new Map<string, NetworkListener>();

  for (const line of lines) {
    const parts = line.replace(/\s+/g, " ").split(" ");
    if (parts.length < 5 || (parts[0] ?? "").toLowerCase() === "netid") {
      continue;
    }

    let state = "";
    let localAddr = "";
    let peerAddr = "";
    let extra = "";

    // default layout: Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
    if (parts.length >= 6 && isSsState(parts[1] ?? "")) {
      state = parts[1] ?? "";
      localAddr = parts[4] ?? "";
      peerAddr = parts[5] ?? "";
      extra = parts.slice(6).join(" ");
    } else if (isSsState(parts[0] ?? "")) {
      // fallback layout without Netid
      state = parts[0] ?? "";
      localAddr = parts[3] ?? "";
      peerAddr = parts[4] ?? "";
      extra = parts.slice(5).join(" ");
    } else {
      continue;
    }

    const pidMatch = extra.match(/pid=(\d+)/);
    const nameMatch = extra.match(/\("([^"]+)"/);
    const pid = pidMatch ? Number.parseInt(pidMatch[1] ?? "0", 10) : 0;
    const processName = nameMatch ? (nameMatch[1] ?? "unknown") : "unknown";

    const local = parseAddress(localAddr);
    const peer = parseAddress(peerAddr);
    const localIp = local.ip;
    const localPort = local.port;

    if (localPort <= 0) {
      continue;
    }

    if (state === "LISTEN" || state === "UNCONN") {
      const key = `${pid}:${localPort}`;
      const existing = listenerMap.get(key);
      if (existing) {
        existing.connectionCount += 1;
      } else {
        listenerMap.set(key, {
          pid,
          name: processName,
          listenIp: localIp,
          port: localPort,
          ipCount: 0,
          connectionCount: 0,
          uploadBytes: 0,
          downloadBytes: 0,
        });
      }
      continue;
    }

    if (!CONNECTION_STATES.has(state)) {
      continue;
    }

    const remoteIp = peer.ip !== "*" ? peer.ip : "0.0.0.0";
    const remotePort = peer.port;
    connections.push({
      localPort,
      remoteIp,
      remotePort,
      state,
      pid,
      processName,
    });

    for (const listener of listenerMap.values()) {
      if (listener.port === localPort) {
        listener.connectionCount += 1;
      }
    }
  }

  for (const listener of listenerMap.values()) {
    const uniqueIps = new Set(
      connections.filter((item) => item.localPort === listener.port).map((item) => item.remoteIp)
    );
    listener.ipCount = uniqueIps.size;
  }

  listeners.push(...listenerMap.values());
  return { listeners, connections };
};

export const parseNetstatOutput = (stdout: string): ParsedNetworkProbe => {
  const lines = normalizeLines(stdout);
  const listeners: NetworkListener[] = [];
  const connections: NetworkConnection[] = [];
  const listenerMap = new Map<string, NetworkListener>();

  for (const line of lines) {
    const normalizedLine = line.replace(/\s+/g, " ").trim();
    if (!normalizedLine || /^active\s+/i.test(normalizedLine)) {
      continue;
    }

    const parts = normalizedLine.split(" ");
    if (parts.length < 6) {
      continue;
    }

    const proto = (parts[0] ?? "").toLowerCase();
    if (!proto.startsWith("tcp") && !proto.startsWith("udp")) {
      continue;
    }

    // netstat -tunap: Proto RecvQ SendQ LocalAddr ForeignAddr State PID/Program
    const localAddr = parts[3] ?? "";
    const peerAddr = parts[4] ?? "";
    const maybeState = parts[5] ?? "";
    const hasStateColumn = maybeState !== "" && !maybeState.includes("/");
    const state = hasStateColumn ? maybeState : "";
    const pidProg = hasStateColumn ? (parts[6] ?? "") : maybeState;

    const pidMatch = pidProg.match(/^(\d+)\//);
    const pid = pidMatch ? Number.parseInt(pidMatch[1] ?? "0", 10) : 0;
    const processName = pidProg.includes("/") ? pidProg.split("/").slice(1).join("/") : "unknown";

    const local = parseAddress(localAddr);
    const localIp = local.ip;
    const localPort = local.port;

    if (localPort <= 0) {
      continue;
    }

    if (state === "LISTEN") {
      const key = `${pid}:${localPort}`;
      if (!listenerMap.has(key)) {
        listenerMap.set(key, {
          pid,
          name: processName,
          listenIp: localIp,
          port: localPort,
          ipCount: 0,
          connectionCount: 0,
          uploadBytes: 0,
          downloadBytes: 0,
        });
      }
      continue;
    }

    if (!state || !CONNECTION_STATES.has(state)) {
      continue;
    }

    const peer = parseAddress(peerAddr);
    const remoteIp = peer.ip !== "*" ? peer.ip : "0.0.0.0";
    const remotePort = peer.port;

    connections.push({
      localPort,
      remoteIp,
      remotePort,
      state,
      pid,
      processName,
    });
  }

  for (const listener of listenerMap.values()) {
    const listenerConnections = connections.filter((item) => item.localPort === listener.port);
    listener.connectionCount = listenerConnections.length;
    listener.ipCount = new Set(listenerConnections.map((item) => item.remoteIp)).size;
  }

  listeners.push(...listenerMap.values());
  return { listeners, connections };
};
