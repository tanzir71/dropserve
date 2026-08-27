# Dropserve — Build Handover Specification

**Document version:** 1.0
**Date:** 2026-08-27
**Audience:** An autonomous coding agent (Codex) building this software in an iterative loop, plus any human reviewing its work.
**Status:** Authoritative. This document is the contract. Where this document and your instincts disagree, this document wins — unless you record an ADR explaining why (see §11.6).

---

## 0. How to use this document

You are going to build **Dropserve** — a free, open-source, extremely user-friendly local web server — from an empty repository to a signed, installable, daily-usable v1.0.

Read this document top to bottom **once** before writing any code. Then work the loop in **§11**.

The document is organised so that:

- **§1–§5** tell you *what you are building and why*. Read these to make good judgement calls when the spec is silent.
- **§6–§9** tell you *how it is built*. These are binding technical decisions.
- **§10** is the milestone ladder — your actual work queue, with machine-checkable acceptance criteria.
- **§11** is the build loop protocol — how to run iterations, when to stop, what you may never do.
- **§12–§16** cover packaging, the landing page, security, hazards, and reference schemas.

Two rules that override everything:

1. **Never mark a milestone done that you have not verified with a command that exits 0.** Acceptance criteria are written as runnable checks for exactly this reason.
2. **Never make the gate green by weakening the gate.** Deleting a test, loosening a lint rule, or editing an acceptance criterion to match your implementation is a build-loop failure, not a build-loop success.

### 0.1 Decisions already closed

These were researched and settled before this document was written. They are not open questions, and reopening one costs an iteration for nothing. If you believe one is wrong, write an ADR that supersedes it — do not simply do something else.

| Decision | Settled as | Where |
|---|---|---|
| Repository | `github.com/tanzir71/dropserve` | §7.3, landing page |
| Licence | **MIT** — already stated publicly on the landing page | M0, §7.3 |
| Language / build | Go 1.23+, `CGO_ENABLED=0` everywhere | ADR-001, §7.1 |
| HTTP core | Own stdlib router; **not** embedded Caddy | ADR-002 |
| PHP | Downloadable `php-cgi` pool over FastCGI; **not** embedded FrankenPHP | ADR-003 |
| Addressing | Path-based, with a stable per-app port as the escape hatch | ADR-004, §6.4 |
| Persistence | Atomic JSON file; **not** a database | ADR-005 |
| Autostart | Per-user logon task; **not** a Windows Service | ADR-007, §8.1 |
| Tailscale | The installed CLI in v1; `tsnet` deferred | ADR-008, §6.8 |
| Frontend | Vanilla HTML/CSS/JS, no build step | ADR-009, §6.5 |
| mDNS library | `libp2p/zeroconf/v2`, fallback `betamos/zeroconf`, decided by the M7 spike | §6.8, M7 |
| First platform | Windows; macOS and Linux after M11 | §3 |
| Databases | SQLite needs nothing; MariaDB/Postgres are on-demand packs | §6.10 |

Every hazard in §15 is mapped to the milestone and test that guards it in **§15.1**. If you find a new hazard, add its row there in the same commit that fixes it.

---

## 1. The product in one paragraph

Dropserve is a single small binary that runs quietly on a computer you already own. You drop a folder into `Dropserve/Apps`. Within a second, that folder is a working website on your network. You open `http://192.168.1.50` from any device in the house and see a searchable index of everything you have ever dropped in — no ports to remember, no config to write, no `httpd.conf`, no control panel with a start/stop button you forgot to press. It starts itself when you log in. When you want something reachable from outside the house, you flip one switch and Tailscale handles it. It is the local server for people who want to *use* a local server, not administer one.

---

## 2. Why this exists — the competitive wedge

### 2.1 What the incumbents are

| Tool | What it is | Where it hurts |
|---|---|---|
| **XAMPP** | Apache + MariaDB + PHP + Perl bundle, ~150 MB+ installer | Control panel with manual start/stop. Port 80/443 conflicts with Skype/IIS/VMware are a rite of passage. You edit `httpd.conf` and `vhosts` by hand to add a site. Nothing is indexed — you have to remember what you put where. |
| **WampServer** | Windows-only Apache/MySQL/PHP | Windows-only. Same manual vhost editing. Tray icon that goes orange and doesn't tell you why. Notoriously sensitive to missing VC++ redistributables. |
| **MAMP / MAMP PRO** | macOS + Windows stack; PRO is paid | The genuinely useful features (multiple hosts, easy SSL) are behind the paid PRO tier. |
| **AMPPS** | Softaculous stack with app installers | Heavy. Bundles a whole app-installer ecosystem most people never use. Update cadence has been uneven. |
| **Laragon** | Windows stack, well-loved for auto-vhosts | Genuinely good and closer to our thesis than the rest — auto virtual hosts, pretty URLs. But Windows-only, the free version is limited, and it is still fundamentally a PHP-developer tool, not a "host my little apps" tool. |
| **ServBay / Herd** | Modern, polished, freemium | Polished but commercial and PHP/Laravel-centric. The good tiers cost money. |
| **DDEV / Lando** | Docker-based, reproducible | Requires Docker Desktop. Excellent for teams; enormous overhead for "I made a tiny HTML tool and want it on my phone." |

### 2.2 What every one of them gets wrong for our user

They are all **stack installers**. They install Apache, MySQL and PHP, hand you a document root, and consider the job done. The user's actual job — *"I made a small thing, I want to open it from my phone in the kitchen"* — remains entirely manual: create a folder, edit a vhost, restart Apache, remember a port, type an IP, hope you spelled the folder right.

### 2.3 Our four differentiators

These are the reasons the product exists. If a design decision weakens one of them, it is the wrong decision.

1. **Hosting is dropping a folder.** No config file, no vhost, no restart, no dialog. The filesystem *is* the configuration.
2. **The index is the product.** One address gets you a searchable, current list of everything you host. You are never asked to remember a port or a path.
3. **It is already running.** Autostart at login is on by default and actually works, headless, with no console window.
4. **Reachable from anywhere is one switch.** LAN by IP out of the box; Tailscale for outside the network; a per-app public toggle when you really need it — with honest warnings.

### 2.4 What "extremely user-friendly" means concretely

Friendliness is not a coat of paint. Encode it as these invariants, and test them:

- **Zero-config is the happy path, not a fallback.** A folder with one `index.html` and no manifest must work perfectly.
- **Never show a URL that does not work.** If mDNS failed to bind, hide the `.local` URL. If Tailscale is not running, do not print a tailnet address. A dead link destroys trust faster than a missing feature.
- **Never write inside a user's app folder.** Not a lockfile, not a cache, not a `.dropserve` marker. Their folder is theirs. All our state lives in our own directory.
- **Every error message names the fix.** Not "bind: permission denied" but "Port 80 is taken by another program. Dropserve is using port 8080 instead — your address is http://192.168.1.50:8080. [Fix this]".
- **One-click escape hatch for every clever feature.** Path-mounting is clever and sometimes breaks apps; there is always a "open this on its own port" button that cannot break.
- **No modal blocks the server from serving.** The dashboard can show warnings; the server keeps serving.

---

## 3. Non-goals

State these plainly in the README so nobody files the wrong issue:

- **Not a production internet-facing web server.** It is a personal/LAN server. Say so.
- **Not a Docker replacement or an orchestrator.** No containers, no compose, no clusters.
- **Not a WordPress/Laravel dev environment first.** PHP is supported as a runtime pack; we are not competing on `wp-cli` integration or Laravel Valet parity in v1.
- **Not a build system.** We run what you give us. We do not run `npm install` for you in v1 (see §6.7 for the deliberate exception path).
- **Not multi-user.** One machine, one person, one tailnet.
- **Not a file sync tool.** Syncthing/Dropbox/OneDrive already exist; we serve what is on disk.

---

## 4. The five product invariants

Write these at the top of `CONTRIBUTING.md`. Any PR that violates one is rejected regardless of how good it otherwise is. Milestone M11 includes an explicit audit against this list.

| # | Invariant | Test that enforces it |
|---|---|---|
| **I1** | A folder dropped into an Apps root is reachable over HTTP in under 2 seconds, with no manifest and no restart. | `TestDropFolderBecomesReachable` |
| **I2** | Dropserve never creates, modifies, or deletes any file inside an app folder. | `TestAppFolderIsReadOnlyToUs` — snapshot hash the tree before and after a full serve+index+watch cycle. |
| **I3** | Every URL surfaced in the UI resolves. No dead links, ever. | `TestAdvertisedURLsAllRespond` — the dashboard's own `/api/urls` output is fetched and each entry must return < 400. |
| **I4** | The server keeps serving healthy apps when any single app is broken, crashing, or missing. | `TestOneBrokenAppDoesNotAffectOthers` |
| **I5** | Dropserve does not bind to a public interface, expose anything to the internet, or install anything into the OS trust store without an explicit, specific user action in that session. | `TestNoImplicitPublicExposure`, `TestTrustStoreRequiresExplicitConsent` |

---

## 5. Golden paths (write these as end-to-end tests)

### GP-1 — First run
1. User runs the installer, or unzips and runs `dropserve.exe`.
2. A first-run window (or terminal wizard if headless) appears with exactly three things: the Apps folder location (editable), a "Start Dropserve when I log in" checkbox (**checked**), and a **Start** button.
3. On Start: the Apps folder is created, an example app is placed in it, the server binds, the browser opens to the dashboard.
4. The dashboard shows one card ("Welcome to Dropserve") and a **Sharing** panel with the LAN URL and a QR code.
5. Total elapsed time from double-click to a working page: **under 20 seconds**, no questions asked beyond that one screen.

### GP-2 — Hosting something
1. User drags `~/Downloads/invoice-tool/` into `Dropserve/Apps/`.
2. Within 2 seconds the dashboard (open in another tab) shows a new card, live, without a refresh.
3. Clicking it opens `http://<lan-ip>/invoice-tool/` and the app works.

### GP-3 — Phone access
1. User clicks the QR icon on any app card.
2. A QR encoding `http://<lan-ip>/<slug>/` appears.
3. Phone camera → app opens. No typing.

### GP-4 — A Node app
1. User drops a folder with `package.json` containing `"scripts": {"start": "node server.js"}`.
2. Dropserve detects it, assigns a port, sets `PORT` in the environment, starts it, waits for health, mounts it.
3. Card shows a green dot and "node". Logs are viewable from the card.
4. If it crashes, the card turns amber and shows the last 50 log lines with the error highlighted — the rest of the server is unaffected.

### GP-5 — The subpath rescue
1. User drops an app whose `index.html` uses `<script src="/app.js">`.
2. Dropserve's post-mount probe notices root-absolute asset references that would 404 under the `/slug/` prefix.
3. The card shows: *"This app expects to live at the root of a site. Dropserve is serving it at its own address instead: http://192.168.1.50:7431/"* — and it already switched. No user action required; a "use the short URL anyway" link is offered for the curious.

### GP-6 — Outside the house
1. User clicks **Sharing → Access from anywhere**.
2. Dropserve detects Tailscale is installed and logged in, and shows `http://darkhorse.tailnet-name.ts.net/` with a copy button and QR.
3. If Tailscale is not installed, the panel shows a short explanation and a link — it does not attempt to install anything.

### GP-7 — Reboot
1. User restarts the machine and logs in.
2. Without opening anything, `http://192.168.1.50/` works within 15 seconds of desktop appearing.
3. No console window flashes. No UAC prompt. No tray balloon unless something is wrong.

---

## 6. Architecture

### 6.0 Process model

**One process.** A single Go binary, no CGO, no sidecars for the core. Child processes exist only for user apps (a Node server, a `php-cgi` pool) and are supervised.

