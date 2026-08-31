import { Button, Dropdown, Input, Tooltip } from "antd";
import type { MenuProps } from "antd";

interface ManagerToolbarProps {
  keyword: string;
  onKeywordChange: (keyword: string) => void;
  onNewConnection: () => void;
  /**
   * 本地终端(A1)。旧管理器的侧栏标题行有这个按钮,V2 收尾时连同侧栏一起删掉了,
   * 于是全应用再也打不开第一个本地终端——这是回归而不是砍削,所以按原位置补回来。
   */
  onOpenLocalTerminal: () => void;
  onImportNextShellFile: () => void;
  onImportNextShellDirectory: () => void;
  onImportFinalShellFile: () => void;
  onImportFinalShellDirectory: () => void;
  /** 云作用域下为 true：导入执行链路只写本地。 */
  importDisabled?: boolean;
  importDisabledReason?: string;
  /** 单个 JSON 文件——"发一份给同事"的形态。 */
  onExportAllToFile: () => void;
  /** 一个目录、每连接一个文件——批量归档的形态。 */
  onExportAllToDirectory: () => void;
  /** 云同步 / 回收站已迁到设置中心(D8),这里只留跳转(C4)。 */
  onOpenCloudSync: () => void;
  onOpenRecycleBin: () => void;
}

/**
 * 全局命令 + 搜索,单行。旧版搜索、工具条各占一行,加上批量条最多三层 chrome 叠在表格上;
 * 目录创建归目录树,这里只留与"整个列表"相关的命令。
 *
 * 导入/导出都是下拉:格式(NextShell / FinalShell)× 粒度(文件 / 文件夹)这四种入口在旧版
 * 是右键子菜单,收敛成单按钮后其中三种直接不可达;导出同理——单文件与逐连接目录是两种
 * 完全不同的用途,不能只留一个。
 */
export const ManagerToolbar = ({
  keyword,
  onKeywordChange,
  onNewConnection,
  onOpenLocalTerminal,
  onImportNextShellFile,
  onImportNextShellDirectory,
  onImportFinalShellFile,
  onImportFinalShellDirectory,
  importDisabled = false,
  importDisabledReason,
  onExportAllToFile,
  onExportAllToDirectory,
  onOpenCloudSync,
  onOpenRecycleBin
}: ManagerToolbarProps) => {
  const importMenu: MenuProps = {
    items: [
      { key: "nextshell-file", label: "NextShell 文件…", onClick: onImportNextShellFile },
      {
        key: "nextshell-directory",
        label: "NextShell 文件夹…",
        onClick: onImportNextShellDirectory
      },
      { type: "divider" },
      { key: "finalshell-file", label: "FinalShell 文件…", onClick: onImportFinalShellFile },
      {
        key: "finalshell-directory",
        label: "FinalShell 文件夹…",
        onClick: onImportFinalShellDirectory
      }
    ]
  };

  const exportMenu: MenuProps = {
    items: [
      { key: "single", label: "导出为单个文件…", onClick: onExportAllToFile },
      {
        key: "directory",
        label: "导出到目录（每连接一个文件）…",
        onClick: onExportAllToDirectory
      }
    ]
  };

  // 云同步/回收站是"离开管理器去别处"的命令，跟列表本身无关，所以收在「更多」里而不是
  // 再往这一行塞两个按钮。
  const moreMenu: MenuProps = {
    items: [
      {
        key: "cloudSync",
        label: "云同步…",
        icon: <i className="ri-git-merge-line" aria-hidden="true" />,
        onClick: onOpenCloudSync
      },
      {
        key: "recycleBin",
        label: "回收站…",
        icon: <i className="ri-delete-bin-line" aria-hidden="true" />,
        onClick: onOpenRecycleBin
      }
    ]
  };

  return (
    <div className="cm2-toolbar">
      <Button
        type="primary"
        icon={<i className="ri-add-line" aria-hidden="true" />}
        onClick={onNewConnection}
      >
        新建连接
      </Button>
      <Tooltip title="本地终端：打开一个本机 shell 标签">
        <Button
          icon={<i className="ri-terminal-box-line" aria-hidden="true" />}
          onClick={onOpenLocalTerminal}
          aria-label="本地终端"
        />
      </Tooltip>
      <Input
        className="cm2-search"
        allowClear
        prefix={<i className="ri-search-line cm2-search-icon" aria-hidden="true" />}
        placeholder="搜索整个作用域：名称、地址、用户名、标签、备注"
        value={keyword}
        onChange={(event) => onKeywordChange(event.target.value)}
      />
      {/* disabled 的按钮不派发鼠标事件，Tooltip 必须挂在外层 span 上才看得到禁用原因。 */}
      <Tooltip title={importDisabled ? importDisabledReason : "导入"}>
        <span className="cm2-toolbar-import">
          <Dropdown menu={importMenu} trigger={["click"]} disabled={importDisabled}>
            <Button
              icon={<i className="ri-upload-2-line" aria-hidden="true" />}
              disabled={importDisabled}
              aria-label="导入"
            />
          </Dropdown>
        </span>
      </Tooltip>
      <Tooltip title="导出全部">
        <Dropdown menu={exportMenu} trigger={["click"]}>
          <Button
            icon={<i className="ri-download-2-line" aria-hidden="true" />}
            aria-label="导出全部"
          />
        </Dropdown>
      </Tooltip>
      {/* D18:网格视图没有列的概念,「列…」下拉随树形表格一起退役。 */}
      <Dropdown menu={moreMenu} trigger={["click"]} placement="bottomRight">
        <Button icon={<i className="ri-more-2-fill" aria-hidden="true" />} aria-label="更多" />
      </Dropdown>
    </div>
  );
};
