# Server mode — the app in a browser tab

The same application, built without a window: the process serves its UI
over HTTP, and any browser tab is the app window. Everything the desktop
build does works the same way — the sidebar, transfers, the safety
ladder, versions, the admin panel — because the page in the browser and
the webview in the desktop build load the same frontend and speak to the
same Go backend through the same bindings (over plain HTTP and a
WebSocket for live events instead of the in-process bridge).

Use it when the app should live on one machine and be reachable from
another: a jump host you RDP into anyway, a home server, a VM in the
basement — anywhere installing the desktop app for every user is more
machinery than the job needs. The host needs no GUI stack at all: on
Linux the server build is pure Go, no GTK, no webkit, no cgo.

## Build

```bash
go build -tags server -o s3b-server ./cmd/s3b
```

Works on every platform Go does. Cross-compile freely — a Linux build
needs no GUI libraries on the build host either (`CGO_ENABLED=0
GOOS=linux go build -tags server …` builds clean on a Windows laptop).
Stamp the version like any other flavor (`-ldflags "-X
main.version=1.2.0-beta.3"`, see [build.md](build.md#version-stamping))
or `s3b version` reports a dev version.

## Run

```bash
./s3b-server
```

The console answers with the address it is listening on:

```
INF Server mode enabled (built with -tags server)
INF Server mode starting address=127.0.0.1:8080
```

Open `http://127.0.0.1:8080` in a browser and you are in the app. Two
environment variables steer the listener (Wails' own convention):

| Variable | Default | Meaning |
|----------|---------|---------|
| `WAILS_SERVER_HOST` | `localhost` | Interface to bind. `0.0.0.0` listens on all of them. |
| `WAILS_SERVER_PORT` | `8080` | TCP port. |

`http://…/health` answers `{"status":"ok"}` — a one-line probe for
uptime monitors, load balancers and shell health checks.

A browser from the Chromium family (Edge, Chrome, Chromium, Brave,
Vivaldi) is the tested surface; Firefox works for browsing but the
battery of live GUI verifications runs on Chromium. Whatever the
browser, keep the tab focused for clipboard gestures (Copy path, Copy
URL): browsers only honor `navigator.clipboard` writes from a page the
user is interacting with — a backgrounded tab's copy can be refused by
the browser itself.

## Reaching it from another machine

By default the server binds `localhost`, so only the machine it runs on
can open it. To let other machines in:

1. Bind all interfaces: `WAILS_SERVER_HOST=0.0.0.0` (and pick a port).
2. Open the port in the host's firewall:
   - Windows: `netsh advfirewall firewall add rule name="s3b server" dir=in action=allow protocol=TCP localport=8080`
   - Linux: `sudo ufw allow 8080/tcp` (or your distro's equivalent)
3. From the client, browse `http://<host>:8080`.

**Read the security section before doing this.** There is no login page
and no built-in TLS; step 1 hands the app to everything that can reach
the port.

## Security model — read this part

Server mode is a single-user tool made reachable, not a multi-user
service. Three properties shape every deployment decision:

- **No authentication.** Every request that reaches the port gets the
  full application — the saved data sources, the transfer engine, the
  admin panel. The gate is the network, not the app.
- **No TLS.** The listener speaks plain HTTP; credentials and object
  bytes cross the wire in the clear unless something in front adds TLS.
- **"Local" means the host.** The Local disk pane browses the *server's*
  filesystem. Downloads land on the host, uploads pull from it, Edit in
  editor opens the editor *on the host* (an editor window nobody at the
  browser sees). A server-mode instance pointed at the host's whole
  drive is, from the browser, remote access to that drive.

Three ways to expose it safely, best first:

**SSH tunnel (one user, zero new surface).** Leave the server on
`localhost` and tunnel:

```bash
ssh -L 8080:127.0.0.1:8080 user@host
# then browse http://127.0.0.1:8080 locally
```

Auth, encryption and revocation are whatever SSH already has.

**Reverse proxy (a team, or any non-SSH client).** Put TLS and an
auth layer in front and bind the app to loopback only. Caddy in one
breath:

```
s3b.example.net {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy manages the certificate; add `basic_auth` (or forward auth to
whatever SSO the network runs) inside the same block. The WebSocket the
app streams events over passes through Caddy and nginx unchanged with
stock `reverse_proxy` / `proxy_pass` settings (they upgrade
`Connection: Upgrade` by default).

**Trusted LAN only.** `WAILS_SERVER_HOST=0.0.0.0` on a network where
every machine is yours — a lab VLAN, a home lab behind its own firewall.
This is the only situation where binding all interfaces without a
proxy is defensible, and it is still plaintext on the wire.

Never put a bare server-mode port on the public internet.

## Secrets and the config dir

The process keeps its store (sources, profiles, settings) in the config
directory — `%LOCALAPPDATA%\s3b` on Windows, `~/.config/s3b` on Linux,
overridable with `S3B_CONFIG` for isolation (the same variable the
verification harness uses to run throwaway instances).

On a desktop that store leans on the OS keyring for secrets. A headless
host has no keyring session, so run with:

```bash
S3B_NO_KEYRING=1 ./s3b-server
```

Secrets then stay inside the config directory (the same fallback the
headless CLI uses on servers) — make sure that directory is readable by
the service user only (`chmod 700`). If the host does have a desktop
session with a keyring and you want the server to use it, leave the
variable unset and the keyring is used as usual; on Windows the server
process uses Credential Manager exactly like the desktop app.

A server instance and a desktop instance on the same machine default to
the same store. Point one of them at a different `S3B_CONFIG` if you
want them independent; note that saved-window geometry is shared state
too, so sharing the store between two concurrently running faces means
the last one to quit wins the placement memory.

## Keeping it running

The process exits cleanly on SIGINT/SIGTERM (in-flight transfers get
the same graceful shutdown the desktop app's exit gate enforces).

**Linux — systemd unit** (`/etc/systemd/system/s3b.service`):

```ini
[Unit]
Description=s3b server mode
After=network-online.target

[Service]
User=s3b
Environment=WAILS_SERVER_HOST=127.0.0.1
Environment=WAILS_SERVER_PORT=8080
Environment=S3B_NO_KEYRING=1
ExecStart=/usr/local/bin/s3b-server
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`sudo systemctl enable --now s3b`, then `curl localhost:8080/health`
to confirm. The loopback bind plus a reverse proxy or tunnel is the
intended shape.

**Windows — Task Scheduler at boot** (`schtasks /create /tn "s3b
server" /tr "C:\path\s3b-server.exe" /sc onstart /ru SYSTEM`, then set
the two `WAILS_*` variables system-wide with `setx /M` or a wrapper
cmd file) or any service wrapper (NSSM, WinSW). A SYSTEM account can
read the whole disk — prefer a dedicated user unless you mean that.

## How it differs from the desktop build

| Behavior | Desktop | Server mode |
|----------|---------|-------------|
| Popout windows (versions, delete, license…) | Native OS windows | Browser windows (`?popout=…` URLs) — same content, browser chrome instead of OS chrome |
| Drag a row out to the desktop | Native OLE drag with the staged file | Works only when the browser runs on the host itself — the drag hands the OS a token-gated loopback URL, and from any other machine that URL points at the client, where nothing listens; use Download / Copy URL instead |
| OS clipboard mirror | Direct | Browser clipboard (tab must be focused) |
| Edit in editor, Open local | Launches on the user's machine | Launches on the **host** |
| First-launch license gate | Full-screen gate in the window | Same gate in the tab; acceptance is shared through the store |
| Multi-instance guard | Second launch focuses the first | No guard — each process is its own app; keep them on separate `S3B_CONFIG`s |

Nothing else changes: sources, transfers, versioning, the safety
ladder and the admin panel behave identically because they are the
same code, reached through a socket instead of a webview.

## Troubleshooting

| Symptom | Cause and fix |
|---------|---------------|
| `failed to listen on …: address already in use` | The port is taken — pick another with `WAILS_SERVER_PORT`, or find the squatter (`netstat -ano \| findstr 8080` / `ss -ltnp`). |
| Browser from another machine gets nothing, localhost works | Bind (`WAILS_SERVER_HOST=0.0.0.0` or a tunnel) and the host firewall — see [Reaching it from another machine](#reaching-it-from-another-machine). |
| Page loads but rows never refresh | The events WebSocket is being dropped (some proxies time idle sockets out) — check the proxy's idle/read timeout, or bypass it. |
| Copy path / Copy URL says `clipboard: write failed` | The tab was not focused — click into the page first; browsers refuse clipboard writes from background tabs. |
| Blank page on an old browser | The frontend needs a current browser engine; use a Chromium-family release from this decade. |
| `s3b version` prints `0.2.0-dev` in the tab's About box | The build was not version-stamped — see [build.md](build.md#version-stamping). |
