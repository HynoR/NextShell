import { useState } from "react";
import { Button } from "antd";

interface ImportSource {
  key: string;
  label: string;
  icon: string;
  description: string;
  buttonText: string;
  handler: () => void | Promise<void>;
}

interface ImportManagerPanelProps {
  importingPreview: boolean;
  onImportNextShell: () => void | Promise<void>;
  onImportNextShellDirectory: () => void | Promise<void>;
  onImportFinalShell: () => void | Promise<void>;
  onImportFinalShellDirectory: () => void | Promise<void>;
}

export const ImportManagerPanel = ({
  importingPreview,
  onImportNextShell,
  onImportNextShellDirectory,
  onImportFinalShell,
  onImportFinalShellDirectory
}: ImportManagerPanelProps) => {
  const sources: ImportSource[] = [
    {
      key: "nextshell-file",
      label: "NextShell 文件",
      icon: "ri-upload-2-line",
      description:
        "从 NextShell 导出的 .json 文件导入连接。支持明文或加密文件，加密文件会提示输入密码。",
      buttonText: "选择文件...",
      handler: onImportNextShell
    },
    {
      key: "nextshell-folder",
      label: "NextShell 文件夹",
      icon: "ri-folder-upload-line",
      description: "选择一个包含 NextShell 连接文件的文件夹进行批量导入。",
      buttonText: "选择文件夹...",
      handler: onImportNextShellDirectory
    },
    {
      key: "finalshell-file",
      label: "FinalShell 文件",
      icon: "ri-file-upload-line",
      description: "从 FinalShell 配置文件导入连接。",
      buttonText: "选择文件...",
      handler: onImportFinalShell
    },
    {
      key: "finalshell-folder",
      label: "FinalShell 文件夹",
      icon: "ri-folder-upload-line",
      description: "选择一个包含 FinalShell 配置文件的文件夹进行批量导入。",
      buttonText: "选择文件夹...",
      handler: onImportFinalShellDirectory
    }
  ];

  const firstSource = sources[0]!;
  const [selectedKey, setSelectedKey] = useState<string>(firstSource.key);
  const selectedSource = sources.find((s) => s.key === selectedKey) ?? firstSource;

  return (
    <div className="mgr-import-layout">
      <div className="mgr-import-sources">
        <div className="mgr-section-label" style={{ margin: "10px 12px 6px" }}>
          导入来源
        </div>
        <div className="mgr-flat-list">
          {sources.map((source) => (
            <button
              key={source.key}
              type="button"
              className={`mgr-flat-item${selectedKey === source.key ? " mgr-flat-item--selected" : ""}`}
              onClick={() => setSelectedKey(source.key)}
            >
              <i className={source.icon} aria-hidden="true" />
              <span className="mgr-flat-item-name">{source.label}</span>
            </button>
          ))}
        </div>
      </div>

      <div className="mgr-import-detail">
        <div className="mgr-import-detail-card">
          <div className="mgr-import-detail-icon">
            <i className={selectedSource.icon} aria-hidden="true" />
          </div>
          <h3 className="mgr-import-detail-title">{selectedSource.label}</h3>
          <p className="mgr-import-detail-desc">{selectedSource.description}</p>
          <Button
            type="primary"
            icon={
              <i
                className={importingPreview ? "ri-loader-4-line ri-spin" : selectedSource.icon}
                aria-hidden="true"
              />
            }
            loading={importingPreview}
            onClick={() => void selectedSource.handler()}
            className="mgr-import-action-btn"
          >
            {selectedSource.buttonText}
          </Button>
          <p className="mgr-import-detail-hint">
            导入前会先弹出预览窗口，可在预览中处理重复连接（跳过/覆盖/保留两者）。
          </p>
        </div>
      </div>
    </div>
  );
};
