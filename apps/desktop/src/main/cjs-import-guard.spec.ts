import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, test } from "vitest";

/**
 * 主进程打包成 ESM。从 CommonJS 依赖里做具名值导入时，Node 的 ESM loader 靠 cjs-module-lexer
 * 静态探测可用的具名导出；探不到的那个名字会在**启动时**炸：
 *
 *   SyntaxError: Named export 'utils' not found. The requested module 'ssh2' is a CommonJS module
 *
 * 这是逐个导出的事，不是整个模块的事——同一个 ssh2 里 `Client` 探得到、`utils` 探不到。
 * typecheck、`bun run build` 都发现不了（打包器乐意原样保留这行导入），**在 vitest 里也发现不了**：
 * vitest 的 CJS interop 会把整个 module.exports 摊平，`utils` 在它眼里是存在的。
 * 所以这里必须起一个真实的 node 子进程，用 ESM loader 自己的视角去验。
 *
 * 探不到时正确的写法是 `createRequire(import.meta.url)`，见 `packages/ssh/src/index.ts` 的
 * `loadSsh2` 与 `packages/ssh/src/key-material.ts` 的 `loadSsh2Utils`。
 */
const CJS_NATIVE_MODULES = ["ssh2", "better-sqlite3", "keytar", "node-pty", "socks"];

// apps/desktop/src/main → 仓库根
const REPO_ROOT = join(import.meta.dirname, "../../../..");
const ROOTS = [join(REPO_ROOT, "apps/desktop/src"), join(REPO_ROOT, "packages")];
// 从这里解析模块，和主进程运行时的解析路径一致。
const RESOLVE_FROM = join(REPO_ROOT, "apps/desktop");

const collectSourceFiles = (dir: string): string[] => {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry === "dist" || entry.startsWith(".")) {
      continue;
    }
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      out.push(...collectSourceFiles(full));
    } else if (/\.tsx?$/.test(entry)) {
      out.push(full);
    }
  }
  return out;
};

/** 收集 `import { a, b as c } from "<module>"` 里的具名绑定；`import type` 会被擦除，跳过。 */
const collectNamedImports = (moduleName: string): Map<string, string[]> => {
  const pattern = new RegExp(
    String.raw`import\s+(?!type\b)\{([^}]*)\}\s*from\s*["']${moduleName}["']`,
    "g"
  );
  const byName = new Map<string, string[]>();

  for (const file of ROOTS.flatMap(collectSourceFiles)) {
    if (/\.(spec|test)\.tsx?$/.test(file)) {
      continue;
    }
    for (const match of readFileSync(file, "utf-8").matchAll(pattern)) {
      for (const clause of (match[1] ?? "").split(",")) {
        const imported = clause.trim().split(/\s+as\s+/)[0]?.trim();
        // 具名子句里也可能混着 `type Foo`，同样擦除，跳过。
        if (!imported || imported.startsWith("type ")) {
          continue;
        }
        const files = byName.get(imported) ?? [];
        files.push(file.replace(REPO_ROOT, ""));
        byName.set(imported, files);
      }
    }
  }
  return byName;
};

type ProbeResult = "ok" | "missing" | "unverifiable";

/**
 * 让真实的 node 去 `import { name } from "module"`。
 * 只有 "Named export … not found" 算失败；模块整体加载不了（原生模块按 Electron ABI 编译）
 * 时无法在这里判定，记为 unverifiable。
 */
const probeNamedExport = (moduleName: string, exportName: string): ProbeResult => {
  try {
    execFileSync(
      process.execPath,
      ["--input-type=module", "-e", `import { ${exportName} } from ${JSON.stringify(moduleName)};`],
      { cwd: RESOLVE_FROM, stdio: "pipe" }
    );
    return "ok";
  } catch (error) {
    const output = String((error as { stderr?: Buffer }).stderr ?? error);
    return output.includes("Named export") ? "missing" : "unverifiable";
  }
};

describe("CommonJS named imports resolve under the ESM loader", () => {
  test.each(CJS_NATIVE_MODULES)("%s", (moduleName) => {
    const used = collectNamedImports(moduleName);
    const missing = [...used.entries()]
      .filter(([name]) => probeNamedExport(moduleName, name) === "missing")
      .map(([name, files]) => `${name} (${files.join(", ")})`);

    expect(
      missing,
      "这些名字 Node 的 ESM loader 探测不到，Electron 启动时会抛 Named export not found；改用 createRequire。"
    ).toEqual([]);
  });
});
