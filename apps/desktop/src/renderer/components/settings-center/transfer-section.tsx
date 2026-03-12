import { Button, Input } from "antd";
import { SettingsCard, SettingsRow } from "./shared-components";
import type { SaveFn } from "./types";

export const TransferSection = ({
  loading, uploadDefaultDir, downloadDefaultDir,
  setUploadDefaultDir, setDownloadDefaultDir, save, pickDirectory,
}: {
  loading: boolean;
  uploadDefaultDir: string;
  downloadDefaultDir: string;
  setUploadDefaultDir: (v: string) => void;
  setDownloadDefaultDir: (v: string) => void;
  save: SaveFn;
  pickDirectory: (title: string, currentPath: string, setter: (v: string) => void, field: "uploadDefaultDir" | "downloadDefaultDir") => Promise<void>;
}) => (
  <SettingsCard title="默认路径" description="统一设置上传/下载默认路径">
    <SettingsRow label="上传默认目录">
      <div className="flex gap-2">
        <Input
          style={{ flex: 1 }}
          value={uploadDefaultDir}
          disabled={loading}
          onChange={(e) => setUploadDefaultDir(e.target.value)}
          onBlur={() => {
            const v = uploadDefaultDir.trim();
            if (v) save({ transfer: { uploadDefaultDir: v } });
          }}
          placeholder="例如 ~/Desktop"
        />
        <Button
          onClick={() => void pickDirectory("选择上传默认目录", uploadDefaultDir, setUploadDefaultDir, "uploadDefaultDir")}
        >
          选择目录
        </Button>
      </div>
    </SettingsRow>
    <SettingsRow label="下载默认目录">
      <div className="flex gap-2">
        <Input
          style={{ flex: 1 }}
          value={downloadDefaultDir}
          disabled={loading}
          onChange={(e) => setDownloadDefaultDir(e.target.value)}
          onBlur={() => {
            const v = downloadDefaultDir.trim();
            if (v) save({ transfer: { downloadDefaultDir: v } });
          }}
          placeholder="例如 ~/Downloads"
        />
        <Button
          onClick={() => void pickDirectory("选择下载默认目录", downloadDefaultDir, setDownloadDefaultDir, "downloadDefaultDir")}
        >
          选择目录
        </Button>
      </div>
    </SettingsRow>
  </SettingsCard>
);
