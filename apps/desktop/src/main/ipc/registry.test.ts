// Static shape test for the IPC invoke registry (run with: bun registry.test.ts).
// Verifies the table itself — no Electron runtime is involved.
import fs from "node:fs";
import { fileURLToPath } from "node:url";
import { IPCChannel } from "../../../../../packages/shared/src/index";
import { ipcInvokeRegistry } from "./registry";

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
    assert(entry.label.trim().length > 0, `entry for channel "${entry.channel}" has an empty label`);
  }
}

// 3. Every channel the preload invokes must have a handler in the registry.
//    The preload is scanned statically (it cannot be imported outside Electron).
{
  const preloadPath = fileURLToPath(new URL("../../preload/index.ts", import.meta.url));
  const preloadSource = fs.readFileSync(preloadPath, "utf8");
  const invokePattern = /\.invoke\(\s*IPCChannel\.(\w+)/g;
  const invokedNames = new Set<string>();
  for (let match = invokePattern.exec(preloadSource); match; match = invokePattern.exec(preloadSource)) {
    const name = match[1];
    if (name) {
      invokedNames.add(name);
    }
  }
  assert(invokedNames.size > 0, "no ipcRenderer.invoke(IPCChannel.*) calls found in preload — scan pattern broken?");

  const registered = new Set<string>(ipcInvokeRegistry.map((entry) => entry.channel));
  const missing: string[] = [];
  for (const name of invokedNames) {
    const channel = (IPCChannel as Record<string, string>)[name];
    assert(channel !== undefined, `preload invokes unknown IPCChannel member "${name}"`);
    if (channel !== undefined && !registered.has(channel)) {
      missing.push(name);
    }
  }
  assert(
    missing.length === 0,
    `preload invokes channels with no registry entry: ${missing.join(", ")}`
  );

  console.log(
    `registry.test: ${ipcInvokeRegistry.length} registry entries cover all ${invokedNames.size} preload invoke channels`
  );
}

console.log("registry.test: all assertions passed");
