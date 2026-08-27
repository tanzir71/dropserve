# Build State

**Current milestone:** M0 — Repository, CI, and the gate
**Last updated:** 2026-08-27T12:34:15Z
**Gate status:** green
**Iterations completed:** 4

## Milestone progress

- [ ] M0 — Repository, CI, and the gate
- [ ] M1 — Scan, mount, serve
- [ ] M2 — The index
- [ ] M3 — Live
- [ ] M4 — Running things
- [ ] M5 — The subpath survival kit
- [ ] M6 — Always there
- [ ] M7 — Findable
- [ ] M8 — HTTPS
- [ ] M9 — PHP and add-ons
- [ ] M10 — Shippable
- [ ] M11 — Hardening and polish

## Current milestone criteria

- [x] `make check` exits 0 on a clean checkout.
- [x] Zero-CGO cross-builds pass for Windows amd64, Linux amd64, and Darwin arm64.
- [x] `dropserve version` prints a semver and injected Git SHA.
- [ ] CI is green on Ubuntu and Windows runners — **BLOCKED:** see `BLOCKED-m0-hosted-ci.md`.
- [x] The module dependency baseline is recorded.
- [x] The shipped-file scan returns no unfinished text.
- [x] The module path and MIT licence are correct.

### Latest gate evidence

Command: `make check`

```text
==> format
==> lint
==> test
ok github.com/tanzir71/dropserve/cmd/dropserve
==> version injection
==> zero-CGO cross-build
    windows/amd64
    linux/amd64
    darwin/arm64
==> shipped-file scan
gate: green
```

### Local CI readiness evidence

The workflow cannot run remotely until the repository exists, but each locally reproducible job is green:

```text
actionlint v1.7.12: no findings
golangci-lint v2.13.1: 0 issues
gitleaks v8.30.1: 2 commits scanned, no leaks found
```

## Decisions made this build (beyond the spec)

- None.

## Open questions for the human

- The required `github.com/tanzir71/dropserve` repository does not exist. Creating it as a public repository is the external publication step needed to run and verify Ubuntu and Windows CI. Awaiting explicit approval; no remote repository was created.

## Deviations from the spec

- None.

## Verify on real hardware

- None for M0.

## Dependency count

0 direct external dependencies; `go list -m all` baseline: 1 module including the main module.
