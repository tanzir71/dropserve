# ADR-009: Vanilla dashboard with no build step

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

The dashboard is one small launcher page that must load quickly over Wi-Fi and embed reproducibly in the Go binary.

## Options considered

1. A JavaScript framework and bundler — adds a Node toolchain, dependency tree, and mismatch risk between source and embedded output.
2. Handwritten HTML, CSS, and JavaScript served from `embed.FS` — sufficient for the interaction model and easy to audit.

## Decision

Build the dashboard with vanilla browser APIs and no frontend build step.

## Consequences

The interface team owns small accessibility and state-management helpers directly; CI enforces a 100 KB asset budget.

