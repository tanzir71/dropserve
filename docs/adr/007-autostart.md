# ADR-007: Per-user native autostart, not a Windows Service

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

Dropserve must start quietly after login, read user folders, and see the user's Tailscale identity without elevation.

## Options considered

1. Windows Service — reliable but elevated, runs under another identity, and does not fit tray or browser interactions.
2. Startup folder or registry Run key — simple but prone to visible windows, user cleanup, and states that are difficult to report honestly.
3. Per-user native logon registration — Scheduled Task on Windows, LaunchAgent on macOS, and systemd user service on Linux.

## Decision

Use per-user OS-native logon mechanisms and always query their real state.

## Consequences

Dropserve needs platform-specific registration and verification code, but install and removal need no administrator account.

