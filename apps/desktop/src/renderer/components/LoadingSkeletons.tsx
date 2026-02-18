import { Skeleton, Spin } from "antd";
import type { CSSProperties, ReactNode } from "react";

// ── PanelSkeleton ─────────────────────────────────────────
// A full-panel skeleton placeholder for loading states.

interface PanelSkeletonProps {
  /** Number of rows to simulate (default 5) */
  rows?: number;
  /** Optional compact mode for smaller panels */
  compact?: boolean;
  /** Optional class name */
  className?: string;
  /** Override style */
  style?: CSSProperties;
}

export const PanelSkeleton = ({ rows = 5, compact = false, className, style }: PanelSkeletonProps) => {
  return (
    <div
      className={className}
      style={{
        padding: compact ? "8px 12px" : "16px 20px",
        display: "flex",
        flexDirection: "column",
        gap: compact ? 8 : 12,
        ...style
      }}
    >
      <Skeleton.Input active size={compact ? "small" : "default"} style={{ width: "40%" }} />
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton.Input
          key={i}
          active
          size="small"
          style={{ width: `${60 + Math.round(Math.random() * 30)}%`, height: compact ? 14 : 18 }}
        />
      ))}
    </div>
  );
};

// ── TableSkeleton ──────────────────────────────────────────
// Simulates a table-like skeleton with configurable columns.

interface TableSkeletonProps {
  rows?: number;
  columns?: number;
  className?: string;
}

export const TableSkeleton = ({ rows = 5, columns = 4, className }: TableSkeletonProps) => {
  const widths = ["20%", "30%", "25%", "15%", "10%", "20%"];
  return (
    <div className={className} style={{ padding: "12px 16px" }}>
      {/* header row */}
      <div style={{ display: "flex", gap: 12, marginBottom: 12 }}>
        {Array.from({ length: columns }, (_, i) => (
          <Skeleton.Input
            key={`h${i}`}
            active
            size="small"
            style={{ width: widths[i % widths.length], height: 14 }}
          />
        ))}
      </div>
      {/* body rows */}
      {Array.from({ length: rows }, (_, r) => (
        <div key={r} style={{ display: "flex", gap: 12, marginBottom: 10 }}>
          {Array.from({ length: columns }, (_, c) => (
            <Skeleton.Input
              key={`${r}-${c}`}
              active
              size="small"
              style={{ width: widths[c % widths.length], height: 12 }}
            />
          ))}
        </div>
      ))}
    </div>
  );
};

// ── InlineSpin ─────────────────────────────────────────────
// A small inline spinner with optional text.

interface InlineSpinProps {
  text?: string;
  size?: "small" | "default";
  children?: ReactNode;
}

export const InlineSpin = ({ text, size = "small", children }: InlineSpinProps) => {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <Spin size={size} />
      {text ? <span style={{ fontSize: 12, color: "var(--t3)" }}>{text}</span> : null}
      {children}
    </span>
  );
};

// ── AppSkeleton ───────────────────────────────────────────
// Full-app skeleton for the initial load state.

export const AppSkeleton = () => {
  return (
    <div className="h-screen flex flex-col overflow-hidden">
      {/* header skeleton */}
      <div
        style={{
          height: 40,
          display: "flex",
          alignItems: "center",
          padding: "0 16px",
          gap: 12,
          borderBottom: "1px solid var(--border)"
        }}
      >
        <Skeleton.Avatar active size="small" shape="square" />
        <Skeleton.Input active size="small" style={{ width: 80 }} />
        <div style={{ flex: 1 }} />
        <Skeleton.Button active size="small" style={{ width: 60 }} />
        <Skeleton.Button active size="small" style={{ width: 80 }} />
        <Skeleton.Button active size="small" style={{ width: 60 }} />
      </div>

      {/* body skeleton */}
      <div style={{ display: "flex", flex: 1, minHeight: 0 }}>
        {/* left panel */}
        <div
          style={{
            width: "18%",
            minWidth: 180,
            borderRight: "1px solid var(--border)",
            padding: "12px 8px",
            display: "flex",
            flexDirection: "column",
            gap: 8
          }}
        >
          <Skeleton.Input active size="small" style={{ width: "70%" }} />
          <Skeleton.Input active size="small" style={{ width: "50%", height: 14 }} />
          <Skeleton.Input active size="small" style={{ width: "60%", height: 14 }} />
          <Skeleton.Input active size="small" style={{ width: "45%", height: 14 }} />
        </div>

        {/* main area */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
          {/* terminal area */}
          <div style={{ flex: "1 1 68%", minHeight: 0, padding: "12px 16px" }}>
            <Skeleton.Input active size="small" style={{ width: "30%", marginBottom: 12 }} />
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {Array.from({ length: 8 }, (_, i) => (
                <Skeleton.Input
                  key={i}
                  active
                  size="small"
                  style={{ width: `${40 + Math.round(Math.random() * 50)}%`, height: 13 }}
                />
              ))}
            </div>
          </div>

          {/* bottom area */}
          <div
            style={{
              flex: "1 1 32%",
              minHeight: 0,
              borderTop: "1px solid var(--border)",
              padding: "12px 16px"
            }}
          >
            <div style={{ display: "flex", gap: 16, marginBottom: 12 }}>
              <Skeleton.Button active size="small" style={{ width: 50 }} />
              <Skeleton.Button active size="small" style={{ width: 50 }} />
              <Skeleton.Button active size="small" style={{ width: 50 }} />
              <Skeleton.Button active size="small" style={{ width: 50 }} />
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {Array.from({ length: 4 }, (_, i) => (
                <Skeleton.Input
                  key={i}
                  active
                  size="small"
                  style={{ width: `${50 + Math.round(Math.random() * 40)}%`, height: 14 }}
                />
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
