# ADR-012: Native process-tree control

Date: 2026-08-28 · Status: accepted · Supersedes: —

## Context

Command apps commonly start through wrappers such as `npm`, which then create the real server as a child or grandchild. Stopping only the wrapper leaves descendants holding their ports and makes the next start fail.

## Options considered

1. Kill only the direct child — portable but demonstrably leaks npm descendants.
2. Shell out to `taskkill` or `pkill` — adds external-command assumptions and cannot guarantee cleanup when Dropserve exits unexpectedly.
3. Use Windows Job Objects and Unix process groups — the operating system owns the tree boundary and can terminate every descendant.

## Decision

Assign each Windows command app to a Job Object configured with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, using the canonical pure-Go `golang.org/x/sys/windows` API. Put each Unix command app in its own process group. Closing the native boundary terminates the whole app tree.

## Consequences

The already-transitive `golang.org/x/sys` module becomes a direct dependency. Process launch remains zero-CGO and cross-compilable. Platform tests must prove grandchild termination before M4 completes.
