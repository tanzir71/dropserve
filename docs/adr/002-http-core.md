# ADR-002: Own HTTP core, not embedded Caddy

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

Dropserve's main behavior is a mount table rebuilt from folders and swapped live in memory.

## Options considered

1. Embedded Caddy — provides excellent static serving and HTTPS, but would require regenerating an external configuration model, enlarge the dependency tree, and obscure the product's core routing behavior.
2. Go standard library — directly supports the immutable mount table, reverse proxy, and `http.ServeContent` behavior Dropserve needs.

## Decision

Build the HTTP core on `net/http` and `net/http/httputil.ReverseProxy`.

## Consequences

Dropserve owns more HTTP edge-case tests, but routing changes remain atomic, cheap, and understandable.

