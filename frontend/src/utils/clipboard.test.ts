import { execFileSync } from "node:child_process";
import { afterEach, expect, it, vi } from "vitest";
import { installationCommand } from "./clipboard";

afterEach(() => vi.unstubAllGlobals());

it.each(["http:", "https:"])("quotes arguments and pins the script for %s", (protocol) => {
  vi.stubGlobal("location", { protocol, host: "monitor.example:8080" });
  expect(installationCommand("token-123")).toBe(`curl '-fsSL' 'https://raw.githubusercontent.com/M1saka10010/SwallowAgent/e430832927629f9c228b92558f409a65b589d7f0/install.sh' | bash '-s' '--' '${protocol === "https:" ? "wss" : "ws"}://monitor.example:8080/report' 'token-123'`);
});

it.each(["", "a'b'c", "spaces tabs\tand\nnewlines", "$(printf INJECTED); `printf INJECTED` & | > < * ? \" \\ $HOME", "--flag"])("preserves a literal token: %j", (token) => {
  const host = "monitor.example';$(printf INJECTED)";
  vi.stubGlobal("location", { protocol: "https:", host });
  const command = installationCommand(token);
  const output = execFileSync("/bin/sh", ["-c", `curl() { :; }; bash() { printf '%s\\0' "$@"; }; ${command}`], { encoding: "utf8" });
  expect(output.split("\0")).toEqual(["-s", "--", `wss://${host}/report`, token, ""]);
});
