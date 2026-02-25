import {
  sftpDownloadPackedSchema,
  sftpUploadPackedSchema
} from "./contracts";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const parsed = sftpDownloadPackedSchema.safeParse({
    connectionId: "11111111-1111-4111-8111-111111111111",
    remoteDir: "/var/www",
    entryNames: ["a.txt", "b.log"],
    localDir: "/tmp",
    taskId: "22222222-2222-4222-8222-222222222222"
  });
  assert(parsed.success, "sftpDownloadPackedSchema should accept valid payload");
})();

(() => {
  const parsed = sftpDownloadPackedSchema.safeParse({
    connectionId: "11111111-1111-4111-8111-111111111111",
    remoteDir: "/var/www",
    entryNames: [],
    localDir: "/tmp"
  });
  assert(!parsed.success, "sftpDownloadPackedSchema should reject empty entryNames");
})();

(() => {
  const parsed = sftpDownloadPackedSchema.safeParse({
    connectionId: "11111111-1111-4111-8111-111111111111",
    remoteDir: "/var/www",
    entryNames: ["../passwd"],
    localDir: "/tmp"
  });
  assert(!parsed.success, "sftpDownloadPackedSchema should reject invalid entry names");
})();

(() => {
  const parsed = sftpUploadPackedSchema.safeParse({
    connectionId: "11111111-1111-4111-8111-111111111111",
    localPaths: ["/tmp/a.txt", "/tmp/b.log"],
    remoteDir: "/var/www",
    taskId: "33333333-3333-4333-8333-333333333333"
  });
  assert(parsed.success, "sftpUploadPackedSchema should accept valid payload");
})();

(() => {
  const parsed = sftpUploadPackedSchema.safeParse({
    connectionId: "11111111-1111-4111-8111-111111111111",
    localPaths: [],
    remoteDir: "/var/www"
  });
  assert(!parsed.success, "sftpUploadPackedSchema should reject empty localPaths");
})();
