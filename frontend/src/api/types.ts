export interface AgentRelease { version: string; assets: string[] }
export interface AgentReleases { releases: AgentRelease[]; latestVersion: string; checkedAt: number; stale: boolean; error?: string }

export interface AuthState {
  authEnabled: boolean;
  monitorVersion?: string;
  loggedIn: boolean;
  user: string;
}

export interface SiteSettings {
  siteName: string;
  siteDescription: string;
}

export interface Usage {
  cpuUsage: number;
  memoryTotal: number;
  memoryUsed: number;
  swapTotal: number;
  swapUsed: number;
  diskTotal: number;
  diskUsed: number;
  netRecv: number;
  netSend: number;
  netRecvSpeed: number;
  netSendSpeed: number;
  load1: number;
  load5: number;
  load15: number;
  timestamp: number;
}

export interface Host {
  agentVersion?: string;
  publicId: string;
  publicKey?: string;
  nickname: string;
  tags: string[];
  hostId: string;
  hostname: string;
  os: string;
  platform: string;
  platformVersion: string;
  kernelArch: string;
  modelName: string;
  cores: number;
  virtualizationRole: string;
  bootTime: number;
  lastSeen: number;
  createdAt: number;
  online: boolean;
  latest?: Usage | null;
}

export interface HostEnrollment extends Host {
  enrollmentToken: string;
  enrollmentExpiresAt: number;
}

export interface Tag {
  id: number;
  name: string;
  createdAt: number;
}

export interface NotificationRule {
  id: number;
  tag: string;
  url: string;
  notifyOnline: boolean;
  notifyOffline: boolean;
  enabled: boolean;
  createdAt: number;
}

export type LiveEvent =
  | { type: "status"; publicId: string; online: boolean }
  | { type: "usage"; publicId: string; data: Usage };

export type ThemePreference = "auto" | "light" | "dark";
export type RangeKey = "5m" | "1h" | "3h" | "1d" | "7d";
