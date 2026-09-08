// Run with the packaged Electron binary and ELECTRON_RUN_AS_NODE=1.
import assert from "node:assert/strict";
import path from "node:path";
import { createRequire } from "node:module";

assert.equal(process.platform, "linux");
assert.ok(process.versions.electron, "Use the packaged Electron runtime");
assert.ok(process.argv[2], "Pass the packaged resources/app.asar path");
const archive = path.resolve(process.argv[2]);
const packagedRequire = createRequire(path.join(archive, "package.json"));
for (const name of ["better-sqlite3", "keytar", "ssh2", "node-pty"]) {
  assert.ok(
    packagedRequire.resolve(name).startsWith(`${archive}/`),
    `${name} must be included in app.asar, not resolved from the build workspace`
  );
}
const Database = packagedRequire("better-sqlite3");
const db = new Database(":memory:");
assert.deepEqual(db.prepare("SELECT 1 AS ok").get(), { ok: 1 });
db.close();
assert.equal(typeof packagedRequire("keytar").getPassword, "function");
assert.equal(typeof packagedRequire("ssh2").Client, "function");

const terminal = packagedRequire("node-pty").spawn("/bin/sh", ["-c", "printf nextshell-pty-ok"], {
  name: "xterm-256color",
  cols: 80,
  rows: 24,
  cwd: "/tmp",
  env: process.env
});
let output = "";
const timeout = setTimeout(() => {
  terminal.kill();
  console.error("Packaged PTY did not exit within 10 seconds");
  process.exit(1);
}, 10_000);
terminal.onData((data) => (output += data));
terminal.onExit(({ exitCode }) => {
  clearTimeout(timeout);
  assert.equal(exitCode, 0);
  assert.match(output, /nextshell-pty-ok/);
  console.log(`Packaged Linux ${process.arch}: SQLite, keytar, SSH and PTY checks passed`);
});
