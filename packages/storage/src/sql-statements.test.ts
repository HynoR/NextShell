import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, test } from "vitest";

/**
 * better-sqlite3 按 Electron ABI 编译,测试进程加载不了。这里直接读源码核对 SQL 形状:
 * 0.3.2 的 `33 values for 32 columns` 就是 INSERT 列清单漏了 folder_id,VALUES 却写了。
 */

const source = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "index.ts"), "utf8");

const splitSqlItems = (raw: string): string[] => {
  const items: string[] = [];
  let buf = "";
  let depth = 0;
  let quote: string | undefined;
  for (let i = 0; i < raw.length; i += 1) {
    const ch = raw[i]!;
    if (quote) {
      buf += ch;
      if (ch === quote) {
        quote = undefined;
      }
      continue;
    }
    if (ch === "'" || ch === '"' || ch === "`") {
      quote = ch;
      buf += ch;
      continue;
    }
    if (ch === "(") {
      depth += 1;
      buf += ch;
      continue;
    }
    if (ch === ")") {
      depth -= 1;
      buf += ch;
      continue;
    }
    if (ch === "," && depth === 0) {
      const item = buf.trim();
      if (item) {
        items.push(item);
      }
      buf = "";
      continue;
    }
    buf += ch;
  }
  const last = buf.trim();
  if (last) {
    items.push(last);
  }
  return items;
};

const parseStringArrayConst = (name: string): string[] => {
  const match = source.match(new RegExp(`const ${name} = \\[([\\s\\S]*?)\\] as const;`));
  expect(match, `${name} array const`).toBeTruthy();
  return [...match![1]!.matchAll(/"([a-z_]+)"/g)].map((item) => item[1]!);
};

const parseStringConst = (name: string): string => {
  const match = source.match(new RegExp(`const ${name} =\\s*"([^"]+)"`));
  expect(match, `${name} string const`).toBeTruthy();
  return match![1]!;
};

const interpolate = (raw: string): string =>
  raw.replace(/\$\{([A-Z_]+)\}/g, (_, name: string) => {
    if (name === "CONNECTION_COLUMNS" || name === "SSH_KEY_COLUMNS") {
      const listName = name.replace(/COLUMNS$/, "COLUMN_LIST");
      return parseStringArrayConst(listName).join(", ");
    }
    if (name === "CONNECTION_VALUES" || name === "SSH_KEY_VALUES") {
      const listName = name.replace(/VALUES$/, "COLUMN_LIST");
      return parseStringArrayConst(listName)
        .map((column) => `@${column}`)
        .join(", ");
    }
    if (name === "FOLDER_COLUMNS") {
      return parseStringConst("FOLDER_COLUMNS");
    }
    throw new Error(`unresolved SQL interpolation ${name}`);
  });

const parseInterfaceFields = (name: string): string[] => {
  const match = source.match(new RegExp(`interface ${name} \\{([\\s\\S]*?)\\n\\}`));
  expect(match, `${name} interface`).toBeTruthy();
  return [...match![1]!.matchAll(/^\s+([a-z_]+):/gm)].map((item) => item[1]!);
};

const parseNamedBindKeys = (fromIndex: number): string[] => {
  const run = source.slice(fromIndex).match(/\.run\(\{([\s\S]*?)\}\)/);
  expect(run, "bind object after INSERT").toBeTruthy();
  return [...run![1]!.matchAll(/^\s+([a-z_]+):/gm)].map((item) => item[1]!);
};

describe("SQL INSERT column/value alignment", () => {
  const inserts = [
    ...source.matchAll(/INSERT INTO ([A-Za-z0-9_]+) \(([\s\S]*?)\)\s*VALUES \(([\s\S]*?)\)/g)
  ];

  test("every INSERT lists the same number of columns and values", () => {
    expect(inserts.length).toBeGreaterThan(10);
    for (const match of inserts) {
      const table = match[1]!;
      const cols = splitSqlItems(interpolate(match[2]!));
      const vals = splitSqlItems(interpolate(match[3]!));
      expect(cols, `${table} columns`).toHaveLength(vals.length);
    }
  });

  test("connections upsert bind covers every declared column including folder_id", () => {
    const columns = parseStringArrayConst("CONNECTION_COLUMN_LIST");
    expect(columns).toContain("folder_id");
    expect(parseInterfaceFields("ConnectionRow")).toEqual(columns);

    const insertAt = source.indexOf("INSERT INTO connections (${CONNECTION_COLUMNS})");
    expect(insertAt).toBeGreaterThan(0);
    const bindKeys = parseNamedBindKeys(insertAt);
    expect(bindKeys.sort()).toEqual([...columns].sort());
  });

  test("ssh_keys upsert bind covers every declared column including key material", () => {
    const columns = parseStringArrayConst("SSH_KEY_COLUMN_LIST");
    expect(columns).toEqual(
      expect.arrayContaining(["key_type", "key_bits", "key_comment", "fingerprint", "public_key_line"])
    );
    expect(parseInterfaceFields("SshKeyRow")).toEqual(columns);

    const insertAt = source.indexOf("INSERT INTO ssh_keys (${SSH_KEY_COLUMNS})");
    expect(insertAt).toBeGreaterThan(0);
    const bindKeys = parseNamedBindKeys(insertAt);
    expect(bindKeys.sort()).toEqual([...columns].sort());
  });

  test("connection_folders create bind matches FOLDER_COLUMNS", () => {
    const columns = splitSqlItems(parseStringConst("FOLDER_COLUMNS"));
    expect(columns).toEqual([
      "id",
      "scope_key",
      "parent_id",
      "name",
      "sort_index",
      "created_at",
      "updated_at"
    ]);
    const insertAt = source.indexOf("INSERT INTO connection_folders (${FOLDER_COLUMNS})");
    expect(insertAt).toBeGreaterThan(0);
    const bindKeys = parseNamedBindKeys(insertAt);
    expect(bindKeys.sort()).toEqual([...columns].sort());
  });
});
