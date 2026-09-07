import { describe, expect, it } from "vitest";

import { matchDangerousCommand, matchSensitiveFile } from "./index";

describe("matchDangerousCommand", () => {
  it.each([
    "rm -rf /",
    "/bin/rm --recursive --force -- /",
    "mkfs.ext4 /dev/sda1",
    "dd if=/dev/zero of=/dev/sda bs=1M",
    "shutdown -h now",
    "reboot",
    ":(){ :|:& };:",
    "echo destroy > /dev/nvme0n1",
    "chmod -R 777 /",
    "kill -9 -1"
  ])("recognizes a dangerous command: %s", (command) => {
    expect(matchDangerousCommand(command)).not.toBeNull();
  });

  it.each([
    "r''m -r -f /",
    "/usr/bin/r\\m -fr -- /*",
    "sudo -- rm -rf //",
    "sudo -u root /sbin/mkfs.xfs /dev/sdb1",
    "dd 'of=/dev/disk0' if=/dev/zero",
    'printf x 2>"/dev/sda"',
    "command rm -rf /",
    "env LC_ALL=C rm -rf /",
    "busybox rm -rf /",
    "sh -c 'rm -rf /'"
  ])("does not allow a simple quoting or path bypass: %s", (command) => {
    expect(matchDangerousCommand(command)).not.toBeNull();
  });

  it.each([
    "ls -la /var/log",
    "du -sh -- * | sort -h | tail -20",
    "systemctl restart sshd",
    "touch /tmp/agent-test",
    "grep error /var/log/app.log 2>/dev/null",
    "rm -rf /tmp/agent-scratch",
    "dd if=./disk.img of=./copy.img",
    "kill -9 4242"
  ])("lets an unlisted command through: %s", (command) => {
    expect(matchDangerousCommand(command)).toBeNull();
  });
});

describe("matchSensitiveFile", () => {
  it("flags .env and .env.* wherever they appear", () => {
    expect(matchSensitiveFile("cat .env")).toBe(".env");
    expect(matchSensitiveFile("cat /srv/app/.env.production")).toBe("/srv/app/.env.production");
    expect(matchSensitiveFile("echo KEY=1 >> .env")).toBe(".env");
    expect(matchSensitiveFile("echo x >.env.local")).toBe(".env.local");
    expect(matchSensitiveFile("sed -i 's/a/b/' ./.env && ls")).toBe("./.env");
  });

  it("leaves look-alikes alone", () => {
    expect(matchSensitiveFile("env | grep PATH")).toBeNull();
    expect(matchSensitiveFile("cat environment.txt")).toBeNull();
    expect(matchSensitiveFile("ls .envrc")).toBeNull();
    expect(matchSensitiveFile("cat .env.example.md")).toBe(".env.example.md");
  });
});
