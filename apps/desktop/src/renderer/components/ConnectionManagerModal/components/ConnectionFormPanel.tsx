import { Form, Tooltip } from "antd";
import type { FormInstance } from "antd";
import type { CloudSyncWorkspaceProfile, ConnectionProfile, ProxyProfile, SshKeyProfile } from "@nextshell/core";
import type { ConnectionUpsertInput } from "@nextshell/shared";
import { formatDateTime, formatRelativeTime } from "../../../utils/formatTime";
import { FIELD_TAB_MAP } from "../constants";
import type { ConnectionFormValues } from "../utils/connectionForm";
import type { FormTab, ManagerMode } from "../types";
import { BasicTab } from "./tabs/BasicTab";
import { PropertyTab } from "./tabs/PropertyTab";
import { NetworkTab } from "./tabs/NetworkTab";
import { AdvancedTab } from "./tabs/AdvancedTab";

interface ConnectionFormPanelProps {
  form: FormInstance<ConnectionUpsertInput>;
  mode: ManagerMode;
  selectedConnection?: ConnectionProfile;
  formTab: FormTab;
  setFormTab: (tab: FormTab) => void;
  authType?: string;
  keepAliveSetting?: boolean;
  saving: boolean;
  connectingFromForm: boolean;
  sshKeys: SshKeyProfile[];
  proxies: ProxyProfile[];
  workspaces: CloudSyncWorkspaceProfile[];
  scopeLocked: boolean;
  revealedLoginPassword?: string;
  revealingLoginPassword: boolean;
  onRevealConnectionPassword: () => void;
  onSave: (values: ConnectionFormValues) => Promise<void>;
  onSaveAndConnect: () => void;
  onDelete: () => void;
  onReset: () => void;
  onCloseForm: () => void;
  onSwitchToIdle: () => void;
  onNewConnection: () => void;
}

