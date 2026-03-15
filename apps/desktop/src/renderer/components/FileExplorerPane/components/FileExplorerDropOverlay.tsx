interface FileExplorerDropOverlayProps {
  pathName: string;
}

export const FileExplorerDropOverlay = ({ pathName }: FileExplorerDropOverlayProps) => (
  <div className="fe-drop-overlay" aria-hidden="true">
    <div className="fe-drop-overlay-card">
      <i className="ri-upload-cloud-2-line" aria-hidden="true" />
      <span>释放以上传到当前目录</span>
      <code>{pathName}</code>
    </div>
  </div>
);
