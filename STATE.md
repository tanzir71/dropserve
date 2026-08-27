# Build State

**Current milestone:** M2 — The index
**Last updated:** 2026-08-27T20:23:42Z
**Gate status:** green
**Iterations completed:** 33

## Milestone progress

- [x] M0 — Repository, CI, and the gate (tag: `m0-complete`)
- [x] M1 — Scan, mount, serve (tag: `m1-complete`)
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

- [x] `GET /` returns the dashboard HTML with a 200 and `Content-Type: text/html`.
- [x] `GET /_dropserve/api/apps` lists every fixture app with the correct type and status.
- [x] Search finds a fixture app by text that appears only in its `README.md`.
- [x] Search finds a fixture app by text that appears only in a filename.
- [x] A name match ranks above a filename-only match.
- [x] Every URL advertised by `GET /_dropserve/api/urls` returns a status below 400 (I3).
- [x] The QR endpoint returns a valid PNG for the requested URL.
- [x] Dashboard assets remain under 100 KB.
- [x] The dashboard handles both zero apps and 200 apps.
- [x] `/_dropserve/*` cannot be shadowed by an app slug.

### M2 deliverables audit

- [ ] Extract index `<title>`/first `<h1>`, byte size, mtime, favicon, and deterministic monogram metadata.
- [ ] Complete read-only app detail, status, and health API surfaces used by the dashboard.
- [ ] Persist the in-memory index atomically as `index.json` for fast cold starts.
- [ ] Wire sharing, QR, copy-link, and card action interactions into the vanilla dashboard.
- [ ] Run the real-binary M2 smoke and a browser-rendered launcher demo before tagging.

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
- `scripts/smoke/m1.ps1`: built and launched the real binary on `127.0.0.1:0`; `http://127.0.0.1:60348/static/` returned 200 with the fixture heading. A matching POSIX smoke script is included.
- `TestPortFallbackAndStatus`: real Windows listeners occupied `127.0.0.1:80` and `:8080`; Dropserve probed and bound `:8000`, served the fixture, atomically persisted the port, exposed a `port_fallback` warning through `dropserve status`, and stopped cleanly on cancellation.
- `TestStaticFileValidatorsAndRange` and `TestDirectoryListingWhenNoIndexExists`: the M1 deliverables audit proves ETag/`If-None-Match`, byte ranges, MIME detection, safe URL-encoded directory listings, and index resolution. The existing I2 fixture now uses deterministic mtimes so delayed Windows directory metadata cannot create a false write report.
- `TestPersistedPortIsPreferredOnRestart`: a random previously selected port is atomically loaded and rebound before the fallback ladder is retried, preserving the original warning rather than inventing a new port-80 diagnosis.

### M1 completion evidence

- Final `make check`: green with race tests, full golangci-lint, version injection, Windows/Linux/macOS zero-CGO cross-builds, and the shipped-file scan.
- Shipped binary: `dropserve 0.0.0-dev (8263b2a)`.
- Final `scripts/smoke/m1.ps1`: `http://127.0.0.1:54826/static/` returned 200 with the fixture heading.
- Hosted Windows CI revealed that its image reserves port 80 with `WSAEACCES` before the test can own it. The acceptance test now treats that failed real bind as proof the port is unavailable, while still using its own listener wherever Windows permits; local Windows continues to exercise owned listeners on both 80 and 8080.
- Corrected hosted CI [run 33076229938](https://github.com/tanzir71/dropserve/actions/runs/33076229938): Ubuntu gate, Windows gate, golangci-lint, and secret scan all passed for the commit referenced by `m1-complete`.

### M2 evidence

- `TestDashboardAtRoot`: the composed server reserves `/` for an embedded vanilla dashboard and returns 200 HTML with the focused launcher search. The responsive light/dark shell, empty/error states, cards, and keyboard controls total 13,409 bytes with no frontend build step.
- `TestAppsAPIListsEveryFixture`: the embedded dashboard API exposes the immutable scanner snapshot; the real static fixture reports slug/name/type/status plus its path URL in stable JSON.
- `TestSearchFindsREADMEContent`: the read-only indexer extracts the first non-heading README paragraph (capped at 200 Unicode characters), and case-insensitive substring/token-prefix search returns the owning app for a term found nowhere else.
- `TestSearchFindsFilename`: file names are indexed read-only to a hard cap of 5,000 files and three levels per app, with dependency/cache directories pruned; a term present only in a nested filename returns the owning app.
- `TestSearchRanksNameAboveFilename`: the explicit 5× name, 3× description, and 1× filename weights place an app named for the query above an app matching only through a file.
- `TestEveryAdvertisedURLWorks` (**I3**): the URLs API advertises the exact origin through which the client reached Dropserve; every returned URL is fetched through a real random-port HTTP server and returns below 400.
- `TestQREndpointReturnsPNG`: validated HTTP(S) input is passed to the handover-approved pure-Go QR encoder; the response independently decodes as a substantial 256×256 PNG and distinct URLs produce distinct images. The test does not decode the QR symbols back to text, avoiding a second decoder dependency; this is the explicit acceptance-criterion fallback limitation.
- `TestEmbeddedAssetsStayUnderBudget`: the exact embedded files total 13,409 of the strict 100,000-byte budget (CSS 7,187; JavaScript 3,444; HTML 2,778), enforced on every gate run.
- `TestDashboardHandlesZeroAndTwoHundredApps`: an empty scan serializes as a real empty list that activates the designed invitation state; a generated 200-app scan returns all 200 unique cards in one unpaginated snapshot. The client filters once and maps that snapshot once per render (linear work, no nested app scan).
- `TestDropserveNamespaceCannotBeShadowed`: an actual `_dropserve` folder is refused by the scanner, absent from the apps API, and cannot replace the dashboard root, embedded assets, or system API responses.

## Decisions made this build (beyond the spec)

- None.

## Open questions for the human

- None.

## Deviations from the spec

- Build-loop process only: the minimal trailing-slash dispatch branch landed with the first router slice, so its dedicated acceptance test passed when added in the following iteration instead of failing first. The product behavior and gate match the criterion.
- On Windows only, the gate retries the full race suite once when every package test passed but Go itself reports an `unlinkat` sharing violation for its completed temporary test executable. Real test failures are never retried. This handles transient antivirus/indexer locks without hiding product failures.
- Build-loop process only: the ranking assertion passed immediately because the weighted scorer was introduced alongside the preceding README/filename search slices. The dedicated acceptance test and full gate now lock the required order.
- Build-loop process only: the namespace-shadowing assertion passed immediately because M1 already refused reserved slugs and the first M2 server composition reserved system paths before app routing. The explicit end-to-end test now locks both layers together.

## Verify on real hardware

- None currently listed for M2.

## Dependency count

2 direct external dependencies (the handover-approved TOML parser and pure-Go QR encoder); M0 baseline: 0 direct / 1 total module. Current `go list -m all`: 3 modules including the main module.