export const ConnectionFormPanel = ({
  form,
  mode,
  selectedConnection,
  formTab,
  setFormTab,
  authType,
  keepAliveSetting,
  saving,
  connectingFromForm,
  sshKeys,
  proxies,
  workspaces,
  scopeLocked,
  revealedLoginPassword,
  revealingLoginPassword,
  onRevealConnectionPassword,
  onSave,
  onSaveAndConnect,
  onDelete,
  onReset,
  onCloseForm,
  onSwitchToIdle,
  onNewConnection
}: ConnectionFormPanelProps) => {
  if (mode === "idle") {
    return (
      <div className="mgr-empty-state">
        <i className="ri-server-line mgr-empty-icon" aria-hidden="true" />
        <div className="mgr-empty-title">选择或新建连接</div>
        <div className="mgr-empty-hint">从左侧列表选择一个连接进行编辑，或点击下方按钮新建连接</div>
        <button type="button" className="mgr-empty-new-btn" onClick={onNewConnection}>
          <i className="ri-add-line" aria-hidden="true" />
          新建连接
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col overflow-hidden">
      <div className="mgr-form-header">
        <div>
          <div className="mgr-form-title">
            {mode === "new" ? "新建连接" : (selectedConnection?.name ?? "编辑连接")}
          </div>
          {mode === "edit" && selectedConnection ? (
            <>
              <div className="mgr-form-subtitle">
                {selectedConnection.username.trim()
                  ? `${selectedConnection.username}@${selectedConnection.host}:${selectedConnection.port}`
                  : `${selectedConnection.host}:${selectedConnection.port}`}
              </div>
              <div className="mgr-form-meta">
                <span
                  className="mgr-form-meta-item"
                  title={`修改时间：${formatDateTime(selectedConnection.updatedAt)}`}
                >
                  <i className="ri-edit-2-line" aria-hidden="true" />
                  {formatRelativeTime(selectedConnection.updatedAt)}
                </span>
                <span className="mgr-form-meta-sep">·</span>
                <span
                  className="mgr-form-meta-item"
                  title={selectedConnection.lastConnectedAt
                    ? `上次连接：${formatDateTime(selectedConnection.lastConnectedAt)}`
                    : "从未连接"}
                >
                  <i className="ri-plug-line" aria-hidden="true" />
                  {selectedConnection.lastConnectedAt
                    ? formatRelativeTime(selectedConnection.lastConnectedAt)
                    : "从未连接"}
                </span>
              </div>
            </>
          ) : (
            <div className="mgr-form-subtitle">填写以下信息后点击保存</div>
          )}
        </div>
        <div className="mgr-form-header-right">
          <span className="mgr-ssh-badge">SSH</span>
          <button
            type="button"
            className="mgr-connect-btn"
            onClick={onSaveAndConnect}
            disabled={saving || connectingFromForm}
            title="保存并连接"
          >
            {connectingFromForm ? (
              <i className="ri-loader-4-line mgr-form-header-icon-spin" aria-hidden="true" />
            ) : (
              <i className="ri-terminal-box-line" aria-hidden="true" />
            )}
            连接
          </button>
          {mode === "edit" ? (
            <Tooltip title="删除连接">
              <button
                type="button"
                className="mgr-form-header-icon-btn mgr-form-header-icon-btn--danger"
                onClick={onDelete}
                aria-label="删除连接"
              >
                <i className="ri-delete-bin-line" aria-hidden="true" />
              </button>
            </Tooltip>
          ) : (
            <Tooltip title="取消">
              <button
                type="button"
                className="mgr-form-header-icon-btn"
                onClick={onSwitchToIdle}
                aria-label="取消"
              >
                <i className="ri-arrow-left-line" aria-hidden="true" />
              </button>
            </Tooltip>
          )}
          <Tooltip title="重置">
            <button
              type="button"
              className="mgr-form-header-icon-btn"
              onClick={onReset}
              aria-label="重置"
            >
              <i className="ri-refresh-line" aria-hidden="true" />
            </button>
          </Tooltip>
          <Tooltip title="保存连接">
            <button
              type="button"
              className="mgr-form-header-icon-btn mgr-form-header-icon-btn--primary"
              onClick={() => form.submit()}
              disabled={saving || connectingFromForm}
              aria-label="保存连接"
            >
              {saving ? (
                <i className="ri-loader-4-line mgr-form-header-icon-spin" aria-hidden="true" />
              ) : (
                <i className="ri-save-line" aria-hidden="true" />
              )}
            </button>
          </Tooltip>
          <Tooltip title="收起表单">
            <button
              type="button"
              className="mgr-form-close-btn"
              onClick={onCloseForm}
              aria-label="收起表单"
            >
              <i className="ri-close-line" aria-hidden="true" />
            </button>
          </Tooltip>
        </div>
      </div>

      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        className="mgr-form"
        onFinish={async (values) => {
          await onSave(values as ConnectionFormValues);
        }}
        onFinishFailed={({ errorFields }) => {
          const firstField = String(errorFields[0]?.name?.[0] ?? "");
          const errTab = FIELD_TAB_MAP[firstField];
          if (errTab) setFormTab(errTab);
        }}
      >
        <div className="mgr-form-tab-bar">
          <button
            type="button"
            title="基本信息"
            className={`mgr-form-tab${formTab === "basic" ? " mgr-form-tab--active" : ""}`}
            onClick={() => setFormTab("basic")}
          >
            <i className="ri-server-line" aria-hidden="true" />
            基本
          </button>
          <button
            type="button"
            title="属性信息"
            className={`mgr-form-tab${formTab === "property" ? " mgr-form-tab--active" : ""}`}
            onClick={() => setFormTab("property")}
          >
            <i className="ri-price-tag-3-line" aria-hidden="true" />
            属性
          </button>
          <button
            type="button"
            title="网络代理"
            className={`mgr-form-tab${formTab === "network" ? " mgr-form-tab--active" : ""}`}
            onClick={() => setFormTab("network")}
          >
            <i className="ri-shield-line" aria-hidden="true" />
            网络
          </button>
          <button
            type="button"
            title="高级设置"
            className={`mgr-form-tab${formTab === "advanced" ? " mgr-form-tab--active" : ""}`}
            onClick={() => setFormTab("advanced")}
          >
            <i className="ri-settings-3-line" aria-hidden="true" />
            高级
          </button>
        </div>

        <div className="mgr-form-tab-body">
          <div style={{ display: formTab === "basic" ? "" : "none" }}>
            <BasicTab
              authType={authType}
              mode={mode}
              selectedConnection={selectedConnection}
              sshKeys={sshKeys}
              revealedLoginPassword={revealedLoginPassword}
              revealingLoginPassword={revealingLoginPassword}
              onRevealConnectionPassword={onRevealConnectionPassword}
            />
          </div>

          <div style={{ display: formTab === "property" ? "" : "none" }}>
            <PropertyTab
              workspaces={workspaces}
              scopeLocked={scopeLocked}
            />
          </div>

          <div style={{ display: formTab === "network" ? "" : "none" }}>
            <NetworkTab keepAliveSetting={keepAliveSetting} proxies={proxies} />
          </div>

          <div style={{ display: formTab === "advanced" ? "" : "none" }}>
            <AdvancedTab />
          </div>
        </div>
      </Form>
    </div>
  );
};
