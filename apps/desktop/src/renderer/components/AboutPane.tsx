import { useCallback, useState } from "react";
import { Button, Tag, message } from "antd";
import type { UpdateCheckResult } from "@nextshell/shared";

const appVersion = __APP_VERSION__;
const githubRepo = __GITHUB_REPO__;

export const AboutPane = () => {
  const [checking, setChecking] = useState(false);
  const [result, setResult] = useState<UpdateCheckResult | null>(null);

  const handleCheckUpdate = useCallback(async () => {
    setChecking(true);
    try {
      const res = await window.nextshell.about.checkUpdate();
      setResult(res);
      if (res.error) {
        void message.warning(`检查更新失败: ${res.error}`);
      } else if (res.hasUpdate) {
        void message.success(`发现新版本 ${res.latestVersion}`);
      } else {
        void message.info("当前已是最新版本");
      }
    } catch {
      void message.error("检查更新失败");
    } finally {
      setChecking(false);
    }
  }, []);

  const repoUrl = `https://github.com/${githubRepo}`;

  return (
    <div className="about-pane">
      <div className="about-header">
        <div className="about-logo">
          <i className="ri-terminal-box-fill" aria-hidden="true" />
          <span className="about-app-name">NextShell</span>
          <Tag color="blue">v{appVersion}</Tag>
        </div>
        <p className="about-desc">
          现代化的 SSH 终端管理工具
        </p>
      </div>

      <div className="about-section">
        <div className="about-row">
          <span className="about-label">
            <i className="ri-github-fill" aria-hidden="true" /> GitHub 仓库
          </span>
          <a
            className="about-link"
            href={repoUrl}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => {
              e.preventDefault();
              void window.nextshell.dialog.openPath({ path: repoUrl, revealInFolder: false });
            }}
          >
            {githubRepo}
            <i className="ri-external-link-line" aria-hidden="true" />
          </a>
        </div>

        <div className="about-row">
          <span className="about-label">
            <i className="ri-information-line" aria-hidden="true" /> 当前版本
          </span>
          <span className="about-value">v{appVersion}</span>
        </div>

        {result?.latestVersion ? (
          <div className="about-row">
            <span className="about-label">
              <i className="ri-download-cloud-line" aria-hidden="true" /> 最新版本
            </span>
            <span className="about-value">
              {result.latestVersion}
              {result.hasUpdate ? (
                <Tag color="green" style={{ marginLeft: 8 }}>有更新</Tag>
              ) : (
                <Tag color="default" style={{ marginLeft: 8 }}>已是最新</Tag>
              )}
            </span>
          </div>
        ) : null}

        {result?.hasUpdate && result.releaseUrl ? (
          <div className="about-row">
            <span className="about-label">
              <i className="ri-links-line" aria-hidden="true" /> 下载地址
            </span>
            <a
              className="about-link"
              href={result.releaseUrl}
              target="_blank"
              rel="noopener noreferrer"
              onClick={(e) => {
                e.preventDefault();
                void window.nextshell.dialog.openPath({ path: result.releaseUrl!, revealInFolder: false });
              }}
            >
              前往 Release 页面
              <i className="ri-external-link-line" aria-hidden="true" />
            </a>
          </div>
        ) : null}
      </div>

      <div className="about-actions">
        <Button
          type="primary"
          icon={<i className="ri-refresh-line" aria-hidden="true" />}
          loading={checking}
          onClick={() => void handleCheckUpdate()}
        >
          检查更新
        </Button>
      </div>
    </div>
  );
};
