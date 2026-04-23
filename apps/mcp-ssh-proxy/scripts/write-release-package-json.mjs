import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const currentDir = path.dirname(fileURLToPath(import.meta.url));
const appDir = path.resolve(currentDir, "..");
const repoRoot = path.resolve(appDir, "..", "..");
const distDir = path.join(appDir, "dist");

const readJson = async (filePath) => JSON.parse(await fs.readFile(filePath, "utf8"));

const appPackage = await readJson(path.join(appDir, "package.json"));
const storagePackage = await readJson(path.join(repoRoot, "packages", "storage", "package.json"));
const securityPackage = await readJson(path.join(repoRoot, "packages", "security", "package.json"));
const sshPackage = await readJson(path.join(repoRoot, "packages", "ssh", "package.json"));

const releasePackage = {
  name: "nextshell-mcp-ssh-proxy",
  version: appPackage.version,
  description: appPackage.description,
  type: "module",
  private: false,
  main: "./index.js",
  bin: {
    "nextshell-mcp-ssh-proxy": "./index.js"
  },
  files: [
    "index.js",
    "README.md"
  ],
  engines: {
    node: ">=20"
  },
  dependencies: {
    "@modelcontextprotocol/sdk": appPackage.dependencies["@modelcontextprotocol/sdk"],
    "better-sqlite3": storagePackage.dependencies["better-sqlite3"],
    "keytar": securityPackage.dependencies["keytar"],
    "socks": sshPackage.dependencies["socks"],
    "ssh2": appPackage.dependencies["ssh2"],
    "zod": appPackage.dependencies["zod"]
  }
};

await fs.mkdir(distDir, { recursive: true });
await fs.writeFile(
  path.join(distDir, "package.json"),
  `${JSON.stringify(releasePackage, null, 2)}\n`,
  "utf8"
);
await fs.copyFile(path.join(appDir, "README.md"), path.join(distDir, "README.md"));
