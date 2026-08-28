# Security policy

## Reporting a vulnerability

Please report security issues privately by emailing [tanzir71@gmail.com](mailto:tanzir71@gmail.com) or by using this repository's private GitHub Security Advisory form. Do not open a public issue for an unpatched vulnerability.

You should receive an acknowledgement within seven days. The project uses a 90-day coordinated-disclosure window unless a shorter timeline is needed because exploitation is already public. Reports should include the affected version, operating system, reproduction steps, impact, and any suggested mitigation.

## Threat model

Dropserve is a personal server for a home or office LAN. It serves files and proxies processes on behalf of the person who controls the computer. It is not a hardened public hosting platform, multi-user service, or sandbox.

Dropserve defends:

- resolved static paths against lexical, encoded, symlink, UNC, and platform-specific traversal;
- dashboard mutations with same-origin validation and a per-process CSRF token;
- optional non-loopback access with a six-digit PIN and HMAC-signed, 30-day, HttpOnly, SameSite session cookie;
- request handling with body, header, connection, timeout, and SSE-client limits;
- accidental public exposure by requiring an explicit per-app Funnel action and exact-slug confirmation;
- operating-system trust changes by keeping local HTTPS and CA installation separate, explicit, and reversible;
- downloaded runtime packs with pinned URLs, SHA-256 verification, archive traversal checks, and executable verification;
- release artifacts with checksums, SPDX SBOMs, provenance attestations, Authenticode identity on Windows, and Sigstore bundles.

The following are deliberate limitations:

- Anyone on the same LAN can open an app whose `visibility` is `lan` unless the optional PIN lock is enabled.
- App code runs with the signed-in user's privileges. Dropserve does not sandbox apps or isolate them from each other or the user's files. Adding an untrusted command app is equivalent to running untrusted code.
- Tailscale Funnel makes the selected app reachable from the public internet. Unless the optional global PIN lock is enabled, Dropserve adds no per-app authentication.
- A self-signed Authenticode certificate provides identity and tamper evidence but does not guarantee Microsoft SmartScreen reputation.

## Supported versions

Until the first stable release, security fixes are made on the `main` branch. After v1.0, the latest stable release and `main` will receive security fixes.

## Safe removal

The supported uninstaller removes Dropserve's process, start-at-login registration, firewall rules, trusted local CA, and files under Dropserve's own install/state directories. It never removes the user's Apps folders.
