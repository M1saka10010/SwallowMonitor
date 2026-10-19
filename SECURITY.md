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
- Custom Agent tokens are limited to 128 ASCII letters, digits, dots, underscores,
  tildes, and hyphens so they remain unambiguous through the pinned installer and
  YAML configuration. Rotate any older custom token containing other characters.

## Credentials and dependencies

If a previous instance was publicly reachable without authentication, treat its
Agent tokens and notification secrets as potentially disclosed. Replace affected
credentials after applying the fix; this update cannot revoke copies already stolen.
Back up the SQLite database securely and restrict configuration/database permissions.

Installation command arguments are shell-quoted. Review the installation script
before running it, preferably without elevated privileges where possible. A fixed
script revision does not guarantee the safety of all artifacts it downloads.

Use the Node version specified in frontend/package.json (Docker uses Node 24).
Run npm ci, npm test, npm run build in frontend before building the Go binary so
the embedded assets include the security fixes. Audit development dependencies as
well as production dependencies. Never expose Vite/Vitest development servers to
untrusted networks.
