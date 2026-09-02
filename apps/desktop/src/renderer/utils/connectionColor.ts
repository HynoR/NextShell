/**
 * 给每个连接一个稳定的标识色：多标签同屏时靠颜色一眼分辨主机。
 * 由 connectionId 哈希取色相,同一连接永远同色;饱和度/亮度固定在
 * 深浅主题下都可读的区间。
 */

const hashString = (value: string): number => {
  let hash = 0;
  for (let i = 0; i < value.length; i++) {
    hash = (hash << 5) - hash + value.charCodeAt(i);
    hash |= 0;
  }
  return Math.abs(hash);
};

/** 色相间隔取 360/24=15°,避免相邻哈希落在肉眼不可分的色相上。 */
export const connectionHue = (connectionId: string): number => (hashString(connectionId) % 24) * 15;

export const connectionColor = (connectionId: string): string =>
  `hsl(${connectionHue(connectionId)} 62% 52%)`;
