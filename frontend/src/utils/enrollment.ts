export const shellQuote = (value: string) => "'" + value.replaceAll("'", "'\"'\"'") + "'";

export function isAgentRelease(value: string) {
  return /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/.test(value) && !/\s/.test(value);
}

function bootstrapCommand(release: string, args: string, proxy: string) {
  if (!isAgentRelease(release)) throw new Error("Invalid Agent release");
  if (!isGithubProxy(proxy)) throw new Error("Invalid GitHub proxy");
  const url = `${proxy ? proxy.replace(/\/+$/, "") + "/" : ""}https://github.com/M1saka10010/SwallowAgent/releases/download/${encodeURIComponent(release)}/bootstrap.sh`;
  const script = "set -eu; umask 077; file=$(mktemp); child=; launching=0; pending=; trap 'rm -f -- \"$file\"' EXIT; stop() { pending=${pending:-$1}; [ \"$launching\" -eq 0 ] || return 0; trap '' INT TERM HUP; if [ -n \"$child\" ]; then kill -TERM \"$child\" 2>/dev/null || true; wait \"$child\" 2>/dev/null || true; fi; exit \"$pending\"; }; trap 'stop 130' INT; trap 'stop 143' TERM; trap 'stop 129' HUP; launching=1; curl --disable --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 300 --output \"$file\" \"$1\" </dev/null & child=$!; launching=0; [ -z \"$pending\" ] || stop \"$pending\"; status=0; wait \"$child\" || status=$?; child=; [ \"$status\" -eq 0 ] || exit \"$status\"; shift; exec 3<&0; launching=1; bash \"$file\" \"$@\" <&3 3<&- & child=$!; launching=0; [ -z \"$pending\" ] || stop \"$pending\"; status=0; wait \"$child\" || status=$?; child=; exit \"$status\"";
  return `(if [ "$(id -u)" -eq 0 ]; then runner=; else runner=sudo; fi; $runner bash -c ${shellQuote(script)} -- ${shellQuote(url)} --version ${shellQuote(release)} ${args}${proxy ? " --github-proxy " + shellQuote(proxy.replace(/\/+$/, "")) : ""})`;
}

export function isGithubProxy(value: string) {
  if (!value) return true;
  if (!/^https:\/\/[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?(\/[A-Za-z0-9._~-]+)*\/?$/.test(value) || /\s/.test(value) || /\/(?:\.|\.\.)(?:\/|$)/.test(value)) return false;
  try { const url = new URL(value); return !!url.hostname && !url.username && !url.password; } catch { return false; }
}

export function enrollmentCommand(token: string, reportUrl: string, release: string, migrate = false, proxy = "") {
  return `printf %s ${shellQuote(token)} | ${bootstrapCommand(release, `--mode ${migrate ? "migrate-token-to-key" : "install"} --report-url ${shellQuote(reportUrl)} --enrollment-token-stdin${reportUrl.startsWith("ws:") ? " --dev-mode" : ""}`, proxy)}`;
}

export function upgradeCommand(release: string, proxy = "") {
  return bootstrapCommand(release, "--mode upgrade", proxy);
}
