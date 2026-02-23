import { parseNetstatOutput, parseSsOutput } from "./network-probe-parser";

const assertTrue = (value: unknown, message: string): void => {
  if (!value) {
    throw new Error(message);
  }
};

const assertEqual = <T>(actual: T, expected: T, message: string): void => {
  if (actual !== expected) {
    throw new Error(`${message}: expected ${String(expected)}, got ${String(actual)}`);
  }
};

(() => {
  const parsed = parseSsOutput([
    "Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port Process",
    'tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=100,fd=3))',
    'tcp ESTAB 0 0 10.0.0.1:22 10.0.0.2:50123 users:(("sshd",pid=100,fd=4))',
  ].join("\n"));

  assertEqual(parsed.listeners.length, 1, "ss parser should parse listener rows");
  assertEqual(parsed.connections.length, 1, "ss parser should parse connection rows");
  assertEqual(parsed.listeners[0]?.pid, 100, "ss parser should parse listener pid");
  assertEqual(parsed.listeners[0]?.port, 22, "ss parser should parse listener port");
  assertEqual(parsed.listeners[0]?.connectionCount, 1, "ss parser should attach connection count");
  assertEqual(parsed.listeners[0]?.ipCount, 1, "ss parser should attach unique ip count");
  assertEqual(parsed.connections[0]?.remoteIp, "10.0.0.2", "ss parser should parse remote ip");
})();

(() => {
  const parsed = parseNetstatOutput([
    "Active Internet connections (only servers)",
    "garbage line that should be ignored",
    "Proto Recv-Q Send-Q Local Address Foreign Address State PID/Program name",
    "tcp 0 0 0.0.0.0:80 0.0.0.0:* LISTEN 99/nginx",
    "tcp 0 0 10.0.0.1:80 10.0.0.3:53000 ESTABLISHED 99/nginx",
    "line with too few fields",
  ].join("\n"));

  assertEqual(parsed.listeners.length, 1, "netstat parser should parse listener rows");
  assertEqual(parsed.connections.length, 1, "netstat parser should parse connection rows");
  assertEqual(parsed.listeners[0]?.name, "nginx", "netstat parser should parse process name");
  assertEqual(parsed.listeners[0]?.connectionCount, 1, "netstat parser should aggregate connection count");
  assertEqual(parsed.connections[0]?.remotePort, 53000, "netstat parser should parse remote port");
})();

(() => {
  const parsed = parseNetstatOutput([
    "Proto Recv-Q Send-Q Local Address Foreign Address State PID/Program name",
    "tcp6 0 0 :::443 :::* LISTEN -",
    "tcp 0 0 127.0.0.1:8080 127.0.0.1:51666 ESTABLISHED -",
    "udp 0 0 0.0.0.0:68 0.0.0.0:* -",
  ].join("\n"));

  assertEqual(parsed.listeners.length, 1, "missing PID should not block listener parsing");
  assertEqual(parsed.listeners[0]?.pid, 0, "missing PID should fallback to 0");
  assertEqual(parsed.listeners[0]?.name, "unknown", "missing process name should fallback to unknown");
  assertEqual(parsed.connections.length, 1, "missing PID should not block connection parsing");
  assertEqual(parsed.connections[0]?.pid, 0, "missing PID in connection should fallback to 0");
  assertEqual(parsed.connections[0]?.processName, "unknown", "missing process name should fallback to unknown");
  assertTrue(parsed.connections[0]?.remotePort === 51666, "connection remote port should still be parsed");
})();
