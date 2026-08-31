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
 * 对话框与视口之间要留出的空间。这里的 width/height 说的是**壳体**(Modal body)的可用范围,
 * 所以高度这一侧还要额外扣掉 Modal 的 `top: 40` 上偏移和标题栏(约 47px):只扣 64 的话,
 * 拖到最大时右下角的缩放手柄会掉到屏幕外面,再也拖不回来。
 */
export const DIALOG_VIEWPORT_MARGIN = { width: 32, height: 120 } as const;

/**
 * 视口再小也要保住的最小可视尺寸。低于它对话框会缩成一条缝——连右下角的缩放手柄都点不到,
 * 于是"缩小一次就再也放不回来"。
 */
export const DIALOG_MIN_VISIBLE = 320;

/**
 * 单个维度的唯一口径:先夹进契约范围,再让视口说了算,最后守住最小可视尺寸。
 * 显示(`fitDialogToViewport`)与拖拽(`resolveDialogResize`)必须共用它——两边算出不同的数,
 * 就会出现"命令式写进 DOM 的高度是 560,React 认为是 520",而 React 因为自己那份值没变
 * 根本不会去改写内联样式,DOM 就永远停在那个顶出视口的尺寸上。
 */
const fitDimension = (
  dimension: "width" | "height",
  value: number,
  available: number
): number => {
  const clamped = clampDialogSize(dimension, value);
  // 视口读数坏掉(NaN/Infinity)时宁可不夹——夹出 NaN 会把尺寸整个写废。
  if (!Number.isFinite(available)) {
    return clamped;
  }
  return Math.max(DIALOG_MIN_VISIBLE, Math.min(clamped, Math.round(available)));
};

/**
 * 再叠一层视口约束:用户可能在大屏上把窗口拖到 2400px,换到笔记本后这个尺寸会把对话框顶出屏幕。
 * 视口小于下限时以视口为准,宁可挤也不能有一部分够不着。
 */
export const fitDialogToViewport = (
  size: { width: number; height: number },
  viewport: { width: number; height: number }
): { width: number; height: number } => ({
  width: fitDimension("width", size.width, viewport.width - DIALOG_VIEWPORT_MARGIN.width),
  height: fitDimension("height", size.height, viewport.height - DIALOG_VIEWPORT_MARGIN.height)
});

/**
 * 拖右下角缩放时的实时尺寸,也就是这一帧要写进 DOM 的值。和 `fitDialogToViewport` 同口径:
 * 拖出来的尺寸再过一次显示口径必须还是它自己,否则松手那一刻壳体会跳一下,小视口下更会
 * 把手柄留在屏幕外面。
 *
 * 注意它**不是**写回偏好的值:契约(`appPreferencesSchema`)不收 900×560 以下,视口比下限还小时
 * 这里会给出低于契约的数。调用方落偏好前要再过一次 `clampDialogSize`——存下限、显示照旧压回视口。
 */
export const resolveDialogResize = (
  size: { width: number; height: number },
  viewport: { width: number; height: number }
): { width: number; height: number } => fitDialogToViewport(size, viewport);
