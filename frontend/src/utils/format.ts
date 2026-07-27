export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit += 1; }
  return `${amount.toFixed(unit ? 1 : 0)} ${units[unit]}`;
}

export const percentage = (used: number, total: number) => total > 0 ? (used / total) * 100 : 0;
export const clampPercentage = (value: number) => Math.min(100, Math.max(0, Number.isFinite(value) ? value : 0));
export const formatDateTime = (timestamp: number) => new Date(timestamp * 1000).toLocaleString();
export const errorMessage = (error: unknown) => error instanceof Error ? error.message.trim() : "发生未知错误";
