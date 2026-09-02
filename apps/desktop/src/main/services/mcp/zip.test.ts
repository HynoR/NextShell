import { describe, expect, it } from "vitest";

import { crc32, createZipArchive } from "./zip";

describe("crc32", () => {
  it("matches the standard test vector", () => {
    // The canonical CRC-32 check value for the ASCII string "123456789".
    expect(crc32(Buffer.from("123456789"))).toBe(0xcbf43926);
  });

  it("returns 0 for empty input", () => {
    expect(crc32(Buffer.alloc(0))).toBe(0);
  });
});

describe("createZipArchive", () => {
  const fixedDate = new Date(2026, 0, 2, 3, 4, 6);

  it("produces a structurally valid single-entry archive", () => {
    const data = Buffer.from("hello world");
    const zip = createZipArchive([{ name: "manifest.json", data }], fixedDate);

    // Local file header
    expect(zip.readUInt32LE(0)).toBe(0x04034b50);
    expect(zip.readUInt16LE(8)).toBe(0); // store, no compression
    expect(zip.readUInt32LE(14)).toBe(crc32(data));
    expect(zip.readUInt32LE(18)).toBe(data.length);
    expect(zip.toString("utf8", 30, 30 + "manifest.json".length)).toBe("manifest.json");
    expect(
      zip.subarray(30 + "manifest.json".length, 30 + "manifest.json".length + data.length)
    ).toEqual(data);

    // End of central directory
    const eocdOffset = zip.length - 22;
    expect(zip.readUInt32LE(eocdOffset)).toBe(0x06054b50);
    expect(zip.readUInt16LE(eocdOffset + 10)).toBe(1); // total entries

    // Central directory sits where the EOCD says it does and matches the entry
    const centralOffset = zip.readUInt32LE(eocdOffset + 16);
    expect(zip.readUInt32LE(centralOffset)).toBe(0x02014b50);
    expect(zip.readUInt32LE(centralOffset + 42)).toBe(0); // local header offset
  });

  it("tracks offsets across multiple entries and stores unix modes", () => {
    const a = Buffer.from("aaa");
    const b = Buffer.from("bbbbbb");
    const zip = createZipArchive(
      [
        { name: "manifest.json", data: a },
        { name: "server/index.js", data: b, mode: 0o755 }
      ],
      fixedDate
    );

    const eocdOffset = zip.length - 22;
    expect(zip.readUInt16LE(eocdOffset + 10)).toBe(2);

    const centralOffset = zip.readUInt32LE(eocdOffset + 16);
    const firstNameLen = zip.readUInt16LE(centralOffset + 28);
    expect(firstNameLen).toBe("manifest.json".length);

    const second = centralOffset + 46 + firstNameLen;
    expect(zip.readUInt32LE(second)).toBe(0x02014b50);
    // The second entry's local header starts right after the first entry.
    expect(zip.readUInt32LE(second + 42)).toBe(30 + "manifest.json".length + a.length);
    // External attributes carry regular-file type + 0755.
    expect(zip.readUInt32LE(second + 38)).toBe(((0o100000 | 0o755) << 16) >>> 0);
    expect(zip.toString("utf8", second + 46, second + 46 + "server/index.js".length)).toBe(
      "server/index.js"
    );
  });
});
