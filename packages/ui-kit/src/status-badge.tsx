import type { CSSProperties } from "react";

type Tone = "green" | "yellow" | "red" | "gray";

const colors: Record<Tone, string> = {
  green: "#1e8e3e",
  yellow: "#b18000",
  red: "#c62828",
  gray: "#5f7080"
};

interface StatusBadgeProps {
  label: string;
  tone?: Tone;
}

const baseStyle: CSSProperties = {
  display: "inline-block",
  padding: "2px 8px",
  borderRadius: 12,
  fontSize: 12,
  color: "#fff"
};

export const StatusBadge = ({
  label,
  tone = "gray"
}: StatusBadgeProps) => {
  return <span style={{ ...baseStyle, backgroundColor: colors[tone] }}>{label}</span>;
};
