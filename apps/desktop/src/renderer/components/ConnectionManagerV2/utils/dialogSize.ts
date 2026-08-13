/**
 * 对话框与栏宽的可用范围。与 `appPreferencesSchema` 里的上下限保持一致——契约层负责拒收存坏的
 * 值,这里负责拖拽过程中不越界。
 */
export const DIALOG_SIZE_LIMITS = {
  width: { min: 900, max: 3840 },
  height: { min: 560, max: 2160 },
  folderColumn: { min: 160, max: 520 },
  detailColumn: { min: 260, max: 720 }
} as const;

export type DialogSizeDimension = keyof typeof DIALOG_SIZE_LIMITS;

export const clampDialogSize = (dimension: DialogSizeDimension, value: number): number => {
  const { min, max } = DIALOG_SIZE_LIMITS[dimension];
  // NaN 会穿过 Math.min/max 原样传出去，必须单独挡；±Infinity 走正常夹取即可。
  if (Number.isNaN(value)) {
    return min;
  }
  return Math.min(max, Math.max(min, Math.round(value)));
};

/**
 * 再叠一层视口约束:用户可能在大屏上把窗口拖到 2400px,换到笔记本后这个尺寸会把对话框顶出屏幕。
 * 视口小于下限时以视口为准,宁可挤也不能有一部分够不着。
 */
export const fitDialogToViewport = (
  size: { width: number; height: number },
  viewport: { width: number; height: number }
): { width: number; height: number } => ({
  width: Math.max(320, Math.min(clampDialogSize("width", size.width), viewport.width - 32)),
  height: Math.max(320, Math.min(clampDialogSize("height", size.height), viewport.height - 64))
});
