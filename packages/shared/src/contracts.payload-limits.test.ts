import {
  sessionWriteSchema,
  sftpDownloadPackedSchema,
  sftpEditWriteFileSchema,
  sftpTransferPackedSchema,
  sftpUploadPackedSchema
} from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

const makeUuid = (index: number): string =>
  `11111111-1111-4111-8111-${index.toString(16).padStart(12, "0")}`;

// ─── sessionWriteSchema: data 上限 1MB ──────────────────────────────────────

(() => {
  const parsed = sessionWriteSchema.safeParse({
    sessionId: makeUuid(1),
    data: "x".repeat(1024 * 1024)
  });
  assert(parsed.success, "sessionWriteSchema should accept 1MB data");
})();

(() => {
  const parsed = sessionWriteSchema.safeParse({
    sessionId: makeUuid(1),
    data: "x".repeat(1024 * 1024 + 1)
  });
  assert(!parsed.success, "sessionWriteSchema should reject data over 1MB");
})();

// ─── packed 传输: entryNames/localPaths 上限 500 ────────────────────────────

(() => {
  const parsed = sftpDownloadPackedSchema.safeParse({
    connectionId: makeUuid(1),
    remoteDir: "/var/www",
    entryNames: Array.from({ length: 501 }, (_, index) => `file-${index}.txt`),
    localDir: "/tmp"
  });
  assert(!parsed.success, "sftpDownloadPackedSchema should reject over 500 entryNames");
})();

(() => {
  const parsed = sftpUploadPackedSchema.safeParse({
    connectionId: makeUuid(1),
    localPaths: Array.from({ length: 501 }, (_, index) => `/tmp/file-${index}.txt`),
    remoteDir: "/var/www"
  });
  assert(!parsed.success, "sftpUploadPackedSchema should reject over 500 localPaths");
})();

(() => {
  const parsed = sftpTransferPackedSchema.safeParse({
    sourceConnectionId: makeUuid(1),
    sourceDir: "/var/src",
    entryNames: Array.from({ length: 501 }, (_, index) => `file-${index}.txt`),
    targetConnectionId: makeUuid(2),
    targetDir: "/var/dst"
  });
  assert(!parsed.success, "sftpTransferPackedSchema should reject over 500 entryNames");
})();

(() => {
  const parsed = sftpTransferPackedSchema.safeParse({
    sourceConnectionId: makeUuid(1),
    sourceDir: "/var/src",
    entryNames: ["a.txt", "dir-a"],
    targetConnectionId: makeUuid(2),
    targetDir: "/var/dst"
  });
  assert(parsed.success, "sftpTransferPackedSchema should accept payload within limits");
})();

// ─── sftpEditWriteFileSchema: 写入目标由调用方给出 + content 上限 10MB ───────

(() => {
  const parsed = sftpEditWriteFileSchema.safeParse({
    connectionId: makeUuid(1),
    remotePath: "/etc/nginx/nginx.conf",
    content: "hello"
  });
  assert(
    parsed.success,
    "sftpEditWriteFileSchema should accept connectionId + remotePath + content"
  );
})();

(() => {
  const parsed = sftpEditWriteFileSchema.safeParse({
    connectionId: makeUuid(1),
    remotePath: "/etc/nginx/nginx.conf",
    content: "x".repeat(10 * 1024 * 1024 + 1)
  });
  assert(!parsed.success, "sftpEditWriteFileSchema should reject content over 10MB");
})();

console.log("contracts.payload-limits.test: all assertions passed");
