# UDDNS

UDDNS is a small dynamic DNS updater. It obtains the current public IP address
from a provider, updates a DNS record through an updater, and can send
notifications when the IP address or update status changes.

[中文 README](README.zh-CN.md) | [Changelog](CHANGELOG.md)

## Features

- IPv4 and IPv6 update support.
- Providers: RouterOS, external IP services, and local network interfaces.
- Updaters: Cloudflare, Aliyun, DuckDNS, LightDNS and Scaleway.
- Notifiers: Telegram and Discord.
- Configurable update interval.
- Structured logs with optional daily rotated file logging and retention.
- Curl installer with optional systemd service installation.
- Built-in release update checks and atomic self-updates with rollback on Unix.
- GoReleaser-based release artifacts for multiple platforms, with SBOMs and
  GitHub Actions provenance attestations.

## Installation

Install the latest release with curl:

```shell
curl -fsSL https://github.com/we11adam/uddns/releases/latest/download/install.sh | sh
```

For a verified installation, download the script first and check its GitHub
Actions provenance before running it:

```shell
curl -fsSLO https://github.com/we11adam/uddns/releases/latest/download/install.sh
gh attestation verify install.sh --repo we11adam/uddns
sh install.sh
```

Official release archives contain only the platform executable. The installer
downloads them only over HTTPS, verifies their checksums, and validates archive
contents before extraction. Each release archive also has an attached SBOM and
a GitHub Actions provenance attestation; after downloading an archive manually,
verify it with:

```shell
gh attestation verify /path/to/archive --repo we11adam/uddns
```

The installer detects systemd and asks whether to install UDDNS as a systemd
service. The generated unit runs as the dedicated unprivileged `uddns` user and
passes the configuration through a protected systemd credential, so the source
config can remain owned by root with mode `0600`. The installer validates the
configuration before starting the main process and restores the previous
executable, unit, and service state if startup fails. For non-interactive
systemd installation:

```shell
curl -fsSL https://github.com/we11adam/uddns/releases/latest/download/install.sh | sh -s -- --systemd --config /etc/uddns.yaml
```

Useful installer options:

```shell
--version <tag>              Install a specific release tag
--install-dir <dir>          Install directory, default: /usr/local/bin
--systemd                    Install or update the systemd service
--no-systemd                 Skip systemd service installation
--config <path>              Config file path used by the systemd service
--interval <duration>        UDDNS_INTERVAL used by the systemd service
--log-dir <dir>              Enable rotated file logging in systemd
--log-retention-days <n>     Calendar days of logs to keep
```

Installer-provided install, config, and log paths must be absolute. If the
selected config is missing or is not a readable regular file, the systemd unit
is enabled but not started.

You can also install from source with Go 1.26.5 or newer:

```shell
go install github.com/we11adam/uddns@latest
```

