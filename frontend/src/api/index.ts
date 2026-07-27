import { jsonBody, requestJson } from "./client";
import type { AuthState, Host, NotificationRule, SiteSettings, Tag, Usage } from "./types";

export const api = {
  me: () => requestJson<AuthState>("/api/me"),
  settings: () => requestJson<SiteSettings>("/api/settings"),
  updateSettings: (settings: SiteSettings) => requestJson<SiteSettings>("/api/settings", { method: "PATCH", body: jsonBody(settings) }),
  hosts: () => requestJson<Host[]>("/api/hosts"),
  host: (id: string, signal?: AbortSignal) => requestJson<Host>(`/api/hosts/${encodeURIComponent(id)}`, { signal }),
  usage: async (id: string, from: number, to: number, signal?: AbortSignal) =>
    (await requestJson<Usage[] | null>(`/api/hosts/${encodeURIComponent(id)}/usage?from=${from}&to=${to}`, { signal })) ?? [],
  createHost: (body: { nickname: string; token: string; tags: string[] }) => requestJson<Host>("/api/hosts", { method: "POST", body: jsonBody(body) }),
  updateHost: (id: string, body: { nickname: string; tags: string[] }) => requestJson<{ status: string }>(`/api/hosts/${encodeURIComponent(id)}`, { method: "PATCH", body: jsonBody(body) }),
  deleteHost: (id: string) => requestJson<{ status: string }>(`/api/hosts/${encodeURIComponent(id)}`, { method: "DELETE" }),
  tags: () => requestJson<Tag[]>("/api/tags"),
  createTag: (name: string) => requestJson<Tag>("/api/tags", { method: "POST", body: jsonBody({ name }) }),
  updateTag: (id: number, name: string) => requestJson<Tag>(`/api/tags/${id}`, { method: "PATCH", body: jsonBody({ name }) }),
  deleteTag: (id: number) => requestJson<{ status: string }>(`/api/tags/${id}`, { method: "DELETE" }),
  notifications: () => requestJson<NotificationRule[]>("/api/notifications"),
  createNotification: (body: Omit<NotificationRule, "id" | "createdAt">) => requestJson<NotificationRule>("/api/notifications", { method: "POST", body: jsonBody(body) }),
  updateNotification: (id: number, body: Omit<NotificationRule, "id" | "createdAt">) => requestJson<NotificationRule>(`/api/notifications/${id}`, { method: "PATCH", body: jsonBody(body) }),
  deleteNotification: (id: number) => requestJson<{ status: string }>(`/api/notifications/${id}`, { method: "DELETE" }),
};
