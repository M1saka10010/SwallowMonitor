import { expect, it } from "vitest";
import { binaryAsset, compareVersions, releaseSupports, stableRelease, versionStatus } from "./releases";
it.each([
 ["v9007199254740993.0.0", "v9007199254740992.0.0", 1],
 ["v1.2.3-99999999999999999999", "v1.2.3-99999999999999999998", 1],
 ["v1.0.0-alpha", "v1.0.0-alpha.1", -1], ["v1.0.0-beta.2", "v1.0.0-beta.11", -1],
 ["v1.0.0-1", "v1.0.0-alpha", -1], ["v1.0.0-rc.1", "v1.0.0", -1],
 ["v1.2.3+abc", "1.2.3+xyz", 0], ["v1.2.3-x-y", "v1.2.3-x-z", -1],
] as const)("compares %s and %s", (a, b, order) => { expect(compareVersions(a,b)).toBe(order); expect(compareVersions(b,a)).toBe(order === 0 ? 0 : -order); });
it.each(["dev", "unknown", "", "v01.0.0", "v1.0.0-01", "1.0.0\n"])("rejects unknown or invalid %s", (v) => expect(compareVersions(v,"v1.0.0")).toBeUndefined());
it("ignores build metadata but excludes prereleases from release selection", () => { expect(stableRelease("v1.0.0+build-a")).toBe(true); expect(stableRelease("v1.0.0-rc.1")).toBe(false); expect(versionStatus("v1.0.0", "v1.0.0", true)).toContain("无法确认"); });
const assets = ["bootstrap.sh", "checksums.txt", "install.sh", "migrate-token-to-key.sh", "upgrade.sh", "swallow-agent-linux-amd64"];
it.each(["install", "migrate-token-to-key", "upgrade"] as const)("gates every required asset for %s", (mode) => {
  expect(releaseSupports({version:"v1.0.0",assets},mode)).toBe(true);
  for (const missing of ["bootstrap.sh","checksums.txt",`${mode}.sh`,"swallow-agent-linux-amd64"]) expect(releaseSupports({version:"v1.0.0",assets:assets.filter(a=>a!==missing)},mode)).toBe(false);
  expect(releaseSupports({version:"v1.0.0",assets},mode,"swallow-agent-linux-arm64")).toBe(false);
});
it("uses the reported kernel architecture and rejects unsupported systems", () => {
 expect(binaryAsset({os:"linux",kernelArch:"x86_64"})).toBe("swallow-agent-linux-amd64");
 expect(binaryAsset({os:"linux",kernelArch:"aarch64"})).toBe("swallow-agent-linux-arm64");
 expect(binaryAsset({os:"windows",kernelArch:"x86_64"})).toBeUndefined();
 expect(binaryAsset({os:"linux",kernelArch:"riscv64"})).toBeUndefined();
});
