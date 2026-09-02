import fsp from "node:fs/promises";
import { describe, expect, test, vi } from "vitest";
import type { WebContents } from "electron";
import type { SshConnection } from "../../../../../packages/ssh/src/index";
import type { SftpEditStatusEvent } from "../../../../../packages/shared/src/index";
import { IPCChannel } from "../../../../../packages/shared/src/index";
import { RemoteEditManager } from "./remote-edit-manager";

const CONNECTION_ID = "11111111-1111-4111-8111-111111111111";
const REMOTE_PATH = "/etc/nginx/nginx.conf";

interface MockConnection {
  uploads: Array<{ localPath: string; remotePath: string }>;
  failUpload: boolean;
  written: Array<{ remotePath: string; content: string }>;
}

const createMockConnection = (): MockConnection & SshConnection => {
  const mock: MockConnection = {
    uploads: [],
    failUpload: false,
    written: []
  };
  const connection = {
    stat: async () => ({ size: 16 }),
    download: async (_remotePath: string, localPath: string) => {
      await fsp.writeFile(localPath, "worker_connections 512;");
    },
    upload: async (localPath: string, remotePath: string) => {
      if (mock.failUpload) {
        throw new Error("sftp write denied");
      }
      mock.uploads.push({ localPath, remotePath });
    },
    readFileContent: async () => Buffer.from("worker_connections 512;", "utf-8"),
    writeFileContent: async (remotePath: string, content: Buffer) => {
      mock.written.push({ remotePath, content: content.toString("utf-8") });
    }
  };
  return Object.assign(mock, connection) as unknown as MockConnection & SshConnection;
};

const createFakeSender = () => {
  const events: SftpEditStatusEvent[] = [];
  const sender = {
    send: (channel: string, payload: SftpEditStatusEvent) => {
      expect(channel).toBe(IPCChannel.SftpEditStatus);
      events.push(payload);
    },
    isDestroyed: () => false,
    once: () => undefined,
    removeListener: () => undefined
  };
  return { sender: sender as unknown as WebContents, events };
};

const createManager = (connection: SshConnection) => {
  const changeHandlers = new Map<string, () => void>();
  const manager = new RemoteEditManager({
    getConnection: async () => connection,
    watch: (localPath, onChange) => {
      changeHandlers.set(localPath, onChange);
      return { close: async () => undefined };
    }
  });
  return { manager, changeHandlers };
};

describe("RemoteEditManager", () => {
  test("readFile/writeFile 是无状态透传(带内容返回)", async () => {
    const connection = createMockConnection();
    const { manager } = createManager(connection);

    const { content } = await manager.readFile(CONNECTION_ID, REMOTE_PATH);
    expect(content).toBe("worker_connections 512;");

    await manager.writeFile(CONNECTION_ID, REMOTE_PATH, "worker_connections 1024;");
    expect(connection.written).toEqual([
      { remotePath: REMOTE_PATH, content: "worker_connections 1024;" }
    ]);
  });

  test("watcher change → 直接 upload,并推送 uploading/synced 事件", async () => {
    const connection = createMockConnection();
    const { manager, changeHandlers } = createManager(connection);
    const { sender, events } = createFakeSender();

    // 编辑器命令用 shell 内建 `true`,避免测试真的拉起外部编辑器
    const { editId, localPath } = await manager.open(CONNECTION_ID, REMOTE_PATH, "true", sender);

    changeHandlers.get(localPath)!();
    await vi.waitFor(() => {
      expect(connection.uploads).toEqual([{ localPath, remotePath: REMOTE_PATH }]);
    });

    const statuses = events.filter((e) => e.editId === editId).map((e) => e.status);
    expect(statuses).toEqual(["downloading", "editing", "uploading", "synced"]);

    await manager.dispose();
  });

  test("upload 失败一次即推送 error 状态事件", async () => {
    const connection = createMockConnection();
    const { manager, changeHandlers } = createManager(connection);
    const { sender, events } = createFakeSender();

    const { editId, localPath } = await manager.open(CONNECTION_ID, REMOTE_PATH, "true", sender);
    connection.failUpload = true;

    changeHandlers.get(localPath)!();
    await vi.waitFor(() => {
      const errorEvent = events.find((e) => e.editId === editId && e.status === "error");
      expect(errorEvent?.message).toContain("上传失败");
      expect(errorEvent?.message).toContain("sftp write denied");
    });
    expect(connection.uploads).toHaveLength(0);

    await manager.dispose();
  });

  test("readFile 对 stat 不可信的文件按实际读到的字节数限额", async () => {
    const connection = createMockConnection();
    connection.readFileContent = async () => Buffer.alloc(10 * 1024 * 1024 + 1);
    const { manager } = createManager(connection);

    await expect(manager.readFile(CONNECTION_ID, REMOTE_PATH)).rejects.toThrow("超过 10MB 限制");
  });

  test("下载失败时用 closed 收回已推送的 downloading 行", async () => {
    const connection = createMockConnection();
    connection.download = async () => {
      throw new Error("sftp read denied");
    };
    const { manager } = createManager(connection);
    const { sender, events } = createFakeSender();

    await expect(manager.open(CONNECTION_ID, REMOTE_PATH, "true", sender)).rejects.toThrow(
      "sftp read denied"
    );
    expect(events.map((e) => e.status)).toEqual(["downloading", "closed"]);
    expect(manager.listSessions()).toHaveLength(0);
  });

  test("上传中 stop():排队的重传不再执行,也不弹 error", async () => {
    const connection = createMockConnection();
    let releaseUpload: () => void = () => undefined;
    connection.upload = async (localPath: string, remotePath: string) => {
      await new Promise<void>((resolve) => {
        releaseUpload = resolve;
      });
      connection.uploads.push({ localPath, remotePath });
    };
    const { manager, changeHandlers } = createManager(connection);
    const { sender, events } = createFakeSender();

    const { editId, localPath } = await manager.open(CONNECTION_ID, REMOTE_PATH, "true", sender);
    const onChange = changeHandlers.get(localPath)!;
    onChange();
    onChange(); // 第二次变更在上传中到达 → pendingUpload
    await vi.waitFor(() => {
      expect(events.some((e) => e.editId === editId && e.status === "uploading")).toBe(true);
    });

    await manager.stop(editId);
    releaseUpload();
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(connection.uploads).toHaveLength(1);
    expect(events.filter((e) => e.editId === editId && e.status === "error")).toHaveLength(0);
    // 在途那次上传完成后仍会发 synced,store 对已关闭会话忽略它;关键是没有第二次 upload、没有 error。
    expect(events.filter((e) => e.editId === editId && e.status === "closed")).toHaveLength(1);
  });
});
