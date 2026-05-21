# UDDNS

UDDNS - Universal (or Ultimate) Dynamic DNS updater

## How to use

### Obtaining the program

You can either:

Install the latest release with curl:

```shell
curl -fsSL https://raw.githubusercontent.com/we11adam/uddns/main/install.sh | sh
```

The installer detects systemd at startup and asks whether to install UDDNS as a
systemd service. For non-interactive installs, pass flags after `sh -s --`:

```shell
curl -fsSL https://raw.githubusercontent.com/we11adam/uddns/main/install.sh | sh -s -- --systemd --config /etc/uddns.yaml
```

Or:

`go install github.com/we11adam/uddns@latest`

or download the binary for you platform directly from the [releases page](https://github.com/we11adam/uddns/releases/)

### Configuration file

Ceate a `uddns.yaml` file as one the following: `./uddns.yaml`, `~/.config/uddns.yaml`,
`/etc/uddns.yaml`. UDDNS will try to read them in order. Additionally, you can specify the configuration file path with the `UDDNS_CONFIG` environment variable.
The file should look like this:

```yaml
providers:
  routeros:
    endpoint: https://192.168.88.1 # RouterOS API endpoint
    username: admin # RouterOS user with API access
    password: "" # RouterOS user password
  ip_service:
    - ifconfig.me # External service to get IP address
# If you use IP routing rules for specific traffic, ensure the domain used by ip_service is excluded.

updaters:
  cloudflare:
    email: "user@exmaple.com" # Cloudflare account email
    apikey: fd25bdc03a4c17450aa4327aa37de4573270f # Cloudflare API key
    domain: ddns.yourdomain.com # Domain to update
    proxy: http://127.0.0.1:2080 # Optional
  # lightdns: # Use LightDNS as the updater
  #   key: bgw99xiio5ewbphb
  #   domain: uddns.dyn.la
notifiers:
  telegram:
    chat_id: -1001234567890 # Telegram chat ID
    token: 1234567890:E2AvwaQsEvkACAF9pVPZAICmbXuzzHFTyyv # Telegram bot token
    proxy: http://127.0.0.1:2080

logging:
  level: info # debug, info, warn, or error
  dir: /var/log/uddns # Optional. Enables daily rotated file logging.
  retention_days: 7 # Number of calendar days to keep, including today.
```

Where:

- `providers` is a list of providers that UDDNS can use to obtain the current public IP address. Currently supported providers are:
  - `routeros`: Get IP address from a Mikrotik RouterOS device
    - `endpoint`: The RouterOS API endpoint
    - `username`: The RouterOS user with API access
    - `password`: The RouterOS user password
  - `ip_service`: Get IP address from an external service
    - `ip.fm`
    - `ifconfig.me`
    - `ip.sb`
    - `3322.org`
  - `netif`: Get IP address from a network interface (not implemented for Windows yet)
    - `name`: The network interface name to get the IP address from
- `updaters` is a list of updaters that UDDNS can use to update the DNS records. Currently supported updaters are:
  - `cloudflare`:
    - `email`: Cloudflare account email
    - `apikey`: Cloudflare API key
    - `domain`: Domain to update, e.g. `ddns.yourdomain.com`
  - `aliyun`:
    - `accesskeyid`: Aliyun access key ID
    - `accesskeysecret`: Aliyun access key secret
    - `domain`: Domain to update, e.g. `ddns.yourdomain.com`
    - `regionid`: cn-hangzhou # Optional
  - `duckdns`:
    - `token`: DuckDNS token
    - `domain`: Domain to update, excluding the `duckdns.org` part.
  - `ligthdns`:
    - `key`: LightDNS DDNS key
    - `domain`: Domain to update.
- `notifiers` is a list of notifiers that UDDNS can use to notify the user of the IP address change. Currently supported notifiers are:
  - `telegram`:
    - `token`: Telegram bot token
    - `chat_id`: Telegram chat ID
    - `proxy`: Proxy URL to use for Telegram API requests if you are behind a (great) firewall. Optional.
- `logging` controls log output:
  - `level`: Log verbosity. One of `debug`, `info`, `warn`, or `error`. Optional, defaults to `info`.
  - `dir`: Directory for daily rotated log files. Optional. If omitted, logs are only written to stdout.
  - `retention_days`: Number of calendar days to keep, including today. Optional, defaults to `7`.

### Running

Run the binary as the following. It will update the DNS record with the current public IP address with a default interval of 30 seconds, which can be overriden with the `UDDNS_INTERVAL` environment variable. The format for specifying the interval is flexible, allowing values such as `60s`, `5m`, `1h`, etc.

Log settings can be configured under the `logging` section in `uddns.yaml`. The corresponding environment variables are `UDDNS_LOG_LEVEL`, `UDDNS_LOG_DIR`, and `UDDNS_LOG_RETENTION_DAYS`; when set, environment variables override the config file.

Set `logging.dir` or `UDDNS_LOG_DIR` to enable file logging with daily calendar rotation. Log files are named `uddns-YYYY-MM-DD.log`. `logging.retention_days` or `UDDNS_LOG_RETENTION_DAYS` controls how many calendar days are kept, including today; the default is `7`.

```shell
nohup ./uddns > uddns.log 2>&1 &
```

## Roadmap

- [ ] Add more providers
- [ ] Add more updaters
- [ ] Add granular configuration options
- [x] Add sensible logging
- [ ] Add tests
- [ ] Add CI/CD
- [ ] Add Dockerfile
- [ ] Add Daemon mode
- [x] Add curl installer
- [x] Add systemd service installer

## Contributing

Pull requests are very welcome! For major changes, please open an issue first to discuss what you would like to change.

## License

MIT
