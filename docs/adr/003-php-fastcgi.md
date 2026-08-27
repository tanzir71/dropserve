# ADR-003: PHP through downloadable FastCGI, not embedded FrankenPHP

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

PHP must remain optional so the base binary stays small and zero-CGO cross-compilation remains reliable.

## Options considered

1. FrankenPHP — attractive embedding and worker features, but requires CGO and a more complex Windows build chain.
2. A downloaded `php-cgi` pack with a pure-Go FastCGI client — conventional, optional, removable, and compatible with zero-CGO builds.

## Decision

Supervise an on-demand `php-cgi` pool and proxy to it with `github.com/yookoala/gofast`.

## Consequences

The runtime manager must verify, unpack, configure, supervise, and fully remove PHP. FrankenPHP may be reconsidered for a later major version.

