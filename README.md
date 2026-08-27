# Dropserve

Dropserve is a free, open-source local web server for people who want to use a local server, not administer one. Its happy path is simple: drop a folder into an Apps directory and open one address to find and launch it.

> The repository is at the M0 foundation stage. The server and dashboard are not implemented yet.

## Product direction

Dropserve will provide path-based hosting, a searchable dashboard, live folder discovery, supervised Node and Python apps, optional PHP and database packs, LAN sharing, and explicit Tailscale integration. The authoritative build contract is [DROPSERVE-HANDOVER.md](DROPSERVE-HANDOVER.md).

## Non-goals

- Dropserve is not a production internet-facing web server.
- It is not a Docker replacement, orchestrator, build system, file-sync tool, or multi-user platform.
- It does not prioritize WordPress or Laravel development over small local apps.

App code runs with your user account's privileges. Anyone on the same LAN can reach apps configured with the default LAN visibility. These are deliberate parts of the personal-server threat model.

## Development

Requirements: Go 1.23 or newer and GNU Make.

```console
make check
make build
./bin/dropserve version
```

`make check` is the merge gate. It verifies formatting, static analysis, race-enabled tests, version injection, zero-CGO cross-builds, and unfinished shipped text.

## Licence

Dropserve is available under the [MIT License](LICENSE).

