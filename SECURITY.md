# Security configuration and upgrade notes

## Production deployment

The server now refuses production startup without a GitHub OAuth client ID,
client secret, non-empty administrator allowlist, and an HTTPS publicUrl origin.
All allowlisted users are administrators. Anonymous monitoring views remain public.
Copy config.example.yaml and supply your actual OAuth configuration before startup.

Terminate TLS at a trusted reverse proxy, proxy WebSocket upgrades and SSE without
buffering, and redirect HTTP to HTTPS. Enable HSTS there after confirming HTTPS
works. The backend HTTP listener must not be exposed directly to untrusted networks.
Compose publishes port 8080 on 127.0.0.1 only. Configure trustedProxies with only
the actual proxy addresses, and ensure the proxy appends the true client address.
Otherwise per-IP limits behind a proxy may apply to all users as one source.

For local development only, set insecureDevelopment: true and listen:
127.0.0.1:8080 (or [::1]:8080). This explicitly grants anonymous management access.
Set publicUrl to the matching loopback origin (for example http://127.0.0.1:8080),
or omit it to derive it from listen. Open that exact origin in the browser.
Do not forward or publish this development listener through a reverse proxy.

## Behavior changes

- Logout is POST-only. Browser management writes reject cross-origin requests.
- OAuth login state is short-lived, one-time, browser-bound, capacity-limited, and
  rate-limited. Empty administrator allowlists no longer admit all GitHub users.
- Notification destinations must resolve exclusively to public IPs, including
  at connection time. Loopback, LAN, link-local, reserved addresses and redirects
  are blocked. Existing internal notification URLs stop working; use a public
  notification endpoint. Outbound proxy environment variables are not used.
- Agent messages are limited to 1 MiB and 120 messages/minute per host. Reports
  must arrive within the connection idle timeout; measurement timestamps must be
  within five minutes of the server clock (zero means server time). Synchronize
  agent clocks. Last-seen always uses server receipt time.
- Deleting a host closes its active connection and deletes all its history tiers.
- Usage API ranges reject negative/far-future timestamps, retain the seven-day
  window cap, and return at most 10,000 points with a bounded query duration.
- SSE is limited to 256 connections globally and 8 per source IP, with write
  deadlines. Edge rate/connection limits are still recommended.
- Sensitive JSON responses are no-store. Do not override this in CDN/proxy rules.
- Agents prove possession of an bound Ed25519 private key using
  a fresh challenge, with a fixed 10-second deadline and 4096-byte message limit
  during authentication. Online state follows successful authentication.
- Use WSS with server certificate verification. Plain WS is only for loopback
  development with Agent devMode enabled; it is not a production fallback.

## Credentials and dependencies

If a previous instance was publicly reachable without authentication, treat its
Agent tokens and notification secrets as potentially disclosed. Replace affected
credentials after applying the fix; this update cannot revoke copies already stolen.
Back up the SQLite database securely and restrict configuration/database permissions.

Default enrollment uses a random 64-hex, 15-minute, single-use credential. Only its
SHA-256 digest and expiry are stored; plaintext is returned once on creation or
administrator renewal, never by list/detail endpoints. Renewal invalidates the old
credential and is forbidden after binding. Consumption atomically binds an unbound
host, never replaces a bound key, and requires no extra administrator approval.
Possession permits first-claim takeover: this is an explicitly accepted risk, not
proof of device identity. Keep the command secret, including shell history, terminal
logs, screenshots and clipboard contents. Passing it via stdin does not erase history.

Choose an explicitly verified published Agent release in the panel; no target version is assumed or prefilled. Strict vSemver validation blocks empty/invalid input and never resolves latest. The command downloads bootstrap.sh from that fixed GitHub release over HTTPS (redirects also HTTPS), into a root-created private temporary file, then executes only after download success and cleans up on exit. Non-root users use sudo. Download stdin is /dev/null; the credential reaches bootstrap only via stdin, never arguments or curl | bash. Bootstrap downloads and checksums the matching release assets; no manual binary download is needed. HTTPS and checksums from the same release do not provide an independent publisher signature: trust the repository/release publisher and protect tags/assets. See AUTHENTICATION.md for installation and upgrades. A lost enrollment response must first be recovered using the existing
private key and normal challenge handshake, not by replacing the key. Unbound legacy
hosts can receive fresh enrollment credentials without losing history; old tokens
remain invalid. Deleting/recreating any host deletes history, so back up first.

Public keys are not secrets. Private keys must remain protected on each Agent.
The legacy Token-to-key migration channel (standalone migrate-token-to-key.sh, its
bootstrap mode and the panel migration command) was removed with Agent v0.1.2 to
keep the Monitor lean. Pending hosts offer only the new-install (install.sh)
command. Refresh credentials on the same old host record to retain history. Bound
hosts use the regular upgrade entry, not any migration path. Hosts still on
released v0.0.2 Token installations must be migrated manually using the Agent
v0.1.1 transition tag (git checkout v0.1.1) following that version's
documentation; old Token authentication remains invalid. A copied private key can still impersonate that device: a newly authenticated
connection for the same key replaces the old one. Automatic enrollment is not hardware attestation
or an in-place key rotation mechanism.

Use the Node version specified in frontend/package.json (Docker uses Node 24).
Run npm ci, npm test, npm run build in frontend before building the Go binary so
the embedded assets include the security fixes. Audit development dependencies as
well as production dependencies. Never expose Vite/Vitest development servers to
untrusted networks.
