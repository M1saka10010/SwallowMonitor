export const shellQuote = (value: string) => "'" + value.replaceAll("'", "'\"'\"'") + "'";

export function isAgentRelease(value: string) {
  return /^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/.test(value) && !/\s/.test(value);
}

export function enrollmentCommand(token: string, reportUrl: string, release: string, migrate = false) {
  if (!isAgentRelease(release)) throw new Error("Invalid Agent release");
  const url = `https://github.com/M1saka10010/SwallowAgent/releases/download/${encodeURIComponent(release)}/bootstrap.sh`;
  const script = "set -eu; umask 077; file=$(mktemp); child=; trap 'rm -f -- \"$file\"' EXIT; stop() { trap '' INT TERM HUP; if [ -n \"$child\" ]; then kill -TERM \"$child\" 2>/dev/null || true; wait \"$child\" 2>/dev/null || true; fi; exit \"$1\"; }; trap 'stop 130' INT; trap 'stop 143' TERM; trap 'stop 129' HUP; curl --disable --fail --silent --show-error --location --proto '=https' --proto-redir '=https' --tlsv1.2 --connect-timeout 15 --max-time 300 --output \"$file\" \"$1\" </dev/null & child=$!; status=0; wait \"$child\" || status=$?; child=; [ \"$status\" -eq 0 ] || exit \"$status\"; shift; exec 3<&0; bash \"$file\" \"$@\" <&3 3<&- & child=$!; status=0; wait \"$child\" || status=$?; child=; exit \"$status\"";
  return `printf %s ${shellQuote(token)} | (if [ "$(id -u)" -eq 0 ]; then runner=; else runner=sudo; fi; $runner bash -c ${shellQuote(script)} -- ${shellQuote(url)} --version ${shellQuote(release)} --mode ${migrate ? "migrate-token-to-key" : "install"} --report-url ${shellQuote(reportUrl)} --enrollment-token-stdin${reportUrl.startsWith("ws:") ? " --dev-mode" : ""})`;
}
