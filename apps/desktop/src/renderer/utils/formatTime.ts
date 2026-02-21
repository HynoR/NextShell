/**
 * Format an ISO 8601 timestamp as a human-readable relative time (e.g. "3 天前").
 */
export function formatRelativeTime(isoString: string): string {
  const diffMs = Date.now() - new Date(isoString).getTime();
  const diffSec = Math.floor(diffMs / 1000);

  if (diffSec < 60) return "刚刚";

  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin} 分钟前`;

  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour} 小时前`;

  const diffDay = Math.floor(diffHour / 24);
  if (diffDay < 30) return `${diffDay} 天前`;

  const diffMonth = Math.floor(diffDay / 30);
  if (diffMonth < 12) return `${diffMonth} 个月前`;

  return `${Math.floor(diffDay / 365)} 年前`;
}

/**
 * Format an ISO 8601 timestamp as a localized date-time string for tooltips.
 */
export function formatDateTime(isoString: string): string {
  return new Date(isoString).toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit"
  });
}
