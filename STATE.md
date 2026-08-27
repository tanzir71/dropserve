# Build State

**Current milestone:** M1 — Scan, mount, serve
**Last updated:** 2026-08-27T12:43:52Z
**Gate status:** green
**Iterations completed:** 6

## Milestone progress

- [x] M0 — Repository, CI, and the gate (tag: `m0-complete`)
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

- [ ] Static fixture mounted at `/static/` returns the expected body and content type.
- [ ] `GET /static` redirects permanently to `/static/`.
- [ ] Slug sanitisation handles spaces, Unicode, unsafe prefixes, and ignored names.
- [ ] Path traversal is refused for encoded, absolute, UNC, and escaping-symlink paths.
- [ ] Slug collisions across roots produce stable suffixes and both apps remain reachable.
- [ ] Case-insensitive collisions and case-only renames behave correctly.
- [ ] Scanner walks a path deeper than 260 characters.
- [ ] Reserved slugs are refused with a warning.
- [ ] A full scan-and-serve cycle leaves every app file and directory mtime unchanged (I2).
- [ ] `dropserve add <path>` writes only a registered-path config entry and serves it.
- [ ] M1 smoke script starts the server on a random port and fetches a mounted fixture.
- [ ] Port fallback skips occupied 80 and 8080, binds 8000, and reports the warning.

### M0 completion evidence

- Local `make check`: green with race tests, version injection, three zero-CGO cross-builds, full golangci-lint, and the shipped-file scan.
- Hosted CI [run 33073059871](https://github.com/tanzir71/dropserve/actions/runs/33073059871): Ubuntu gate, Windows gate, golangci-lint, and secret scan all passed.
- Demo:

  ```text
  make build
  dropserve 0.0.0-dev (b5c2d9f)
  ```
- The first hosted attempt exposed Windows CRLF conversion and a first-push secret-scan range edge case. Repository-wide LF attributes fixed the platform gate; the next normal push fixed the Git range and passed.

## Decisions made this build (beyond the spec)

- None.

## Open questions for the human

- None.

## Deviations from the spec

- None.

## Verify on real hardware

- None currently listed for M1.

## Dependency count

0 direct external dependencies; `go list -m all` baseline: 1 module including the main module.
