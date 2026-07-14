// Static shape test for the IPC invoke registry (run with: bun registry.test.ts).
// Verifies the table itself — no Electron runtime is involved.
import fs from "node:fs";
import { fileURLToPath } from "node:url";
import { IPCChannel } from "../../../../../packages/shared/src/index";
import { ipcInvokeRegistry } from "./registry";

const expectedEventChannelNames = [
  "SessionData",
  "SessionStatus",
  "MonitorSystemData",
  "MonitorProcessData",
  "MonitorNetworkData",
  "SftpEditStatus",
  "SftpTransferStatus",
  "CloudSyncStatusEvent",
  "CloudSyncAppliedEvent",
  "TracerouteData",
  "DebugLogEvent"
] as const;

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(message);
  }
}

// 1. No duplicate channels — a duplicate would silently overwrite an earlier
//    ipcMain.handle registration.
{
  const seen = new Set<string>();
  for (const entry of ipcInvokeRegistry) {
    assert(!seen.has(entry.channel), `duplicate registry entry for channel "${entry.channel}"`);
    seen.add(entry.channel);
  }
}

// 2. Every entry with a schema must carry a non-empty label (it is the
//    user-facing validation error prefix).
for (const entry of ipcInvokeRegistry) {
  if (entry.schema !== null) {
    assert(
      entry.label.trim().length > 0,
      `entry for channel "${entry.channel}" has an empty label`
    );
  }
}

const collectTypeScriptFiles = (directory: string): string[] => {
  const files: string[] = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const fullPath = `${directory}/${entry.name}`;
    if (entry.isDirectory()) {
      files.push(...collectTypeScriptFiles(fullPath));
    } else if (entry.isFile() && entry.name.endsWith(".ts") && !entry.name.endsWith(".test.ts")) {
      files.push(fullPath);
    }
  }
  return files;
};

const assertSameNames = (
  actual: Set<string>,
  expected: ReadonlySet<string>,
  label: string
): void => {
  const missing = [...expected].filter((name) => !actual.has(name));
  const extra = [...actual].filter((name) => !expected.has(name));
  assert(
    missing.length === 0 && extra.length === 0,
    `${label} mismatch; missing: ${missing.join(", ") || "none"}; extra: ${extra.join(", ") || "none"}`
  );
};

// 3. The preload invoke surface and registry must match in both directions.
//    The preload is scanned statically (it cannot be imported outside Electron).
{
  const preloadPath = fileURLToPath(new URL("../../preload/index.ts", import.meta.url));
  const preloadSource = fs.readFileSync(preloadPath, "utf8");
  const invokePattern = /\binvoke\(\s*IPCChannel\.(\w+)/g;
  const invokedNames = new Set<string>();
  for (
    let match = invokePattern.exec(preloadSource);
    match;
    match = invokePattern.exec(preloadSource)
  ) {
    const name = match[1];
    if (name) {
      invokedNames.add(name);
    }
  }
  assert(
    invokedNames.size > 0,
    "no typed invoke(IPCChannel.*) calls found in preload — scan pattern broken?"
  );

  const registered = new Set<string>(ipcInvokeRegistry.map((entry) => entry.channel));
  const registeredNames = new Set<string>();
  for (const [name, channel] of Object.entries(IPCChannel)) {
    if (registered.has(channel)) {
      registeredNames.add(name);
    }
  }

  const missingHandlers: string[] = [];
  for (const name of invokedNames) {
    const channel = (IPCChannel as Record<string, string>)[name];
    assert(channel !== undefined, `preload invokes unknown IPCChannel member "${name}"`);
    if (channel !== undefined && !registered.has(channel)) {
      missingHandlers.push(name);
    }
  }
  assert(
    missingHandlers.length === 0,
    `preload invokes channels with no registry entry: ${missingHandlers.join(", ")}`
  );
  assertSameNames(registeredNames, invokedNames, "invoke registry/preload");

  console.log(
    `registry.test: ${ipcInvokeRegistry.length} registry entries cover all ${invokedNames.size} preload invoke channels`
  );
}

// 4. One-way event channels must stay aligned with preload subscriptions, and
//    each expected event channel must still appear in non-test main-process code.
{
  const preloadPath = fileURLToPath(new URL("../../preload/index.ts", import.meta.url));
  const preloadSource = fs.readFileSync(preloadPath, "utf8");
  const subscriptionPattern = /ipcRenderer\.on\(\s*IPCChannel\.(\w+)/g;
  const subscribedNames = new Set<string>();
  for (
    let match = subscriptionPattern.exec(preloadSource);
    match;
    match = subscriptionPattern.exec(preloadSource)
  ) {
    const name = match[1];
    if (name) {
      subscribedNames.add(name);
    }
  }

  const expectedNames = new Set<string>(expectedEventChannelNames);
  assertSameNames(subscribedNames, expectedNames, "event channel subscriptions");

  const mainDirectory = fileURLToPath(new URL("..", import.meta.url));
  const mainChannelNames = new Set<string>();
  const channelPattern = /IPCChannel\.(\w+)/g;
  for (const filePath of collectTypeScriptFiles(mainDirectory)) {
    const source = fs.readFileSync(filePath, "utf8");
    for (let match = channelPattern.exec(source); match; match = channelPattern.exec(source)) {
      const name = match[1];
      if (name) {
        mainChannelNames.add(name);
      }
    }
  }

  const missingMainReferences = expectedEventChannelNames.filter(
    (name) => !mainChannelNames.has(name)
  );
  assert(
    missingMainReferences.length === 0,
    `event channels missing from main-process code: ${missingMainReferences.join(", ")}`
  );
  console.log(`registry.test: ${expectedEventChannelNames.length} event channels are aligned`);
}

console.log("registry.test: all assertions passed");
