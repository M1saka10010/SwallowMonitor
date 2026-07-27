import type { RangeKey, Usage } from "../../api/types";

export const RANGES: Record<RangeKey, { label: string; seconds: number }> = {
  "5m": { label: "5 分钟", seconds: 300 },
  "1h": { label: "1 小时", seconds: 3600 },
  "3h": { label: "3 小时", seconds: 10800 },
  "1d": { label: "1 天", seconds: 86400 },
  "7d": { label: "7 天", seconds: 604800 },
};

export const isRangeKey = (value: string | null): value is RangeKey => Boolean(value && value in RANGES);

export function estimateSampleInterval(points: Usage[]): number {
  const deltas = points.slice(1).map((point, index) => point.timestamp - points[index].timestamp).filter((delta) => delta > 0).sort((a, b) => a - b);
  if (!deltas.length) return 60;
  return Math.min(300, Math.max(1, deltas[Math.floor(deltas.length * 0.25)]));
}

export interface TimelinePoint { timestamp: number; usage: Usage | null; offline: boolean }

export function buildTimeline(points: Usage[], from: number, to: number): TimelinePoint[] {
  const ordered = [...points].filter((point) => point.timestamp > 0).sort((a, b) => a.timestamp - b.timestamp);
  if (!ordered.length) return [{ timestamp: from, usage: null, offline: true }, { timestamp: to, usage: null, offline: true }];
  const interval = estimateSampleInterval(ordered);
  const threshold = Math.max(interval * 3, 60);
  const result: TimelinePoint[] = [];
  const first = ordered[0];
  if (first.timestamp - from > threshold) result.push({ timestamp: from, usage: null, offline: true }, { timestamp: Math.max(from + 1, first.timestamp - interval), usage: null, offline: true });
  ordered.forEach((point, index) => {
    const previous = ordered[index - 1];
    if (previous && point.timestamp - previous.timestamp > threshold) {
      result.push({ timestamp: previous.timestamp + interval, usage: null, offline: true });
      result.push({ timestamp: Math.max(previous.timestamp + interval + 1, point.timestamp - interval), usage: null, offline: true });
    }
    result.push({ timestamp: point.timestamp, usage: point, offline: false });
  });
  const last = ordered.at(-1)!;
  if (to - last.timestamp > threshold) result.push({ timestamp: last.timestamp + interval, usage: null, offline: true }, { timestamp: to, usage: null, offline: true });
  return result;
}
