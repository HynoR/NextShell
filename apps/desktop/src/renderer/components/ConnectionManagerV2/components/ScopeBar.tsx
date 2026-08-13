import { Segmented, Select } from "antd";
import type { ManagerScope } from "../utils/scopes";
import type { ResourceTab } from "../types";

interface ScopeBarProps {
  scopes: ManagerScope[];
  activeScope: ManagerScope;
  onSelectScope: (key: string) => void;
  resourceTab: ResourceTab;
  onSelectResource: (tab: ResourceTab) => void;
}

/**
 * 顶部只有两个控件:作用域(隔离域)和资源类型。作用域是最高层过滤——切换后目录、表格、密钥与
 * 代理全部只显示该域,跨域引用在结构上就发生不了。
 */
export const ScopeBar = ({
  scopes,
  activeScope,
  onSelectScope,
  resourceTab,
  onSelectResource
}: ScopeBarProps) => (
  <div className="cm2-scope-bar">
    <label className="cm2-scope-label" htmlFor="cm2-scope-select">
      作用域
    </label>
    <Select
      id="cm2-scope-select"
      className="cm2-scope-select"
      size="small"
      value={activeScope.key}
      onChange={onSelectScope}
      options={scopes.map((scope) => ({
        value: scope.key,
        label: (
          <span className="cm2-scope-option">
            <i
              className={scope.kind === "cloud" ? "ri-cloud-line" : "ri-hard-drive-2-line"}
              aria-hidden="true"
            />
            {scope.label}
          </span>
        )
      }))}
    />
    <Segmented<ResourceTab>
      size="small"
      value={resourceTab}
      onChange={onSelectResource}
      options={[
        { label: "连接", value: "connections" },
        { label: "密钥", value: "keys" },
        { label: "代理", value: "proxies" }
      ]}
    />
  </div>
);
