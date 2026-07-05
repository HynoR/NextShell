import React from "react";

interface AppErrorBoundaryProps {
  children: React.ReactNode;
}

interface AppErrorBoundaryState {
  hasError: boolean;
  errorMessage?: string;
}

const containerStyle: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  height: "100vh",
  width: "100vw",
  background: "#0d1117",
  color: "#dde4f0",
  fontFamily: '"Inter", "PingFang SC", system-ui, sans-serif'
};

const cardStyle: React.CSSProperties = {
  maxWidth: 420,
  width: "90%",
  padding: "28px 32px",
  borderRadius: 8,
  background: "#1d2435",
  border: "1px solid rgba(255,255,255,0.16)",
  textAlign: "center"
};

const titleStyle: React.CSSProperties = {
  margin: "0 0 8px",
  fontSize: 16,
  fontWeight: 600
};

const messageStyle: React.CSSProperties = {
  margin: "0 0 20px",
  fontSize: 12,
  color: "#8893a6",
  wordBreak: "break-all",
  maxHeight: 96,
  overflow: "auto"
};

const buttonRowStyle: React.CSSProperties = {
  display: "flex",
  justifyContent: "center",
  gap: 12
};

const buttonStyle: React.CSSProperties = {
  padding: "6px 16px",
  borderRadius: 4,
  border: "1px solid rgba(255,255,255,0.16)",
  background: "transparent",
  color: "#dde4f0",
  fontSize: 13,
  cursor: "pointer"
};

const primaryButtonStyle: React.CSSProperties = {
  ...buttonStyle,
  border: "1px solid #4d9fff",
  background: "#4d9fff",
  color: "#ffffff"
};

export class AppErrorBoundary extends React.Component<AppErrorBoundaryProps, AppErrorBoundaryState> {
  state: AppErrorBoundaryState = { hasError: false };

  static getDerivedStateFromError(error: unknown): AppErrorBoundaryState {
    return {
      hasError: true,
      errorMessage: error instanceof Error ? error.message : String(error)
    };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo): void {
    try {
      console.error("[nextshell:render-error]", error, errorInfo.componentStack);
    } catch {
      // Logging must never throw inside the boundary.
    }
  }

  handleReset = (): void => {
    this.setState({ hasError: false, errorMessage: undefined });
  };

  handleReload = (): void => {
    window.location.reload();
  };

  render(): React.ReactNode {
    if (!this.state.hasError) {
      return this.props.children;
    }

    return (
      <div style={containerStyle}>
        <div style={cardStyle}>
          <h2 style={titleStyle}>界面发生异常</h2>
          <p style={messageStyle}>{this.state.errorMessage ?? "发生未知错误"}</p>
          <div style={buttonRowStyle}>
            <button type="button" style={primaryButtonStyle} onClick={this.handleReset}>
              恢复界面
            </button>
            <button type="button" style={buttonStyle} onClick={this.handleReload}>
              重新加载
            </button>
          </div>
        </div>
      </div>
    );
  }
}
