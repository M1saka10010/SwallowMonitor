import type { AgentRelease, Host } from "../api/types";
import { isAgentRelease } from "./enrollment";

function parts(value: string) {
  const tag = value.startsWith("v") ? value : `v${value}`;
  if (!isAgentRelease(tag)) return undefined;
  const [core, pre] = tag.slice(1).split("+")[0].split(/-(.*)/s);
  return { core: core.split("."), pre: pre?.split(".") };
}
const numeric = (a: string, b: string) => a.length !== b.length ? Math.sign(a.length - b.length) : a === b ? 0 : a < b ? -1 : 1;
export function compareVersions(a: string, b: string): number | undefined {
  const left = parts(a), right = parts(b);
  if (!left || !right) return undefined;
  for (let i = 0; i < 3; i++) { const order = numeric(left.core[i], right.core[i]); if (order) return order; }
  if (!left.pre || !right.pre) return left.pre ? -1 : right.pre ? 1 : 0;
  for (let i = 0; i < Math.max(left.pre.length, right.pre.length); i++) {
    const x = left.pre[i], y = right.pre[i];
    if (x === undefined || y === undefined) return x === undefined ? -1 : 1;
    if (x === y) continue;
    const nx = /^\d+$/.test(x), ny = /^\d+$/.test(y);
    return nx && ny ? numeric(x, y) : nx !== ny ? nx ? -1 : 1 : x < y ? -1 : 1;
  }
  return 0;
}
export const stableRelease = (value: string) => isAgentRelease(value) && !parts(value)?.pre;
export function binaryAsset(host: Pick<Host, "os" | "kernelArch">) {
  if (host.os.toLowerCase() !== "linux") return undefined;
  const arch = ({ x86_64: "amd64", amd64: "amd64", aarch64: "arm64", arm64: "arm64" } as Record<string, string>)[host.kernelArch];
  return arch ? `swallow-agent-linux-${arch}` : undefined;
}
export function releaseSupports(release: AgentRelease | undefined, mode: "install" | "upgrade", binary?: string) {
  if (!release || !stableRelease(release.version)) return false;
  return ["bootstrap.sh", "checksums.txt", `${mode}.sh`].every((asset) => release.assets.includes(asset)) &&
    (binary ? release.assets.includes(binary) : ["amd64", "arm64"].some((arch) => release.assets.includes(`swallow-agent-linux-${arch}`)));
}
export function versionStatus(current: string, latest: string, uncertain: boolean) {
  const order = compareVersions(current, latest);
  if (compareVersions(current, current) === undefined) return "版本未知，无法判断是否需升级";
  if (uncertain || order === undefined) return "发布状态未知，无法确认是否最新";
  return order < 0 ? `需升级 · 最新正式版 ${latest}` : order === 0 ? "已是最新正式版" : "高于最新正式版";
}
