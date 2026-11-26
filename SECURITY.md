# Security Policy

## Network & Trust Model

Perch is designed to run on a **private network** (such as Tailscale or a trusted LAN) between your phone and your own computer:

- `perch-bridge` binds to port `45999` by default and requires a bearer pairing token for API requests.
- Traffic between the Android app and `perch-bridge` relies on your underlying private network transport (for example, Tailscale WireGuard encryption).
- **Do not expose `perch-bridge` directly to the public internet.** Always use Tailscale, a VPN, or a firewall restricted to trusted interfaces.
- Pairing tokens are stored locally in `~/.cache/perch-bridge/` with mode `0700`.
- Perch sends no telemetry and requires no hosted account. Your source code and prompts stay on your machine.
- The optional `relay/` module enforces a strict push-payload field whitelist (`relay/test/payload_test.go`) so notification summaries cannot carry code or terminal transcripts.

## Reporting a Vulnerability

If you discover a security issue, please report it privately using GitHub Security Advisories on `th2zz/perch` rather than opening a public issue.
