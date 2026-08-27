# ADR-005: Atomic JSON state, not a database

Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context

Persistent core state is only a few dozen app records, ports, and toggles read at startup and replaced on change.

## Options considered

1. A database — offers transactions and queries but creates a dependency, migrations, and a larger corruption surface without a matching need.
2. JSON replaced atomically with one backup — readable, dependency-free, and proportionate to the state size.

## Decision

Persist state as an atomic JSON file using a temporary sibling, `os.Rename`, and one `.bak` copy.

## Consequences

Writes replace the whole document and must be serialized. Recovery and atomicity receive integration tests.