```
dropserve (single process)
├── HTTP server        :80   (fallback ladder, §6.6)
├── HTTPS server       :443  (optional, §6.9)
├── Router             path-mount table, rebuilt atomically on change
├── Scanner            walks Apps roots, classifies apps
├── Watcher            fsnotify + 30s reconcile sweep
├── Indexer            in-memory search index, persisted as JSON
├── Supervisor         child processes for type=command apps
├── Discovery          LAN IP monitor, mDNS responder, Tailscale probe
├── Dashboard          embed.FS static assets + JSON API under /_dropserve
└── Tray (optional)    build-tagged; the binary must run fully headless without it
```

### 6.1 The Apps folder contract

**Default root:** `%USERPROFILE%\Dropserve\Apps` on Windows, `~/Dropserve/Apps` elsewhere. Multiple roots are supported; the config holds an ordered list.

Rules for what becomes an app:

- Each **immediate subdirectory** of a root is one app. Its directory name is the **slug** after sanitising: lowercase, spaces and underscores → `-`, strip anything outside `[a-z0-9-]`, collapse repeats, trim hyphens. `My Notes App` → `my-notes-app`.
- A **loose file** at the root of an Apps directory is also an app if it is `.html`/`.htm`. `scratch.html` → slug `scratch`, served as a single page. This is a deliberate convenience: people paste single HTML files.
- **Ignored** at the app level: names beginning with `.` or `_`, plus `node_modules`, `venv`, `.venv`, `__pycache__`, `vendor`, `.git`, `dist` *(only when it is the app root's own name — `dist` inside an app is served normally)*.
- **Slug collisions** (two roots both containing `notes`): first root in config order wins; the loser gets `notes-2` and a dashboard warning naming both paths. Never silently shadow.
- Reserved slugs, refused with a warning: `_dropserve`, `api`, `health`, `.well-known`.

**Invariant I2 restated at implementation level:** the scanner opens files read-only. The watcher does not create marker files. Detection results, thumbnails, indexes, logs, and assigned ports all live in the state directory. If you ever find yourself writing a path under an Apps root, stop.

### 6.2 App detection

Run in this order; first match wins. Record the matched rule in state so the dashboard can explain itself ("detected as: Node app, from package.json start script").

| # | Condition | Result |
|---|---|---|
| 1 | `dropserve.json` present with `type` set | Use it verbatim. This is the user's override and is never second-guessed. |
| 2 | `Procfile` present with a `web:` line | `type: command`, command = that line |
| 3 | `package.json` with `scripts.start` | `type: command`, command = `npm start` (or `pnpm`/`yarn`/`bun` if the matching lockfile exists) |
| 4 | `package.json` with no start script but a `main`/`index.js`/`server.js` | `type: command`, command = `node <that file>` |
| 5 | `index.php` present, or ≥1 `.php` and no `index.html` | `type: php` |
| 6 | `app.py`, `main.py`, `server.py`, or `wsgi.py` present | `type: command`, command = `python <that file>` |
| 7 | `index.html` or `index.htm` present | `type: static` |
| 8 | A single executable file (`.exe`, or unix +x) and nothing else obvious | `type: command`, command = that binary |
| 9 | Anything else with at least one file | `type: static` with directory listing enabled |
| 10 | Empty directory | `type: static`, card shown greyed with "This folder is empty" |

**Runtime availability is checked, not assumed.** If rule 3 matches but `node` is not on `PATH`, the app is mounted in a *needs-runtime* state: the card shows "Needs Node.js — [How to install]" and the URL serves a friendly explanation page, not a 502. Same for `python`, and for `php` when the PHP pack is not installed (§6.10).

### 6.3 Routing model

**Primary addressing is path-based on the root host.** This is the only scheme that works with zero configuration for LAN clients, because it requires no DNS whatsoever.

```
http://<host>/                 → Dashboard
http://<host>/_dropserve/...   → Dashboard assets + JSON API (always available, never shadowed)
http://<host>/<slug>/          → App
http://<host>:<appport>/       → The same app, alone, at its own root (escape hatch)
```

The router holds an immutable mount table. Changes build a **new** table and swap it atomically with `atomic.Pointer[Table]` — never mutate a live table, never hold a lock across a proxy call.

Matching is longest-prefix on the first path segment. A request for `/notes` (no trailing slash) 301s to `/notes/` — this matters enormously for relative asset resolution and must be tested.

**Why not port-per-app as the primary scheme:** the user's stated requirement is "remembering the IP address should be enough." Ports defeat that.

**Why not subdomains as the primary scheme:** `notes.dropserve.local` requires either a DNS server the user must configure, mDNS wildcard support that Windows does not reliably provide, or hosts-file edits — all of which are exactly the friction we exist to remove. Subdomains are offered as an *optional* addressing mode when Tailscale MagicDNS or a real local DNS is available (post-v1).

### 6.4 The subpath survival kit

Serving an app under `/slug/` breaks any app that hardcodes root-absolute paths. This is the single biggest source of "it doesn't work" in tools like this. Handle it in layers, and make the last layer unbreakable.

**Layer 1 — Tell the app where it lives.** For `type: command` apps, set on every proxied request:
- `X-Forwarded-Prefix: /<slug>`
- `X-Script-Name: /<slug>` (older frameworks)
- `X-Forwarded-Host`, `X-Forwarded-Proto`, `X-Forwarded-For`
- and in the child's environment at start: `DROPSERVE_BASE_PATH=/<slug>/`, `DROPSERVE_BASE_URL=http://<host>/<slug>/`, plus common framework conventions (`BASE_PATH`, `PUBLIC_URL`, `VITE_BASE`, `NEXT_PUBLIC_BASE_PATH`) — set only if not already present in the app's own env.

**Layer 2 — Fix the response on the way out.**
- Rewrite `Location:` headers on 3xx that point at `/` to `/<slug>/`.
- Rewrite `Set-Cookie` `Path=/` to `Path=/<slug>/`.
- For `Content-Type: text/html` responses **under 2 MB and not chunked-streamed**, inject `<base href="/<slug>/">` immediately after `<head>` **if and only if** the document has no existing `<base>` element. Controlled per app by `base_href: auto | always | never`, default `auto`.
- Never touch any other content type. Never attempt to rewrite JavaScript or CSS — that way lies madness.

**Layer 3 — Detect failure and route around it.** After an app is first mounted, fetch its index once and scan the HTML for root-absolute references (`src="/…"`, `href="/…"`, `url(/…)` in inline styles, `fetch("/…")` in inline scripts) that are not `//` protocol-relative and not already covered by an injected base. If any are found **and** a HEAD to the corresponding prefixed URL 404s, mark the app `prefers_own_port`.

**Layer 4 — The unbreakable escape hatch.** Every app is assigned a **stable dedicated port** at first detection, persisted in state, reused forever (so bookmarks survive). Apps marked `prefers_own_port` have their dashboard card link to `http://<host>:<port>/` instead of the path URL, with a one-line explanation. Both URLs continue to work. The user never has to understand any of this — and a user who *does* understand can flip it with one click.

### 6.5 The Index (dashboard)

This is the product's face and the user's stated core requirement: *"everything I drop in there should be indexed so I don't have to guess."*

**Layout**
- A single page. No routing framework, no build step, no npm. Vanilla HTML/CSS/JS in `internal/dashboard/assets/`, served from `embed.FS`. Budget: **< 100 KB total, < 400 ms to interactive on a mid-range phone over Wi-Fi.** Enforce with a size test in CI.
- **Search box is focused on load.** `/` refocuses it. Arrow keys move through results; Enter opens; `Ctrl/Cmd+Enter` opens in a new tab. It should feel like a launcher, because that is what it is.
- Below it, a responsive grid of app cards, sorted by last-used then last-modified.

**What is indexed** (rebuilt on change, held in memory, persisted to `index.json` for fast cold start):
- App name (manifest `name`, else the folder name prettified)
- Slug and full folder path
- Manifest `description`
- `<title>` and the first `<h1>` of the app's index document
- First non-heading paragraph of `README.md` / `README.txt`, truncated to 200 chars
- File names within the app, capped at 5,000 per app and 3 levels deep (so searching "invoice" finds the app containing `invoice-template.html`)
- Detected type, tags from the manifest

Matching is a simple case-insensitive substring + token-prefix match with field weighting (name 5×, description 3×, filenames 1×). **Do not add a search library.** The corpus is dozens of apps; a linear scan over an in-memory slice is microseconds and has no failure modes.

**Each card shows:** icon (app `favicon.ico`/`icon.png` → manifest `icon` emoji → generated monogram from the first letters, coloured by hashing the slug), name, description, type badge, status dot, size, last-modified.

**Card actions** (in a `⋯` menu, not cluttering the card): Open · Open on its own port · Copy link · Show QR · Open folder in file manager · View logs · Restart · Pin to top · Hide from index.

**States that must have designed empty/error views, not stack traces:**
- No apps yet → the drop-a-folder invitation with an **Open Apps Folder** button
- App is starting → card with a pulse and "starting…"
- App crashed → amber card, last 50 log lines, **Restart** button
- App needs a runtime → card explains which runtime and links to install instructions
- Port fallback in effect → a dismissible banner naming the real address

**Sharing panel** (always visible, top-right or a dedicated tab):
- This device's URLs, each with copy and QR: `http://<lan-ip>/`, `http://<hostname>.local/` (only if mDNS actually bound), `http://<magicdns>/` (only if Tailscale is up)
- A note if the LAN IP changed since last boot, with a one-line explanation of DHCP reservations
- Per-app public sharing (Funnel) lives here, not on cards — it deserves the friction (§6.8)

**JSON API** (used by the dashboard, and a legitimate integration surface — document it):
```
GET  /_dropserve/api/apps            → [{slug,name,desc,type,status,urls,port,icon,size,mtime,...}]
GET  /_dropserve/api/apps/{slug}     → one app, with detection reasoning and warnings
GET  /_dropserve/api/search?q=       → ranked results
GET  /_dropserve/api/urls            → advertised URLs (this is what invariant I3 tests)
GET  /_dropserve/api/status          → version, uptime, ports, discovery state, warnings[]
GET  /_dropserve/api/logs/{slug}     → SSE stream of that app's log ring buffer
POST /_dropserve/api/apps/{slug}/restart
POST /_dropserve/api/apps/{slug}/settings
POST /_dropserve/api/rescan
GET  /_dropserve/api/qr?url=         → PNG QR code
GET  /_dropserve/healthz             → 200 "ok" (used by autostart verification and tests)
```
All mutating endpoints require a same-origin check and a CSRF token from `/api/status`. They are refused entirely from non-loopback origins when the PIN lock (§6.11) is enabled and unauthenticated.

### 6.6 Ports and binding

Windows makes port acquisition genuinely unpredictable — not because of Unix-style privileged-port rules, but because of `http.sys` URL reservations (IIS, Web Deploy, BranchCache, some VPN clients) and **excluded port ranges** reserved by WinNAT/Hyper-V/WSL, which return `WSAEACCES (10013)` on bind even for an administrator. Do not reason about this — **probe it.**

**Fallback ladder for the main HTTP listener:** `80 → 8080 → 8000 → 8888 → 3000 → <random high port>`. HTTPS: `443 → 8443 → 4443 → <random>`.

Rules:
- Probe by actually binding, not by reading a port list.
- The chosen port is **persisted** and preferred on the next start, so URLs are stable across reboots. Re-attempt the higher-priority port only when the user asks ("Try port 80 again") or on a fresh install.
- If the port is not 80, every surfaced URL includes the port, and a dismissible dashboard banner explains it in one sentence with a **Diagnose** button that runs and displays: `netsh http show servicestate` summary, `netsh int ipv4 show excludedportrange protocol=tcp`, and the PID/name holding the port (via `net.Dial` + platform lookup). Give the user the actual answer, not a shrug.
- Per-app dedicated ports are allocated from **7400–7999**, checked against the excluded ranges, and persisted per slug.

Also required: on first bind on Windows the firewall will prompt. Ship a `dropserve firewall allow` subcommand that adds the rule via `netsh advfirewall`, offered (not forced) by the installer, and explain in the UI that "Private networks" is the checkbox that matters.

### 6.7 Runtime supervisor (`type: command`)

For each command app:

1. **Allocate** its stable port. Export `PORT` (name configurable via `port_env`) plus `HOST=127.0.0.1` and the `DROPSERVE_*` variables from §6.4.
2. **Start** with `cwd` = app directory, env = OS env + manifest `env` + ours. On Windows, create the process in a **Job Object** with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`; on Unix, put it in its own process group. *This is not optional* — without it, `npm start → node` leaves orphaned processes holding ports after every restart, which is the classic failure of every tool in this category.
3. **Capture** stdout/stderr into a 256 KB ring buffer in memory plus a rotating file in the state dir (5 files × 1 MB). Never let a chatty app fill the disk.
4. **Health-check**: TCP dial the port with 100 ms → 2 s exponential backoff for up to 30 s; then HTTP `GET health_path` (default `/`) expecting < 500. Only mount the route once healthy. Before that, the path serves a lightweight "starting…" page that auto-refreshes.
5. **Restart policy**: on exit, restart with backoff `1s, 2s, 4s, 8s, 16s, 30s`. After 5 failures within 10 minutes, stop and mark `crashed`; surface logs; never loop silently.
6. **Lazy start** (`autostart: false` in the manifest, or a global "start apps on demand" setting, default **on** for machines with < 8 GB RAM): the app is not started until the first request for its path, which serves a cold-start page. Idle apps are stopped after a configurable idle timeout (default: never in v1; make it a setting).
7. **Shutdown**: on Dropserve exit, send `SIGTERM` (Unix) / `CTRL_BREAK_EVENT` then Job Object close (Windows), wait 5 s, then kill the tree. Dropserve must never leave a child running after it exits — test this.

### 6.8 Discovery and remote access

Presented to the user as one **Sharing** panel with a tiered list. Every entry is verified before display (invariant I3).

**Tier 1 — LAN IP (always).** Pick the primary non-loopback IPv4 by choosing the interface with the default route; ignore virtual adapters (`vEthernet`, `VirtualBox Host-Only`, `Tailscale`, `Hyper-V`, `WSL`) by name and by RFC-1918 heuristics. Watch for changes every 30 s and update the panel live.

**Tier 2 — mDNS / `.local` (best effort).** Advertise `dropserve.local` (configurable) plus an `_http._tcp` service record so it appears in network browsers. Library: **`github.com/libp2p/zeroconf/v2`** — a maintained fork of the widely-used but stalled `grandcat/zeroconf`, pure Go, with `Register()` for advertising as well as browsing. **Fallback: `github.com/betamos/zeroconf`**, which explicitly documents testing on Windows/macOS/Linux and publishes and browses on a single socket — the design most likely to coexist with an OS responder. Decide with the spike in M7's first criterion, not by discussion.

> ⚠️ **Windows hazard.** Windows 10 (1803+) and 11 ship their own mDNS responder that holds UDP/5353. A second responder must bind with `SO_REUSEADDR` (and `SO_REUSEPORT` on platforms that have it) or it will fail. If binding fails, **log it, hide the `.local` URL, and move on** — do not show the user a broken address, and do not attempt to stop the OS service.

**Tier 3 — Tailscale (the headline integration).**

*v1 approach: integrate with the user's installed Tailscale client via its CLI/LocalAPI.* This inherits the user's existing node identity and MagicDNS name and requires no auth key, no second node, and no login flow inside our app. It is dramatically less to explain and less to go wrong.

- **Detect**: locate the `tailscale` binary (`PATH`, plus `C:\Program Files\Tailscale\tailscale.exe`, `/usr/bin/tailscale`, `/Applications/Tailscale.app/Contents/MacOS/Tailscale`). Run `tailscale status --json`. Parse `Self.DNSName` and `Self.TailscaleIPs`.
- **Show**: `http://<magicdns-name>/` in the Sharing panel with copy + QR. If MagicDNS is off, show the `100.x.y.z` address instead and a one-line note.
- **Serve (optional, one click)**: `tailscale serve --bg --https=443 http://127.0.0.1:<our-port>` gives real, publicly-trusted HTTPS on the tailnet with **no certificate to install on any device** — this is strictly better than our own local CA and should be presented as the recommended way to get HTTPS. Provide a matching "Stop serving" action.
- **Funnel (per-app, deliberately high-friction)**: `tailscale funnel` exposes a service to the public internet on ports **443, 8443, or 10000 only**, requires HTTPS certificates enabled for the tailnet, MagicDNS on, and a `funnel` node attribute in the tailnet policy (default grant is `autogroup:member`). It is available on all Tailscale plans including free, with non-configurable bandwidth limits.
  - Default **off**. Never whole-server — per app only.
  - Turning it on requires typing the app's slug to confirm, and shows plainly: *"This puts <name> on the public internet. Anyone with the link can reach it. It has no password unless you added one."*
  - While active: a persistent, non-dismissible banner in the dashboard and a distinct tray icon state.
  - Auto-expire after 8 hours unless the user pins it. Log every enable/disable with a timestamp to the state dir.
- **Preconditions are checked and explained, not assumed.** If HTTPS certs are not enabled for the tailnet, say exactly that and link to the admin console page.

*Post-v1 option:* embed a dedicated node with `tailscale.com/tsnet`, giving Dropserve its own tailnet identity (`dropserve.<tailnet>.ts.net`), `ListenTLS`, and `ListenFunnel`, independent of whether the user's client is running. Record as a new ADR superseding ADR-008 if and when it is built; do not build it in v1.

**Tier 4 — QR codes for everything.** Pure-Go generation via `github.com/skip2/go-qrcode`, served from `/_dropserve/api/qr?url=`. Never call an external QR service — that would leak the user's addresses.

### 6.9 TLS and the local CA

HTTP is the default and stays enabled. HTTPS is **additive and opt-in**, because forcing HTTPS on a LAN where other devices do not trust our CA is a worse experience than plain HTTP.

- On first HTTPS enable, generate a local CA (ECDSA P-256, 10-year root) into the state dir with `0600` on the key.
- Issue a leaf covering `localhost`, `127.0.0.1`, `::1`, the machine hostname, `<hostname>.local`, `dropserve.local`, and every current LAN IPv4/IPv6. Regenerate when the address set changes.
- **Trust installation is a separate, explicit, explained action.** Use `github.com/smallstep/truststore` (`truststore.InstallFile`) — the same approach `mkcert` popularised. The UI must say, in plain words: *"This adds a certificate authority created on this computer to this computer's trust store, so browsers here stop warning about Dropserve's HTTPS. It only affects this machine. You can remove it any time."* Provide `dropserve trust --install` / `--uninstall` and a dashboard button for both.
- **Recommend the Tailscale route above this one** in the UI, because it needs no trust changes on any device.
- Never auto-redirect HTTP → HTTPS by default. Offer it as a setting for people who have installed the CA everywhere.

### 6.10 PHP and add-on packs

Base install ships **no PHP, no database engine**. That is the point — the base download should be ~15–25 MB, versus XAMPP's 150 MB+.

**PHP pack** (installed with one click from the dashboard when a PHP app is detected, or from Settings → Add-ons):
- Downloads the official PHP Windows binary (or the platform equivalent) into the state dir, verifies a pinned SHA-256, unpacks.
- Dropserve runs a small pool of `php-cgi` processes on loopback and speaks FastCGI to them using `github.com/yookoala/gofast`.
- Pool sizing: `max(2, min(4, NumCPU))`, restarted on crash by the same supervisor.
- `php.ini` is generated by us into the state dir with development-friendly defaults (`display_errors=On`, `upload_max_filesize=64M`, `post_max_size=64M`, `memory_limit=256M`, timezone from the OS) and a comment header saying "edit via Dropserve Settings; this file is regenerated."
- Per-app PHP version selection is **out of scope for v1** — one version, chosen in Settings. Record as a known limitation.

> **Alternative considered and deliberately deferred:** [FrankenPHP](https://frankenphp.dev/) embeds PHP directly into a Go binary as a library (MIT licence, PHP 8.2+, native Windows support since v1.12.0) and would give worker mode and hot reload. It requires **CGO**, which breaks our zero-CGO / trivial-cross-compile guarantee and complicates the Windows build chain considerably. See ADR-003. Revisit for v2 if PHP performance ever becomes a real complaint.

**Database add-ons** (one click each, from the dashboard):
- **SQLite** needs no engine — apps bring their own driver. What Dropserve provides is a **browser**: any `*.db` / `*.sqlite` / `*.sqlite3` file found inside an app folder gets a "Browse database" action that opens a read-mostly table viewer in the dashboard. Use `modernc.org/sqlite` (pure Go, no CGO) so this costs nothing at build time.
- **MariaDB** and **PostgreSQL**: downloaded on demand, unpacked into the state dir, supervised as child processes, with data directories under the state dir (never inside app folders). Surfaced as one card each in Settings → Add-ons with Start/Stop, the connection string, and a copy button. Only start when at least one app needs them, or when the user pins them running.
- Every add-on must be **fully removable** from the same screen, leaving nothing behind.

### 6.11 Access control

- **Default bind: all interfaces**, because LAN access by IP is the stated requirement. This is a deliberate, documented choice — say so in the README and the first-run screen ("Anyone on your Wi-Fi can reach these apps").
- **Per-app `visibility`**: `lan` (default) · `local` (127.0.0.1 only) · `tailnet` (only source IPs in `100.64.0.0/10`) · `public` (Funnel, §6.8). Enforced in the router by source-IP check before the proxy call, with a clean 403 page explaining which visibility setting blocked it.
- **PIN lock** (off by default): a 6-digit PIN, HMAC-signed session cookie valid 30 days per device, protecting everything except `/_dropserve/healthz`. Intended for coffee-shop Wi-Fi. When enabled, requests from loopback are still exempt so the local user is never locked out of their own machine.
- **Always on, not optional:** path traversal defence (clean, then verify the resolved path is still under the app root — test with `..%2f`, UNC paths, symlinks, and 8.3 short names on Windows), a request size limit, a header size limit, and no directory listing above an app root.

### 6.12 Configuration and state

Nothing the user must edit. Everything the user *may* edit, in one readable file.

```
%APPDATA%\Dropserve\config.toml          # user-editable, hot-reloaded on change
%LOCALAPPDATA%\Dropserve\
  ├── state.json         # app records, assigned ports, settings toggles
  ├── index.json         # search index snapshot for fast cold start
  ├── ca/                # local CA (0600 key)
  ├── logs/              # dropserve.log + per-app rotating logs
  ├── runtimes/          # downloaded PHP / MariaDB / Postgres packs
  └── data/              # add-on data directories
```

Unix equivalents follow the XDG spec (`~/.config/dropserve/`, `~/.local/share/dropserve/`).

`config.toml` is hot-reloaded: a malformed edit logs a clear error and **keeps the last good config running** — never crash on a bad config file. Provide `dropserve config validate` and `dropserve config path`.

---

## 7. Technology decisions

### 7.1 The stack

| Layer | Choice | Why |
|---|---|---|
| Language | **Go 1.23+** | One static binary per OS, no runtime to install, trivial cross-compilation, excellent stdlib HTTP, first-class process and filesystem control. |
| CGO | **Disabled. `CGO_ENABLED=0` everywhere.** | Cross-compilation stays a one-liner; no C toolchain in CI; no MSVC/mingw pain on Windows. This constraint drives several decisions below and is not negotiable without an ADR. |
| HTTP core | stdlib `net/http` + `net/http/httputil.ReverseProxy` | We control routing entirely; an embedded server framework would fight our mount model. |
| File watching | `github.com/fsnotify/fsnotify` | De-facto standard. Paired with a periodic reconcile sweep (see hazards, §15). |
| Config | `github.com/pelletier/go-toml/v2` | TOML is the most human-editable format for a file we ask people to open by hand. |
| Tray | `github.com/getlantern/systray` behind build tag `tray` | Mature and small. The binary must build and run fully without it. |
| QR | `github.com/skip2/go-qrcode` | Pure Go, no network calls. |
| mDNS | `github.com/libp2p/zeroconf/v2` (default) · `github.com/betamos/zeroconf` (fallback) | Pure Go. Decided by the M7 spike, which is a 20-minute runnable test, not a judgement call. |
| CA trust | `github.com/smallstep/truststore` | Cross-platform trust-store installation, the approach `mkcert` proved. |
| FastCGI (PHP pack) | `github.com/yookoala/gofast` | Mature pure-Go FastCGI *client*; Go's stdlib `net/http/fcgi` is the server side only and is not what we need. |
| SQLite (DB browser) | `modernc.org/sqlite` | Pure Go, no CGO. |
| Frontend | **Vanilla HTML/CSS/JS, no framework, no build step**, served from `embed.FS` | The dashboard must be instant on a phone. A framework would add a toolchain, a build step in CI, and 200 KB for no benefit at this scale. |
| Tests | stdlib `testing` + `net/http/httptest` | Fast, hermetic, no infrastructure. E2E browser tests only where genuinely needed (M11). |
| Release | `goreleaser` + GitHub Actions | Reproducible matrix builds, checksums, changelog. |
| Installer (Windows) | **Inno Setup** for the friendly path, plain `.zip` for the nerd path | Free, scriptable in CI, produces a normal-feeling Windows installer. |

**Dependency budget: 12 direct dependencies, hard cap.** Every addition requires an ADR justifying why the stdlib will not do. This is a durability decision — a tool people run at login for years should not have a 400-package dependency tree.

### 7.2 Architecture Decision Records

Create `docs/adr/NNN-title.md` for each. These are pre-decided; write them up as accepted with the reasoning below, and add new ones as you go.

**ADR-001 — Go, not Node/Electron/Rust.**
Node would need a runtime shipped or installed. Electron would put a 120 MB Chromium in a background service — absurd for a program whose job is to be invisible. Rust is a fine alternative but Go's process management, stdlib HTTP, and cross-compilation story are a better fit for this specific product, and the contributor pool for "small Go server" is large.

**ADR-002 — Our own HTTP core, not embedded Caddy.**
Caddy is excellent and would give automatic HTTPS, a mature file server, and an admin API for free. We decline it because: (a) our entire value is a bespoke routing model driven by a folder scan, which means we would be generating and reloading Caddy JSON on every filesystem event rather than swapping an in-memory table; (b) it roughly triples the dependency tree and binary size; (c) the pieces we actually want from it — a local CA and trust installation — are available directly as `smallstep/truststore`. *Escape hatch:* if the static-file layer proves troublesome (range requests, conditional GETs, compression), the answer is to lean harder on `http.ServeContent`, not to adopt Caddy mid-build.

**ADR-003 — PHP via downloadable `php-cgi` + FastCGI, not embedded FrankenPHP.**
FrankenPHP (MIT, PHP 8.2+, native Windows since v1.12.0) embeds PHP as a Go library and is genuinely impressive. It requires CGO, which violates our zero-CGO constraint and would make the Windows build chain substantially harder to keep green in an automated loop. FastCGI to a supervised `php-cgi` pool is the boring, proven approach, keeps PHP fully optional, and keeps the base download small. Revisit in v2.

**ADR-004 — Path-based routing primary, per-app dedicated port as escape hatch.**
See §6.3 and §6.4 for the reasoning. The escape hatch is what makes the clever part safe.

**ADR-005 — JSON state file, not a database.**
State is a few dozen records read at startup and written on change. A database would add a dependency, a migration story, and a corruption mode, for nothing. Write atomically (temp file + `os.Rename`) and keep one `.bak`.

**ADR-006 — No Docker requirement.**
DDEV and Lando are better tools for reproducible team environments. Requiring Docker Desktop for "serve this folder" is precisely the friction this product removes.

**ADR-007 — Autostart via OS-native mechanisms, not a Windows Service.**
A service runs as SYSTEM, cannot easily open a browser or show a tray icon, and needs elevation to install. A per-user logon task needs no admin rights, runs as the user (so it can read the user's folders and their Tailscale identity), and is trivially removable. See §8 and milestone M6.

**ADR-008 — Tailscale via the installed CLI in v1, `tsnet` deferred.**
See §6.8.

**ADR-009 — Vanilla frontend, no build step.**
A build step in the dashboard means a Node toolchain in CI and a class of "works in dev, broken in the embedded build" bugs. At this size, plain files win outright.

### 7.3 Repository layout

```
dropserve/
├── cmd/
│   └── dropserve/main.go          # CLI entry, subcommand dispatch, tray bootstrap
├── internal/
│   ├── app/                       # App model, detection, manifest parsing
│   ├── scanner/                   # Apps-root walking, slug rules, collision handling
│   ├── watcher/                   # fsnotify + reconcile sweep + debounce
│   ├── router/                    # mount table, atomic swap, prefix matching
│   ├── proxy/                     # ReverseProxy wiring, header + cookie + base-href rewriting
│   ├── static/                    # static file serving, SPA fallback, directory listing
│   ├── supervisor/                # child processes, job objects, health, restart, logs
│   ├── indexer/                   # search index build + query
│   ├── dashboard/                 # HTTP handlers, JSON API
│   │   └── assets/                # index.html, app.css, app.js, icons  (embed.FS)
│   ├── discovery/                 # LAN IP, mDNS, Tailscale probe + serve/funnel control
│   ├── tlsca/                     # local CA, leaf issuance, truststore install
│   ├── runtimes/                  # add-on pack download/verify/unpack/supervise
│   ├── autostart/                 # windows.go / darwin.go / linux.go
│   ├── config/                    # TOML load, validate, hot reload
│   ├── state/                     # atomic JSON persistence
│   ├── ports/                     # bind probing, fallback ladder, exclusion diagnosis
│   └── tray/                      # build-tagged tray UI
├── testdata/
│   └── fixtures/                  # sample apps: static/, php/, node/, python/, broken/, absolute-paths/
├── docs/
│   ├── index.html                 # GitHub Pages landing page  (see §13)
│   ├── .nojekyll
│   └── adr/
├── .github/workflows/
│   ├── ci.yml                     # gate on every push/PR
│   └── release.yml                # tag → goreleaser → sign → publish
├── packaging/
│   ├── windows/dropserve.iss      # Inno Setup script
│   └── windows/sign.ps1
├── Makefile                       # or Taskfile.yml — `make check` is the gate
├── STATE.md                       # the build-loop ledger (see §11)
├── README.md
├── CONTRIBUTING.md
└── LICENSE                        # MIT — decided; the landing page and README already say so
```

### 7.4 CLI surface

The GUI is the product, but everything must be doable headlessly — that is what makes it testable, and what makes it work on a mini-PC in a cupboard.

```
dropserve                       # start (tray if available, else foreground) — the default
dropserve serve                 # run in the foreground, no tray, log to stdout
dropserve status                # JSON: version, port, apps, discovery state, warnings
dropserve open                  # open the dashboard in the default browser
dropserve apps                  # list apps as a table
dropserve add <path>            # register an extra folder as an app (config entry, never a symlink)
dropserve logs <slug> [-f]      # tail an app's logs
dropserve restart <slug>
dropserve autostart enable|disable|status
dropserve trust install|uninstall|status
dropserve firewall allow        # Windows: add the inbound rule
dropserve tailscale status|serve|unserve|funnel <slug>|unfunnel <slug>
dropserve runtime install php|mariadb|postgres
dropserve config path|validate|edit
dropserve doctor                # THE support command — see below
dropserve version
```

**`dropserve doctor` is a first-class feature, not an afterthought.** It prints, in plain language with ✅/⚠️/❌ per line: the version; which port it got and why not 80 if applicable; excluded port ranges on Windows; whether the firewall rule exists; the Apps roots and whether they are readable; every app with its detected type and any warning; runtime availability (`node`, `python`, `php`); mDNS bind status; Tailscale status; autostart registration status *as read from the OS, not from our own config*; and the last 20 error-level log lines. When a user files an issue, this output should be the entire bug report.

---

## 8. Autostart, tray, and first run

### 8.1 Autostart

Autostart is a differentiator, so it must be *reliable*, *invisible*, and *honestly reported*.

**Windows — a per-user Scheduled Task at logon.** Not the Startup folder (a visible console window flashes, and users "clean it up"), not the `Run` registry key (silently disabled by Task Manager's Startup tab with no way for us to tell), and not a Windows Service (needs elevation, runs as SYSTEM, cannot see the user's Tailscale identity or reach their profile folders cleanly).

Register via `schtasks.exe /Create /XML <file> /TN "Dropserve"` with:
- Trigger: **At log on**, this user only
- Action: the binary with `--background`
- **Run only when user is logged on** (so no elevation and no stored password)
- Highest privileges: **off** (we do not need admin, and asking for it would be a red flag)
- "Stop the task if it runs longer than": **disabled** — this is the single most common mistake; the default 3-day limit will silently kill the server
- "Start the task only if the computer is on AC power": **disabled**
- Restart on failure: 3 attempts, 1 minute apart
- Delay: 10 seconds after logon, so we are not fighting the rest of the login storm

**macOS** — `~/Library/LaunchAgents/dev.dropserve.agent.plist` with `RunAtLoad` and `KeepAlive.SuccessfulExit=false`, loaded via `launchctl bootstrap gui/$UID`.

**Linux** — `~/.config/systemd/user/dropserve.service` with `Restart=on-failure`, enabled via `systemctl --user enable --now`, plus `loginctl enable-linger $USER` (offered, explained) for headless boxes where the server should run without a login session.

**Rules for all platforms:**
- `dropserve autostart status` reads the **actual OS state** (query the task/agent/unit), never a boolean we stored. A stored flag that disagrees with reality is exactly how tools in this category earn a reputation for "it says it's on but it isn't."
- Enabling and disabling are idempotent and never require elevation.
- The uninstaller removes the registration. Verify this in the packaging tests.
- After enabling, run a verification: query the task, and surface a green check in the UI only if the query succeeded.

### 8.2 Background mode

`--background` must produce **no console window on Windows**. Build the Windows binary with `-ldflags="-H=windowsgui"` for the GUI/tray variant, and ship the console variant as `dropserve-cli.exe` for terminal use. Both from the same source; differ only by link flags and build tag.

### 8.3 Tray

Minimal by design. Icon states: **running** (normal), **warning** (amber — an app crashed, port fallback in effect, or mDNS failed), **sharing publicly** (distinct icon — Funnel is active), **paused**.

Menu: Open Dashboard · Open Apps Folder · Copy LAN Link · ─── · Pause Serving · Start at Login ✓ · ─── · Run Doctor · Quit.

The tray is **optional at build time and at runtime.** On a headless machine, `dropserve serve` must be a complete product.

### 8.4 First run

One screen. Three controls. Nothing else. (See GP-1.)

The example app placed in the Apps folder should be a genuinely useful tiny utility — not a "hello world". Suggestion: a single-file unit/currency converter or a QR generator, ~200 lines, MIT, in `testdata/example-app/`, so the very first thing the user sees is the product's own thesis working.

---

## 9. Voice and interface copy

The product's differentiator is friendliness, and friendliness lives mostly in sentences. Hold the UI copy to these rules and review them in M11.

- **Name the fix, not the fault.** ❌ "EADDRINUSE" → ✅ "Port 80 is being used by another program, so Dropserve is at http://192.168.1.50:8080 instead."
- **Never use jargon the user did not introduce.** No "reverse proxy", "vhost", "document root", "bind", "SAPI", "daemon" in the primary UI. "Server", "folder", "address", "app" are enough. Jargon is allowed in `doctor` output and logs, which are for debugging.
- **Second person, present tense, active voice.** "Drop a folder here" not "Folders may be placed in this directory".
- **No exclamation marks, no emoji in system messages.** Emoji are fine as user-chosen app icons.
- **Numbers over adjectives.** "3 apps · 41 MB" beats "several apps".
- **Every destructive action states what is lost and what is not.** "Removing the PHP pack deletes the downloaded PHP files. Your apps and their files are untouched."
- **Warnings are dismissible; errors are actionable.** A warning the user cannot dismiss must be something genuinely dangerous — Funnel being live is the only one that qualifies.

---

## 10. Milestone ladder

This is your work queue. Work them **in order**. Each milestone has an ID you will reference in `STATE.md` and in commit messages.

Acceptance criteria are written as things a machine can check. Where a criterion says "assert", it means a Go test. Where it says "script", it means a step in `scripts/smoke/<milestone>.sh` (and `.ps1` for Windows-specific ones) that exits non-zero on failure.

A milestone is **done** when: every acceptance criterion passes, `make check` is green, the demo script has been run and its output pasted into `STATE.md`, and the work is committed and tagged `m<N>-complete`.

---

### M0 — Repository, CI, and the gate

**Goal:** an empty but fully wired project where the gate is meaningful from commit one.

**Deliverables:** module init; `cmd/dropserve` printing a version; `Makefile` with `check`, `build`, `test`, `lint`, `run`; `.golangci.yml`; GitHub Actions CI on `ubuntu-latest` + `windows-latest`; `LICENSE` (**MIT** — already decided, and already stated on the landing page and in the README; do not re-open it); `README.md` skeleton; `STATE.md` initialised from the template in §11.1; `docs/adr/` with ADR-001 … ADR-009 written up from §7.2.

**Acceptance criteria**
- `make check` exits 0 on a clean checkout.
- `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...` succeeds; same for `linux/amd64`, `darwin/arm64`.
- `dropserve version` prints a semver plus the git SHA, injected via `-ldflags -X`.
- CI is green on both runners.
- `go list -m all | wc -l` recorded in `STATE.md` as the dependency baseline.
- Script: `make check` includes a placeholder scan over shipped files only —
  `grep -rIn -E 'USER/|YOUR-|TODO:|FIXME:|<placeholder>' --exclude-dir=.git --exclude=DROPSERVE-HANDOVER.md --exclude=STATE.md --exclude-dir=docs/adr .` — and returns nothing.
  (The exclusions matter: this specification and the build ledger legitimately contain those strings, and a scan that matches its own definition fails forever.)
- The module path is `github.com/tanzir71/dropserve` and `LICENSE` contains the MIT text with the correct copyright line.

**Escape hatch:** none. Do not proceed past M0 with a red gate.

---

### M1 — Scan, mount, serve (the core magic)

**Goal:** a folder in the Apps root is reachable over HTTP. This milestone alone is already more useful than a stopped XAMPP.

**Deliverables:** `config`, `state`, `ports`, `scanner`, `router`, `static` packages; `dropserve serve` binding via the fallback ladder; slug rules; static serving with index resolution, `ETag`/`If-None-Match`, `Range` support (via `http.ServeContent`), correct MIME types, and directory listing when no index exists.

**Acceptance criteria**
- Assert: `testdata/fixtures/static/` mounted at `/static/` returns 200 with the right body and `Content-Type`.
- Assert: `GET /static` (no slash) returns 301 to `/static/`.
- Assert: slug sanitisation table — at minimum `My Notes` → `my-notes`, `Ünïcødé Tool` → a stable ASCII slug, `..evil` → rejected, `_scratch` → ignored, `.hidden` → ignored.
- Assert: path traversal is refused for `../`, `..%2f`, `%2e%2e%5c`, an absolute path, a UNC path, and a symlink pointing outside the app root (skip the symlink case on Windows if the runner cannot create one; note the skip in `STATE.md`).
- Assert: two roots containing the same folder name produce `notes` and `notes-2`, and both are reachable.
- Assert (**hazard 7**): slug collision detection is case-insensitive — `Notes` and `notes` in two different roots collide; and on a case-insensitive filesystem, renaming `notes` → `Notes` is reported as a rename, not as a delete plus a create.
- Assert (**hazard 6**): the scanner walks a fixture tree whose deepest path exceeds 260 characters without error. Construct it in the test rather than committing it. On Windows this requires extended-length (`\\?\`) path handling in the walker — the test exists to force that.
- Assert: reserved slugs (`_dropserve`, `api`, `health`) are refused and a warning is recorded.
- Assert (**I2**): hash every file and directory mtime under a fixture app before and after a full scan+serve cycle; the hashes are identical.
- Assert (**hazard 15**): `dropserve add <path>` on a folder outside every Apps root results in exactly one new `config.toml` entry, that folder being served, and **zero** filesystem changes anywhere else — no symlink, no copy, no marker file.
- Script: start the server on a random port, `curl` the mounted fixture, assert 200.
- Assert: the port ladder — occupy 80 and 8080 with test listeners, start Dropserve, assert it binds 8000 and that `dropserve status` reports the port and a `port_fallback` warning.

**Demo script:** create a folder with one `index.html`, start `dropserve serve`, open the printed URL, see the page.

---

### M2 — The index

**Goal:** one address shows everything, searchably.

**Deliverables:** `indexer` and `dashboard` packages; the vanilla frontend; the JSON API from §6.5; QR endpoint; monogram icon generation; favicon extraction.

**Acceptance criteria**
- Assert: `GET /` returns the dashboard HTML with a 200 and `Content-Type: text/html`.
- Assert: `GET /_dropserve/api/apps` lists every fixture app with the correct `type` and `status`.
- Assert: search for a string appearing only inside a fixture app's `README.md` returns that app.
- Assert: search for a string appearing only in a *filename* inside a fixture app returns that app.
- Assert: field weighting — an app whose *name* matches ranks above one whose *filename* matches.
- Assert (**I3**): every URL in `GET /_dropserve/api/urls` returns < 400 when fetched.
- Assert: `GET /_dropserve/api/qr?url=http://example.test/` returns a valid PNG that decodes back to that URL (use a decoder in the test, or assert PNG magic bytes + non-trivial size and record the limitation).
- Script: total size of `internal/dashboard/assets/` after build is **under 100 KB**; fail the test above that.
- Assert: the dashboard renders correctly with **zero** apps (the empty state) and with 200 apps (no pagination bug, no O(n²) render).
- Assert: `/_dropserve/*` cannot be shadowed by an app slug.

**Demo script:** with four fixture apps present, open `/`, type three letters, hit Enter, land on the right app.

---

### M3 — Live

**Goal:** dropping a folder works without a restart, within 2 seconds.

**Deliverables:** `watcher` package — fsnotify on each Apps root, 500 ms debounce, recursive watching capped at 3 levels with a per-app watch budget, plus a 30-second full reconcile sweep as a safety net; Server-Sent Events endpoint so the open dashboard updates live.

**Acceptance criteria**
- Assert (**I1**): create a directory with an `index.html` inside a temp Apps root; within **2 seconds**, `GET /<slug>/` returns 200. Use a polling helper with a deadline, not a fixed sleep.
- Assert: deleting an app removes the route within 2 seconds and returns 404 with the friendly not-found page, not a panic.
- Assert: renaming an app changes the slug and the old slug 404s.
- Assert: rapid changes (create 20 folders in a tight loop) settle to a correct final state and trigger **at most 3** index rebuilds — proving debounce works.
- Assert: the reconcile sweep catches a change made while the watcher was deliberately stopped (simulate by disabling the watcher, mutating the tree, then invoking reconcile directly).
- Assert: the SSE stream emits an `apps-changed` event on a change and the connection survives at least 3 events.
- Assert: watching a root that does not exist yet does not crash; when the directory appears, it is picked up.
- Assert (**hazard 5**): a file marked with the cloud-placeholder attribute (`FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS`, simulated in the test via an injected stat interface) is listed in the index by name but never opened by the indexer, so a cloud-only folder cannot stall the scan. On non-Windows this test asserts the interface is wired and skips the attribute check.
- Assert (**hazard 5**): if a configured Apps root resolves inside a known sync folder (`OneDrive`, `Dropbox`, `Google Drive`, `iCloud Drive` in the path), a warning is recorded and surfaced in `doctor` and the dashboard, naming the root and recommending `%USERPROFILE%\Dropserve`.

**Escape hatch:** if `fsnotify` proves unreliable on a Windows path type (network drive, OneDrive-synced folder, very large tree), fall back to polling **for that root only**, at a 2-second interval, and record it in `STATE.md`. Do not abandon fsnotify globally.

---

### M4 — Running things

**Goal:** a Node or Python app dropped in a folder just works.

**Deliverables:** `supervisor` package with Windows Job Objects and Unix process groups; detection rules 2–4, 6, 8 from §6.2; health checking; restart policy; log ring buffer + rotating files; the log-viewing API and UI; runtime-availability detection with the friendly "needs Node.js" state.

**Acceptance criteria**
- Assert: fixture `testdata/fixtures/node/` (a 20-line `http` server reading `process.env.PORT`) is detected as `command`, started, health-checked, and `GET /node/` proxies to it with the correct body. Skip with a clear message if `node` is absent from the runner, and ensure CI installs Node so it does not skip.
- Assert: same for a Python fixture.
- Assert: a fixture that exits immediately with code 1 is restarted with backoff, gives up after 5 attempts inside the window, ends in `crashed`, and its logs contain the error output.
- Assert (**I4**): with a crashed app and a healthy app both present, the healthy app still returns 200 and the dashboard still renders.
- Assert (**process tree**): start a fixture that spawns a grandchild process; stop the app; assert the grandchild's PID is gone within 5 seconds. This is the Job Object test and it is mandatory on Windows.
- Assert: on Dropserve shutdown, no child processes survive.
- Assert: a 10 MB burst of log output does not grow process memory beyond the ring buffer bound, and the on-disk log rotates rather than growing unbounded.
- Assert: an app whose runtime is missing from `PATH` mounts in `needs-runtime` state and serves the explanation page with status 200 (not 502).
- Assert: lazy start — an app with `autostart: false` is not running until the first request, then starts and serves.

---

### M5 — The subpath survival kit

**Goal:** apps that assume they own the root of a site do not embarrass us.

**Deliverables:** everything in §6.4 — header forwarding, `Location` and `Set-Cookie` rewriting, conditional `<base href>` injection, the absolute-path probe, `prefers_own_port`, per-app stable port allocation, and the card UI that explains it in one sentence.

**Acceptance criteria**
- Assert: a fixture returning a 302 to `/login` produces a client-visible redirect to `/<slug>/login`.
- Assert: a fixture setting `Set-Cookie: s=1; Path=/` yields `Path=/<slug>/`.
- Assert: an HTML response with no `<base>` gets `<base href="/<slug>/">` injected directly after `<head>`; one that already has a `<base>` is left byte-identical.
- Assert: a non-HTML response (JSON, JS, CSS, PNG) is byte-identical through the proxy — hash in, hash out.
- Assert: a 5 MB HTML response is **not** rewritten (over the 2 MB cap) and passes through unmodified.
- Assert: `testdata/fixtures/absolute-paths/` is flagged `prefers_own_port`, its dashboard card links to `http://127.0.0.1:<port>/`, and that URL serves the app correctly at its root.
- Assert: assigned per-app ports are stable across a restart of Dropserve (persisted in state).
- Assert: `X-Forwarded-Prefix`, `X-Forwarded-Host`, `X-Forwarded-Proto` arrive at a command app with the right values.
- Assert: WebSocket upgrade through the proxy works for a command app (a fixture echo server) — this is easy to break and easy to miss.

---

### M6 — Always there

**Goal:** it starts at login, invisibly, and honestly reports whether it did.

**Deliverables:** `autostart` package with per-OS implementations; `--background` mode and `-H=windowsgui` build variant; the tray behind build tag `tray`; the first-run screen; `dropserve doctor`.

**Acceptance criteria**
- Script (Windows, CI): `dropserve autostart enable` then `schtasks /Query /TN Dropserve /XML` succeeds and the XML contains `<LogonTrigger>`, `ExecutionTimeLimit` set to `PT0S`, and no `<RunLevel>HighestAvailable</RunLevel>`.
- Assert: `dropserve autostart status` after an external `schtasks /Delete` reports **disabled** — proving it reads the OS, not a stored flag.
- Script: `enable` twice in a row succeeds (idempotent); `disable` twice succeeds.
- Script (Linux CI): the systemd user unit is written, `systemd-analyze verify` passes.
- Assert: `dropserve doctor` exits 0 on a healthy setup, exits 1 when a required condition fails, and its output contains a line for every check listed in §7.4.
- Script: the `-H=windowsgui` binary launched with `--background` produces no console window — verify by asserting the process has no attached console (`GetConsoleWindow() == 0` via a tiny test helper), since a screenshot test is not viable in CI.
- Assert: first-run detection is based on the absence of the state file; running it twice does not re-show the wizard or re-copy the example app if the user deleted it.

---

### M7 — Findable

**Goal:** the Sharing panel, and Tailscale as a first-class path out of the house.

**Deliverables:** `discovery` package — LAN IP selection with virtual-adapter filtering and change monitoring, mDNS responder with graceful degradation, Tailscale detection/status/serve/funnel; the Sharing UI; QR everywhere.

**Acceptance criteria**
- **Spike, first thing:** write `scripts/spike/mdns.go` that advertises `dropserve-spike.local` with `libp2p/zeroconf/v2`, runs for 20 seconds, and reports whether the bind succeeded. Run it on Windows (where the OS responder holds UDP/5353) and on Linux. If the default library fails to bind on Windows, repeat with `betamos/zeroconf`. Adopt whichever binds on both, record the result and the raw output in `STATE.md`, and write it up as an ADR. If **neither** binds on Windows, ship without mDNS on Windows — the `.local` URL is simply absent (invariant I3 is satisfied by hiding it) — and record that as the outcome. Do not spend more than one iteration on this.
- Assert: virtual-adapter filtering — given a synthetic interface list containing `vEthernet (WSL)`, `Tailscale`, `VirtualBox Host-Only` and one real adapter, the real one is chosen.
- Assert: when no LAN IP is available (loopback only), the Sharing panel shows the loopback URL and no broken entries — **I3** holds.
- Assert: mDNS bind failure is caught, logged, and results in the `.local` URL being **absent** from `/api/urls`, not present-and-broken.
- Assert: Tailscale detection with a faked `tailscale status --json` fixture parses `Self.DNSName` and produces the right URL; with a fixture where `BackendState` is `Stopped` or `NeedsLogin`, the panel shows the correct explanation and no URL.
- Assert: with no `tailscale` binary present, the panel shows the "not installed" explanation and does not error.
- Assert (**I5**): enabling Funnel requires a confirmation token that matches the app slug; a request without it is refused with 400 and no `tailscale funnel` process is spawned (verify with an injected fake executor).
- Assert: Funnel state is persisted with a timestamp and auto-expires after 8 hours (test with an injected clock).
- Assert: while Funnel is active, `/api/status` includes a `public_sharing_active` warning and it is non-dismissible in the UI.
- Assert (**hazard 11**): simulate resume-from-sleep by injecting a network-change event with a different LAN IP and a listener that has been closed underneath the server. Within one monitor interval the server re-probes the network, re-establishes any dead listener, updates every advertised URL, and `/api/status` reports the new address. No restart, no lost apps.
- Assert (**hazard 12**): when the LAN IP changes, the dashboard raises a persistent-until-dismissed notice naming the old and new addresses and linking to the DHCP-reservation explainer.
- Manual (record in `STATE.md`): on a real tailnet, `tailscale serve` produces a working HTTPS URL and `unserve` cleanly removes it. Use a throwaway tailnet or do it once — repeated toggling burns Let's Encrypt rate limits (**hazard 14**).

---

### M8 — HTTPS

**Goal:** working local HTTPS for people who want it, without ever surprising anyone.

**Deliverables:** `tlsca` package; leaf issuance covering the current address set with regeneration on change; `smallstep/truststore` install/uninstall; the UI that explains it; the recommendation to prefer the Tailscale route.

**Acceptance criteria**
- Assert: a generated leaf validates against the generated root for `localhost`, `127.0.0.1`, the hostname, and a synthetic LAN IP.
- Assert: the CA private key file mode is `0600` on Unix; on Windows, assert the ACL grants only the current user (or record the limitation explicitly in `STATE.md` if the check proves impractical).
- Assert: when the LAN IP changes, a new leaf is issued containing the new IP and the old one is superseded within one monitor interval.
- Assert (**I5**): trust installation is **never** invoked during startup, scanning, or any code path other than the explicit `trust install` command / dashboard button — prove it with a fake truststore interface and assert zero calls across a full server lifecycle test.
- Assert: `trust uninstall` removes what `trust install` added (fake at unit level; verify manually on Windows once and record it).
- Assert: HTTP → HTTPS redirect is **off** by default and the HTTP listener still serves everything when HTTPS is enabled.
- Assert (**hazard 13**): an issued leaf's `NotBefore` is at least one hour in the past, so a machine with a slightly wrong clock still accepts a freshly-issued certificate. Verify with an injected clock skewed 30 minutes fast and 30 minutes slow — the certificate validates in both cases.
- Assert: HTTPS listener failure (port taken) degrades to HTTP-only with a warning and does not prevent startup.

---

### M9 — PHP and add-ons

**Goal:** XAMPP parity for the cases people actually hit, without XAMPP's weight.

**Deliverables:** `runtimes` package — manifest of downloadable packs with pinned URLs and SHA-256; download with progress, verify, unpack, register; `php-cgi` pool + `gofast` FastCGI proxying; generated `php.ini`; SQLite file browser; MariaDB and PostgreSQL packs; the Add-ons UI with full removal.

**Acceptance criteria**
- Assert: pack integrity — a tampered download (wrong SHA-256) is rejected, deleted, and reported clearly.
- Assert: with the PHP pack installed, `testdata/fixtures/php/index.php` returns the expected output through `GET /php/`, including `$_GET`, `$_POST`, a file upload, and `PATH_INFO`.
- Assert: the FastCGI request parameters are correct — `SCRIPT_FILENAME`, `DOCUMENT_ROOT`, `REQUEST_URI`, `SCRIPT_NAME` reflect the app root and the `/slug/` prefix.
- Assert: a PHP fatal error produces a readable error page and does not take down the pool.
- Assert: killing a `php-cgi` worker externally causes the pool to recover within 5 seconds.
- Assert: removing the PHP pack deletes the runtime directory and leaves the app fixtures byte-identical (**I2** again).
- Assert: the SQLite browser lists tables and the first 100 rows of a fixture `.db` without holding a write lock on it.
- Assert: MariaDB/Postgres data directories are created under the state dir, never under an Apps root.
- Assert: with no packs installed, the binary and all tests still pass — packs are genuinely optional.

---

### M10 — Shippable

**Goal:** someone who is not you can install it and use it.

**Deliverables:** `goreleaser` config; GitHub Actions release workflow; Inno Setup installer; self-signed Authenticode signing with checksums and Sigstore attestation (§12); auto-update *check* (not auto-install); `docs/index.html` landing page live on GitHub Pages; README with screenshots; an uninstall path that actually removes autostart, firewall rule, and trusted CA.

**Acceptance criteria**
- Script: `goreleaser release --snapshot --clean` produces artefacts for `windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`, `darwin/arm64`, `darwin/amd64`, plus `checksums.txt`.
- Script: the Windows installer installs silently (`/VERYSILENT`), the binary runs, `dropserve healthz` returns ok, the uninstaller runs silently, and afterwards: no scheduled task, no install directory, and no leftover firewall rule. Run this in a Windows CI job.
- Script: `signtool verify /pa dropserve.exe` succeeds against the self-signed certificate.
- Script: the landing page is valid HTML (run an HTML validator or a linter in CI) and contains no external resource references — assert zero `http://`/`https://` URLs in `src`, `href` to stylesheets/scripts, or `url(` in CSS.
- Assert: the update check hits the GitHub releases API, compares semver, and **never downloads or executes anything** — it only surfaces a notification with a link. Verify with a fake HTTP client.
- Script: fresh-machine test — a clean Windows VM/container, install, drop a folder, reach it from another host on the network. Record the transcript in `STATE.md`.

---

### M11 — Hardening and polish

**Goal:** the product invariants hold, and the sharp edges are filed.

**Deliverables:** a security pass, a performance pass, an accessibility pass, a copy review against §9, and the invariant audit against §4.

**Acceptance criteria**
- Assert: the five invariant tests from §4 all exist, are named as specified, and pass.
- Script: `govulncheck ./...` reports no known-vulnerable dependencies.
- Script: `gosec ./...` passes, or every suppression carries a one-line justification comment.
- Assert: fuzz test the slug sanitiser and the path resolver for 60 seconds each with no crash and no escape from the app root (`go test -fuzz`).
- Script: performance floor — with 200 fixture apps, dashboard first byte < 100 ms and `/api/apps` < 200 ms on the CI runner; static file serving sustains ≥ 500 req/s for a 10 KB file.
- Script: memory floor — resident memory with 50 static apps and no command apps stays under 60 MB after a 5-minute soak.
- Assert: accessibility — the dashboard is fully keyboard-navigable (tab order test on the rendered DOM), every interactive element has an accessible name, colour contrast meets WCAG AA for both themes. Automate with `axe-core` in a headless browser test, or, if that proves heavy, assert the structural rules (labels, roles, `:focus-visible` styles present) and record the trade-off.
- Manual, recorded in `STATE.md`: read every user-facing string against §9 and list the ones you changed.

---

## 11. The build loop protocol

You are building this over many iterations, each starting with no memory of the last. `STATE.md` is your memory. Treat it as the most important file in the repository.

### 11.1 `STATE.md` template

Create this in M0 and update it at the end of **every** iteration, before committing.

```markdown
# Build State

**Current milestone:** M3 — Live
**Last updated:** 2026-08-27T14:02Z
**Gate status:** green   <!-- green | red -->
**Iterations completed:** 27

## Milestone progress
- [x] M0 — Repository, CI, and the gate    (tag: m0-complete)
- [x] M1 — Scan, mount, serve              (tag: m1-complete)
- [x] M2 — The index                       (tag: m2-complete)
- [ ] M3 — Live                            (6 of 7 criteria passing)
- [ ] M4 …

## Current milestone criteria
- [x] I1: folder reachable within 2s
- [x] deletion removes route
- [ ] rapid-change debounce: FLAKY — passes locally, fails on windows-latest ~1 in 4 runs. Investigating; see BLOCKED-m3-debounce.md
- [x] reconcile sweep
...

## Decisions made this build (beyond the spec)
- 2026-08-25 — Chose `grandcat/zeroconf` over `brutella/dnssd`: dnssd could not bind 5353 alongside the Windows responder in testing. ADR-010 written.
- 2026-08-26 — Debounce window raised 300ms → 500ms after Windows fsnotify duplicate-event observation.

## Open questions for the human
- None. / Or: "Q1: Should lazy-start default to on below 8GB RAM, or always off? Assumed 'on below 8GB' per §6.7 and proceeded."

## Deviations from the spec
- §6.5 search: added a 300ms input debounce not specified. Harmless, improves feel on phones.

## Dependency count
14 direct (baseline at M0: 3). Cap is 12 direct + tray/test-only — **OVER CAP, must justify or remove before M11.**
```

### 11.2 The iteration

Every iteration, in this exact order:

1. **Read `STATE.md`.** Do not read the whole repository first; read the ledger, then only what you need.
2. **Run the gate** (`make check`). If it is red, fixing it is the entire iteration. A red gate blocks all feature work, no exceptions.
3. **Pick exactly one target**: the lowest-numbered unmet acceptance criterion in the current milestone. If everything in the current milestone passes, run the demo script, record its output, tag `m<N>-complete`, advance the milestone, and end the iteration there.
4. **Write the failing test first.** Run it. Confirm it fails for the reason you expect. A test that passes before you write the implementation is testing the wrong thing.
5. **Implement the smallest change that makes it pass.**
6. **Run the full gate again.** Not just your test — the whole thing, with `-race`.
7. **Update `STATE.md`**: tick the criterion, note any decision or deviation, update the timestamp and iteration count.
8. **Commit** with a conventional-commit message referencing the milestone: `feat(watcher): debounce fsnotify events [M3]`. One logical change per commit.

### 11.3 Stop conditions and escape hatches

- **Three strikes.** If the same acceptance criterion fails three iterations running, stop attacking it. Write `BLOCKED-<milestone>-<short-name>.md` containing: the criterion, what you tried (all three approaches, specifically), the exact failure output, and your best hypothesis. Note it in `STATE.md`, then move to the next **independent** criterion. Never let one stuck criterion stall the whole build.
- **Flaky is failing.** A test that passes 3 in 4 runs is a bug, either in the test or in the code. Mark it, do not tick it. Never add a retry loop to hide a race.
- **Platform skips are declared.** If a criterion genuinely cannot be verified on the available runner, `t.Skip` with a message naming the reason, and list it under a **Verify on real hardware** section in `STATE.md`. Skipped is not passed.
- **Budget.** If a milestone exceeds roughly 40 iterations, stop and write a summary of where the time went into `STATE.md` before continuing. Something is probably wrong with the approach.

### 11.4 Prohibitions

These are absolute. Violating one is a build failure even if everything is green.

1. **Do not weaken the gate.** No deleting tests, no `t.Skip` without a declared reason, no loosening lint rules, no `//nolint` without a justification comment, no editing an acceptance criterion to match what you built.
2. **Do not stub and tick.** A criterion is met when the real behaviour works, not when a function exists that returns the right shape.
3. **Do not add a dependency** outside §7.1's list without writing an ADR in the same commit that explains why the standard library will not do.
4. **Do not write inside a user's app folder** (invariant I2), in code or in tests against real directories.
5. **Do not commit secrets.** The signing certificate, its password, and any tokens live in GitHub Actions secrets and nowhere else. Add a `gitleaks` step to CI in M0.
6. **Do not implement anything that exposes the machine to the internet by default** (invariant I5). Funnel and trust installation are explicit user actions, always.
7. **Do not silently change a decision recorded in this document or an ADR.** Write a new ADR that supersedes the old one and reference it in `STATE.md`.
8. **Do not reformat or refactor unrelated code** in a feature commit. It hides the change.

### 11.5 Testing strategy

| Level | What | Where | Speed |
|---|---|---|---|
| **Unit** | slug rules, path resolution, detection rules, header rewriting, base-href injection, ranking, config parsing | alongside the package | < 1 s total |
| **Integration** | full server on a random port with a temp Apps root, real HTTP requests via `httptest.Server` and `net/http` | `internal/*/..._integration_test.go` | < 30 s total |
| **Fixture apps** | real tiny apps in `testdata/fixtures/` — static, php, node, python, absolute-paths, broken, spawns-grandchild, websocket-echo, large-html | shared across tests | — |
| **Platform** | autostart registration, job objects, firewall, port exclusions | `..._windows_test.go` etc., guarded by build tags | CI matrix |
| **Smoke** | `scripts/smoke/m<N>.{sh,ps1}` — end-to-end, run in CI and by the demo step | — | < 2 min |
| **Fuzz** | slug sanitiser, path resolver, manifest parser | `go test -fuzz` in a nightly CI job | 60 s each |

Rules: every test creates its own temp directory and cleans it up; no test binds a fixed port (always `:0` then read the assigned port); no test sleeps a fixed duration to wait for something — poll with a deadline; tests must pass with `-race` and with `-count=3`.

### 11.6 Recording decisions

When you decide something this document did not decide, write `docs/adr/NNN-title.md`:

```markdown
# ADR-0NN: <title>
Date: 2026-08-27 · Status: accepted · Supersedes: —

## Context
What forced a choice. Include the concrete failure or constraint you hit.

## Options considered
1. … — cost/benefit
2. … — cost/benefit

## Decision
What you chose, in one sentence.

## Consequences
What is now harder. What we will have to revisit. What test guards it.
```

Then add one line to `STATE.md` under "Decisions made this build". This is how a human reviewing the build in six months understands why the code looks the way it does.

### 11.7 When to ask the human

Almost never — the spec is deliberately detailed so you can proceed. Ask only when a decision is **irreversible and materially changes the product**: a decision to break invariant I5, an unavoidable change to the public repository name or licence, or a discovery that a core assumption in §6 is impossible on the target platform. Otherwise: make the most reasonable choice, record it in `STATE.md` under "Deviations", and keep moving.

---

## 12. Packaging, signing, and release

### 12.1 Artefacts

| Platform | Artefacts |
|---|---|
| Windows | `Dropserve-Setup-x.y.z.exe` (Inno Setup, per-user install, no admin required) · `dropserve_x.y.z_windows_amd64.zip` (portable) · arm64 of both |
| macOS | `dropserve_x.y.z_darwin_arm64.tar.gz` + `_amd64` (notarisation is out of scope until macOS is a supported platform) |
| Linux | `.tar.gz` for amd64/arm64 · `.deb` and `.rpm` via goreleaser · an AUR PKGBUILD if someone volunteers |
| All | `checksums.txt` + its signature · SBOM (`syft` or goreleaser's built-in) |

Per-user Windows install (into `%LOCALAPPDATA%\Programs\Dropserve`) is deliberate: **no UAC prompt during installation**, which removes the scariest moment for a non-technical user, and matches ADR-007's per-user autostart.

### 12.2 Code signing — what is actually achievable

Be honest with yourself and with users here, because the internet is full of wishful thinking on this topic.

**What a self-signed Authenticode certificate gives you:** a stable publisher identity, tamper-evidence (a modified binary fails `signtool verify`), and a way for a user or an IT admin who has imported your public certificate to verify the file is genuinely yours.

**What it does not give you:** relief from SmartScreen. Microsoft Defender SmartScreen builds reputation from CA-issued certificates and download volume. A self-signed certificate carries no reputation and will not suppress the "Windows protected your PC" screen. Since 2023, publicly-trusted code-signing certificates must have their private keys on hardware or an approved HSM/cloud service, which is why cheap or free CA-issued options effectively no longer exist for individuals. Do not promise users a warning-free install.

**Therefore, the v1 signing plan:**

1. **Self-signed Authenticode**, generated once and stored as a GitHub Actions secret:
   ```powershell
   $c = New-SelfSignedCertificate -Type CodeSigningCert `
        -Subject "CN=Dropserve, O=Dropserve Project" `
        -CertStoreLocation Cert:\CurrentUser\My `
        -KeyUsage DigitalSignature -KeyLength 3072 `
        -NotAfter (Get-Date).AddYears(10)
   Export-PfxCertificate -Cert $c -FilePath dropserve-signing.pfx -Password $pw
   Export-Certificate  -Cert $c -FilePath dropserve-public.cer   # publish this in the repo
   ```
   Sign in CI with `signtool sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $env:PFX /p $env:PFX_PASS`. **Timestamping is mandatory** — without it, signatures die when the certificate expires.
2. **SHA-256 checksums** published for every artefact, and the `checksums.txt` itself signed.
3. **Sigstore keyless signing** of release artefacts via `cosign` in GitHub Actions using OIDC — free, no key to protect, and increasingly the expected practice for open-source releases. Publish the verification command in the README.
4. **Package managers as the friction-free path.** Submit to **Scoop** (trivial, a JSON manifest in a bucket) and **winget** (a YAML manifest PR to `microsoft/winget-pkgs`). `winget install Dropserve` sidesteps the browser download warning entirely and is the install route the landing page should lead with once accepted. On macOS/Linux, a Homebrew tap and the `.deb`/`.rpm` serve the same purpose.
5. **Document the warning honestly** in the README and on the landing page: one short paragraph explaining that the app is signed with the project's own certificate, why SmartScreen still warns, how to verify the checksum, and that `winget`/`scoop` avoids it. Users trust projects that explain this far more than projects that stay quiet about it.

### 12.3 Release workflow

Tag `vX.Y.Z` → GitHub Actions:
1. `make check` on the full matrix (a red gate blocks the release).
2. `goreleaser release --clean` → binaries, archives, checksums, SBOM, changelog from conventional commits.
3. Build the Inno Setup installer; sign both `dropserve.exe` and the installer; verify with `signtool verify /pa`.
4. `cosign sign-blob` each artefact (keyless, OIDC).
5. Run the fresh-machine smoke script against the built installer in a clean Windows job.
6. Publish the GitHub Release; update the Scoop bucket and open the winget PR.
7. Deploy `docs/` to GitHub Pages.

Versioning is semver. `v0.x` until M11 is complete; `v1.0.0` is the first release where all five invariants have passing tests and the fresh-machine test has been run on real hardware.

### 12.4 Update checking

Check the GitHub releases API at most once every 24 hours, on startup and daily. **Surface only** — show a dashboard badge and a tray menu item linking to the release page. Never download, never execute, never auto-update. Provide a setting to turn the check off entirely, and make the first-run screen mention that it happens.

---

## 13. The landing page (GitHub Pages)

A file is provided alongside this document: **`docs/index.html`** — copy it into the repository at that path, add an empty `docs/.nojekyll`, and set GitHub Pages to serve from the `main` branch `/docs` folder.

### 13.1 Requirements it already satisfies

- **Single self-contained file.** No external stylesheet, script, font, or image. Everything inline; icons are inline SVG. This means it renders identically offline, has no third-party tracking, and cannot break because a CDN moved.
- **Theme-aware** via `prefers-color-scheme`, with both palettes meeting WCAG AA contrast.
- **Responsive** down to 320 px with no horizontal scroll; the comparison table scrolls inside its own container.
- **Fast.** Under ~25 KB, no render-blocking anything.
- **Honest.** The install section states the SmartScreen situation plainly rather than hiding it.

### 13.2 What to keep updated as the build progresses

- The repository is **`github.com/tanzir71/dropserve`** and the page already points at it. If the repo is ever renamed or moved to an organisation, update the eight links in `docs/index.html` and the badges in `README.md` in the same commit.
- The download links point at `/releases/latest` — they work as soon as the first release exists.
- Swap the hero placeholder for a real screen recording once M6 is done: a 6–10 second silent loop of a folder being dragged into the Apps window and the dashboard card appearing. That single clip will do more selling than every paragraph on the page. Keep it under 2 MB, `autoplay muted loop playsinline`, and give it a `poster`.
- Add screenshots of the dashboard (light and dark) after M2.
- Keep the comparison table factually defensible. If a competitor adds a feature, update the row. Never claim a competitor lacks something it has.

---

## 14. Security model

State this in `SECURITY.md` and link it from the landing page.

**The threat model:** Dropserve serves files and proxies local processes onto a home or office LAN, on behalf of one person who controls the machine. It is not a hardened public web server, and the documentation says so.

**What is defended:**
- **Path traversal** — every resolved path is verified to remain under its app root after cleaning; fuzz-tested (M11).
- **Slug injection** — slugs are sanitised to `[a-z0-9-]` before ever touching the filesystem or the router.
- **Request abuse** — header and body size limits, read/write timeouts, a connection cap, and an SSE client cap.
- **CSRF on the dashboard API** — same-origin check plus a token; all mutating endpoints.
- **Supply chain** — pinned dependency versions, `govulncheck` in CI, pinned SHA-256 for every downloadable runtime pack, Sigstore attestation on releases.
- **Accidental exposure** — invariant I5: nothing reaches the internet and nothing enters the OS trust store without an explicit action in that session.

**What is explicitly *not* defended, and must be documented:**
- Anyone on the same LAN can reach any app with `visibility: lan`. That is the intended behaviour; the first-run screen says it in plain words.
- App code runs with the user's full privileges. Dropserve does not sandbox apps — dropping a folder is equivalent to running its code, and the docs must say exactly that.
- Apps are not isolated from each other or from the user's filesystem.
- Funnel exposes an app to the entire internet with no authentication unless the app provides its own.

**Reporting:** `SECURITY.md` with a contact address and a 90-day disclosure window.

---

## 15. Known hazards

The specific things that will bite. Each one has cost someone a week somewhere.

1. **Orphaned child processes on Windows.** `npm start` spawns `node`; killing `npm` leaves `node` holding the port; the next restart fails with `EADDRINUSE` and the app looks permanently broken. **Fix:** Job Objects with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, tested in M4. This is the single most likely source of "it worked yesterday" bug reports.
2. **`WSAEACCES` on port 80 that is not a permissions problem.** WinNAT/Hyper-V/WSL reserve *excluded port ranges*; binding inside one fails even as administrator. Diagnose with `netsh int ipv4 show excludedportrange protocol=tcp`, and note that `http.sys` reservations (IIS, Web Deploy, some VPN clients) are a separate cause found via `netsh http show servicestate`. Never assume the cause; report the real one in `doctor`.
3. **UDP 5353 is already held** by the Windows mDNS responder. Bind with `SO_REUSEADDR`, and degrade silently if it still fails.
4. **fsnotify buffer overflow on large trees.** Windows `ReadDirectoryChangesW` has a fixed buffer; a `node_modules` with 40,000 files will overflow it and events will be *dropped silently*. Cap watch depth, skip ignored directories before watching them, and keep the 30-second reconcile sweep as the real safety net.
5. **OneDrive / network drives.** `%USERPROFILE%\Desktop` and `Documents` are frequently OneDrive-synced, where fsnotify is unreliable and files may be cloud-only placeholders that block on read. Detect cloud-only files (`FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS`) and either skip them in the indexer or handle the stall; default the Apps root to `%USERPROFILE%\Dropserve` which is usually *not* synced, and warn if the configured root is.
6. **Windows path length.** Deep `node_modules` trees exceed `MAX_PATH`. Use extended-length paths (`\\?\`) in the scanner, or the walk will fail on real projects.
7. **Case-insensitive filesystems.** `Notes` and `notes` are the same directory on Windows and default macOS. Slug collision detection must be case-insensitive, and the scanner must not report a rename when only case changed.
8. **The dashboard shadowing an app.** An app called `api` or `_dropserve` must never be able to break the dashboard. Reserved slugs, tested in M1.
9. **Response rewriting breaking binaries.** The `<base href>` injector must gate on `Content-Type: text/html` *and* size *and* absence of `Content-Encoding` it cannot decode. Getting this wrong corrupts downloads in a way that is maddening to debug. The "hash in, hash out" test in M5 exists for exactly this.
10. **Antivirus quarantine.** Unsigned or newly-signed Go binaries that open ports and spawn processes are a classic false-positive shape. Expect it, mention it in the FAQ, and submit false positives to Microsoft's portal when they occur.
11. **Laptop sleep.** On resume, the LAN IP may have changed, listeners may be broken, and children may have died. Re-probe the network and re-verify listeners on resume; on Windows, listen for `WM_POWERBROADCAST`, and in any case have the 30-second monitor catch it.
12. **DHCP changing the IP.** The user's whole mental model is "remember the IP". When it changes, say so prominently in the dashboard and link to a one-paragraph explanation of DHCP reservation on a typical router. Consider offering the `.local` name as the more durable answer once mDNS is proven.
13. **Time and clock skew** in the local CA. A machine with a wrong clock will reject freshly-issued certificates. Backdate the leaf's `NotBefore` by an hour.
14. **Let's Encrypt rate limits** via Tailscale HTTPS. Repeatedly toggling `serve`/`funnel` in testing can exhaust them and lock the tailnet out of certificate issuance for hours. Test against a fake executor; touch the real tailnet sparingly.
15. **Symlinks need administrator rights or Developer Mode on Windows.** The obvious implementation of `dropserve add <path>` — symlink the folder into an Apps root — fails for a normal user. It also means Dropserve would be writing into an Apps root, which breaks invariant I2's spirit. **Fix:** `add` writes a registered-path entry into `config.toml`; the scanner treats registered paths as single apps alongside the roots it walks. No symlink is ever created. Guarded by an M1 assertion that `add` produces a config entry and zero filesystem changes.
16. **`goreleaser` and `CGO_ENABLED=0` must agree.** A single dependency that pulls in cgo (an easy accident with SQLite or a tray library) turns every cross-compile red at release time rather than at commit time. **Fix:** the M0 gate already cross-builds all three targets on every commit, which catches it the same day it is introduced. Never relax that step to "build the host target only" to make CI faster.

### 15.1 Hazard coverage map

Every hazard above is either guarded by a test or consciously accepted. Nothing is left as prose only. If you add a hazard, add its row here in the same commit.

| # | Hazard | Guarded by | Where |
|---|---|---|---|
| 1 | Orphaned child processes | `TestProcessTreeIsKilled` (grandchild PID gone within 5s) | M4 |
| 2 | `WSAEACCES` / excluded port ranges | Port-ladder test with occupied 80 and 8080; `doctor` reports the real cause | M1, M6 |
| 3 | UDP 5353 already held | M7 spike decides the library; failure hides the `.local` URL (I3) | M7 |
| 4 | fsnotify buffer overflow | Watch-depth cap + 30s reconcile sweep test | M3 |
| 5 | OneDrive / cloud-only files | Placeholder-attribute test + sync-folder warning test | M3 |
| 6 | Windows `MAX_PATH` | Deep-tree walk test (>260 chars) | M1 |
| 7 | Case-insensitive filesystems | Case-insensitive collision + rename test | M1 |
| 8 | Dashboard shadowed by an app | Reserved-slug test | M1, M2 |
| 9 | Response rewriting corrupting binaries | "Hash in, hash out" test for non-HTML; 5 MB cap test | M5 |
| 10 | Antivirus false positives | **Accepted.** Documented in the FAQ; submit to Microsoft's portal when it happens | M10 |
| 11 | Laptop sleep / resume | Injected network-change + dead-listener recovery test | M7 |
| 12 | DHCP changing the IP | IP-change notice test | M7 |
| 13 | Clock skew vs the local CA | Backdated `NotBefore` test with a skewed clock | M8 |
| 14 | Let's Encrypt rate limits | **Accepted.** Fake executor in tests; real tailnet touched once, manually | M7 |
| 15 | Windows symlink privileges | `add` writes config, never a symlink; zero-filesystem-change assertion | M1 |
| 16 | cgo creeping in | Cross-build all targets in `make check` on every commit | M0 |

---

## 16. Reference

### 16.1 `dropserve.json` manifest (all fields optional)

```jsonc
{
  "name": "Invoice Maker",              // display name; default: prettified folder name
  "description": "Makes PDF invoices",  // one line, shown on the card and indexed
  "icon": "📄",                          // emoji, or a path to an image inside the app
  "tags": ["work", "pdf"],              // indexed, filterable

  "type": "static",                     // static | php | command  — overrides detection
  "command": "node server.js",          // for type=command
  "port_env": "PORT",                   // env var carrying the assigned port
  "env": { "NODE_ENV": "production" },  // merged over the OS environment
  "health_path": "/",                   // probed until < 500 before mounting
  "autostart": true,                    // false = start lazily on first request

  "index": "index.html",                // entry document for static apps
  "spa": false,                         // true = unmatched paths fall back to index
  "directory_listing": false,           // show a listing when no index exists
  "base_href": "auto",                  // auto | always | never  (§6.4 layer 2)

  "visibility": "lan",                  // lan | local | tailnet | public
  "pinned": false,                      // sort to the top of the dashboard
  "hidden": false                       // omit from the index entirely
}
```

Unknown keys are ignored with a dashboard warning naming the key — never a hard failure. A malformed manifest logs a clear parse error and the app falls back to full auto-detection rather than disappearing.

### 16.2 `config.toml`

```toml
[server]
apps_roots  = ["C:\\Users\\you\\Dropserve\\Apps"]
http_port   = 0            # 0 = use the fallback ladder and remember the result
https_port  = 0
bind        = "0.0.0.0"    # "127.0.0.1" to make it local-only
app_port_range = [7400, 7999]

[dashboard]
title        = "Dropserve"
theme        = "auto"      # auto | light | dark
pin_to_root  = ""          # a slug to serve at "/" instead of the dashboard

[discovery]
mdns         = true
mdns_name    = "dropserve"
tailscale    = true

[security]
pin_enabled  = false
pin_hash     = ""

[runtimes]
php_version  = "8.3"
lazy_start   = "auto"      # auto | always | never

[updates]
check = true
```

### 16.3 Glossary for the README

**App** — a folder in an Apps root. **Slug** — the URL-safe name derived from the folder name. **Apps root** — a folder Dropserve watches. **Mount** — the act of making an app reachable at a URL. **Pack** — an optional downloadable runtime (PHP, MariaDB, PostgreSQL). **Own port** — the dedicated per-app port that always works even when path-mounting does not. **Tailnet** — your private Tailscale network. **Funnel** — Tailscale's public-internet exposure feature.

### 16.4 Sources consulted for this specification

- Caddy — [Automatic HTTPS](https://caddyserver.com/docs/automatic-https), [Admin API](https://caddyserver.com/docs/api)
- FrankenPHP — [project site](https://frankenphp.dev/), [Go library](https://pkg.go.dev/github.com/dunglas/frankenphp), [Windows support announcement](https://dunglas.dev/2026/03/windows-support-for-frankenphp-its-finally-alive/)
- Tailscale — [Funnel](https://tailscale.com/docs/features/tailscale-funnel), [tsnet](https://tailscale.com/kb/1244/tsnet), [tsnet API](https://pkg.go.dev/tailscale.com/tsnet)
- Microsoft — [WSAEACCES on bind](https://learn.microsoft.com/en-us/troubleshoot/windows-server/networking/error-10013-wsaeacces-is-returned), [code-signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options)
- Libraries — [gofast](https://github.com/yookoala/gofast), [smallstep/truststore](https://pkg.go.dev/github.com/smallstep/truststore)
- Competitors — [XAMPP](https://www.apachefriends.org/), [AMPPS](https://www.ampps.com/), [MAMP](https://www.mamp.info/en/windows/), [WampServer](https://sourceforge.net/projects/wampserver/), [Laragon](https://laragon.org/)

---

## 17. Definition of done for v1.0

Ship when all of these are true. Not before, and not much after.

- [ ] M0 through M11 complete, each tagged, `STATE.md` clean.
- [ ] The five invariant tests in §4 exist by name and pass on Windows and Linux CI.
- [ ] All seven golden paths in §5 have been walked by a human on real hardware and recorded in `STATE.md`.
- [ ] A person who has never seen the project installs it and reaches an app from their phone **without asking a question**. This is the real test. Find someone and watch them do it without helping.
- [ ] `dropserve doctor` output is sufficient to diagnose every failure mode you encountered during the build.
- [ ] README, SECURITY.md, and the landing page are accurate — including about SmartScreen.
- [ ] Uninstall leaves nothing behind: no scheduled task, no firewall rule, no trusted CA, no files outside the user's own Apps folders.
