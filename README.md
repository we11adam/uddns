# UDDNS

UDDNS is a small dynamic DNS updater. It obtains the current public IP address
from a provider, updates a DNS record through an updater, and can send
notifications when the IP address or update status changes.

[中文 README](README.zh-CN.md) | [Changelog](CHANGELOG.md)

## Features

- IPv4 and IPv6 update support.
- Providers: RouterOS, external IP services, and local network interfaces.
- Updaters: Cloudflare, Aliyun, DuckDNS, and LightDNS.
- Notifier: Telegram.
- Configurable update interval.
- Structured logs with optional daily rotated file logging and retention.
- Curl installer with optional systemd service installation.
- GoReleaser-based release artifacts for multiple platforms.

## Installation

Install the latest release with curl:

```shell
curl -fsSL https://raw.githubusercontent.com/we11adam/uddns/main/install.sh | sh
```

The installer detects systemd and asks whether to install UDDNS as a systemd
service. For non-interactive systemd installation:

```shell
curl -fsSL https://raw.githubusercontent.com/we11adam/uddns/main/install.sh | sh -s -- --systemd --config /etc/uddns.yaml
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

You can also install with Go:

```shell
go install github.com/we11adam/uddns@latest
```

Or download a binary from the [releases page](https://github.com/we11adam/uddns/releases/).

## Configuration

UDDNS looks for a config file in this order:

1. Path provided with `-c`.
2. `UDDNS_CONFIG`.
3. `./uddns.yaml`.
4. `~/.config/uddns.yaml`.
5. `/etc/uddns.yaml`.

Example:

```yaml
providers:
  routeros:
    endpoint: https://192.168.88.1
    username: admin
    password: ""
    insecure: true
  ip_service:
    - ifconfig.me
    - ip.fm
  netif:
    name: ppp0

updaters:
  cloudflare:
    apitoken: your-cloudflare-api-token
    # Or use email + apikey:
    # email: user@example.com
    # apikey: your-cloudflare-api-key
    domain: ddns.example.com
    proxy: http://127.0.0.1:2080
  aliyun:
    accesskeyid: your-access-key-id
    accesskeysecret: your-access-key-secret
    domain: ddns.example.com
    regionid: cn-hangzhou
  duckdns:
    token: your-duckdns-token
    domain: your-subdomain
  lightdns:
    key: your-lightdns-key
    domain: ddns.example.com

notifiers:
  telegram:
    chat_id: -1001234567890
    token: 1234567890:telegram-bot-token
    proxy: http://127.0.0.1:2080

logging:
  level: info
  dir: /var/log/uddns
  retention_days: 7
```

Configure at least one provider and one updater. If multiple providers or
updaters are configured, UDDNS uses the first one that can be initialized.

### Providers

- `routeros`: Reads IP addresses from a MikroTik RouterOS device.
  - `endpoint`: RouterOS API endpoint.
  - `username`: RouterOS username.
  - `password`: RouterOS password.
  - `insecure`: Skip TLS verification. Optional, defaults to `true`.
- `ip_service`: Reads the public IP from external services.
  - Supported services: `ip.fm`, `ifconfig.me`, `ip.sb`, `3322.org`.
- `netif`: Reads IP addresses from a local network interface.
  - `name`: Network interface name.

### Updaters

- `cloudflare`:
  - `apitoken`: Cloudflare API token.
  - `email` and `apikey`: Alternative Cloudflare API key authentication.
  - `domain`: DNS record to update, for example `ddns.example.com`.
  - `proxy`: Optional HTTP proxy.
- `aliyun`:
  - `accesskeyid`: Aliyun access key ID.
  - `accesskeysecret`: Aliyun access key secret.
  - `domain`: DNS record to update.
  - `regionid`: Optional, defaults to `cn-hangzhou`.
- `duckdns`:
  - `token`: DuckDNS token.
  - `domain`: DuckDNS subdomain without `.duckdns.org`.
- `lightdns`:
  - `key`: LightDNS DDNS key.
  - `domain`: DNS record to update.

### Notifiers

- `telegram`:
  - `token`: Telegram bot token.
  - `chat_id`: Telegram chat ID.
  - `proxy`: Optional HTTP proxy.

## Running

Run directly:

```shell
uddns -c /etc/uddns.yaml
```

Or run in the background:

```shell
nohup uddns -c /etc/uddns.yaml > uddns.log 2>&1 &
```

The default update interval is `30s`. Override it with `UDDNS_INTERVAL`:

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

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for release history and unreleased changes.

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
