# Dropserve local API

The dashboard uses this API, and local tools may use it as an integration surface. It is available beneath `/_dropserve/` on every active Dropserve HTTP or HTTPS listener.

## Read-only endpoints

| Method | Path | Result |
|---|---|---|
| `GET` | `/_dropserve/api/apps` | Visible app index. Add `?include_hidden=1` to include hidden cards. |
| `GET` | `/_dropserve/api/apps/{slug}` | One app with detection details and warnings. |
| `GET` | `/_dropserve/api/search?q=…` | Ranked, visible app matches. |
| `GET` | `/_dropserve/api/urls` | Only currently verified listener, LAN, mDNS, and Tailscale addresses. |
| `GET` | `/_dropserve/api/status` | Version, uptime, ports, discovery/sharing state, warnings, update notice, and CSRF token. |
| `GET` | `/_dropserve/api/logs/{slug}` | Command-app snapshot as JSON; request `Accept: text/event-stream` for live `logs` events. |
| `GET` | `/_dropserve/api/events` | Server-sent `apps-changed` events. |
| `GET` | `/_dropserve/api/qr?url=…` | Local PNG QR code for an HTTP(S) URL. |
| `GET` | `/_dropserve/api/databases/{slug}?file=…` | Read-only SQLite tables and the first 100 rows for a database discovered in that app. |
| `GET` | `/_dropserve/api/addons` | Optional PHP, MariaDB, and PostgreSQL installation/process status. |
| `GET` | `/_dropserve/api/https/root.pem` | The local root certificate, after one has been generated. |
| `GET` | `/_dropserve/healthz` | Plain `ok` while the server is ready. |

## Mutations

| Method | Path | JSON body |
|---|---|---|
| `POST` | `/_dropserve/api/apps/{slug}/restart` | `{}` |
| `POST` | `/_dropserve/api/apps/{slug}/settings` | `{"pinned":true}` and/or `{"hidden":true}` |
| `POST` | `/_dropserve/api/rescan` | `{}` |
| `POST` | `/_dropserve/api/open-folder` | `{}` for the Apps root or `{"slug":"notes"}` |
| `POST` | `/_dropserve/api/addons/{name}` | `{"action":"install"}`; actions are `install`, `remove`, `start`, or `stop`. |
| `POST` | `/_dropserve/api/https` | `{"enabled":true}` |
| `POST` | `/_dropserve/api/trust` | `{"installed":true}` |
| `POST` | `/_dropserve/api/network-change/dismiss` | `{}` |
| `POST` | `/_dropserve/api/sharing/tailscale` | `{"enabled":true}` |
| `POST` | `/_dropserve/api/sharing/funnel/{slug}` | To start: `{"enabled":true,"confirmation":"<exact slug>"}`. To stop: `{"enabled":false}`. |

Every mutation requires both:

1. `X-Dropserve-CSRF: <csrf_token>` using the current token returned by `GET /_dropserve/api/status`.
2. An `Origin` whose scheme and authority exactly match the Dropserve request URL.

When PIN lock is enabled, a non-loopback client must first authenticate at `/_dropserve/login`; the API then also requires its signed session cookie. The local `dropserve` CLI obtains the current token and sends the matching origin automatically.

Responses are intentionally local-machine APIs, not a versioned public cloud contract. Clients should ignore fields they do not recognize.
