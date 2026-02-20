import path from "node:path";
import { readFileSync } from "node:fs";
import { execSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import electron from "vite-plugin-electron/simple";
import { defineConfig } from "vite";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const resolveBaseVersion = (): string => {
  const parsed = JSON.parse(readFileSync(path.resolve(__dirname, "package.json"), "utf-8")) as {
    version?: string;
  };
  const version = parsed.version?.trim();
  return version && version.length > 0 ? version : "0.0.0";
};

const resolveGitShortSha = (): string | undefined => {
  try {
    const shortSha = execSync("git rev-parse --short HEAD", {
      cwd: __dirname,
      stdio: ["ignore", "pipe", "ignore"]
    }).toString("utf-8").trim();
    return shortSha.length > 0 ? shortSha : undefined;
  } catch {
    return undefined;
  }
};

const resolveBuildVersion = (): string => {
  const envVersion = process.env["NEXTSHELL_BUILD_VERSION"]?.trim();
  if (envVersion) {
    return envVersion;
  }
  const baseVersion = resolveBaseVersion();
  const shortSha = resolveGitShortSha() ?? "unknown";
  return `${baseVersion}-dev+${shortSha}`;
};

const appVersion = resolveBuildVersion();
const githubRepo = (process.env["NEXTSHELL_GITHUB_REPO"] ?? process.env["VITE_GITHUB_REPO"] ?? "HynoR/NextShell").trim();
const pkg = (name: string) => path.resolve(__dirname, "../../packages", name, "src");
const aliases = {
  "@nextshell/core": path.join(pkg("core"), "index.ts"),
  "@nextshell/shared": path.join(pkg("shared"), "index.ts"),
  "@nextshell/storage": path.join(pkg("storage"), "index.ts"),
  "@nextshell/security": path.join(pkg("security"), "index.ts"),
  "@nextshell/ssh": path.join(pkg("ssh"), "index.ts"),
  "@nextshell/terminal": path.join(pkg("terminal"), "index.ts"),
  "@nextshell/ui-kit": path.join(pkg("ui-kit"), "index.ts")
};

export default defineConfig({
  plugins: [
    tailwindcss(),
    react(),
    electron({
      main: {
        entry: "src/main/index.ts",
        vite: {
          define: {
            "process.env.VITE_GITHUB_REPO": JSON.stringify(githubRepo),
            "process.env.VITE_APP_VERSION": JSON.stringify(appVersion)
          },
          resolve: {
            alias: aliases
          },
          build: {
            rollupOptions: {
              output: {
                entryFileNames: "main/[name].js",
                chunkFileNames: "main/[name].js",
                assetFileNames: "main/[name].[ext]"
              },
              external: (id) =>
                id === "ssh2" ||
                id.startsWith("ssh2/") ||
                id.includes("/ssh2/") ||
                id === "better-sqlite3" ||
                id.startsWith("better-sqlite3/") ||
                id.includes("/better-sqlite3/") ||
                id === "electron-log" ||
                id.startsWith("electron-log/") ||
                id.includes("/electron-log/") ||
                id === "keytar" ||
                id.startsWith("keytar/") ||
                id.includes("/keytar/") ||
                id.endsWith(".node")
            }
          }
        }
      },
      preload: {
        input: path.join(__dirname, "src/preload/index.ts"),
        vite: {
          resolve: {
            alias: aliases
          },
          build: {
            rollupOptions: {
              output: {
                entryFileNames: "preload/[name].mjs",
                chunkFileNames: "preload/[name].mjs",
                assetFileNames: "preload/[name].[ext]"
              }
            }
          }
        }
      }
    })
  ],
  resolve: {
    alias: aliases
  },
  define: {
    __APP_VERSION__: JSON.stringify(appVersion),
    __GITHUB_REPO__: JSON.stringify(githubRepo)
  },
  server: {
    port: 5173,
    strictPort: true
  }
});
