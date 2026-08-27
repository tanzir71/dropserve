# Build State

**Current milestone:** M0 — Repository, CI, and the gate
**Last updated:** 2026-08-27T12:29:12Z
**Gate status:** green
**Iterations completed:** 1

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
- [ ] CI is green on Ubuntu and Windows runners.
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

## Decisions made this build (beyond the spec)

- None.

## Open questions for the human

- None.

## Deviations from the spec

- None.

## Verify on real hardware

- None for M0.

## Dependency count

0 direct external dependencies; `go list -m all` baseline: 1 module including the main module.
