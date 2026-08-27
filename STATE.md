# Build State

**Current milestone:** M4 — Running things
**Last updated:** 2026-08-27T21:29:46Z
**Gate status:** green
**Iterations completed:** 59

## Milestone progress

- [x] M0 — Repository, CI, and the gate (tag: `m0-complete`)
- [x] M1 — Scan, mount, serve (tag: `m1-complete`)
- [x] M2 — The index (tag: `m2-complete`)
- [x] M3 — Live (tag: `m3-complete`)
- [ ] M4 — Running things
- [ ] M5 — The subpath survival kit
- [ ] M6 — Always there
- [ ] M7 — Findable
- [ ] M8 — HTTPS
- [ ] M9 — PHP and add-ons
- [ ] M10 — Shippable
- [ ] M11 — Hardening and polish

## Current milestone criteria

- [x] Assert: fixture `testdata/fixtures/node/` (a 20-line `http` server reading `process.env.PORT`) is detected as `command`, started, health-checked, and `GET /node/` proxies to it with the correct body. Skip with a clear message if `node` is absent from the runner, and ensure CI installs Node so it does not skip.
- [x] Assert: same for a Python fixture.
- [x] Assert: a fixture that exits immediately with code 1 is restarted with backoff, gives up after 5 attempts inside the window, ends in `crashed`, and its logs contain the error output.
- [x] Assert (**I4**): with a crashed app and a healthy app both present, the healthy app still returns 200 and the dashboard still renders.
- [x] Assert (**process tree**): start a fixture that spawns a grandchild process; stop the app; assert the grandchild's PID is gone within 5 seconds. This is the Job Object test and it is mandatory on Windows.
- [x] Assert: on Dropserve shutdown, no child processes survive.
- [x] Assert: a 10 MB burst of log output does not grow process memory beyond the ring buffer bound, and the on-disk log rotates rather than growing unbounded.
- [x] Assert: an app whose runtime is missing from `PATH` mounts in `needs-runtime` state and serves the explanation page with status 200 (not 502).
- [ ] Assert: lazy start — an app with `autostart: false` is not running until the first request, then starts and serves.

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
- `TestEmbeddedAssetsStayUnderBudget`: the exact embedded files currently total 23,011 of the strict 100,000-byte budget (CSS 10,649; JavaScript 8,456; HTML 3,906), enforced on every gate run.
- `TestDashboardHandlesZeroAndTwoHundredApps`: an empty scan serializes as a real empty list that activates the designed invitation state; a generated 200-app scan returns all 200 unique cards in one unpaginated snapshot. The client filters once and maps that snapshot once per render (linear work, no nested app scan).
- `TestDropserveNamespaceCannotBeShadowed`: an actual `_dropserve` folder is refused by the scanner, absent from the apps API, and cannot replace the dashboard root, embedded assets, or system API responses.
- `TestBuildExtractsDashboardMetadata` and `TestBuildGeneratesDeterministicMonogram`: the read-only index captures `<title>`, first `<h1>`, total regular-file bytes, newest file mtime, direct favicon/icon URLs, and stable initials/colours; the dashboard renders image icons or monograms from that metadata.
- `TestDashboardReadOnlyOperationalAPIs`: `healthz` returns plain `ok`; status exposes version/commit, live uptime, request listener port, a non-nil warnings list, and a cryptographically random CSRF token; per-app detail exposes the full path and detection reason while unknown slugs return 404.
- `TestIndexCacheRoundTripsAtomically` and `TestServerPersistsIndexOutsideAppRoot`: the complete public metadata plus private filename-search fields round-trip through versioned JSON and an fsynced sibling-temp replacement; normal serving writes only `index.json` beside machine state, with load support for cold-start reuse and no app-folder changes.
- `TestDashboardInteractionSurfaceIsWired`: the shipped assets connect the verified-URL sharing panel, clipboard with fallback, local QR dialog, and each card's Open/Copy/Show QR menu with accessible expanded state. `node --check` passes and the full asset bundle remains 77% below budget.
- `scripts/smoke/m2.ps1`: the real binary served four fixture apps at `http://127.0.0.1:53508/`; dashboard HTML, apps API, README-only search, and local QR PNG all passed. Matching PowerShell/POSIX smoke scripts are now run by hosted CI.
- Browser-rendered demo: the in-app Browser fallback was used because the skill-prescribed standalone executable was not installed. Four cards rendered with the search box focused; typing `fie` left one result and Enter opened `/field-notes/`; verified sharing, card actions, and a loaded 256×256 QR worked with zero console errors or overlays. Visual review found an unreadable dark-mode app-count pill; its contrast was fixed and the corrected `4 apps` pill was re-verified in a fresh build.

