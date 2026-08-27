# ADR-010: Generate QR codes locally in pure Go

Date: 2026-08-28 · Status: accepted · Supersedes: —

## Context

Dropserve shows QR codes for private LAN and app addresses. Sending those addresses to a hosted QR service would leak local network information and make a core offline feature depend on the internet. A correct QR encoder is also sufficiently intricate that a new in-house implementation would carry avoidable interoperability risk.

## Options considered

1. A hosted QR API — small locally, but discloses addresses and fails offline.
2. A handwritten encoder — dependency-free, but security-irrelevant custom complexity with a large standards test surface.
3. `github.com/skip2/go-qrcode` — the handover-selected pure-Go encoder; one direct module, no transitive modules, no CGO, and no network calls at runtime.

## Decision

Use the pinned `github.com/skip2/go-qrcode` module to encode validated HTTP(S) URLs into PNG responses entirely inside the Dropserve process.

## Consequences

The direct dependency count increases by one. Cross-builds remain `CGO_ENABLED=0`, QR generation works offline, and private URLs never leave the machine. The endpoint caps input length and accepts only HTTP or HTTPS URLs.
