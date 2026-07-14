import { describe, expect, test } from "bun:test";
import {
  ensureTarGzName,
  fileTypeLabel,
  formatFileSize,
  formatModifiedTime,
  inferName,
  isPermissionDenied,
  joinLocalPath,
  joinRemotePath,
  normalizeRemotePath,
  shellEscape
} from "./shared";

describe("FileExplorerPane shared helpers", () => {
  test("normalizes remote paths and joins path segments safely", () => {
    expect(normalizeRemotePath("")).toBe("/");
    expect(normalizeRemotePath("foo//bar/")).toBe("/foo/bar");
    expect(joinRemotePath("/", "logs")).toBe("/logs");
    expect(joinRemotePath("/var/", "/tmp/")).toBe("/var/tmp");
  });

  test("joins local paths without duplicating separators", () => {
    expect(joinLocalPath("/tmp", "foo.txt")).toBe("/tmp/foo.txt");
    expect(joinLocalPath("/tmp/", "foo.txt")).toBe("/tmp/foo.txt");
    expect(joinLocalPath("C:\\tmp\\", "foo.txt")).toBe("C:\\tmp\\foo.txt");
  });

  test("formats archive names and file labels", () => {
    expect(ensureTarGzName("backup")).toBe("backup.tar.gz");
    expect(ensureTarGzName("backup.tar.gz")).toBe("backup.tar.gz");
    expect(fileTypeLabel("directory")).toBe("文件夹");
    expect(fileTypeLabel("link")).toBe("链接");
    expect(fileTypeLabel("file")).toBe("文件");
  });

  test("formats file metadata for the table", () => {
    expect(formatFileSize(0, false)).toBe("0 B");
    expect(formatFileSize(1024, false)).toBe("1.0 KB");
    expect(formatFileSize(512, true)).toBe("");
    expect(formatModifiedTime("2026-03-15T01:02:03.000Z")).toMatch(/^2026\/03\/15 \d{2}:\d{2}$/);
    expect(formatModifiedTime("not-a-date")).toBe("NaN/NaN/NaN NaN:NaN");
  });

  test("supports remote shell actions", () => {
    expect(isPermissionDenied("Permission denied")).toBe(true);
    expect(isPermissionDenied("operation not permitted")).toBe(true);
    expect(isPermissionDenied("file missing")).toBe(false);
    expect(shellEscape("/tmp/it's.txt")).toBe("'/tmp/it'\\''s.txt'");
  });

  test("infers display names from paths", () => {
    expect(inferName("foo/bar.txt")).toBe("bar.txt");
    expect(inferName("C:\\foo\\bar.txt")).toBe("bar.txt");
    expect(inferName("")).toBe("file");
  });
});
