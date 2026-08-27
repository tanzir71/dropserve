# Build State

**Current milestone:** M1 — Scan, mount, serve
**Last updated:** 2026-08-27T13:03:40Z
**Gate status:** green
**Iterations completed:** 16

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

- [x] Static fixture mounted at `/static/` returns the expected body and content type.
- [x] `GET /static` redirects permanently to `/static/`.
- [x] Slug sanitisation handles spaces, Unicode, unsafe prefixes, and ignored names.
- [x] Path traversal is refused for encoded, absolute, UNC, and escaping-symlink paths.
- [x] Slug collisions across roots produce stable suffixes and both apps remain reachable.
- [x] Case-insensitive collisions and case-only renames behave correctly.
- [x] Scanner walks a path deeper than 260 characters.
- [x] Reserved slugs are refused with a warning.
- [x] A full scan-and-serve cycle leaves every app file and directory mtime unchanged (I2).
- [x] `dropserve add <path>` writes only a registered-path config entry and serves it.
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

### M1 evidence

- `TestStaticFixtureMounted`: the read-only scanner discovers `testdata/fixtures/static/`, the immutable router mounts it at `/static/`, and the response is 200 HTML containing the fixture heading.
- `TestMissingTrailingSlashRedirects`: `GET /static` returns 301 with `Location: /static/` and preserves the query string.
- `TestSlugSanitisation`: spaces normalize to hyphens, Unicode names produce deterministic ASCII, `..evil` is rejected with an actionable warning, and dot/underscore convenience folders remain silently ignored.
- `TestPathTraversalIsRefused`: raw and encoded parent traversal, Windows backslashes, absolute paths, UNC paths, and an escaping symlink all return `ErrUnsafePath`. The symlink assertion passed on this Windows machine without a platform skip.
- `TestSlugCollisionsRemainReachable`: ordered roots produce `notes` and `notes-2`; each URL serves the correct source, and the warning names both full paths.
- `TestCaseInsensitiveCollisionAndRename`: `Notes` and `notes` collide across roots, while a `notes` → `Notes` change is one rename with zero added or removed apps.
- `TestScannerWalksLongPaths`: the scanner counts a generated payload whose Windows path exceeds 280 characters, using extended-length walk roots on Windows.
- `TestReservedSlugsAreRefusedWithWarnings`: `_dropserve`, `api`, `health`, and `.well-known` are never mounted and each produces a named, actionable warning.
- `TestAppFolderIsReadOnlyToUs`: SHA-256, size, mode, and nanosecond mtime snapshots for every app file and directory are identical before and after scan plus HTTP serving.
- `TestAddRegistersPathWithoutChangingApp`: the CLI atomically writes one `registered_apps` TOML entry, leaves no temp/symlink/copy/marker files, preserves the app snapshot, and the registered path serves at its slug.

## Decisions made this build (beyond the spec)

- None.

## Open questions for the human

- None.

## Deviations from the spec

- Build-loop process only: the minimal trailing-slash dispatch branch landed with the first router slice, so its dedicated acceptance test passed when added in the following iteration instead of failing first. The product behavior and gate match the criterion.

## Verify on real hardware

- None currently listed for M1.

## Dependency count

1 direct external dependency (the handover-approved TOML parser); M0 baseline: 0 direct / 1 total module. Current `go list -m all`: 2 modules including the main module.
