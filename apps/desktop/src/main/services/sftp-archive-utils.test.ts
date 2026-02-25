import {
  buildRemoteTarCreateCommand,
  buildRemoteTarExtractCommand,
  buildRemoteRemoveFileCommand,
  normalizeArchiveName,
  normalizeRemoteEntryNames,
  shellEscape
} from "./sftp-archive-utils";

const assert = (condition: boolean, message: string): void => {
  if (!condition) {
    throw new Error(message);
  }
};

(() => {
  const escaped = shellEscape("/tmp/a'b c");
  assert(escaped === "'/tmp/a'\\''b c'", "shellEscape should escape single quote");
})();

(() => {
  const archive = normalizeArchiveName(" backup 2026 ", "fallback");
  assert(archive === "backup 2026.tar.gz", "normalizeArchiveName should append tar.gz");
})();

(() => {
  const archive = normalizeArchiveName("bad:/name?.tgz", "fallback");
  assert(archive === "bad-name-.tar.gz", "normalizeArchiveName should sanitize invalid chars");
})();

(() => {
  const names = normalizeRemoteEntryNames([" a.txt ", "b.log"]);
  assert(names.length === 2, "normalizeRemoteEntryNames should keep valid names");
  assert(names[0] === "a.txt", "normalizeRemoteEntryNames should trim names");
})();

(() => {
  let thrown = false;
  try {
    normalizeRemoteEntryNames(["../etc/passwd"]);
  } catch {
    thrown = true;
  }
  assert(thrown, "normalizeRemoteEntryNames should reject invalid names");
})();

(() => {
  const command = buildRemoteTarCreateCommand("/var/www", "/tmp/bundle.tar.gz", ["a.txt", "b.log"]);
  assert(
    command === "tar -C '/var/www' -czf '/tmp/bundle.tar.gz' -- 'a.txt' 'b.log'",
    "buildRemoteTarCreateCommand should match expected command"
  );
})();

(() => {
  const command = buildRemoteTarExtractCommand("/tmp/u.tar.gz", "/opt/app");
  assert(
    command === "mkdir -p '/opt/app' && tar -xzf '/tmp/u.tar.gz' -C '/opt/app'",
    "buildRemoteTarExtractCommand should match expected command"
  );
})();

(() => {
  const command = buildRemoteRemoveFileCommand("/tmp/u.tar.gz");
  assert(command === "rm -f '/tmp/u.tar.gz'", "buildRemoteRemoveFileCommand should match expected command");
})();
