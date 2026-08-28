# ADR-013: libp2p zeroconf for mDNS

Date: 2026-08-28 · Status: accepted · Supersedes: —

## Context

Dropserve may advertise a `.local` hostname only when the mDNS responder can coexist with the operating system's own UDP/5353 listener. The M7 handover therefore requires a bounded real-network spike on Windows and Linux before choosing a library. A failed bind must degrade by omitting the `.local` URL, never by advertising a dead link.

## Options considered

1. `github.com/libp2p/zeroconf/v2` — the preferred pure-Go implementation, contingent on a successful Windows bind.
2. `github.com/betamos/zeroconf` — the prescribed fallback if the preferred implementation cannot share UDP/5353 on Windows.
3. No Windows mDNS — the required safe fallback if neither library can bind alongside the native responder.

## Decision

Pin `github.com/libp2p/zeroconf/v2` at v2.2.0. The exact `scripts/spike/mdns.go` program advertised `dropserve-spike.local.` for 20 seconds and shut down cleanly on Windows/amd64 and Linux/amd64. The Windows run included the native Ethernet interface and the WSL virtual multicast interface; the Linux run used its real `eth0`. Both reported `mDNS bind succeeded`, so the fallback library was not evaluated.

## Consequences

The base module gains one direct pure-Go dependency plus its DNS/network transitive modules. Production discovery must catch every responder startup failure and omit the `.local` entry from advertised URLs. The zero-CGO cross-build and invariant-I3 URL checks remain mandatory.
