import { execFileSync, spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { expect, it } from "vitest";
import { enrollmentCommand, isAgentRelease, isGithubProxy, upgradeCommand, shellQuote } from "./enrollment";

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

it.each(["download", "bootstrap"].flatMap(stage => ["waiting", "before-launch", "before-register"].flatMap(window => ["INT", "TERM", "HUP"].map(signal => ({ stage, window, signal })))))("forwards and reaps $signal at $stage/$window", async ({ stage, window, signal }) => {
  const dir = mkdtempSync(join(tmpdir(), "enrollment-signal-"));
  const ready = join(dir, "ready");
  execFileSync("mkfifo", [ready, ready + ".idle"]);
  const childScript = 'trap \'kill -TERM "$PARENT"; echo child-stopped >&2; exit 143\' TERM; echo "$BASHPID" > "$PID_FILE"; echo ready > "$READY"; exec 4<>"$READY.idle"; read -r -t 3 line <&4; echo child-expired >&2; exit 99';
  const setup = 'id() { echo 0; }; curl() { while [ "$1" != --output ]; do shift; done; if [ "$STAGE" = download ]; then PARENT=$$; eval "$CHILD_SCRIPT"; else printf %s "$CHILD_SCRIPT" > "$2"; fi; }; rm() { command rm "$@"; echo cleaned >&2; }; export -f id curl rm; ';
  let command = enrollmentCommand("secret", "wss://example.com/report", "v9.8.7");
  let launch = 0;
  command = command.replaceAll("launching=1; ", match => ++launch === (stage === "download" ? 1 : 2)
    ? match + 'export PARENT=$$; ' + (window === "before-launch" ? 'kill -' + signal + ' "$$"; kill -TERM "$$"; ' : "") : match);
  let registration = 0;
  command = command.replaceAll("child=$!;", match => {
    if (++registration !== (stage === "download" ? 1 : 2)) return match;
    const barrier = 'read -r -t 3 ready < "$READY"; ';
    const send = 'kill -' + signal + ' "$$"; kill -TERM "$$"; ';
    return window === "waiting" ? match + barrier : barrier + (window === "before-register" ? send : "") + match;
  });
  let waiting = 0;
  command = command.replaceAll("status=0;", match => ++waiting === (stage === "download" ? 1 : 2) && window === "waiting" ? 'kill -' + signal + ' "$$"; ' + match : match);
  const child = spawn("bash", ["-c", setup + command], { detached: true, env: { ...process.env, CHILD_SCRIPT: childScript, STAGE: stage, READY: ready, PID_FILE: join(dir, "pid") }, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "", stderr = "";
  child.stdout.on("data", data => { stdout += data; });
  child.stderr.on("data", data => { stderr += data; });
  let timedOut = false;
  const timer = setTimeout(() => { timedOut = true; process.kill(-child.pid!, "SIGKILL"); }, 4500);
  try {
    const [code, exitSignal] = await new Promise<[number | null, string | null]>((resolve, reject) => {
      child.on("error", reject);
      child.on("close", (code, sig) => resolve([code, sig]));
    });
    const diagnostic = JSON.stringify({ code, exitSignal, timedOut, stdout, stderr });
    expect(timedOut, diagnostic).toBe(false);
    expect(exitSignal, diagnostic).toBe(null);
    expect(code, diagnostic).toBe({ INT: 130, TERM: 143, HUP: 129 }[signal as "INT" | "TERM" | "HUP"]);
    expect(stderr, diagnostic).toMatch(/child-stopped[\s\S]*cleaned/);
    expect(stdout, diagnostic).toBe("");
    const pid = Number(readFileSync(join(dir, "pid"), "utf8"));
    expect(() => process.kill(pid, 0), diagnostic).toThrow();
  } finally {
    clearTimeout(timer);
    try { process.kill(-child.pid!, "SIGKILL"); } catch { /* The process group has already exited. */ }
    rmSync(dir, { recursive: true, force: true });
  }
});
it.each(["http://proxy.example.com", "https://u:p@proxy.example.com", "https://proxy.example.com?x", "https://proxy.example.com#x", "https://proxy.example.com\n", "https://proxy.example.com/%0a", "https://proxy.example.com/../x", "https://proxy.example.com/./x", "https://[::1]", "https://proxy.example.com//", "https://proxy.example.com/中文", "https://proxy.example.com/a'b"])("rejects unsupported proxy %s", (proxy) => { expect(isGithubProxy(proxy)).toBe(false); expect(() => upgradeCommand("v1.0.0", proxy)).toThrow(); });
it.each(["https://proxy.example.com", "https://proxy.example.com:8443/path_1/~cache/", ""])("accepts safe proxy %s", (proxy) => expect(isGithubProxy(proxy)).toBe(true));
it.each([false,true])("upgrade downloads safely before execution (failure=%s)", (fail) => {
  const setup = `id() { echo 0; }; curl() { while [ "$1" != --output ]; do shift; done; echo "$3" >&2; printf %s 'printf "%s\\n" "$@"' > "$2"; return ${fail ? 1 : 0}; }; rm() { command rm "$@"; echo cleaned >&2; }; export -f id curl rm; `;
  const command = upgradeCommand("v9.8.7", "https://proxy.example.com/cache/");
  expect(command).not.toContain("enrollment-token"); expect(command).not.toContain("--report-url");
  const result = spawnSync("bash", ["-c",setup + command], {encoding:"utf8"});
  expect(result.status).toBe(fail ? 1 : 0);
  expect(result.stdout).toBe(fail ? "" : "--version\nv9.8.7\n--mode\nupgrade\n--github-proxy\nhttps://proxy.example.com/cache\n");
  expect(result.stderr).toContain("https://proxy.example.com/cache/https://github.com/M1saka10010/SwallowAgent/releases/download/v9.8.7/bootstrap.sh");
  expect(result.stderr).toContain("cleaned");
});
