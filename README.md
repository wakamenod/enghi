# enghi

*English · [日本語](README.ja.md)*

A local-only personal wiki + GTD server for macOS and Linux. Runs as a background
service, used from the browser; nothing leaves the machine. Instant full-text search
(SQLite FTS5), `[[wikilinks]]` resolved by title, export to plain Markdown any time.

## Installation

```sh
brew install wakamenod/tap/enghi
brew services start enghi     # Registers with launchd on macOS, systemd on Linux
```

Open http://127.0.0.1:7777/. See `/guide` for GTD introduction and usage (English and
Japanese).

Without Homebrew, unpack the tarball from
[Releases](https://github.com/wakamenod/enghi/releases) and run `enghi install-agent
-load` to set up the background service.

## Configuration

`~/.config/enghi/config.toml`. Runs with defaults if omitted.

```toml
port = 7777
db_path    = "~/.local/share/enghi/enghi.db"
export_dir = "~/.local/share/enghi/export"
revision_compact_minutes = 10   # re-edits within this window overwrite the last revision

files_db_path  = "~/.local/share/enghi/enghi-files.db"   # Tracks db_path if omitted
backup_dir     = "~/.local/share/enghi/backup"
backup_keep    = 7       # Generations to keep
backup_enabled = true
```

## Commands

```
enghi [serve]          Start resident server
enghi export [--dir D] Export everything to Markdown (--dir CLI only; API uses config value)
enghi doctor [--fix]   Integrity check. --fix repairs stray NFD text
enghi backup [--list]  Database backup (--dir sets destination). Runs daily while resident
enghi files [--prune]  List images and files. --prune removes unreferenced files
enghi install-agent    Write service config. -load registers and starts it
enghi version          Print version
```

All commands accept `--config` to specify the configuration file path.

## Service Management and Recovery

`install-agent` writes a launchd plist to `~/Library/LaunchAgents` on macOS, or a
systemd user unit to `~/.config/systemd/user` on Linux. **On Linux, run `loginctl
enable-linger` to keep the service running after logout.**

Backups use `VACUUM INTO`, capturing the database cleanly without missing WAL entries.
To restore, stop the server and replace the file:

```sh
brew services stop enghi        # Or launchctl bootout / systemctl --user stop
cp ~/.local/share/enghi/backup/enghi-2026-09-21.db ~/.local/share/enghi/enghi.db
rm -f ~/.local/share/enghi/enghi.db-wal ~/.local/share/enghi/enghi.db-shm
brew services start enghi
```

## Secure Mobile Access at Home

enghi binds only to `127.0.0.1` and returns 403 unless the `Host` header is loopback.
To access it from a phone, **place Caddy in front to handle TLS and authentication**.
Leave enghi bound to loopback.

```
iPhone ──https──▶ Caddy (LAN:443)  ──http──▶ enghi (127.0.0.1:7777)
                   ├ TLS termination
                   └ Password verification
```

**Never bind to `0.0.0.0`.** enghi has no authentication; doing so gives read/write
access to everyone on your Wi-Fi.

### 1. Install Caddy and hash your password

```sh
brew install caddy
caddy hash-password        # Enter interactively; do not store plaintext in config
```

### 2. Write the Caddyfile

`$(brew --prefix)/etc/Caddyfile`. Name matches `scutil --get LocalHostName` + `.local`
(e.g. `junnomacbook-pro.local`).

```
junnomacbook-pro.local {
    tls internal
    basic_auth {
        jun    $2a$14$...(caddy hash-password output)...
    }
    reverse_proxy 127.0.0.1:7777
}
```

```sh
brew services start caddy
```

### 3. Allow the hostname in enghi

`~/.config/enghi/config.toml`:

```toml
allowed_hosts = ["junnomacbook-pro.local"]
```

Requests are accepted only when the incoming `Host` header matches. Wildcards are not
allowed (`*.local` produces an error on startup). Defaults to loopback only if omitted.

Restart enghi after editing.

### 4. Trust the certificate on iPhone

Because `tls internal` uses Caddy's self-generated root CA, Safari will warn on first
access. Transfer the root certificate (find its path via `caddy trust` or in Caddy's
data directory) to your iPhone, then enable "Full Trust" under **Settings → General →
About → Certificate Trust Settings**.

Ignoring the warning lets you browse pages, but `wss://` will be rejected, breaking live
updates (such as focus sync from Emacs).

### Notes

- **Never omit `basic_auth`.** Without it, anyone on the same LAN has read/write access
- `basic_auth` was previously named `basicauth`. Verify the syntax for your installed
  Caddy version
- Basic auth uses the browser's native prompt, so password manager autofill (like
  1Password) does not work. Paste a long random password once; the browser will remember
  it
- **Not accessible outside your home network** (home LAN only). For remote access, use
  an external tunnel like Tailscale or Cloudflare Tunnel. On the enghi side, simply add
  that hostname to `allowed_hosts`
- The `.local` name tracks IP changes on your Mac. No static IP needed

## Recurring Task Syntax

Follows org-mode repeater syntax (stored directly in the `recurrence` column):

| Syntax | Meaning | Basis |
|---|---|---|
| `+1d` `+2w` `+1m` `+1y` | Fixed interval | Previous **scheduled date** + interval. Advances by one step |
| `++1w` | Fixed interval, catch up to future | Adds interval repeatedly until after today |
| `.+3d` | Completion-based | **Completion date** + interval |
| `weekly:mon,thu` | Specific days each week | Next matching weekday |
| `monthly:25` / `monthly:last` | Monthly | Next matching date / end of month |
| `yearly:04-01` | Yearly | Next matching date |

Completing (or skipping) a task generates **only the next single instance**, so there is
always at most one open instance per series. Generated instances always start in the
`scheduled` state. Dates that do not exist (a 31st in a short month, Feb 29 in a
non-leap year) clamp to the last day of that month.

## Keyboard Shortcuts (Web UI)

`/` Search · `g d`/`g w`/`g i`/`g n`/`g p` Navigation · `c` Quick capture ·
`e` Edit · `j`/`k` List navigation · `Enter` Open · `Esc` Dismiss

## Using from Emacs

See **[enghi.el](https://github.com/wakamenod/enghi.el)**.

## Development

**The `sqlite_fts5` build tag and `CGO_ENABLED=1` are required** (FTS5 and the trigram
tokenizer); the build fails on purpose without them (`cmd/enghi/require_*.go`). cgo
rules out cross-compiling, so release binaries are built on native runners.

```sh
make build test vet     # browser tests need: npx playwright install chromium webkit
```

When adding to `docs/guide/`, **give every heading an explicit `{#id}` anchor** —
auto-generated IDs diverge between English and Japanese and break the in-app `?` links.
