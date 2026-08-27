# ADR-008: Installed Tailscale CLI in version 1

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

Remote access should reuse the identity and network the user already configured, with no second login flow or secret.

## Options considered

1. Embedded `tsnet` — independent and powerful, but creates a second tailnet node and an authentication lifecycle inside Dropserve.
2. The installed Tailscale CLI and local state — uses the existing node, MagicDNS name, and user session.

## Decision

Detect and control Tailscale through the installed client in version 1; defer `tsnet`.

## Consequences

Remote sharing depends on a running Tailscale installation. Missing, logged-out, and unsupported states need clear explanations rather than dead links.

