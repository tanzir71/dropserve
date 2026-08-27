# ADR-011: fsnotify with periodic reconciliation

Date: 2026-08-28 · Status: accepted · Supersedes: —

## Context

Dropserve must notice app-folder changes on Windows, Linux, and macOS without continuously walking every configured root. Operating-system notification buffers can overflow, and notification behavior is less reliable on network and sync-backed folders.

## Options considered

1. Poll every root — portable and simple, but repeatedly scans unchanged trees and makes sub-two-second updates expensive.
2. Write one native watcher per operating system — avoids a dependency but duplicates subtle platform code already maintained elsewhere.
3. Use `github.com/fsnotify/fsnotify` and retain a periodic full reconcile — a small, pure-Go cross-platform dependency with prompt native events, while reconciliation supplies correctness when events are lost.

## Decision

Pin `github.com/fsnotify/fsnotify` and debounce its events for 500 milliseconds. Watch depth and per-app watch counts are bounded. A 30-second full reconcile remains the correctness safety net; unreliable roots may fall back individually to two-second polling.

## Consequences

The base module gains one direct dependency and fsnotify's platform syscall module. Native events improve responsiveness but are treated as hints, never as the sole source of truth. The zero-CGO cross-build gate continues to guard portability.
