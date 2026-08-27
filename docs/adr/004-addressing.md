# ADR-004: Path addressing with a dedicated-port escape hatch

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

LAN clients need one memorable address without local DNS configuration, while some apps assume they own the URL root.

## Options considered

1. A primary port per app — robust but makes users remember ports.
2. Subdomains — readable but depend on DNS or hosts-file changes that do not work as a zero-configuration LAN default.
3. Paths with a stable dedicated port available for every app — one primary address plus an unbreakable compatibility route.

## Decision

Serve apps primarily at `/<slug>/` and assign each app a stable port in the 7400–7999 range.

## Consequences

The proxy needs a subpath survival kit and failure probe; every app still has a root-mounted address when rewriting is insufficient.