Or download a binary from the [releases page](https://github.com/we11adam/uddns/releases/).

## Self-update

Print the running build version or check for a newer stable release without
changing any files:

```shell
uddns version
uddns version --json
uddns self-update --check
uddns self-update --check --json
```

On Linux, macOS, and FreeBSD, install the latest stable release with:

```shell
uddns self-update
```

The updater selects the exact GoReleaser asset for the current platform,
downloads it over HTTPS, verifies its SHA-256 value from `checksums.txt`,
strictly validates the single-file archive, and confirms the staged binary's
reported version. It then creates an adjacent `.previous` backup and atomically
replaces the installed executable. The resolved target must be a regular file.
Because the operating system may resolve a symlink before UDDNS can inspect the
launch path, the updater cannot reliably identify every package-manager
installation. Use the package manager's own upgrade command when it manages the
installed binary.

If the executable is installed in a root-owned directory, run its absolute path
with `sudo`, for example:

```shell
sudo /usr/local/bin/uddns self-update
sudo systemctl restart uddns.service
```

The first self-update release does not restart a running service automatically.
Restart the service after a successful update to run the new binary. Restore
the previous binary with:

```shell
sudo /usr/local/bin/uddns self-update --rollback
sudo systemctl restart uddns.service
```

An interrupted replacement may leave `<binary>.update-pending`, which contains
the pre-update executable and is intentionally never discarded automatically.
Inspect both files with `version --json`. If the installed target is the new
version, move `.update-pending` to `.previous`; if the two files are identical,
remove only `.update-pending`. Resolve this recovery file before running another
update or rollback.

Use `--version v1.9.1` to select a specific stable release. Downgrades require
`--allow-downgrade` and are limited to v1.9.0 or newer because earlier binaries
cannot perform the staged version check; install an older release manually if
needed. Builds reporting version `dev` require `--allow-dev`. Windows can check
releases but cannot yet replace a running executable.

## Configuration

UDDNS looks for a config file in this order:

1. Path provided with `-c`.
2. `UDDNS_CONFIG`.
3. `./uddns.yaml`.
4. `~/.config/uddns.yaml`.
5. `/etc/uddns.yaml`.

An unreadable path selected explicitly with `-c` or `UDDNS_CONFIG` is an error
and does not fall back to another location. UDDNS does not load `.env` files
automatically; set environment variables in your shell or service manager. On
Unix, the selected config file must not grant any permissions to group or other
users. Secure it before starting UDDNS:

```shell
chmod 600 /etc/uddns.yaml
```

The protected copy supplied through systemd credentials is an exception:
systemd may represent its service-user ACL as mode `0440`. UDDNS accepts only
the tightly controlled, root-owned file directly under
`$CREDENTIALS_DIRECTORY`; an ordinary group-readable config remains rejected.

### Simple Mode

Use simple mode when one UDDNS process updates one DNS record. This is the
traditional configuration format and remains fully supported.

```yaml
providers:
  use: ip_service
  routeros:
    endpoint: https://192.168.88.1
    username: admin
    password: ""
    insecure: false
  ip_service:
    - ifconfig.me
    - ip.fm
  netif:
    name: ppp0

updaters:
  use: cloudflare
  cloudflare:
    apitoken: your-cloudflare-api-token
    # Or use email + apikey:
    # email: user@example.com
    # apikey: your-cloudflare-api-key
    domain: ddns.example.com
    # Optional. Set when the DNS zone cannot be inferred from the last two labels.
    # zone: example.com
    proxy: http://127.0.0.1:2080
  aliyun:
    accesskeyid: your-access-key-id
    accesskeysecret: your-access-key-secret
    domain: ddns.example.com
    # Optional. Set when the DNS zone cannot be inferred from the last two labels.
    # zone: example.com
    regionid: cn-hangzhou
  duckdns:
    token: your-duckdns-token
    domain: your-subdomain
  lightdns:
    key: your-lightdns-key
    domain: ddns.example.com

notifiers:
  use: telegram
  telegram:
    chat_id: -1001234567890
    token: 1234567890:telegram-bot-token
    proxy: http://127.0.0.1:2080

# Optional. auto uses updater API verification when available.
verify: auto

logging:
  level: info
  dir: /var/log/uddns
  retention_days: 7
```

Configure at least one provider and one updater. Use `providers.use` and
`updaters.use` to select a specific implementation when multiple entries are
present. If `use` is omitted, UDDNS checks configured entries in deterministic
registry order. A configured but invalid provider or updater stops startup with
its configuration error instead of silently falling back to another entry.
Notifiers are optional; when configured, `notifiers.use` selects the notifier
and invalid notifier configuration stops startup.

### Advanced Jobs Mode

Use jobs mode when one process should update multiple DNS records, use different
DNS services, or update only selected address families. Providers describe how
to obtain IP addresses, updaters describe how to access a DNS service, and each
job connects one provider to one updater and one DNS record.

```yaml
providers:
  ip_service:
    - ifconfig.me
    - ip.fm
  netif:
    name: ppp0

updaters:
  cloudflare:
    apitoken: your-cloudflare-api-token
    # proxy: http://127.0.0.1:2080
  duckdns:
    token: your-duckdns-token

jobs:
  - name: home-cloudflare
    provider: ip_service
    updater: cloudflare
    record: home.example.com
    # Optional. Set when the DNS zone cannot be inferred from the last two labels.
    zone: example.com
    families: [ipv4, ipv6]
    verify: updater_api

  - name: nas-duckdns
    provider: netif
    updater: duckdns
    record: your-subdomain
    families: [ipv4]
    verify: off

notifiers:
  use: telegram
  telegram:
    chat_id: -1001234567890
    token: 1234567890:telegram-bot-token

logging:
  level: info
  dir: /var/log/uddns
  retention_days: 7
```

Job fields:

- `name`: Optional unique job name. Defaults to `job-<n>` when omitted.
- `provider`: Provider implementation to use, for example `ip_service`,
  `routeros`, or `netif`.
- `updater`: Updater implementation to use, for example `cloudflare`,
  `aliyun`, `duckdns`, `lightdns`, or `scaleway`.
- `record`: DNS record to update. For DuckDNS this is the subdomain without
  `.duckdns.org`.
- `zone`: Optional DNS zone override for Cloudflare, Aliyun, and Scaleway.
- `families`: Optional address families. Supported values are `ipv4` and
  `ipv6`; omitted means both.
- `verify`: Optional verification mode. Supported values are `auto`, `off`, and
  `updater_api`; omitted means `auto`.

When `jobs` is present, each job has its own last IPv4/IPv6 state and is run
sequentially on the global update interval. Without `jobs`, UDDNS behaves as a
single implicit `default` job using the simple-mode config. Notifications from
named jobs are prefixed with the job name.

Transient public-IP service and DNS update requests are retried. Repeated
provider, strict-verification, or updater failures apply exponential backoff
with jitter only to the affected job; other jobs continue to run.

Jobs select provider and updater implementation names. Jobs that use the same
implementation share that implementation's configuration, for example all
`cloudflare` jobs use `updaters.cloudflare` credentials.

Verify behavior:

- `auto`: Use updater API verification when the selected updater supports it;
  otherwise skip verification.
- `off`: Do not verify DNS records before deciding whether to update.
- `updater_api`: Require the selected updater to query the current DNS record
  through its DNS provider API. Cloudflare, Aliyun, and Scaleway support this.
  DuckDNS and LightDNS do not, so `config check` fails if they are used with
  `verify: updater_api`.

When updater API verification is active, UDDNS updates if the detected IP
differs from the job's last successful IP, or if the current DNS record returned
by the updater API does not match the detected IP. A matching record on startup
initializes local state without an unnecessary rewrite. `auto` verifies on
startup, when the provider IP changes, and periodically while stable, querying
only the configured address families. A temporary `auto` verification failure
does not block an update caused by a changed provider IP. `updater_api` remains
strict, verifies every cycle, and skips the job's current cycle if verification
fails.

### Providers

- `routeros`: Reads IP addresses from a MikroTik RouterOS device.
  - `endpoint`: RouterOS API endpoint.
  - `username`: RouterOS username.
  - `password`: RouterOS password.
  - `insecure`: Skip TLS verification. Optional, defaults to `false`.
- `ip_service`: Reads the public IP from external services.
  - Supported services: `ip.fm`, `ifconfig.me`, `ip.sb`, `3322.org`.
  - Only public, globally routable addresses are accepted.
- `netif`: Reads IP addresses from a local network interface.
  - `name`: Network interface name.

### Updaters

- `cloudflare`:
  - `apitoken`: Cloudflare API token.
  - `email` and `apikey`: Alternative Cloudflare API key authentication.
  - `domain`: DNS record to update, for example `ddns.example.com`.
  - `zone`: Optional DNS zone, for example `example.co.uk`.
  - `proxy`: Optional HTTP or HTTPS proxy.
- `aliyun`:
  - `accesskeyid`: Aliyun access key ID.
  - `accesskeysecret`: Aliyun access key secret.
  - `domain`: DNS record to update.
  - `zone`: Optional DNS zone, for example `example.co.uk`.
  - `regionid`: Optional, defaults to `cn-hangzhou`.
- `duckdns`:
  - `token`: DuckDNS token.
  - `domain`: DuckDNS subdomain without `.duckdns.org`.
- `lightdns`:
  - `key`: LightDNS DDNS key.
  - `domain`: DNS record to update.
- `scaleway`:
  - `access_key`: Scaleway access key.
  - `secret_key`: Scaleway secret key.
  - `project_id`: Scaleway project ID.
  - `domain`: DNS record to update.
  - `zone`: Optional DNS zone, for example `example.co.uk`.
  - `ttl`: Optional DNS record TTL in seconds, defaults to `150`.

### Notifiers

- `telegram`:
  - `token`: Telegram bot token.
  - `chat_id`: Telegram chat ID.
  - `proxy`: Optional HTTP or HTTPS proxy.
- `discord`:
  - `url`: Discord webhook URL.
  - `id`: Discord webhook ID. (Alternatively to `url`)
  - `token`: Discord webhook token. (Alternatively to `url`)
  - `proxy`: Optional HTTP or HTTPS proxy.

Cloudflare, Discord and Telegram proxy values must be absolute URLs with a host. URL
userinfo credentials are supported; paths other than `/`, queries, and
fragments are rejected.

## Running

Run directly:

```shell
uddns -c /etc/uddns.yaml
```

Check configuration without starting the updater:

```shell
uddns config check -c /etc/uddns.yaml
```

Or run in the background:

```shell
nohup uddns -c /etc/uddns.yaml > uddns.log 2>&1 &
```

The default update interval is `30s`. `UDDNS_INTERVAL` accepts Go duration
strings from `10s` through `24h`; invalid or out-of-range values emit a warning
and fall back to `30s`:

```shell
UDDNS_INTERVAL=5m uddns -c /etc/uddns.yaml
```

## Logging

Logging can be configured in `uddns.yaml`:

```yaml
logging:
  level: info
  dir: /var/log/uddns
  retention_days: 7
```

- `level`: `debug`, `info`, `warn`, or `error`. Default: `info`.
- `dir`: Enables file logging when set. If omitted, logs only go to stdout.
- `retention_days`: Number of calendar days to keep, including today. Default: `7`.

File logs rotate by local calendar date and are named:

```text
uddns-YYYY-MM-DD.log
```

Environment variables override config-file logging values:

- `UDDNS_LOG_LEVEL`
- `UDDNS_LOG_DIR`
- `UDDNS_LOG_RETENTION_DAYS`

UDDNS creates managed log directories with mode `0700` and log files with mode
`0600`. Symlinks and non-regular log targets are rejected.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history and unreleased changes.
Chinese release notes are available in [CHANGELOG.zh-CN.md](CHANGELOG.zh-CN.md).

## Roadmap

- [ ] Add more providers.
- [ ] Add more updaters.
- [ ] Add granular configuration options.
- [ ] Add Dockerfile.
- [ ] Add daemon mode beyond systemd installation.
- [x] Add sensible logging.
- [x] Add tests.
- [x] Add CI/CD release workflow.
- [x] Add curl installer.
- [x] Add systemd service installer.

## Contributing

Pull requests are welcome. For major changes, please open an issue first to
discuss what you would like to change.

## License

MIT
