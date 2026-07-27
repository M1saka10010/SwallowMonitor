import type { LiveEvent, Usage } from "../api/types";

function isUsage(value: unknown): value is Usage {
  if (!value || typeof value !== "object") return false;
  const usage = value as Partial<Usage>;
  return typeof usage.timestamp === "number" && typeof usage.cpuUsage === "number";
}

export function parseLiveEvent(raw: string): LiveEvent | null {
  try {
    const value = JSON.parse(raw) as Record<string, unknown>;
    if (typeof value.publicId !== "string" || !value.publicId) return null;
    if (value.type === "status" && typeof value.online === "boolean") return { type: "status", publicId: value.publicId, online: value.online };
    if (value.type === "usage" && isUsage(value.data)) return { type: "usage", publicId: value.publicId, data: value.data };
    return null;
  } catch {
    return null;
  }
}
