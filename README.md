# Dropserve

Dropserve is a free, open-source local web server for people who want to use a local server, not administer one. Drop a folder into your Apps directory and it becomes reachable from a searchable dashboard without configuration or a restart.

The current build serves static sites and supervised Node or Python apps, updates live as folders change, preserves stable per-app ports, and handles apps that cannot run safely below a URL prefix. It includes first-run setup, diagnostics, per-user autostart, and an optional desktop tray. LAN discovery, Tailscale sharing, HTTPS, runtime packs, and installers are the next milestones in the authoritative [build handover](DROPSERVE-HANDOVER.md).

## Getting started

Requirements: Go 1.23 or newer and GNU Make.

```console
make check
make build
```

On Windows, `make build` creates two binaries:

- `bin/dropserve.exe` is the console-free desktop build with the tray.
- `bin/dropserve-cli.exe` is the terminal build for commands and diagnostics.

Run `dropserve.exe` with no arguments. The one-screen setup lets you choose the Apps folder and whether Dropserve should start at login. Starting setup creates a small example app, launches the server, and opens the dashboard. On Linux and macOS, run `bin/dropserve`; the same first-run flow opens in the default browser.

Useful terminal commands:

```console
dropserve serve
dropserve add PATH
dropserve status
dropserve doctor
dropserve autostart enable
dropserve autostart status
dropserve autostart disable
```

Windows uses a current-user Scheduled Task, Linux uses a systemd user unit, and macOS uses a per-user LaunchAgent. Linux users who need serving to continue without an active login can enable systemd lingering after registration; the command prints the exact `loginctl` instruction.

The Windows desktop tray can open the dashboard or Apps folder, copy the local link, pause serving, manage start-at-login, run the doctor, and quit. Source builds can opt into the tray with `go build -tags tray ./cmd/dropserve`; the normal source build remains fully headless.

## Non-goals

- Dropserve is not a production internet-facing web server.
- It is not a Docker replacement or an orchestrator.
- It is not a build system and does not run package installation for apps.
- It is not a WordPress or Laravel development environment first.
- It is not multi-user.
- It is not a file-sync tool.

App code runs with your user account's privileges. Anyone on the same LAN can reach apps configured with the default LAN visibility. These are deliberate parts of the personal-server threat model.

## Development

`make check` is the merge gate. It verifies formatting, static analysis, race-enabled tests, version injection, zero-CGO Windows/Linux/macOS builds, the optional Windows tray build, and unfinished shipped text. See [CONTRIBUTING.md](CONTRIBUTING.md) for the product invariants that every change must preserve.

## Licence

Dropserve is available under the [MIT License](LICENSE).
