import { execFileSync, spawnSync } from "node:child_process";
import { expect, it } from "vitest";
import { enrollmentCommand, isAgentRelease, shellQuote } from "./enrollment";

it.each([false, true])("explicitly opts local WS into development mode (migrate=%s)", (migrate) => {
  expect(enrollmentCommand("a".repeat(64), "ws://localhost:8080/report", "v9.8.7", migrate)).toMatch(/ --dev-mode\)$/);
  expect(enrollmentCommand("a".repeat(64), "wss://monitor.example.com/report", "v9.8.7", migrate)).not.toContain("--dev-mode");
});

it.each(["a".repeat(64), "a'b ; $(printf injected) \n `printf bad` \" \\ %s", ""])("quotes literal shell data: %s", (value) => {
  expect(execFileSync("bash", ["-c", "printf %s " + shellQuote(value)], { encoding: "utf8" })).toBe(value);
});

it.each(["", "latest", "1.2.3", "v01.2.3", "v1.2.3-01", "v1.2.3-", "v1.2.3+a..b", "v1.2.3\n", "v1.2.3;id", "v1.2.3/../../main"])("rejects unsafe or non-SemVer release %s", (release) => {
  expect(isAgentRelease(release)).toBe(false);
  expect(() => enrollmentCommand("secret", "wss://example.com/report", release)).toThrow();
});
it.each(["v0.0.0", "v9.8.7", "v1.2.3-rc.1+build.01"])("accepts strict SemVer %s", (release) => expect(isAgentRelease(release)).toBe(true));

it.each([false, true])("downloads before execution, isolates stdin, quotes arguments and cleans up (migrate=%s)", (migrate) => {
  const token = "a'b $(printf bad)";
  const url = "wss://monitor.example.com/report?x='; printf injected; #";
  const setup = "id() { printf '%s' \"$TEST_UID\"; }; sudo() { echo sudo-called >&2; \"$@\"; }; curl() { [ -z \"$(cat)\" ] || return 99; case \"$*\" in *\"--proto =https --proto-redir =https\"*) ;; *) return 98;; esac; while [ \"$1\" != --output ]; do shift; done; file=$2; case \"$3\" in https://github.com/M1saka10010/SwallowAgent/releases/download/v9.8.7/bootstrap.sh) ;; *) return 97;; esac; printf '%s\n' 'printf \"%s\\n\" \"$@\"; cat' > \"$file\"; [ \"${FAIL_DOWNLOAD:-0}\" = 0 ]; }; rm() { command rm \"$@\"; [ ! -e \"${!#}\" ] && echo cleaned >&2; }; export -f id sudo curl rm; ";
  for (const uid of ["0", "1000"]) {
    const result = spawnSync("bash", ["-c", setup + enrollmentCommand(token, url, "v9.8.7", migrate)], { encoding: "utf8", env: { ...process.env, TEST_UID: uid } });
    expect(result.status).toBe(0);
    expect(result.stdout).toBe(["--version", "v9.8.7", "--mode", migrate ? "migrate-token-to-key" : "install", "--report-url", url, "--enrollment-token-stdin", token].join("\n"));
    expect(result.stderr).toContain("cleaned");
    expect(result.stderr.includes("sudo-called")).toBe(uid !== "0");
  }
  const failure = spawnSync("bash", ["-c", setup + enrollmentCommand(token, url, "v9.8.7", migrate)], { encoding: "utf8", env: { ...process.env, TEST_UID: "0", FAIL_DOWNLOAD: "1" } });
  expect(failure.status).not.toBe(0);
  expect(failure.stdout).toBe("");
  expect(failure.stderr).toContain("cleaned");
});

it.each(["download", "bootstrap"])("forwards termination and waits before cleanup during %s", (stage) => {
  const child = 'trap \'echo child-stopped >&2; exit 143\' TERM; kill -TERM "$PPID"; while :; do :; done';
  const setup = 'id() { echo 0; }; curl() { while [ "$1" != --output ]; do shift; done; printf %s "$CHILD_SCRIPT" > "$2"; if [ "$STAGE" = download ]; then trap \'echo child-stopped >&2; exit 143\' TERM; kill -TERM "$$"; while :; do :; done; fi; }; rm() { command rm "$@"; echo cleaned >&2; }; export -f id curl rm; ';
  const result = spawnSync("bash", ["-c", setup + enrollmentCommand("secret", "wss://example.com/report", "v9.8.7")], { encoding: "utf8", timeout: 5000, env: { ...process.env, CHILD_SCRIPT: child, STAGE: stage } });
  expect(result.status).toBe(143);
  expect(result.stderr).toMatch(/child-stopped[\s\S]*cleaned/);
  expect(result.stdout).toBe("");
});