### M2 completion evidence

- Final local `make check`: green with race tests, full golangci-lint, version injection, Windows/Linux/macOS zero-CGO cross-builds, and the shipped-file scan.
- Shipped binary: `dropserve 0.0.0-dev (7cda9d6)`.
- Final `scripts/smoke/m2.ps1`: the real binary rendered four apps; README-only search and local QR passed at `http://127.0.0.1:55855/`.
- Hosted CI [run 33114496357](https://github.com/tanzir71/dropserve/actions/runs/33114496357): Ubuntu gate, Windows gate, golangci-lint, secret scan, and both platform-native M2 smoke scripts all passed.

### M3 evidence

- `TestFolderAddedIsServedWithinTwoSeconds` starts from an empty real Apps root and HTTP server, creates `live-notes/index.html`, and polls requests against a two-second deadline. The new fsnotify path debounces the native events for 500 ms, rescans read-only, swaps immutable router/dashboard snapshots, and served the exact body in about 660 ms on Windows.
- `internal/watcher` watches each existing root plus app subdirectories to a three-level cap and 256-watch per-app budget, skips dependency/cache directories, and invokes the same full reconcile path every 30 seconds as an event-loss safety net.
- `TestDeletedFolderIsRemovedWithinTwoSeconds` deletes a mounted app, then accepts the result only after the immutable scan snapshot is empty and the URL returns the friendly Dropserve HTML 404. The native removal path settled in about 650 ms without a panic.
- `TestRenamedFolderChangesSlugWithinTwoSeconds` renames a real mounted folder and polls both URLs plus the immutable scan snapshot. Within about 650 ms the old slug returned 404 while the new slug served the exact original body and was the sole indexed app.
- `TestRapidChangesAreDebounced` creates 20 app folders and indexes in a tight loop, waits for a 600 ms quiescent window, verifies all 20 published routes return 200, and rejects more than three successful snapshot rebuilds after the initial scan. The race-enabled full gate passes with the monotonic rebuild counter.
- `TestReconcileCatchesChangesWhileWatcherIsStopped` closes the native watcher, creates a new app, proves the snapshot remains unchanged, invokes the same full reconcile entry point used by the 30-second sweep, and immediately verifies the recovered route, body, and scan snapshot.
- `TestSSEStreamSurvivesThreeAppChanges` holds one real `text/event-stream` request open while creating three apps one at a time and receives three `apps-changed` events on that same connection. The stream publishes only after successful immutable swaps, coalesces notifications for slow clients, and enforces a 64-client bound.
- `TestMissingRootIsPickedUpWhenItAppears` starts with a nonexistent configured `Apps` path, then creates the root, app, and index. Startup records an actionable `root_missing` warning instead of failing; a temporary watch on the nearest existing ancestor detects the new root and serves the app in about 650 ms.
- `TestCloudPlaceholderIsNamedButNeverOpened` injects the indexer's read-only file-access/stat boundary and marks the declared HTML index with simulated `FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS`. On Windows the filename remains searchable while any attempted `Open` fails the test; non-Windows runners assert the interface is consulted. The production Windows adapter reads the native attribute from `Win32FileAttributeData` without hydrating content.
- `TestSyncRootWarningAppearsInDashboardStatus` configures an actual root under a `OneDrive` path and requires the dashboard warning to name the absolute root and recommend `%USERPROFILE%\Dropserve`; the launcher turns the status warning into a visible notice. `TestDoctorReportsSyncRootWarning` proves the same information appears in `dropserve doctor` for a Dropbox-backed root. Segment detection also covers Google Drive, iCloud Drive, and organisation-suffixed OneDrive names.
- The dashboard now opens `/_dropserve/api/events` with `EventSource` and reloads its immutable apps API snapshot after every `apps-changed` event, completing the live open-dashboard path without a frontend build step.
- `TestWatchDepthAndPerAppBudgetAreBounded` constructs a deep and wide app with `node_modules`, then inspects the real native watch set: no fourth-level or dependency path is watched, and the configured four-watch test budget is exact. `TestSyncRootWarningNamesAllKnownProviders` locks all four named sync providers.
- Real-binary browser demo: `dropserve 0.0.0-dev (d8dc490)` started against an empty random-port root. Visual verification first exposed that the empty apps API encoded `null`, which made the JavaScript launcher show its error state; the API and regression helper now enforce `[]`. In the corrected build the same open page changed from `0 apps` to a rendered `Live Canvas` card after the folder appeared, with no reload and zero console errors.

### M3 completion evidence

- Final local `make check`: green with race tests, full golangci-lint, version injection, Windows/Linux/macOS zero-CGO cross-builds, and the shipped-file scan.
- Shipped binary: `dropserve 0.0.0-dev (a1a05b6)`.
- The real-binary browser demo verified an empty-to-one-app SSE update on the same open page, with the corrected empty state and zero console errors.
- Hosted CI [run 33116161221](https://github.com/tanzir71/dropserve/actions/runs/33116161221): Ubuntu gate, Windows gate, golangci-lint, secret scan, platform smoke, watcher bounds, cloud placeholder behavior, and all live HTTP acceptance tests passed.

### M4 evidence

- `TestNodeFixtureIsDetectedStartedHealthyAndProxied` exercises the real `testdata/fixtures/node/` package: rule 3 records `Node app from package.json start script`, runs `npm start` with an allocated `PORT` and loopback `HOST`, waits through TCP plus HTTP health checks, and proxies `/node/` to the exact fixture body in about 770 ms locally.
- The first passing request exposed that killing npm alone left its `cmd.exe` and Node descendants alive. ADR-012 records the native fix: each Windows app is assigned to a `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` Job Object through `x/sys/windows`, while Unix uses a separate process group. The rerun left no fixture process behind, and the zero-CGO cross-build gate passes all three targets.
- CI now installs Node 24 explicitly with the official `actions/setup-node` action on both gate runners, so the M4 acceptance test cannot silently rely on runner image state.
- `TestPythonFixtureIsDetectedStartedHealthyAndProxied` exercises the real `testdata/fixtures/python/server.py`: rule 6 records `Python app from server.py`, runs it with allocated loopback environment, health-checks it, and proxies `/python/` to the exact fixture body in about 330 ms locally. CI installs Python 3.13 explicitly with the official setup action.
- `TestImmediateFailureRestartsFiveTimesThenCrashes` drives a real package whose start script prints `intentional fixture failure` and exits 1. The supervisor shares its 256 KB log ring across five isolated attempts, waits the production `1s, 2s, 4s, 8s` backoff sequence (with short injected delays in tests), then mounts a friendly status-200 stopped page and reports `crashed`, five attempts, and the captured error through both app metadata and the log endpoint. The full gate is green and a post-gate process inspection found no fixture descendants.
- `TestCrashedAppDoesNotBlockHealthyApp` registers only the broken and healthy Node packages, drives the broken app through all five attempts, then proves the dashboard still renders with status 200 and `/node/` still proxies the exact healthy fixture body. This locks invariant I4 at the HTTP boundary.
- Windows-only `TestProcessTreeIsKilled` runs a real `npm start` package whose Node server spawns a long-lived Node grandchild. It verifies the reported grandchild PID is live, closes Dropserve, and observes that exact PID exit in about 820 ms—well inside the mandatory five-second window. The full gate and a separate process inventory both found no surviving process-tree fixture descendants.
- Cross-platform `TestShutdownLeavesNoCommandChild` reads the healthy Node process's real PID through its fixture endpoint, proves it is live, closes Dropserve, and observes it exit in about 810 ms within the five-second deadline. The test failed first when the endpoint did not exist, the full gate is green, the Unix test variant cross-compiles, and a separate Windows process inventory found no surviving Node fixture process.
- `TestTenMegabyteLogBurstIsMemoryBoundedAndRotatesDisk` writes one 10 MB burst through the real concurrent log sink. It asserts the in-memory tail is exactly 256 KB, only the current file plus four backups remain, every file is at most 1 MB, and total retained disk data is at most 5 MB. Command stdout/stderr now share this sink across restart attempts; servers with persistent state place the files under the state directory's `logs/` folder. The focused test passes in about 40 ms and the full race/lint/cross-build gate is green.
- `TestMissingRuntimeMountsFriendlyNeedsRuntimePage` empties `PATH`, registers the real Node package, and first observed the old `crashed` state. Runtime availability is now checked before launch: the app mounts immediately as `needs-runtime`, its dashboard metadata carries that state, and `/node/` returns a status-200 page that names Node.js and says to install it. No futile restart loop or 502 is exposed, and the full gate is green.

## Decisions made this build (beyond the spec)

- 2026-08-28 — ADR-010 records the handover-prescribed local pure-Go QR encoder. It adds one direct module with no transitive modules and prevents local addresses from leaking to a hosted QR service.
- 2026-08-28 — ADR-011 records the handover-prescribed fsnotify watcher paired with periodic reconciliation. fsnotify adds one direct module plus its platform syscall module while preserving zero-CGO cross-builds.
- 2026-08-28 — ADR-012 records native command-app process-tree isolation. The already-transitive pure-Go `golang.org/x/sys` module becomes direct so Windows Job Objects can kill npm and every descendant when the supervisor closes.

## Open questions for the human

- None.

## Deviations from the spec

- Build-loop process only: the minimal trailing-slash dispatch branch landed with the first router slice, so its dedicated acceptance test passed when added in the following iteration instead of failing first. The product behavior and gate match the criterion.
- On Windows only, the gate retries the full race suite once when every package test passed but Go itself reports an `unlinkat` sharing violation for its completed temporary test executable. Real test failures are never retried. This handles transient antivirus/indexer locks without hiding product failures.
- Build-loop process only: the ranking assertion passed immediately because the weighted scorer was introduced alongside the preceding README/filename search slices. The dedicated acceptance test and full gate now lock the required order.
- Build-loop process only: the namespace-shadowing assertion passed immediately because M1 already refused reserved slugs and the first M2 server composition reserved system paths before app routing. The explicit end-to-end test now locks both layers together.
- Build-loop process only: the M4 I4 assertion passed immediately because the preceding restart-policy slice had to retain a crashed handler instead of aborting reconciliation. The dedicated mixed-health end-to-end test now locks dashboard and healthy-app availability together.
- Build-loop process only: the mandatory Windows grandchild assertion passed immediately because native Job Object containment was added when the first Node proxy test exposed the leak. The dedicated PID-liveness test now guards the full `npm → node → grandchild` tree and its five-second deadline on every Windows gate.
- Build-loop process only: the M3 rename assertion passed when first added because the preceding live-create slice deliberately reconciles the entire immutable scan and mount table, which already treats a filesystem rename as one removal plus one addition. The dedicated two-URL deadline test now locks that behavior.

## Verify on real hardware

- None currently listed for M4.

## Dependency count

4 direct external dependencies (the handover-approved TOML parser, pure-Go QR encoder, fsnotify, and Windows syscall bridge); M0 baseline: 0 direct / 1 total module. Current `go list -m all`: 5 modules including the main module.
